// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/onboard"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/scan"
)

type onboardFlags struct {
	home, name, depth, privacy, lang string
	privacySet                       bool
	budget                           int
	price                            float64
	exclude                          []string
	yes, asJSON                      bool
	next, status                     bool
	done, skip                       string
	tokens                           int
	notes                            string // --notes: facts to write to the raw page (- = stdin)
}

// exit codes beyond 0/1/2
const (
	exitNeedsConsent = 3 // the person must agree before anything is read
	exitBudget       = 4 // the ingest budget is used up; ask the person
)

func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

func runOnboard(cmd string, pos []string, f onboardFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	onboard.SetTemplates(assets.WikiTemplates())
	fail := func(err error) int { fmt.Fprintln(stderr, "hpk project "+cmd+":", err); return 1 }
	arg := func(what string) (string, bool) {
		if len(pos) == 0 {
			fmt.Fprintf(stderr, "hpk project %s: missing %s\n", cmd, what)
			return "", false
		}
		return pos[0], true
	}
	load := func() (*project.Project, int) {
		n, ok := arg("project name")
		if !ok {
			return nil, 2
		}
		p, err := project.Load(f.home, n)
		if err != nil {
			return nil, fail(err)
		}
		return p, 0
	}

	switch cmd {
	case "scan":
		dir, ok := arg("folder")
		if !ok {
			return 2
		}
		r, err := scan.Run(dir)
		if err != nil {
			return fail(err)
		}
		if f.asJSON {
			return printJSON(stdout, r)
		}
		printScan(stdout, r)
		return 0

	case "add":
		dir, ok := arg("folder")
		if !ok {
			return 2
		}
		r, err := scan.Run(dir)
		if err != nil {
			return fail(err)
		}
		if !f.yes || f.depth == "" || !f.privacySet {
			// Step 1: ask first. Only names and sizes were looked at.
			est, _ := onboard.Plan(r, orStr(f.depth, onboard.Full), f.exclude, f.budget, f.price, false)
			if f.asJSON {
				printJSON(stdout, map[string]any{"needs_consent": true, "scan": r, "estimate_full": est})
				return exitNeedsConsent
			}
			printConsent(stdout, r, f)
			return exitNeedsConsent
		}
		name := f.name
		if name == "" {
			name = strings.ToLower(filepath.Base(r.Root))
		}
		p, est, err := onboard.Add(f.home, onboard.Options{Root: r.Root, Name: name, Depth: f.depth, Privacy: f.privacy,
			Language: f.lang, Exclude: f.exclude, Budget: f.budget, Price: f.price}, r)
		if err != nil {
			if est.OverBy > 0 {
				printEstimate(stdout, est)
			}
			return fail(err)
		}
		if f.asJSON {
			return printJSON(stdout, map[string]any{"project": p, "estimate": est})
		}
		fmt.Fprintf(stdout, "project %s added (%s, %s) — wiki at %s\n", p.Name, p.Mode(), f.depth, p.WikiDir())
		printEstimate(stdout, est)
		fmt.Fprintf(stdout, "\nNext: Hermes reads the queue with the hpk-project skill (hpk project ingest %s --next).\n", p.Name)
		return 0

	case "ingest":
		p, code := load()
		if p == nil {
			return code
		}
		switch {
		case f.done != "" || f.skip != "":
			src := f.done
			if f.skip != "" {
				src = f.skip
			}
			if f.notes != "" && f.done != "" {
				var raw []byte
				var err error
				if f.notes == "-" {
					raw, err = io.ReadAll(io.LimitReader(stdin, onboard.MaxNotes+1))
				} else {
					raw, err = os.ReadFile(f.notes)
				}
				if err != nil {
					return fail(err)
				}
				dst, err := onboard.WriteRaw(p, src, raw)
				if err != nil {
					return fail(err)
				}
				fmt.Fprintln(stdout, "wrote", dst)
			}
			st, err := onboard.Done(p, src, f.tokens, f.skip != "")
			if err != nil {
				return fail(err)
			}
			fmt.Fprintf(stdout, "%s marked %s · used %d of %d tokens · step: %s\n", src, map[bool]string{true: "skipped", false: "done"}[f.skip != ""], st.UsedTokens, st.Budget, st.Step)
			return 0
		case f.next:
			it, st, err := onboard.Next(p)
			if errors.Is(err, onboard.ErrBudget) {
				fmt.Fprintf(stderr, "stop: %v (next: %s, about %d tokens; used %d of %d)\n", err, it.Path, it.Tokens, st.UsedTokens, st.Budget)
				return exitBudget
			}
			if err != nil {
				return fail(err)
			}
			if it == nil {
				if f.asJSON {
					return printJSON(stdout, map[string]any{"next": nil, "step": st.Step})
				}
				fmt.Fprintf(stdout, "the queue is empty · step: %s\n", st.Step)
				return 0
			}
			abs := filepath.Join(p.Root, filepath.FromSlash(it.Path))
			if f.asJSON {
				return printJSON(stdout, map[string]any{"next": it, "file": abs, "raw": filepath.Join(p.WikiDir(), filepath.FromSlash(it.Raw)), "used": st.UsedTokens, "budget": st.Budget})
			}
			fmt.Fprintf(stdout, "read:  %s (%s, about %d tokens)\nwrite: %s\n", abs, it.Kind, it.Tokens, filepath.Join(p.WikiDir(), filepath.FromSlash(it.Raw)))
			return 0
		default:
			st, err := onboard.LoadState(p)
			if err != nil {
				return fail(err)
			}
			if f.asJSON {
				return printJSON(stdout, st)
			}
			for _, it := range st.Queue {
				fmt.Fprintf(stdout, "%-8s %6d  %s\n", it.Status, it.Tokens, it.Path)
			}
			fmt.Fprintf(stdout, "used %d of %d tokens · step: %s\n", st.UsedTokens, st.Budget, st.Step)
			return 0
		}

	case "step":
		p, code := load()
		if p == nil {
			return code
		}
		if len(pos) < 2 {
			fmt.Fprintln(stderr, "hpk project step: give organise, health or done")
			return 2
		}
		st, err := onboard.Advance(p, pos[1])
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%s · step: %s\n", p.Name, st.Step)
		return 0

	case "health":
		p, code := load()
		if p == nil {
			return code
		}
		r, err := scan.Run(p.Root)
		if err != nil {
			return fail(err)
		}
		fs := onboard.Health(r)
		if f.asJSON {
			return printJSON(stdout, fs)
		}
		if len(fs) == 0 {
			fmt.Fprintln(stdout, "no problems found by the 0-token checks")
		}
		for _, x := range fs {
			loc := x.File
			if x.Line > 0 {
				loc = fmt.Sprintf("%s:%d", x.File, x.Line)
			}
			fmt.Fprintf(stdout, "%-5s %-15s %s %s\n", x.ID, x.Kind, loc, x.Detail)
		}
		return 0

	case "resume":
		p, code := load()
		if p == nil {
			return code
		}
		st, err := onboard.LoadState(p)
		if err != nil {
			return fail(err)
		}
		counts := map[string]int{}
		for _, it := range st.Queue {
			counts[it.Status]++
		}
		hint := map[string]string{
			"ingest":   "Hermes continues the queue: hpk project ingest " + p.Name + " --next",
			"organise": "Hermes drafts architecture, glossary and decisions pages from raw/ingest (hpk wiki draft), then: hpk project step " + p.Name + " health",
			"health":   "run hpk project health " + p.Name + " and let the person pick what to fix, then: hpk project step " + p.Name + " done",
			"done":     "nothing left; cron keeps it fresh (hpk project changed " + p.Name + ")",
		}[st.Step]
		if f.asJSON {
			return printJSON(stdout, map[string]any{"step": st.Step, "counts": counts, "used": st.UsedTokens, "budget": st.Budget, "next": hint})
		}
		fmt.Fprintf(stdout, "%s · step %s · %d done, %d pending, %d skipped · %d of %d tokens\nnext: %s\n",
			p.Name, st.Step, counts["done"], counts["pending"], counts["skipped"], st.UsedTokens, st.Budget, hint)
		return 0

	case "changed":
		p, code := load()
		if p == nil {
			return code
		}
		ch, err := onboard.Changed(p)
		if err != nil {
			return fail(err)
		}
		if f.asJSON {
			return printJSON(stdout, ch)
		}
		if len(ch) == 0 {
			fmt.Fprintln(stdout, "nothing changed since the last ingest")
		}
		for _, c := range ch {
			fmt.Fprintln(stdout, c)
		}
		return 0
	}
	return 2
}

func printScan(w io.Writer, r scan.Result) {
	fmt.Fprintf(w, "%s\n  %d files · %s\n", r.Root, r.Files, langs(r.Languages))
	if r.Git.IsRepo {
		fmt.Fprintf(w, "  git: %d commits by %d contributors, %d branches, last %s\n", r.Git.Commits, r.Git.Contributors, r.Git.Branches, r.Git.LastCommit)
	} else {
		fmt.Fprintln(w, "  not a git repository")
	}
	if r.TestCmd != "" {
		fmt.Fprintf(w, "  tests: %s\n", r.TestCmd)
	}
	fmt.Fprintf(w, "  knowledge sources: %d (light: %d)\n", len(r.Sources), countLight(r.Sources))
	for _, s := range r.Sources {
		fmt.Fprintf(w, "    %-9s %s\n", s.Kind, s.Path)
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(w, "  never opened (secrets): %d\n", len(r.Skipped))
		for _, s := range r.Skipped {
			fmt.Fprintf(w, "    %s\n", s.Path)
		}
	}
}

func printConsent(w io.Writer, r scan.Result, f onboardFlags) {
	full, _ := onboard.Plan(r, onboard.Full, f.exclude, f.budget, f.price, false)
	light, _ := onboard.Plan(r, onboard.Light, f.exclude, f.budget, f.price, false)
	fmt.Fprintf(w, "Add %s to hpk?\n\nSo far hpk only listed file names and sizes. Nothing was read.\n\n", r.Root)
	fmt.Fprintf(w, "Choose how much to read into the wiki:\n")
	fmt.Fprintf(w, "  --depth full   %d sources, about %d tokens, about $%.2f\n", len(full.Sources), full.Tokens, full.CostUSD)
	fmt.Fprintf(w, "  --depth light  %d sources (README + docs), about %d tokens, about $%.2f\n", len(light.Sources), light.Tokens, light.CostUSD)
	fmt.Fprintf(w, "  --depth skip   set up the project and wiki, read nothing\n\n")
	fmt.Fprintf(w, "And who may see what it learns (--privacy):\n  shared    lessons may be promoted to the shared wiki (a person approves each)\n  private   nothing goes to the shared wiki; nothing leaves this machine\n  isolated  private, and it does not read the shared wiki either\n\n")
	if len(r.Skipped) > 0 {
		fmt.Fprintf(w, "Never opened, whatever you choose: %d secret files (.env, keys, credentials).\n", len(r.Skipped))
	}
	fmt.Fprintf(w, "Agent instruction files (AGENTS.md, CLAUDE.md …) are checked for problems but never copied into the wiki.\n")
	fmt.Fprintf(w, "Cost is an estimate for a cheap model (input + about 15%% output); the budget is %d tokens.\n\n", full.Budget)
	fmt.Fprintf(w, "To go ahead: hpk project add %s --depth light --privacy private --yes\n", r.Root)
}

func printEstimate(w io.Writer, e onboard.Estimate) {
	fmt.Fprintf(w, "%d sources queued · about %d tokens · about $%.2f · budget %d\n", len(e.Sources), e.Tokens, e.CostUSD, e.Budget)
	if len(e.Excluded) > 0 {
		fmt.Fprintf(w, "left out: %s\n", strings.Join(e.Excluded, ", "))
	}
}

func langs(m map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range m {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	var parts []string
	for i, x := range list {
		if i == 5 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s %d", x.k, x.v))
	}
	return strings.Join(parts, ", ")
}

func countLight(s []scan.Source) int {
	n := 0
	for _, x := range s {
		if x.Light {
			n++
		}
	}
	return n
}

func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
