// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

const projectUsage = `hpk project — projects and their privacy

  hpk project new <name> --root DIR [--privacy shared|private|isolated]
  hpk project list [--json]
  hpk project show <name> [--json]
  hpk project privacy <name> <shared|private|isolated> [--yes]

Add an existing folder (docs/DESIGN.md §8):
  hpk project scan <dir> [--json]                  0 tokens: names, sizes, git counts; secrets never opened
  hpk project add <dir> --depth full|light|skip --privacy MODE --yes
                  [--name N] [--exclude GLOB …] [--budget TOKENS] [--price USD_PER_MTOK] [--lang en|th]
                  without --yes, --depth and --privacy it only shows the plan and asks
  hpk project ingest <name> --next | --done PATH [--tokens N] [--notes FILE|-] | --skip PATH | --status
  hpk project step <name> organise|health|done
  hpk project health <name> [--json]               0 tokens: broken links, missing files, repeated rules
  hpk project resume <name>                        where the add-project flow stopped
  hpk project changed <name>                       ingested sources that changed since

Opening a project up (towards shared) needs --yes from a person and never promotes
old knowledge by itself. Every command takes --home DIR (default: Hermes home).
`

func runProject(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(stdout, projectUsage)
		return 0
	}
	cmd, rest := args[0], args[1:]
	var pos []string
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		pos, rest = append(pos, rest[0]), rest[1:]
	}
	fs := flag.NewFlagSet("project "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	home := fs.String("home", "", "Hermes home")
	root := fs.String("root", "", "project source directory (new)")
	mode := fs.String("privacy", "shared", "privacy mode (new)")
	yes := fs.Bool("yes", false, "a person confirms opening the project up (privacy)")
	asJSON := fs.Bool("json", false, "print JSON")
	name := fs.String("name", "", "project name (add)")
	depth := fs.String("depth", "", "full | light | skip (add)")
	budget := fs.Int("budget", 0, "ingest token budget (add)")
	price := fs.Float64("price", 0, "USD per 1M input tokens, for the estimate only (add)")
	lang := fs.String("lang", "en", "language for talking to the person (add)")
	next := fs.Bool("next", false, "show the next source to read (ingest)")
	done := fs.String("done", "", "mark a source as read (ingest)")
	skip := fs.String("skip", "", "skip a source (ingest)")
	tokens := fs.Int("tokens", 0, "tokens a source really cost (ingest --done)")
	notes := fs.String("notes", "", "file with the facts found (ingest --done); - reads stdin. hpk writes them to the wiki")
	status := fs.Bool("status", false, "show the queue (ingest)")
	var exclude multi
	fs.Var(&exclude, "exclude", "glob of sources to leave out (add, repeatable)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	h := *home
	if h == "" {
		h = project.Home()
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "hpk project "+cmd+":", err); return 1 }
	need := func(n int, what string) bool {
		if len(pos) < n {
			fmt.Fprintf(stderr, "hpk project %s: missing %s\n", cmd, what)
			return false
		}
		return true
	}

	switch cmd {
	case "new":
		if !need(1, "name") {
			return 2
		}
		r := *root
		if r != "" {
			if abs, err := filepath.Abs(r); err == nil {
				r = abs
			}
		}
		p, err := project.Create(h, pos[0], r, *mode)
		if err != nil {
			return fail(err)
		}
		if _, err := wiki.Open(p.WikiDir()).Init(p.Name, assets.WikiTemplates()); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "project %s created (%s) at %s\n", p.Name, p.Mode(), p.Dir)
		return 0
	case "list":
		list, err := project.List(h)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, list)
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "no projects yet: hpk project new <name> --root DIR")
		}
		for _, p := range list {
			fmt.Fprintf(stdout, "%-24s %-9s %s\n", p.Name, p.Mode(), p.Root)
		}
		return 0
	case "show":
		if !need(1, "name") {
			return 2
		}
		p, err := project.Load(h, pos[0])
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(stdout, map[string]any{"project": p, "wiki": p.WikiDir(), "origin": privacy.Origin(p)})
		}
		fmt.Fprintf(stdout, "%s\n  privacy: %s\n  root:    %s\n  wiki:    %s\n", p.Name, p.Mode(), p.Root, p.WikiDir())
		return 0
	case "privacy":
		if !need(2, "name and mode") {
			return 2
		}
		p, err := project.SetPrivacy(h, pos[0], pos[1], *yes)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%s is now %s\n", p.Name, p.Mode())
		return 0
	case "scan", "add", "ingest", "step", "health", "resume", "changed":
		return runOnboard(cmd, pos, onboardFlags{home: h, name: *name, depth: *depth, privacy: *mode, privacySet: flagSet(fs, "privacy"),
			budget: *budget, price: *price, lang: *lang, exclude: exclude, yes: *yes, asJSON: *asJSON,
			next: *next, done: *done, skip: *skip, tokens: *tokens, notes: *notes, status: *status}, os.Stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "hpk project: unknown command %q\n\n%s", cmd, projectUsage)
		return 2
	}
}

// promote is `hpk wiki promote <page> --project NAME [--to team|users/<id>]`.
func promote(home, name, page, to string, stdout, stderr io.Writer) int {
	p, err := project.Load(home, name)
	if err != nil {
		fmt.Fprintln(stderr, "hpk wiki promote:", err)
		return 1
	}
	d, err := privacy.Promote(home, p, page, to, assets.WikiTemplates())
	if err != nil {
		fmt.Fprintln(stderr, "hpk wiki promote:", err)
		return 1
	}
	where := filepath.Join(project.SharedDir(home), filepath.FromSlash(orTeam(to)))
	fmt.Fprintf(stdout, "draft %d in the shared wiki (%s) → %s\nreview it, then a person approves: hpk wiki approve %d --wiki %s\n", d.N, where, d.Meta.Target, d.N, where)
	return 0
}

func orTeam(to string) string {
	if to == "" {
		return "team"
	}
	return to
}
