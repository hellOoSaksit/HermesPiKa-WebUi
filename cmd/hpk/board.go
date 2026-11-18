// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"hpk.local/hpk/internal/board"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
)

const boardUsage = `hpk board — who is working on what (docs/DESIGN.md §20; 0 tokens)

  hpk board check  --task T [--lane L] [--path GLOB …] [--wiki PAGE …]   what would overlap
  hpk board claim  --task T [--lane L] [--path GLOB …] [--wiki PAGE …] [--session S] [--yes]
                   a duplicate (same task) always stops; a file overlap needs --yes from a person
  hpk board heartbeat <claim|session> [--path FILE …]
  hpk board release <claim|session>
  hpk board who <task words | path | lane | claim>   answered from the board, nobody disturbed
  hpk board list [--all] [--json]
  hpk board stale [--after 30m]                       flag claims with no heartbeat (never releases)
  hpk board notices [--all] [--read]
  hpk board ask <session> "question" [--from S]       queued into that session, never interrupting
  hpk board answer <id> --status … --progress … --eta … --files …
  hpk board asks
  hpk board diffcheck [--base main]                   files changed in two worktrees at once
  hpk board render                                    write BOARD.md from board.db

Every command takes --project NAME (or $HPK_PROJECT) and --home DIR.
`

const exitNeedsPerson = 5

func runBoard(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(stdout, boardUsage)
		return 0
	}
	cmd, rest := args[0], args[1:]
	var pos []string
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		pos, rest = append(pos, rest[0]), rest[1:]
	}
	fs := flag.NewFlagSet("board "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	home := fs.String("home", "", "Hermes home")
	proj := fs.String("project", os.Getenv("HPK_PROJECT"), "project name")
	task := fs.String("task", "", "task (check, claim)")
	lane := fs.String("lane", "", "lane (check, claim)")
	sess := fs.String("session", "", "session id or number (claim)")
	from := fs.String("from", "", "asking session (ask)")
	yes := fs.Bool("yes", false, "a person confirms a file overlap (claim)")
	all := fs.Bool("all", false, "include finished claims / read notices")
	markRead := fs.Bool("read", false, "mark notices as read (notices)")
	after := fs.Duration("after", 30*time.Minute, "no heartbeat for this long = stale")
	base := fs.String("base", "", "main branch for diffcheck (default: current branch of the project root)")
	asJSON := fs.Bool("json", false, "print JSON")
	status := fs.String("status", "", "answer: status")
	progress := fs.String("progress", "", "answer: how far")
	eta := fs.String("eta", "", "answer: expected finish")
	files := fs.String("files", "", "answer: files being touched")
	var paths, wiki multi
	fs.Var(&paths, "path", "path or glob, project-relative (repeatable)")
	fs.Var(&wiki, "wiki", "wiki page (repeatable)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	h := *home
	if h == "" {
		h = project.Home()
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "hpk board "+cmd+":", err); return 1 }
	if *proj == "" {
		fmt.Fprintln(stderr, "hpk board: give --project NAME (or set HPK_PROJECT)")
		return 2
	}
	p, err := project.Load(h, *proj)
	if err != nil {
		return fail(err)
	}
	db, err := board.Open(p)
	if err != nil {
		return fail(err)
	}
	defer db.Close()
	sessRef := func(ref string) string {
		if ref == "" {
			return ""
		}
		if s, err := session.Get(p, ref); err == nil {
			return s.ID
		}
		return ref
	}
	arg := func() (string, bool) {
		if len(pos) == 0 {
			fmt.Fprintf(stderr, "hpk board %s: missing argument\n", cmd)
			return "", false
		}
		return pos[0], true
	}

	switch cmd {
	case "check", "claim":
		sc := board.Scope{Task: *task, Lane: *lane, Paths: paths, Wiki: wiki}
		if cmd == "check" {
			ov, err := db.Check(sc, sessRef(*sess))
			if err != nil {
				return fail(err)
			}
			if *asJSON {
				return printJSON(stdout, ov)
			}
			printOverlaps(stdout, ov)
			return 0
		}
		c, ov, err := db.ClaimWork(sc, currentUser(), sessRef(*sess), *yes)
		if errors.Is(err, board.ErrNeedsPerson) {
			if *asJSON {
				printJSON(stdout, map[string]any{"claimed": false, "overlaps": ov})
				return exitNeedsPerson
			}
			printOverlaps(stdout, ov)
			fmt.Fprintln(stdout, "\nNot claimed. A person chooses (§20.6):\n  1. wait until it is released\n  2. take another part (hpk board list)\n  3. work together: hpk board ask <session> \"…\"\n  4. take it over (the claim's owner or a maintainer approves)")
			if hasLevel(ov, board.Files) && !hasLevel(ov, board.Duplicate) {
				fmt.Fprintln(stdout, "Only files overlap: a person may confirm with --yes.")
			}
			return exitNeedsPerson
		}
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, map[string]any{"claimed": true, "claim": c, "overlaps": ov})
		}
		fmt.Fprintf(stdout, "claimed %s: %s\n", c.ID, c.Task)
		printOverlaps(stdout, ov)
		return 0

	case "heartbeat":
		ref, ok := arg()
		if !ok {
			return 2
		}
		ov, err := db.Heartbeat(sessRef(ref), paths)
		if err != nil {
			return fail(err)
		}
		printOverlaps(stdout, ov)
		return 0

	case "release":
		ref, ok := arg()
		if !ok {
			return 2
		}
		list, err := db.Release(sessRef(ref))
		if err != nil {
			return fail(err)
		}
		for _, c := range list {
			fmt.Fprintf(stdout, "released %s: %s\n", c.ID, c.Task)
		}
		return 0

	case "who":
		q := strings.Join(pos, " ")
		list, err := db.Who(q)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, list)
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "nobody holds a claim on that")
		}
		for _, c := range list {
			fmt.Fprintf(stdout, "%s  %s · %s · session %s · heartbeat %s ago · %s\n    %s\n", c.ID, c.Task, c.Owner, dashStr(c.Session), time.Since(c.Heartbeat).Round(time.Minute), c.Status, strings.Join(c.Paths, " "))
		}
		return 0

	case "list":
		list, err := db.Claims(*all)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, list)
		}
		for _, c := range list {
			fmt.Fprintf(stdout, "%s  %-13s %-10s %s · %s\n", c.ID, c.Status, dashStr(c.Lane), c.Task, c.Owner)
		}
		return 0

	case "stale":
		list, err := db.MarkStale(*after)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%d claims are now stale\n", len(list))
		return 0

	case "notices":
		list, err := db.Notices(*all)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			printJSON(stdout, list)
		} else {
			if len(list) == 0 {
				fmt.Fprintln(stdout, "no notices")
			}
			for _, n := range list {
				fmt.Fprintf(stdout, "%s %-8s %-18s %s\n", n.At.Local().Format("01-02 15:04"), n.Level, n.Kind, n.Text)
			}
		}
		if *markRead {
			return boolCode(db.MarkRead() == nil)
		}
		return 0

	case "ask":
		if len(pos) < 2 {
			fmt.Fprintln(stderr, "hpk board ask: give the session and the question")
			return 2
		}
		target := sessRef(pos[0])
		owner := ""
		if s, err := session.Get(p, target); err == nil {
			owner = s.Owner
		}
		a, err := db.Ask(sessRef(*from), target, strings.Join(pos[1:], " "), owner)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "question %d queued for %s; it is delivered as:\n%s\n", a.ID, target, board.QueuedText(a))
		return 0

	case "answer":
		ref, ok := arg()
		if !ok {
			return 2
		}
		id, err := strconv.ParseInt(ref, 10, 64)
		if err != nil {
			return fail(fmt.Errorf("answer needs the question number"))
		}
		a, err := db.Answer(id, map[string]string{"status": *status, "progress": *progress, "eta": *eta, "files": *files})
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "answered question %d from %s\n", a.ID, dashStr(a.From))
		return 0

	case "asks":
		list, err := db.Asks()
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, list)
		}
		for _, a := range list {
			fmt.Fprintf(stdout, "%3d %-9s %s → %s: %s %v\n", a.ID, a.Status, dashStr(a.From), a.To, a.Question, a.Answer)
		}
		return 0

	case "diffcheck":
		b := *base
		if b == "" {
			b = "HEAD"
			if out, err := gitOut(p.Root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
				b = out
			}
		}
		clashes, err := db.DiffWorktrees(p.Root, b)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, clashes)
		}
		if len(clashes) == 0 {
			fmt.Fprintln(stdout, "no file is changed in two worktrees")
		}
		for _, c := range clashes {
			fmt.Fprintf(stdout, "🟠 %s  changed in %s\n", c.File, strings.Join(c.Worktrees, ", "))
		}
		return 0

	case "render":
		kind, file := boardKind(p)
		out, err := db.Render(p.Root, file, kind)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "wrote", out)
		return 0

	default:
		fmt.Fprintf(stderr, "hpk board: unknown command %q\n\n%s", cmd, boardUsage)
		return 2
	}
}

func printOverlaps(w io.Writer, ov []board.Overlap) {
	icon := map[string]string{board.Duplicate: "🔴 duplicate", board.Files: "🟠 files", board.Related: "🟡 related"}
	for _, o := range ov {
		fmt.Fprintf(w, "%s  %s · %s (%s) · %s, heartbeat %s ago", icon[o.Level], o.Claim.ID, o.Claim.Task, o.Claim.Owner, o.Why, time.Since(o.Claim.Heartbeat).Round(time.Minute))
		if len(o.Files) > 0 {
			fmt.Fprintf(w, " · %s", strings.Join(o.Files, ", "))
		}
		fmt.Fprintln(w)
	}
}

func hasLevel(ov []board.Overlap, level string) bool {
	for _, o := range ov {
		if o.Level == level {
			return true
		}
	}
	return false
}

func boardKind(p *project.Project) (string, string) {
	raw, err := os.ReadFile(p.Dir + "/project.yaml")
	if err != nil {
		return "hpk", "BOARD.md"
	}
	var f struct {
		Board struct {
			Kind string `yaml:"kind"`
			File string `yaml:"file"`
		} `yaml:"board"`
	}
	_ = yaml.Unmarshal(raw, &f)
	return f.Board.Kind, f.Board.File
}

func dashStr(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

func boolCode(ok bool) int {
	if ok {
		return 0
	}
	return 1
}

func gitOut(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}
