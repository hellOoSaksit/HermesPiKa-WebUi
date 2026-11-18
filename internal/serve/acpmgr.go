// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"hpk.local/hpk/internal/acp"
	"hpk.local/hpk/internal/gates"
	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/notify"
	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
)

// Msg is one item of a chat log, in the Console's shape (js/data.js chat entries).
type Msg struct {
	T         string  `json:"t"` // msg | tool | permit | note
	Who       string  `json:"who,omitempty"`
	Author    string  `json:"author,omitempty"`
	At        int64   `json:"at"`
	Text      string  `json:"text,omitempty"`
	Kind      string  `json:"kind,omitempty"` // note: turnError | stopped (the Console words it in the person's language)
	How       string  `json:"how,omitempty"`
	Streaming bool    `json:"streaming,omitempty"`
	Name      string  `json:"name,omitempty"`    // tool
	Summary   string  `json:"summary,omitempty"` // tool
	Out       string  `json:"out,omitempty"`     // tool
	ID        string  `json:"id,omitempty"`      // permit
	RedLine   string  `json:"redLine,omitempty"` // permit
	Command   string  `json:"command,omitempty"` // permit
	Why       string  `json:"why,omitempty"`     // permit
	Decision  string  `json:"decision,omitempty"`
	From      string  `json:"from,omitempty"` // switch: the model before
	To        string  `json:"to,omitempty"`   // switch: the model after
	Way       string  `json:"way,omitempty"`  // switch: continue | compact
	Fill      float64 `json:"fill,omitempty"` // switch: full history as a share of the new window
	ToolID    string  `json:"-"`
}

type liveSession struct {
	project   string
	hpkID     string
	hermesID  string
	log       []*Msg
	current   *Msg // the agent message being streamed
	streaming bool
	model     string // provider:model the session uses now
	ctxUsed   int    // Hermes' usage_update: estimated request tokens
	ctxSize   int    // and the model's context window
	carry     string // brief and recall to send in front of the next message (after a model switch)
}

type permit struct {
	id       string
	project  string
	hpkID    string
	msg      *Msg
	options  []acp.PermissionOption
	created  time.Time
	decision chan string
}

// Launcher starts an ACP agent for a project. Tests swap in a fake agent.
type Launcher func(p *project.Project, h acp.Handlers) (*acp.Client, error)

// ACPManager owns the hermes-acp processes for sessions started from the Console:
// one process per project (WIKI_PATH and HPK_PROJECT are per process, see
// docs/m0-findings.md §14), kept running so a turn does not pay the ~7 s start-up.
type ACPManager struct {
	mu       sync.Mutex
	home     string
	launch   Launcher
	clients  map[string]*acp.Client
	sessions map[string]*liveSession // by hpk session id
	byHermes map[string]*liveSession
	permits  map[string]*permit
	models   map[string]acp.Models // what hermes-acp offers, by project
	nextPerm int
	notify   func(event, data string)
	me       func() string
}

// NewACPManager creates an idle manager; nothing starts until a session does.
func NewACPManager(home string, launch Launcher, notify func(event, data string), me func() string) *ACPManager {
	if launch == nil {
		launch = launchHermes
	}
	return &ACPManager{home: home, launch: launch, clients: map[string]*acp.Client{}, sessions: map[string]*liveSession{},
		byHermes: map[string]*liveSession{}, permits: map[string]*permit{}, models: map[string]acp.Models{}, notify: notify, me: me}
}

func launchHermes(p *project.Project, h acp.Handlers) (*acp.Client, error) {
	cmd := isolate.Command("hermes-acp")
	cmd.Dir = p.Root
	cmd.Env = append(os.Environ(), "HPK_PROJECT="+p.Name, "WIKI_PATH="+p.WikiDir(), "PATH="+hpkPath(p))
	return acp.StartCmd(cmd, h)
}

// Close stops every agent process.
func (m *ACPManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	// hermes-acp may hand sessions to a running gateway, which outlives this
	// process: stop the turns hpk started, so nothing keeps working unwatched.
	for _, ls := range m.sessions {
		if c := m.clients[ls.project]; c != nil && ls.hermesID != "" {
			_ = c.Cancel(ls.hermesID)
		}
	}
	for _, c := range m.clients {
		c.Close()
	}
}

// IsLive reports whether serve holds this session open.
func (m *ACPManager) IsLive(hpkID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[hpkID]
	return ok
}

// Transcript returns a copy of a live session's chat log.
// ProjectOf names the project of a chat this serve holds ("" when it holds none).
func (m *ACPManager) ProjectOf(hpkID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ls, ok := m.sessions[hpkID]; ok {
		return ls.project
	}
	return ""
}

func (m *ACPManager) Transcript(hpkID string) ([]Msg, bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ls, ok := m.sessions[hpkID]
	if !ok {
		return nil, false, false
	}
	out := make([]Msg, 0, len(ls.log))
	for _, x := range ls.log {
		out = append(out, *x)
	}
	return out, true, ls.streaming
}

func (m *ACPManager) client(p *project.Project) (*acp.Client, error) {
	if over, why := privacy.OverMoney(m.home, p, time.Now()); over {
		return nil, fmt.Errorf("%s. Raise the cap in Settings to start a session", why)
	}
	// local_only: refuse before Hermes sends anything (the brief, AGENTS.md) to a remote model.
	if _, _, err := privacy.CheckActiveProfile(m.home, p); err != nil {
		return nil, err
	}
	m.mu.Lock()
	c := m.clients[p.Name]
	m.mu.Unlock()
	if c != nil {
		select {
		case <-c.Done():
		default:
			return c, nil
		}
	}
	name := p.Name
	c, err := m.launch(p, acp.Handlers{
		Update:     func(sid string, u json.RawMessage) { m.onUpdate(sid, u) },
		Permission: func(r acp.PermissionRequest) string { return m.onPermission(name, r) },
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := c.Initialize(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("hermes-acp initialize: %w", err)
	}
	m.mu.Lock()
	m.clients[name] = c
	m.mu.Unlock()
	return c, nil
}

// Start opens a recorded session in Hermes and sends its brief plus an optional first
// message; show is what the chat displays as the person's message (the brief stays out of view).
func (m *ACPManager) Start(p *project.Project, hpkID, first, show, author string) error {
	return m.startOn(p, hpkID, first, show, author, "")
}

// startOn is Start on a given model (provider:model); "" keeps the profile's.
func (m *ACPManager) startOn(p *project.Project, hpkID, first, show, author, model string) error {
	s, err := session.Get(p, hpkID)
	if err != nil {
		return err
	}
	c, err := m.client(p)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	hid, models, err := c.NewSessionModels(ctx, s.Cwd)
	if err != nil {
		return err
	}
	if model != "" && model != models.Current {
		if err := c.SetModel(ctx, hid, model); err != nil {
			return fmt.Errorf("hermes did not switch the new session to %s: %w", model, err)
		}
		models.Current = model
	}
	if _, err := session.Attach(p, s.ID, hid); err != nil {
		return err
	}
	ls := &liveSession{project: p.Name, hpkID: s.ID, hermesID: hid, model: models.Current}
	m.mu.Lock()
	if len(models.Available) > 0 {
		m.models[p.Name] = models
	}
	m.sessions[s.ID] = ls
	m.byHermes[hid] = ls
	m.mu.Unlock()
	brief := ""
	if raw, err := os.ReadFile(s.Brief); err == nil {
		brief = string(raw)
	}
	text := strings.TrimSpace(brief + "\n\n" + first)
	m.addUser(ls, orStr(show, first), "", author)
	go m.run(c, ls, text)
	m.notify("sessions", p.Name)
	return nil
}

// Prompt sends a message the way the Console's send menu says: send, queue, steer or interrupt.
func (m *ACPManager) Prompt(p *project.Project, hpkID, text, how, author string) error {
	m.mu.Lock()
	ls := m.sessions[hpkID]
	c := m.clients[p.Name]
	m.mu.Unlock()
	if ls == nil || c == nil {
		return errors.New("this session is not open in hpk serve; start it from the Console or with hpk session start")
	}
	if how == "send" && ls.streaming {
		how = "queue" // never interrupt by accident
	}
	switch how {
	case "queue":
		text = "/queue " + text
	case "steer":
		text = "/steer " + text
	case "interrupt":
		_ = c.Cancel(ls.hermesID)
	}
	m.addUser(ls, strings.TrimPrefix(strings.TrimPrefix(text, "/queue "), "/steer "), map[string]string{"send": "", "queue": "queue", "steer": "steer", "interrupt": "interrupt"}[how], author)
	// After a model switch the brief and recalled pages ride in front of the next message.
	m.mu.Lock()
	if carry := ls.carry; carry != "" && how != "steer" {
		ls.carry = ""
		if how == "queue" {
			text = "/queue " + carry + strings.TrimPrefix(text, "/queue ")
		} else {
			text = carry + text
		}
	}
	m.mu.Unlock()
	if how == "queue" || how == "steer" {
		// Hermes takes these as commands inside the running turn.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = c.Prompt(ctx, ls.hermesID, text)
		}()
		return nil
	}
	go m.run(c, ls, text)
	return nil
}

// Cancel stops the current turn.
func (m *ACPManager) Cancel(hpkID string) error {
	m.mu.Lock()
	ls := m.sessions[hpkID]
	var c *acp.Client
	if ls != nil {
		c = m.clients[ls.project]
	}
	m.mu.Unlock()
	if ls == nil || c == nil {
		return errors.New("session is not open")
	}
	return c.Cancel(ls.hermesID)
}

func (m *ACPManager) run(c *acp.Client, ls *liveSession, text string) {
	m.mu.Lock()
	ls.streaming = true
	m.mu.Unlock()
	m.notify("chat", ls.hpkID)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	reason, err := c.Prompt(ctx, ls.hermesID, text)
	m.mu.Lock()
	ls.streaming = false
	ls.endStream()
	if err != nil {
		ls.log = append(ls.log, &Msg{T: "note", Kind: "turnError", At: time.Now().UnixMilli(), Text: "The turn ended with an error: " + err.Error()})
	} else if reason == "cancelled" {
		ls.log = append(ls.log, &Msg{T: "note", Kind: "stopped", At: time.Now().UnixMilli(), Text: "Stopped."})
	}
	proj, hpkID := ls.project, ls.hpkID
	m.mu.Unlock()
	m.notify("chat", ls.hpkID)
	if err == nil && reason != "cancelled" {
		if p, e := project.Load(m.home, proj); e == nil {
			title := hpkID
			if x, e := session.Get(p, hpkID); e == nil {
				title = x.Title
			}
			go notify.Send(p, notify.Report, `"`+trimTo(title, 80)+`" finished a turn. Read it in HPK Console.`, time.Now())
		}
	}
}

func trimTo(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (m *ACPManager) addUser(ls *liveSession, text, how, author string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	m.mu.Lock()
	ls.log = append(ls.log, &Msg{T: "msg", Who: "user", Author: orStr(author, m.me()), At: time.Now().UnixMilli(), Text: text, How: how})
	m.mu.Unlock()
	m.notify("chat", ls.hpkID)
}

// onUpdate turns ACP session/update notifications into chat entries.
func (m *ACPManager) onUpdate(hermesID string, raw json.RawMessage) {
	var u struct {
		Kind       string          `json:"sessionUpdate"`
		Content    json.RawMessage `json:"content"`
		ToolCallID string          `json:"toolCallId"`
		Title      string          `json:"title"`
		Status     string          `json:"status"`
	}
	if json.Unmarshal(raw, &u) != nil {
		return
	}
	m.mu.Lock()
	ls := m.byHermes[hermesID]
	if ls == nil {
		m.mu.Unlock()
		return
	}
	now := time.Now().UnixMilli()
	event := "chat"
	switch u.Kind {
	case "agent_message_chunk":
		var c struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(u.Content, &c)
		if ls.current == nil {
			ls.current = &Msg{T: "msg", Who: "agent", At: now, Streaming: true}
			ls.log = append(ls.log, ls.current)
		} else {
			event = "chat-stream"
		}
		ls.current.Text += c.Text
	case "usage_update":
		var x struct {
			Size int `json:"size"`
			Used int `json:"used"`
		}
		_ = json.Unmarshal(raw, &x)
		ls.ctxSize, ls.ctxUsed = x.Size, x.Used
		m.mu.Unlock()
		return
	case "tool_call":
		ls.endStream()
		ls.log = append(ls.log, &Msg{T: "tool", At: now, Name: u.Title, Summary: u.Status, ToolID: u.ToolCallID})
	case "tool_call_update":
		for i := len(ls.log) - 1; i >= 0; i-- {
			if ls.log[i].T == "tool" && ls.log[i].ToolID == u.ToolCallID {
				if u.Status != "" {
					ls.log[i].Summary = u.Status
				}
				if u.Title != "" {
					ls.log[i].Name = u.Title
				}
				break
			}
		}
	default:
		m.mu.Unlock()
		return
	}
	id := ls.hpkID
	m.mu.Unlock()
	m.notify(event, id)
}

// onPermission holds a permission request until a person decides in the Console.
// No answer within the Hermes timeout means deny (fail closed).
func (m *ACPManager) onPermission(projectName string, r acp.PermissionRequest) string {
	var tc struct {
		Title    string         `json:"title"`
		Kind     string         `json:"kind"`
		RawInput map[string]any `json:"rawInput"`
	}
	_ = json.Unmarshal(r.ToolCall, &tc)
	cmd := ""
	for _, k := range []string{"command", "path", "file_path"} {
		if s, ok := tc.RawInput[k].(string); ok {
			cmd = s
			break
		}
	}
	tool := "terminal"
	if _, ok := tc.RawInput["path"]; ok {
		tool = "write_file"
	}
	line := ""
	// A red line hpk's own guard raised names itself in the title ("… — push: git push");
	// Hermes then shows a synthetic "<terminal> (plugin approval rule)" input, so trust the title.
	if m := redLineTitle.FindStringSubmatch(tc.Title); m != nil {
		line = m[1]
		if m[2] != "" && (cmd == "" || strings.HasPrefix(cmd, "<")) {
			cmd = m[2]
		}
	} else if hits := gates.Check(tool, tc.RawInput, nil, ""); len(hits) > 0 {
		line = hits[0].Line
	}
	m.mu.Lock()
	ls := m.byHermes[r.SessionID]
	m.nextPerm++
	id := fmt.Sprintf("perm-%d", m.nextPerm)
	msg := &Msg{T: "permit", At: time.Now().UnixMilli(), ID: id, RedLine: line, Command: orStr(cmd, tc.Title), Why: tc.Title}
	pm := &permit{id: id, project: projectName, msg: msg, options: r.Options, created: time.Now(), decision: make(chan string, 1)}
	if ls != nil {
		pm.hpkID = ls.hpkID
		ls.endStream()
		ls.log = append(ls.log, msg)
	}
	m.permits[id] = pm
	m.mu.Unlock()
	if ls != nil {
		session.SetStatus(mustProject(m.home, projectName), ls.hpkID, session.Waiting)
		m.notify("chat", ls.hpkID)
	}
	m.notify("approvals", projectName)
	if p, err := project.Load(m.home, projectName); err == nil {
		what := orStr(cmd, tc.Title)
		if line != "" {
			what = line + " red line: " + what
		}
		go notify.Send(p, notify.Approval, "The agent is waiting for you ("+trimTo(what, 120)+"). Answer in HPK Console, Approvals, within 5 minutes or it is denied.", time.Now())
	}

	var choice string
	select {
	case choice = <-pm.decision:
	case <-time.After(290 * time.Second): // Hermes gives up at 300 s
		choice = "deny"
	}
	m.mu.Lock()
	delete(m.permits, id)
	msg.Decision = choice
	m.mu.Unlock()
	if ls != nil {
		session.SetStatus(mustProject(m.home, projectName), ls.hpkID, session.Running)
		m.notify("chat", ls.hpkID)
	}
	m.notify("approvals", projectName)
	return pickOption(r.Options, choice)
}

// Decide answers a held permission of a project: approve (once), session, or deny.
// A permit of another project is "not found": the decider was allowed to see projectName only.
func (m *ACPManager) Decide(id, projectName, decision string) error {
	m.mu.Lock()
	pm := m.permits[id]
	m.mu.Unlock()
	if pm == nil || pm.project != projectName {
		return fmt.Errorf("no pending permission %s (it may have timed out)", id)
	}
	switch decision {
	case "approve", "once", "session", "deny":
	default:
		return fmt.Errorf("decision must be approve, session or deny")
	}
	select {
	case pm.decision <- decision:
	default:
	}
	return nil
}

// Pending lists held permissions of a project as Console approvals.
func (m *ACPManager) Pending(projectName string) []jsApproval {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []jsApproval
	for _, pm := range m.permits {
		if pm.project != projectName {
			continue
		}
		out = append(out, jsApproval{ID: pm.id, Kind: "permission", RedLine: orStr(pm.msg.RedLine, "control"), Session: pm.hpkID,
			Title: orStr(pm.msg.Why, "Permission requested"), Detail: pm.msg.Command, Created: pm.created.UnixMilli(), Project: projectName})
	}
	return out
}

// pickOption maps a Console decision to one of the options the agent offered.
func pickOption(opts []acp.PermissionOption, decision string) string {
	want := map[string][]string{
		"approve": {"allow_once", "allow"},
		"once":    {"allow_once", "allow"},
		"session": {"allow_session", "allow_always", "allow"},
		"deny":    {"reject_once", "deny", "reject"},
	}[decision]
	for _, w := range want {
		for _, o := range opts {
			if o.Kind == w || o.OptionID == w {
				return o.OptionID
			}
		}
	}
	for _, w := range want {
		for _, o := range opts {
			if strings.Contains(strings.ToLower(o.Kind+" "+o.OptionID), w) {
				return o.OptionID
			}
		}
	}
	return "" // cancelled = denied
}

func mustProject(home, name string) *project.Project {
	p, err := project.Load(home, name)
	if err != nil {
		return &project.Project{Name: name}
	}
	return p
}

// endStream closes the agent message being streamed (a tool call or permission request follows it).
func (ls *liveSession) endStream() {
	if ls.current != nil {
		ls.current.Streaming = false
		ls.current = nil
	}
}

// hpkPath puts hpk on the agent's PATH, so the skills' `hpk …` commands work in
// Console sessions: the installed copy first (<home>/hpk/bin), then this binary's folder.
func hpkPath(p *project.Project) string {
	dirs := []string{filepath.Join(filepath.Dir(filepath.Dir(p.Dir)), "bin")}
	if self, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(self))
	}
	return strings.Join(append(dirs, os.Getenv("PATH")), string(os.PathListSeparator))
}

var redLineTitle = regexp.MustCompile(`Red line [^—]*— ([a-z-]+): (?:(.*): <[a-z_]+> \(plugin approval rule\)$)?`)
