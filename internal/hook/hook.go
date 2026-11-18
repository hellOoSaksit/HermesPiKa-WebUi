// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package hook implements `hpk hook <name>`, the command Hermes runs on its events.
//
// Wire format (verified against Hermes v0.23.1, see docs/m0-findings.md):
//   - stdin: JSON {hook_event_name, tool_name, tool_input, session_id, cwd, profile, extra}
//   - stdout for pre_tool_call: {"action":"block"|"approve", "message":...}
//     "approve" sends the call to Hermes' human-approval gate (a person decides).
//     Printing nothing lets the call through.
//   - exit code 2 blocks a pre_tool_call.
//
// M0 ships the plumbing and fail-closed behaviour; real guards arrive in M2 (privacy)
// and M4 (red lines).
package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hpk.local/hpk/internal/board"
	"hpk.local/hpk/internal/cron"
	"hpk.local/hpk/internal/gates"
	"hpk.local/hpk/internal/mcp"
	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
	"hpk.local/hpk/internal/team"
	"hpk.local/hpk/internal/vault"
)

// inputStrings collects the string values of a tool's input (paths, commands, patterns).
func inputStrings(in map[string]any) []string {
	var out []string
	truncated := false
	var walk func(v any, depth int)
	walk = func(v any, depth int) {
		if depth > 8 || len(out) > 2000 {
			truncated = true
			return
		}
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case []any:
			for _, y := range x {
				walk(y, depth+1)
			}
		case map[string]any:
			for _, y := range x {
				walk(y, depth+1)
			}
		}
	}
	walk(in, 0)
	if truncated {
		out = append(out, tooBig)
	}
	return out
}

// Event is what Hermes sends on stdin.
type Event struct {
	HookEventName string         `json:"hook_event_name"`
	ToolName      string         `json:"tool_name,omitempty"`
	ToolInput     map[string]any `json:"tool_input,omitempty"`
	SessionID     string         `json:"session_id,omitempty"`
	Cwd           string         `json:"cwd,omitempty"`
	Profile       string         `json:"profile,omitempty"`
	Extra         map[string]any `json:"extra,omitempty"`
}

// Verdict is hpk's decision before it is written in Hermes' dialect.
type Verdict int

const (
	Allow  Verdict = iota // let the call through
	Ask                   // a human must decide (Hermes approval gate)
	Inform                // add context for the agent (non pre_tool_call events)
	Block                 // never run it
)

// Reply is the JSON Hermes reads back.
type Reply struct {
	Action  string `json:"action,omitempty"`  // block | approve
	Context string `json:"context,omitempty"` // extra context for the agent
	Message string `json:"message,omitempty"` // shown to the agent and the human
}

// guards that must never let an action through when they fail.
var failClosed = map[string]bool{"red_line_guard": true, "privacy_guard": true}

// Run handles one hook call and returns the process exit code.
func Run(name string, in io.Reader, out io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			code = answer(out, broken(name, fmt.Sprintf("hook %s failed: %v", name, r)))
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err != nil {
		return answer(out, broken(name, "could not read the event"))
	}
	// Windows shells may prefix a UTF-8 byte-order mark; it is not part of the JSON.
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	var ev Event
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &ev); err != nil {
			return answer(out, broken(name, "the event is not JSON"))
		}
	}
	switch name {
	case "red_line_guard":
		return answer(out, trace(name, ev, redLineGuard(ev)))
	case "approval_audit":
		return answer(out, approvalAudit(ev))
	case "board_guard":
		return answer(out, boardGuard(ev))
	case "board_heartbeat":
		r := boardHeartbeat(ev)
		if r.v == Allow {
			r = misplacedNote(ev)
		}
		return answer(out, r)
	case "child_report":
		return answer(out, childReport(ev))
	case "privacy_guard":
		return answer(out, trace(name, ev, privacyGuard(ev)))
	case "model_route":
		return answer(out, modelRoute(ev))
	case "model_usage":
		return answer(out, modelUsage(ev))
	case "session_link":
		return answer(out, sessionLink(ev))
	case "compaction_note":
		return answer(out, compactionNote(ev))
	case "noop":
		// Measures start-up latency (docs/DESIGN.md §21.5).
		return answer(out, result{Allow, ""})
	default:
		return answer(out, broken(name, "unknown hook "+name))
	}
}

type result struct {
	v   Verdict
	why string
}

// A guard that cannot decide blocks; any other hook steps aside.
func broken(name, why string) result {
	if failClosed[name] {
		return result{Block, why + " (fail closed)"}
	}
	return result{Allow, ""}
}

func answer(out io.Writer, r result) int {
	switch r.v {
	case Block:
		_ = json.NewEncoder(out).Encode(Reply{Action: "block", Message: r.why})
		return 2
	case Ask:
		_ = json.NewEncoder(out).Encode(Reply{Action: "approve", Message: r.why})
		return 0
	case Inform:
		_ = json.NewEncoder(out).Encode(Reply{Context: r.why})
		return 0
	default:
		return 0 // silence means allow
	}
}

// privacyGuard applies the project privacy mode to one tool call (§6.3).
// Anything it cannot resolve blocks: the guard fails closed.
func privacyGuard(ev Event) result {
	if ev.HookEventName != "" && ev.HookEventName != "pre_tool_call" {
		return result{Allow, ""}
	}
	// .env files hpk wrote from the person's vault: their values are not for the AI.
	in := inputStrings(ev.ToolInput)
	if path, hit := vault.TouchesManaged(project.Home(), ev.Cwd, in...); hit {
		return result{Block, vault.ErrManaged(path)}
	}
	// the vault itself, the browser's key file and recordings, and hpk's local API for them
	if why := privateTouch(in); why != "" {
		return result{Block, why}
	}
	p, err := privacy.ResolveSession(project.Home(), ev.Cwd, ev.SessionID)
	if err != nil {
		return result{Block, "privacy guard: " + err.Error() + " (fail closed)"}
	}
	r := result{Allow, ""}
	if v, why := privacy.Decide(p, privacy.Call{Tool: ev.ToolName, Input: ev.ToolInput, Cwd: ev.Cwd}); v == privacy.Block {
		r = result{Block, why}
	} else if v, why := privacy.CheckModel(project.Home(), p, ev.SessionID); v == privacy.Block {
		r = result{Block, why}
	}
	// What each MCP is used by (Console > MCP): names only, never inputs.
	if server, tool, ok := mcp.SplitTool(ev.ToolName); ok {
		u := mcp.Use{At: time.Now().UTC(), Server: server, Tool: tool, Session: ev.SessionID, Blocked: r.v != Allow}
		if p != nil {
			u.Project, u.Root = p.Name, p.Root
		}
		mcp.Record(project.Home(), u)
	}
	return r
}

// modelRoute records where a session sends model requests (pre_api_request and
// pre_auxiliary_call). Hermes cannot block on these events, so privacy_guard
// reads the record on the next tool call and refuses tools for a local_only
// project whose session uses a remote model.
func modelRoute(ev Event) result {
	if ev.HookEventName != "" && ev.HookEventName != "pre_api_request" && ev.HookEventName != "pre_auxiliary_call" {
		return result{Allow, ""}
	}
	r := privacy.Route{Provider: firstStr(ev.Extra, "provider"), BaseURL: firstStr(ev.Extra, "base_url"), Model: firstStr(ev.Extra, "model")}
	_ = privacy.RecordRoute(project.Home(), ev.SessionID, r)
	return result{Allow, ""}
}

// modelUsage adds what one request cost (post_api_request, post_auxiliary_call)
// to the session record, so the Console shows real tokens, not estimates.
func modelUsage(ev Event) result {
	if ev.HookEventName != "" && ev.HookEventName != "post_api_request" && ev.HookEventName != "post_auxiliary_call" {
		return result{Allow, ""}
	}
	u, _ := ev.Extra["usage"].(map[string]any)
	if u == nil {
		return result{Allow, ""}
	}
	r := privacy.Route{Provider: firstStr(ev.Extra, "provider"), BaseURL: firstStr(ev.Extra, "base_url"), Model: firstStr(ev.Extra, "response_model", "model")}
	_ = privacy.RecordUsage(project.Home(), ev.SessionID, r, u)
	return result{Allow, ""}
}

// sessionLink links a Hermes cron run to its hpk project and job (pre_llm_call).
// Cron runs inside the gateway, so neither cwd nor env names the project; the
// prompt hpk wrote carries a tag instead.
func sessionLink(ev Event) result {
	if ev.HookEventName != "" && ev.HookEventName != "pre_llm_call" {
		return result{Allow, ""}
	}
	home := project.Home()
	if name, _ := project.LinkOf(home, ev.SessionID); name == "" {
		msg, _ := ev.Extra["user_message"].(string)
		if name, job, ok := cron.ParseJobTag(msg); ok {
			if p, err := project.Load(home, name); err == nil {
				_ = project.LinkJob(p, ev.SessionID, job)
			}
		}
	}
	return folderContext(ev)
}

// compactionNote records that Hermes compacted a session's context (post_compaction).
// Hermes passes counts only, not the summary, so hpk cannot save what was dropped;
// the hpk-wiki skill asks the agent to save a summary before that point.
func compactionNote(ev Event) result {
	if ev.HookEventName != "" && ev.HookEventName != "post_compaction" {
		return result{Allow, ""}
	}
	p, err := privacy.ResolveSession(project.Home(), ev.Cwd, ev.SessionID)
	if err != nil || p == nil {
		return result{Allow, ""}
	}
	num := func(k string) string {
		if v, ok := ev.Extra[k].(float64); ok {
			return fmt.Sprint(int64(v))
		}
		return "?"
	}
	_ = project.Audit(p.Dir, "context compacted", fmt.Sprintf("session %s: %s -> %s tokens, %s messages removed", ev.SessionID, num("before_tokens"), num("after_tokens"), num("messages_removed")))
	return result{Allow, ""}
}

// overBudget stops a cron run that went past its job budget, mid-run: Hermes has no
// per-job limit, so the next tool call is refused and the agent has to wrap up.
func overBudget(p *project.Project, session string) (bool, string) {
	if p == nil {
		return false, ""
	}
	home := project.Home()
	_, job := project.LinkOf(home, session)
	if job == "" {
		return false, ""
	}
	j, err := cron.JobByName(p, job)
	seen, _ := privacy.LoadSeen(home, session)
	if err != nil || seen == nil || j.Budget <= 0 || seen.Weighted() <= int64(j.Budget) {
		return false, ""
	}
	return true, fmt.Sprintf("the %s job of %s is over its budget (%d of %d tokens, cache reads counted at a tenth). Stop now: no more tools in this run. Report what you did and what is left", job, p.Name, seen.Weighted(), j.Budget)
}

// redLineGuard applies red lines and the Manual / Ask / Auto mode (§11).
// Red lines answer "approve": Hermes then asks a person, in every mode.
func redLineGuard(ev Event) result {
	if ev.HookEventName != "" && ev.HookEventName != "pre_tool_call" {
		return result{Allow, ""}
	}
	// .env files hpk wrote from the person's vault: their values are not for the AI.
	in := inputStrings(ev.ToolInput)
	if path, hit := vault.TouchesManaged(project.Home(), ev.Cwd, in...); hit {
		return result{Block, vault.ErrManaged(path)}
	}
	// the vault itself, the browser's key file and recordings, and hpk's local API for them
	if why := privateTouch(in); why != "" {
		return result{Block, why}
	}
	p, err := privacy.ResolveSession(project.Home(), ev.Cwd, ev.SessionID)
	if err != nil {
		return result{Block, "red-line guard: " + err.Error() + " (fail closed)"}
	}
	if over, why := overBudget(p, ev.SessionID); over {
		return result{Block, why}
	}
	if over, why := personOver(ev.SessionID); over {
		return result{Block, why + ". No more tools until an owner raises the cap (Console, Team) or the period ends. Report what you did and stop"}
	}
	if p != nil {
		if over, why := privacy.OverMoney(project.Home(), p, time.Now()); over {
			return result{Block, why + ". No more tools until a person raises the cap (Console, Settings) or the period ends. Report what you did and stop"}
		}
	}
	now := time.Now()
	c := gates.Call{Tool: ev.ToolName, Input: ev.ToolInput, Cwd: ev.Cwd, Session: ev.SessionID}
	r := gates.Decide(p, c, now)
	if r.Ask {
		if p != nil {
			var lines []string
			for _, h := range r.Hits {
				lines = append(lines, h.Line)
			}
			_ = gates.Record(p, gates.Decision{At: now.UTC(), Kind: "ask", Mode: r.Mode, Lines: lines, Tool: c.Tool, Detail: gates.Summary(c), Session: c.Session, By: "hpk"})
		}
		return result{Ask, r.Reason}
	}
	if p != nil && r.Mode == gates.Auto && writes[c.Tool] {
		_ = gates.Record(p, gates.Decision{At: now.UTC(), Kind: "auto-allow", Mode: r.Mode, Tool: c.Tool, Detail: gates.Summary(c), Session: c.Session, By: "auto"})
	}
	return result{Allow, ""}
}

var writes = map[string]bool{"write_file": true, "patch": true, "terminal": true, "execute_code": true, "process_manage": true, "delegate_task": true}

// approvalAudit records what a person decided on an approval request (§11.4).
// It only logs; it never changes the decision.
func approvalAudit(ev Event) result {
	p, err := privacy.ResolveSession(project.Home(), ev.Cwd, ev.SessionID)
	if err != nil || p == nil {
		return result{Allow, ""}
	}
	outcome := ""
	for _, k := range []string{"choice", "decision", "outcome", "response", "approved"} {
		if v, ok := ev.Extra[k]; ok {
			outcome = fmt.Sprint(v)
			break
		}
	}
	_ = gates.Record(p, gates.Decision{At: time.Now().UTC(), Kind: "human", Tool: ev.ToolName, Detail: ev.HookEventName,
		Session: ev.SessionID, By: "person", Outcome: outcome})
	return result{Allow, ""}
}

// childReport runs on on_session_end / on_session_finalize. A sub-task that ends
// without sending its report (§9) is put in front of its main chat, so nothing
// finishes silently. It never blocks.
func childReport(ev Event) result {
	p, err := privacy.ResolveSession(project.Home(), ev.Cwd, ev.SessionID)
	if err != nil || p == nil || ev.SessionID == "" {
		return result{Allow, ""}
	}
	s, err := session.Get(p, ev.SessionID)
	if err != nil {
		return result{Allow, ""}
	}
	status := fmt.Sprint(ev.Extra["status"])
	switch status {
	case "failed", "error":
		session.SetStatus(p, s.ID, session.Failed)
	case "interrupted", "cancelled":
		session.SetStatus(p, s.ID, session.Stopped)
	}
	if s.Parent != "" && !s.Reported {
		_ = gates.Record(p, gates.Decision{At: time.Now().UTC(), Kind: "no-report", Tool: ev.HookEventName, Detail: s.Num + " " + s.Title + " ended its turn without a report", Session: s.ID, By: "hpk"})
	}
	return result{Allow, ""}
}

// boardGuard (pre_tool_call): editing a file someone else holds a claim on is an
// overlap (§20.4 🟠) — a person decides before the edit goes ahead.
func boardGuard(ev Event) result {
	if !fileWriters[ev.ToolName] {
		return result{Allow, ""}
	}
	p, err := privacy.ResolveSession(project.Home(), ev.Cwd, ev.SessionID)
	if err != nil || p == nil {
		return result{Allow, ""} // not a governed project; the board has nothing to say
	}
	rel := projectRel(p, ev.Cwd, firstStr(ev.ToolInput, "path", "file_path"))
	if rel == "" {
		return result{Allow, ""}
	}
	db, err := board.Open(p)
	if err != nil {
		return result{Allow, ""}
	}
	defer db.Close()
	ov, err := db.Check(board.Scope{Paths: []string{rel}}, sessionRef(p, ev.SessionID))
	if err != nil {
		return result{Allow, ""}
	}
	for _, o := range ov {
		if o.Level == board.Files || o.Level == board.Duplicate {
			return result{Ask, fmt.Sprintf("%s is held by %s in claim %s (%s, heartbeat %s ago). Edit anyway?",
				rel, o.Claim.Owner, o.Claim.ID, o.Claim.Task, time.Since(o.Claim.Heartbeat).Round(time.Minute))}
		}
	}
	return result{Allow, ""}
}

// boardHeartbeat (post_tool_call): keeps this session's claims alive and widens
// their scope with files it touched; new overlaps come back as context.
func boardHeartbeat(ev Event) result {
	p, err := privacy.ResolveSession(project.Home(), ev.Cwd, ev.SessionID)
	if err != nil || p == nil || ev.SessionID == "" {
		return result{Allow, ""}
	}
	var touched []string
	if fileWriters[ev.ToolName] {
		if rel := projectRel(p, ev.Cwd, firstStr(ev.ToolInput, "path", "file_path")); rel != "" {
			touched = append(touched, rel)
		}
	}
	db, err := board.Open(p)
	if err != nil {
		return result{Allow, ""}
	}
	defer db.Close()
	ov, _ := db.Heartbeat(sessionRef(p, ev.SessionID), touched)
	if len(ov) > 0 {
		o := ov[0]
		return result{Inform, fmt.Sprintf("[hpk board] %s also touches %s held by %s (%s). Tell the person and coordinate before going further.",
			o.Claim.ID, strings.Join(o.Files, ", "), o.Claim.Owner, o.Claim.Task)}
	}
	return result{Allow, ""}
}

// sessionRef maps a Hermes session id to hpk's own id when hpk started it.
func sessionRef(p *project.Project, hermesID string) string {
	if s, err := session.Get(p, hermesID); err == nil {
		return s.ID
	}
	return hermesID
}

// projectRel turns a tool path into a project-relative one; paths inside a
// worktree map back to the same project path.
func projectRel(p *project.Project, cwd, path string) string {
	if path == "" {
		return ""
	}
	abs := path
	if !filepath.IsAbs(abs) && cwd != "" {
		abs = filepath.Join(cwd, path)
	}
	rel, err := filepath.Rel(p.Root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "worktrees/") {
		parts := strings.SplitN(rel, "/", 3)
		if len(parts) < 3 {
			return ""
		}
		rel = parts[2]
	}
	return rel
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// trace keeps one line per guard decision that is not a silent allow, so a
// person (and hpk doctor) can see what a guard saw: <home>/hpk/hooks.log.
func trace(name string, ev Event, r result) result {
	if r.v == Allow {
		return r
	}
	proj := "-"
	if p, err := privacy.ResolveSession(project.Home(), ev.Cwd, ev.SessionID); err != nil {
		proj = "error: " + err.Error()
	} else if p != nil {
		proj = p.Name
	}
	line := fmt.Sprintf("%s\t%s\tverdict=%d\tsession=%s\tcwd=%s\tHPK_PROJECT=%s\tproject=%s\t%s\n",
		time.Now().UTC().Format(time.RFC3339), name, r.v, ev.SessionID, ev.Cwd, os.Getenv("HPK_PROJECT"), proj, r.why)
	path := filepath.Join(project.Home(), "hpk", "hooks.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 512<<10 {
		_ = os.Rename(path, path+".1")
	}
	if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.WriteString(line)
		f.Close()
	}
	return r
}

// personOver applies a team member's own money caps to the chats they started.
func personOver(session string) (bool, string) {
	home := project.Home()
	owner := project.OwnerOf(home, session)
	if owner == "" {
		return false, ""
	}
	c, err := team.Load(home)
	if err != nil || c == nil || !c.Enabled {
		return false, ""
	}
	m, ok := c.Find(owner)
	if !ok {
		return false, ""
	}
	return privacy.OverPerson(home, m.ID, m.BudgetDaily, m.BudgetMonthly, time.Now())
}

// tooBig marks a tool input too large or deep to check: it is refused.
const tooBig = "\x00hpk:input-too-big"

// privateTouch: the person's vault, browser profile, recordings and agent key,
// and hpk's own Console API, are not for the AI. Paths are normalised first
// (case, slashes, %-escapes, ./ and //) so spelling variants match too. This
// is a guard against accidents and simple attempts, not a sandbox; the real
// boundary is running the AI under its own OS account (docs/isolation.md).
func privateTouch(in []string) string {
	port := consolePort()
	for _, s := range in {
		if s == tooBig {
			return "this tool input is too large for hpk to check; split it into smaller calls"
		}
		l := normPath(s)
		for _, bad := range []string{"isolate setup", "isolate teardown", "isolate run", "ai-relay", "ai-account", "ai-exec", "isolation.json"} {
			if strings.Contains(l, bad) {
				return "who the AI runs as (hpk isolate) is the person's decision; ask them to run it in their own terminal"
			}
		}
		for _, bad := range []string{"hpk/vault", "vault.bin", "key.dpapi", "hpk/browser/agent.json", "hpk/browser/runs", "hpk/browser/profile", "hpk/private/", "ai-account.dpapi", "/api/vault", "/api/browser/control", "devtoolsactiveport"} {
			if strings.Contains(l, bad) {
				return "the person's vault, browser profile and recordings are not for the AI; use secrets by name ({{secret:name}}) through the hpk-browser tools"
			}
		}
		if port != "" {
			for _, h := range []string{"127.0.0.1:", "localhost:", "[::1]:", "0.0.0.0:", "127.1:"} {
				if strings.Contains(l, h+port) {
					return "hpk's Console API (port " + port + ") is for the person, not for the AI"
				}
			}
		}
	}
	return ""
}

func normPath(s string) string {
	l := strings.ToLower(strings.ReplaceAll(s, "\\", "/"))
	for i := 0; i < 2; i++ {
		if u, err := url.PathUnescape(l); err == nil {
			l = u
		}
	}
	for strings.Contains(l, "//") {
		l = strings.ReplaceAll(l, "//", "/")
	}
	for strings.Contains(l, "/./") {
		l = strings.ReplaceAll(l, "/./", "/")
	}
	return l
}

// consolePort is the port of the running hpk serve (from agent.json), or "".
func consolePort() string {
	raw, err := os.ReadFile(filepath.Join(project.Home(), "hpk", "browser", "agent.json"))
	if err != nil {
		return ""
	}
	var a struct{ URL string }
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	if u, err := url.Parse(a.URL); err == nil {
		return u.Port()
	}
	return ""
}
