// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"hpk.local/hpk/internal/acp"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
)

const sessionUsage = `hpk session — main chats and sub-tasks (docs/DESIGN.md §9–10)

  hpk session new --project P --name "auth › api-tests" [--parent 1] [--var version=1.4.0]
                  [--task TEXT] [--read FILE …] [--no-worktree]
                  records the session, makes its worktree (sub-tasks) and writes its brief
  hpk session start <num> --project P   open it in Hermes over ACP and send the brief (uses a model)
  hpk session list --project P [--json] the tree
  hpk session show <num> --project P    details and brief path
  hpk session rename <num> <title> --project P [--no-sync]
  hpk session status <num> <queued|running|waiting|done|failed|stopped> --project P
  hpk session report <num> --project P [--final] < report.txt
                  a sub-task's ≤15-line report: Done/Decided/Lessons/Open/Question
  hpk session inbox <num> --project P [--json]   what came back to a main chat
`

func runSession(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(stdout, sessionUsage)
		return 0
	}
	cmd, rest := args[0], args[1:]
	var pos []string
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		pos, rest = append(pos, rest[0]), rest[1:]
	}
	fs := flag.NewFlagSet("session "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	home := fs.String("home", "", "Hermes home")
	proj := fs.String("project", os.Getenv("HPK_PROJECT"), "project name")
	name := fs.String("name", "", "session name (new)")
	parent := fs.String("parent", "", "parent session number or id (new)")
	lane := fs.String("lane", "", "lane (new)")
	task := fs.String("task", "", "what to do (new)")
	noWT := fs.Bool("no-worktree", false, "sub-task works in the project root instead of its own worktree (new)")
	noSync := fs.Bool("no-sync", false, "rename in hpk only (rename)")
	final := fs.Bool("final", false, "the sub-task is finished (report)")
	asJSON := fs.Bool("json", false, "print JSON")
	var vars, read multi
	fs.Var(&vars, "var", "title template value key=value (new, repeatable)")
	fs.Var(&read, "read", "file or wiki page to read first (new, repeatable)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	h := *home
	if h == "" {
		h = project.Home()
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "hpk session "+cmd+":", err); return 1 }
	if *proj == "" {
		fmt.Fprintln(stderr, "hpk session: give --project NAME (or set HPK_PROJECT)")
		return 2
	}
	p, err := project.Load(h, *proj)
	if err != nil {
		return fail(err)
	}
	need := func(n int) bool {
		if len(pos) < n {
			fmt.Fprintf(stderr, "hpk session %s: missing argument\n\n%s", cmd, sessionUsage)
			return false
		}
		return true
	}

	switch cmd {
	case "new":
		kv := map[string]string{}
		for _, v := range vars {
			k, x, _ := strings.Cut(v, "=")
			kv[strings.TrimSpace(k)] = strings.TrimSpace(x)
		}
		s, err := session.New(p, session.NewOptions{Parent: *parent, Name: *name, Lane: *lane, Vars: kv, Task: *task, Read: read,
			Worktree: *parent != "" && !*noWT, Owner: currentUser()})
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, s)
		}
		fmt.Fprintf(stdout, "%s  %s\n  cwd:   %s\n  brief: %s\n", s.Num, s.Title, s.Cwd, s.Brief)
		if s.Branch != "" {
			fmt.Fprintf(stdout, "  branch: %s\n", s.Branch)
		}
		fmt.Fprintf(stdout, "start it in Hermes: hpk session start %s --project %s\n", s.Num, p.Name)
		return 0

	case "list":
		list, err := session.List(p)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, list)
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "no sessions yet")
		}
		for _, s := range list {
			indent := strings.Repeat("  ", strings.Count(s.Num, "."))
			fmt.Fprintf(stdout, "%s%-6s %-8s %s\n", indent, s.Num, s.Status, s.Title)
		}
		return 0

	case "show":
		if !need(1) {
			return 2
		}
		s, err := session.Get(p, pos[0])
		if err != nil {
			return fail(err)
		}
		return printJSON(stdout, s)

	case "rename":
		if !need(2) {
			return 2
		}
		var sync func(string, string) error
		if !*noSync {
			sync = hermesRename
		}
		s, err := session.Rename(p, pos[0], strings.Join(pos[1:], " "), sync)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%s  %s\n", s.Num, s.Title)
		return 0

	case "status":
		if !need(2) {
			return 2
		}
		s, err := session.SetStatus(p, pos[0], pos[1])
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%s  %s\n", s.Num, s.Status)
		return 0

	case "report":
		if !need(1) {
			return 2
		}
		text, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
		if err != nil {
			return fail(err)
		}
		s, r, err := session.File(p, pos[0], string(text), *final)
		if err != nil {
			return fail(err)
		}
		where := "its main chat"
		if s.Parent == "" {
			where = "this chat"
		}
		fmt.Fprintf(stdout, "report from %s sent to %s · status %s\n", s.Num, where, s.Status)
		if r.Question != "" {
			fmt.Fprintln(stdout, "it asks a question; the main chat answers or passes it to a person")
		}
		return 0

	case "inbox":
		if !need(1) {
			return 2
		}
		items, err := session.Inbox(p, pos[0])
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, items)
		}
		if len(items) == 0 {
			fmt.Fprintln(stdout, "nothing came back yet")
		}
		for _, it := range items {
			fmt.Fprintf(stdout, "%s  %-8s from %s %s\n  Done: %s\n", it.At.Local().Format("01-02 15:04"), it.Kind, it.From, it.Title, it.Report.Done)
			if it.Report.Question != "" {
				fmt.Fprintf(stdout, "  Question: %s\n", it.Report.Question)
			}
		}
		return 0

	case "start":
		if !need(1) {
			return 2
		}
		return startSession(p, pos[0], stdout, stderr)

	default:
		fmt.Fprintf(stderr, "hpk session: unknown command %q\n\n%s", cmd, sessionUsage)
		return 2
	}
}

// hermesRename uses the official CLI (about 3 s per call, docs/m0-findings.md §12).
func hermesRename(hermesID, title string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "hermes", "sessions", "rename", hermesID, title).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// startSession opens the session in Hermes over ACP, sends its brief, and stays
// attached for one turn. Permission requests come to this terminal; with no
// terminal they are denied (fail closed). A long-lived owner arrives with hpk serve (M7).
func startSession(p *project.Project, ref string, stdout, stderr io.Writer) int {
	s, err := session.Get(p, ref)
	if err != nil {
		fmt.Fprintln(stderr, "hpk session start:", err)
		return 1
	}
	brief, err := os.ReadFile(s.Brief)
	if err != nil {
		fmt.Fprintln(stderr, "hpk session start: brief:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	tty := isTerminal(os.Stdin)
	in := bufio.NewReader(os.Stdin)
	c, err := acp.Start(ctx, acp.Handlers{
		Update: func(_ string, u json.RawMessage) { printUpdate(stdout, u) },
		Permission: func(r acp.PermissionRequest) string {
			fmt.Fprintf(stdout, "\n⚠ permission requested: %s\n", strings.TrimSpace(string(r.ToolCall)))
			if !tty {
				fmt.Fprintln(stdout, "  no terminal to ask: denied (fail closed)")
				return ""
			}
			for i, o := range r.Options {
				fmt.Fprintf(stdout, "  %d) %s\n", i+1, o.Name)
			}
			fmt.Fprint(stdout, "  choose: ")
			line, _ := in.ReadString('\n')
			var n int
			if _, err := fmt.Sscan(strings.TrimSpace(line), &n); err == nil && n >= 1 && n <= len(r.Options) {
				return r.Options[n-1].OptionID
			}
			return ""
		},
	}, "hermes-acp")
	if err != nil {
		fmt.Fprintln(stderr, "hpk session start: cannot run hermes-acp:", err)
		return 1
	}
	defer c.Close()
	if _, err := c.Initialize(ctx); err != nil {
		fmt.Fprintln(stderr, "hpk session start: initialize:", err)
		return 1
	}
	hid, err := c.NewSession(ctx, s.Cwd)
	if err != nil {
		fmt.Fprintln(stderr, "hpk session start: session/new:", err)
		return 1
	}
	if _, err := session.Attach(p, s.ID, hid); err != nil {
		fmt.Fprintln(stderr, "hpk session start:", err)
		return 1
	}
	_ = hermesRename(hid, s.Title) // titles are set by hpk, not left to auto-naming
	fmt.Fprintf(stdout, "%s opened in Hermes as %s\n", s.Num, hid)
	reason, err := c.Prompt(ctx, hid, string(brief))
	if err != nil {
		session.SetStatus(p, s.ID, session.Failed)
		fmt.Fprintln(stderr, "\nhpk session start:", err)
		return 1
	}
	fmt.Fprintf(stdout, "\nturn ended: %s\n", reason)
	return 0
}

func printUpdate(w io.Writer, raw json.RawMessage) {
	var u struct {
		Kind    string `json:"sessionUpdate"`
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Title string `json:"title"`
	}
	if json.Unmarshal(raw, &u) != nil {
		return
	}
	switch u.Kind {
	case "agent_message_chunk":
		fmt.Fprint(w, u.Content.Text)
	case "tool_call":
		fmt.Fprintf(w, "\n· %s\n", u.Title)
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// runACPProbe checks the handshake with hermes-acp without opening a session or calling a model.
func runACPProbe(stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := acp.Start(ctx, acp.Handlers{}, "hermes-acp")
	if err != nil {
		fmt.Fprintln(stderr, "hpk acp probe:", err)
		return 1
	}
	defer c.Close()
	t0 := time.Now()
	r, err := c.Initialize(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "hpk acp probe: initialize:", err)
		return 1
	}
	return printJSON(stdout, map[string]any{"protocol_version": r.ProtocolVersion, "agent": r.AgentInfo, "capabilities": r.AgentCapabilities,
		"auth_methods": r.AuthMethods, "handshake_ms": time.Since(t0).Milliseconds()})
}
