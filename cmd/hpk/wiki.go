// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

const wikiUsage = `hpk wiki — the project wiki (0 tokens; files + git)

  hpk wiki init [--project NAME]
  hpk wiki search <words> [--limit 10] [--json]
  hpk wiki draft --target lessons/x.md --source FILE:LINE [--source …] [--title T]
                 [--summary S] [--confidence low|medium|high] [--author A] [--file F | stdin]
  hpk wiki drafts [--json]
  hpk wiki approve <1,3 | 2-4> [--by NAME | --by auto] [--json]
  hpk wiki discard <nums> [--why TEXT] [--by NAME]
  hpk wiki stale
  hpk wiki lint [--json]
  hpk wiki log <note>
  hpk wiki raw <name.md> [--file F | stdin]           untouched source material under raw/ (no draft)
  hpk wiki promote <page> --project NAME [--to team|users/<id>]

Every command takes --wiki DIR, or --project NAME for that project's wiki
(default: $WIKI_PATH, then ./wiki).
`

// multi collects a repeatable flag.
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func runWiki(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(stdout, wikiUsage)
		return 0
	}
	cmd, rest := args[0], args[1:]
	// Allow one positional value before the flags: `hpk wiki approve 1,3 --by mali`.
	var pos string
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		pos, rest = rest[0], rest[1:]
	}
	fs := flag.NewFlagSet("wiki "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("wiki", "", "wiki directory")
	asJSON := fs.Bool("json", false, "print JSON")
	project_ := fs.String("project", "", "project name: init names the wiki; other commands use that project's wiki")
	target := fs.String("target", "", "page path, e.g. lessons/csv-encoding.md (draft)")
	title := fs.String("title", "", "page title (draft)")
	summary := fs.String("summary", "", "one line: what this adds or changes (draft)")
	confidence := fs.String("confidence", "", "low | medium | high (draft)")
	author := fs.String("author", "", "who drafted it (draft)")
	file := fs.String("file", "", "read the draft body from this file instead of stdin (draft)")
	by := fs.String("by", "", "who decides: a name, or auto (approve, discard)")
	why := fs.String("why", "", "reason (discard)")
	limit := fs.Int("limit", 10, "max results (search)")
	to := fs.String("to", "team", "shared wiki to promote into: team or users/<id> (promote)")
	home := fs.String("home", "", "Hermes home (with --project)")
	var sources multi
	fs.Var(&sources, "source", "source of the claims: file:line, commit or session id (repeatable)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if pos == "" && fs.NArg() > 0 {
		pos = strings.Join(fs.Args(), " ")
	}

	h := *home
	if h == "" {
		h = project.Home()
	}
	if cmd == "promote" {
		if *project_ == "" || pos == "" {
			fmt.Fprintln(stderr, "hpk wiki promote: give the page and --project NAME")
			return 2
		}
		return promote(h, *project_, pos, *to, stdout, stderr)
	}
	wikiDir := resolveWikiDir(*dir)
	if *dir == "" && *project_ != "" && cmd != "init" {
		if p, err := project.Load(h, *project_); err == nil {
			wikiDir = p.WikiDir()
		}
	}
	w := wiki.Open(wikiDir)
	fail := func(err error) int { fmt.Fprintln(stderr, "hpk wiki "+cmd+":", err); return 1 }
	if cmd != "init" && !w.Exists() {
		return fail(fmt.Errorf("no wiki at %s; run `hpk wiki init --wiki %s`", w.Dir, w.Dir))
	}

	switch cmd {
	case "init":
		created, err := w.Init(*project_, assets.WikiTemplates())
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, map[string]any{"dir": w.Dir, "created": created})
		}
		if len(created) == 0 {
			fmt.Fprintln(stdout, "wiki already set up at", w.Dir)
		} else {
			fmt.Fprintln(stdout, "wiki created at", w.Dir, "("+strings.Join(created, ", ")+")")
		}
		return 0

	case "search":
		hits, err := w.Search(pos, *limit)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, hits)
		}
		if len(hits) == 0 {
			fmt.Fprintln(stdout, "no matches")
		}
		for _, h := range hits {
			fmt.Fprintf(stdout, "%s — %s\n", h.File, h.Title)
			if h.Snippet != "" {
				fmt.Fprintf(stdout, "    %s\n", h.Snippet)
			}
		}
		return 0

	case "draft":
		var body []byte
		var err error
		if *file != "" {
			body, err = os.ReadFile(*file)
		} else {
			body, err = io.ReadAll(io.LimitReader(stdin, 1<<20))
		}
		if err != nil {
			return fail(err)
		}
		d, err := w.CreateDraft(wiki.DraftInput{Target: *target, Title: *title, Sources: sources, Summary: *summary,
			Confidence: *confidence, Author: *author, Body: string(body)})
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, d)
		}
		fmt.Fprintf(stdout, "draft %d → %s (%s); waits for approval: hpk wiki approve %d\n", d.N, d.Meta.Target, d.Meta.Kind, d.N)
		return 0

	case "drafts":
		ds, err := w.Drafts()
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, ds)
		}
		if len(ds) == 0 {
			fmt.Fprintln(stdout, "no drafts waiting")
		}
		for _, d := range ds {
			fmt.Fprintf(stdout, "%3d  %-5s %s — %s\n", d.N, d.Meta.Kind, d.Meta.Target, d.Meta.Summary)
		}
		return 0

	case "approve", "discard":
		nums, err := wiki.ParseNums(pos)
		if err != nil {
			return fail(err)
		}
		who := *by
		if who == "" {
			who = currentUser()
		}
		var res []wiki.Result
		if cmd == "approve" {
			res, err = w.Approve(nums, who)
		} else {
			res, err = w.Discard(nums, who, *why)
		}
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, res)
		}
		code := 0
		for _, r := range res {
			if r.Error != "" {
				fmt.Fprintf(stdout, "✗ %d: %s\n", r.N, r.Error)
				code = 1
			} else {
				fmt.Fprintf(stdout, "✓ %d %s %s\n", r.N, r.Done, r.Target)
			}
		}
		return code

	case "stale":
		moved, err := w.MoveStale()
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%d drafts moved to _drafts/stale/\n", len(moved))
		return 0

	case "lint":
		fs, err := w.Lint()
		if err != nil {
			return fail(err)
		}
		errs := 0
		for _, f := range fs {
			if f.Level == "error" {
				errs++
			}
		}
		if *asJSON {
			printJSON(stdout, fs)
		} else {
			for _, f := range fs {
				loc := f.File
				if f.Line > 0 {
					loc = fmt.Sprintf("%s:%d", f.File, f.Line)
				}
				fmt.Fprintf(stdout, "%-5s %s [%s] %s\n", f.Level, loc, f.Code, f.Text)
			}
			fmt.Fprintf(stdout, "%d errors, %d warnings\n", errs, len(fs)-errs)
		}
		if errs > 0 {
			return 1
		}
		return 0

	case "raw":
		if pos == "" {
			fmt.Fprintln(stderr, "hpk wiki raw: give a page name, e.g. sessions/2026-11-17.md")
			return 2
		}
		var body []byte
		var err error
		if *file != "" {
			body, err = os.ReadFile(*file)
		} else {
			body, err = io.ReadAll(io.LimitReader(stdin, 1<<20))
		}
		if err != nil {
			return fail(err)
		}
		dst, err := w.WriteRaw(pos, body)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "wrote", dst)
		return 0

	case "log":
		if err := w.Log(pos); err != nil {
			return fail(err)
		}
		return 0

	default:
		fmt.Fprintf(stderr, "hpk wiki: unknown command %q\n\n%s", cmd, wikiUsage)
		return 2
	}
}

func resolveWikiDir(flagDir string) string {
	if flagDir != "" {
		return flagDir
	}
	if env := os.Getenv("WIKI_PATH"); env != "" {
		return env
	}
	return "wiki"
}

// currentUser names the person approving when --by is not given.
func currentUser() string {
	if v := os.Getenv("HPK_USER"); v != "" {
		return v
	}
	if u, err := user.Current(); err == nil {
		name := u.Username
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			name = name[i+1:]
		}
		return name
	}
	return "unknown"
}
