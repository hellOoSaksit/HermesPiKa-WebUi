// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"hpk.local/hpk/internal/gates"
	"hpk.local/hpk/internal/project"
)

const gatesUsage = `hpk gates — humans in the loop (docs/DESIGN.md §11)

  hpk gates mode <project>                          show the mode now
  hpk gates mode <project> manual|ask|auto [--for 2h] [--session ID]
                                                    Auto falls back to Ask when the time runs out
  hpk gates check [<project>] --tool terminal --cmd "git push origin main"
  hpk gates check [<project>] --tool write_file --path AGENTS.md
                                                    dry run: what the guard would decide
  hpk gates lines                                   the red lines (every mode asks a person)
  hpk gates digest <project> [--since 24h] [--json] what Auto let through, numbered
  hpk gates audit <project> [--since 168h] [--json] every decision, oldest first

Every command takes --home DIR (default: Hermes home).
`

var lineText = map[string]string{
	gates.Push:     "push, tag, open or merge a PR, publish a release",
	gates.Delete:   "delete files, folders, worktrees, branches, databases or sessions",
	gates.Secrets:  "read or show .env and other secret files",
	gates.Control:  "change files that steer agents (AGENTS.md, CLAUDE.md, SOUL.md, skills, project.yaml, SCHEMA.md, Hermes config)",
	gates.Model:    "change the model or credentials mid-chat",
	gates.WikiCore: "write wiki decisions/, architecture/ or people/ directly",
	gates.Promote:  "write to the shared wiki or change a project's privacy",
	gates.Egress:   "send data off this machine outside the project's delivery channel",
	gates.Extra:    "the project's own red lines (gates.red_lines_extra entries written as re:<pattern>)",
}

func runGates(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(stdout, gatesUsage)
		return 0
	}
	cmd, rest := args[0], args[1:]
	var pos []string
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		pos, rest = append(pos, rest[0]), rest[1:]
	}
	fs := flag.NewFlagSet("gates "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	home := fs.String("home", "", "Hermes home")
	forDur := fs.Duration("for", 0, "how long Auto lasts, e.g. 2h (mode)")
	session := fs.String("session", "", "limit Auto to one session (mode)")
	tool := fs.String("tool", "terminal", "tool name (check)")
	cmdLine := fs.String("cmd", "", "command (check)")
	pathArg := fs.String("path", "", "file path (check)")
	since := fs.Duration("since", 24*time.Hour, "how far back (digest, audit)")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	h := *home
	if h == "" {
		h = project.Home()
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "hpk gates "+cmd+":", err); return 1 }
	load := func() (*project.Project, bool) {
		if len(pos) == 0 {
			fmt.Fprintf(stderr, "hpk gates %s: missing project name\n", cmd)
			return nil, false
		}
		p, err := project.Load(h, pos[0])
		if err != nil {
			fail(err)
			return nil, false
		}
		return p, true
	}
	now := time.Now()

	switch cmd {
	case "mode":
		p, ok := load()
		if !ok {
			return 1
		}
		if len(pos) == 1 {
			st := gates.Current(p, now)
			if *asJSON {
				return printJSON(stdout, st)
			}
			fmt.Fprintf(stdout, "%s: %s", p.Name, st.Mode)
			if st.Mode == gates.Auto && !st.AutoUntil.IsZero() {
				fmt.Fprintf(stdout, " until %s (%s left)", st.AutoUntil.Local().Format("15:04"), st.AutoUntil.Sub(now).Round(time.Minute))
			}
			if st.Session != "" {
				fmt.Fprintf(stdout, " · session %s only", st.Session)
			}
			fmt.Fprintln(stdout)
			return 0
		}
		st, err := gates.SetMode(p, pos[1], *forDur, *session, currentUser(), now)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%s is now %s", p.Name, st.Mode)
		if !st.AutoUntil.IsZero() {
			fmt.Fprintf(stdout, " until %s", st.AutoUntil.Local().Format("2006-01-02 15:04"))
		}
		fmt.Fprintln(stdout, " · red lines still ask a person")
		return 0

	case "check":
		var p *project.Project
		if len(pos) > 0 {
			var ok bool
			if p, ok = load(); !ok {
				return 1
			}
		}
		input := map[string]any{}
		if *cmdLine != "" {
			input["command"] = *cmdLine
		}
		if *pathArg != "" {
			input["path"] = *pathArg
		}
		r := gates.Decide(p, gates.Call{Tool: *tool, Input: input}, now)
		if *asJSON {
			return printJSON(stdout, r)
		}
		if r.Ask {
			fmt.Fprintf(stdout, "ASK  %s\n", r.Reason)
			for _, part := range gates.SplitCommand(*cmdLine) {
				line := "-"
				for _, h := range r.Hits {
					if h.Detail == part.Cmd {
						line = h.Line
					}
				}
				fmt.Fprintf(stdout, "     %-9s %s\n", line, part.Cmd)
			}
			return 0
		}
		fmt.Fprintf(stdout, "ALLOW (%s mode)\n", r.Mode)
		return 0

	case "lines":
		for _, k := range []string{gates.Push, gates.Delete, gates.Secrets, gates.Control, gates.Model, gates.WikiCore, gates.Promote, gates.Egress, gates.Extra} {
			fmt.Fprintf(stdout, "%-9s %s\n", k, lineText[k])
		}
		return 0

	case "digest", "audit":
		p, ok := load()
		if !ok {
			return 1
		}
		var list []gates.Decision
		var err error
		if cmd == "digest" {
			list, err = gates.Digest(p, now.Add(-*since))
		} else {
			list, err = gates.Decisions(p, now.Add(-*since))
		}
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, list)
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "nothing in this period")
		}
		for i, d := range list {
			who := d.By
			if d.Outcome != "" {
				who += " " + d.Outcome
			}
			fmt.Fprintf(stdout, "%3d  %s  %-10s %-6s %s %s %s\n", i+1, d.At.Local().Format("01-02 15:04"), d.Kind, who, strings.Join(d.Lines, ","), d.Tool, d.Detail)
		}
		if cmd == "digest" && len(list) > 0 {
			fmt.Fprintln(stdout, "\nTo undo: wiki changes → git revert in the wiki; file changes → Hermes checkpoints (/rollback) in that session.")
		}
		return 0

	default:
		fmt.Fprintf(stderr, "hpk gates: unknown command %q\n\n%s", cmd, gatesUsage)
		return 2
	}
}
