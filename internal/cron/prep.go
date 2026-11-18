// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package cron

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hpk.local/hpk/internal/board"
	"hpk.local/hpk/internal/gates"
	"hpk.local/hpk/internal/onboard"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

// Lock: a run that has not called `hpk cron done` within this time is considered dead.
const lockTimeout = 3 * time.Hour

// PrepResult says what the monitor script prints and why.
type PrepResult struct {
	Output string `json:"output"` // exactly what Hermes compares with the last tick
	Work   bool   `json:"work"`   // true: new work, the model will run
	Why    string `json:"why"`    // for people: work | nothing new | paused | locked
}

// Prep does the 0-token part of a job and returns the monitor output. To keep the
// model asleep it repeats the previous output byte for byte when there is nothing
// to do, when the job is paused for budget, or while a run is still going.
// Output never contains timestamps (Hermes compares exact bytes).
func Prep(home string, p *project.Project, name string, now time.Time) (PrepResult, error) {
	if _, err := JobByName(p, name); err != nil {
		return PrepResult{}, err
	}
	if _, err := Account(home, p); err != nil {
		return PrepResult{}, err
	}
	st, err := Load(p)
	if err != nil {
		return PrepResult{}, err
	}
	js := st.job(name)
	same := func(why string) (PrepResult, error) {
		return PrepResult{Output: js.LastOutput, Work: false, Why: why}, Save(p, st)
	}
	if js.Paused {
		return same("paused: " + js.PauseReason + " (hpk cron allow " + name + ")")
	}
	if !js.LockedAt.IsZero() && now.Sub(js.LockedAt) < lockTimeout {
		return same("locked: the previous run has not finished")
	}

	var items []string
	switch name {
	case "nightly":
		items = nightlyWork(p, js, now)
	case "weekly":
		items = weeklyWork(p, js, now)
	case "digest":
		items = digestWork(p, js, now)
	}
	if len(items) == 0 {
		return same("nothing new")
	}
	// ASCII on purpose: the bytes travel through Hermes' Python on every OS.
	// Only the work goes in: Hermes wakes the model when this text changes, so a
	// budget edit alone must not look like new work (the prompt carries the budget).
	out := fmt.Sprintf("hpk %s - project %s\n%s\n", name, p.Name, strings.Join(items, "\n"))
	if out == js.LastOutput {
		return same("nothing new")
	}
	js.LastOutput, js.LockedAt = out, now.UTC()
	return PrepResult{Output: out, Work: true, Why: "work"}, Save(p, st)
}

// Done ends a run: releases the lock and moves the watermark forward.
func Done(p *project.Project, name string, now time.Time) error {
	st, err := Load(p)
	if err != nil {
		return err
	}
	js := st.job(name)
	js.LockedAt = time.Time{}
	js.Watermark = now.UTC()
	if head, err := gitOut(p.Root, "rev-parse", "HEAD"); err == nil {
		js.LastCommit = head
	}
	if err := Save(p, st); err != nil {
		return err
	}
	if w := wiki.Open(p.WikiDir()); w.Exists() {
		used := 0
		for _, u := range History(p) {
			if u.Job == name && !u.At.Before(now.Add(-24*time.Hour)) {
				used += u.Tokens
			}
		}
		_ = w.Log(fmt.Sprintf("cron %s done (%d tokens recorded in the last 24 h)", name, used))
	}
	return nil
}

// nightlyWork: commits since the last run, changed knowledge sources, new session
// reports, plus 0-token board upkeep (stale claims, worktree overlaps).
func nightlyWork(p *project.Project, js *JobState, now time.Time) []string {
	var items []string
	if js.LastCommit != "" {
		if out, err := gitOut(p.Root, "log", "--format=%h %s", js.LastCommit+"..HEAD"); err == nil && out != "" {
			for _, l := range strings.Split(out, "\n") {
				items = append(items, "commit "+l)
			}
		}
	} else if out, err := gitOut(p.Root, "log", "-20", "--format=%h %s"); err == nil && out != "" {
		for _, l := range strings.Split(out, "\n") {
			items = append(items, "commit "+l)
		}
	}
	if ch, err := onboard.Changed(p); err == nil {
		for _, c := range ch {
			items = append(items, "changed "+c)
		}
	}
	reports, _ := filepath.Glob(filepath.Join(p.WikiDir(), "raw", "sessions", "*.md"))
	sort.Strings(reports)
	for _, r := range reports {
		if fi, err := os.Stat(r); err == nil && fi.ModTime().After(js.Watermark) {
			items = append(items, "report raw/sessions/"+filepath.Base(r))
		}
	}
	if db, err := board.Open(p); err == nil {
		db.MarkStale(30 * time.Minute)
		if branch, err := gitOut(p.Root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			if clashes, err := db.DiffWorktrees(p.Root, branch); err == nil {
				for _, c := range clashes {
					items = append(items, fmt.Sprintf("overlap %s in %s", c.File, strings.Join(c.Worktrees, ", ")))
				}
			}
		}
		db.Close()
	}
	return items
}

// weeklyWork: lint findings and stale drafts (moved aside first, 0 tokens).
func weeklyWork(p *project.Project, js *JobState, now time.Time) []string {
	w := wiki.Open(p.WikiDir())
	if !w.Exists() {
		return nil
	}
	var items []string
	if moved, _ := w.MoveStale(); len(moved) > 0 {
		items = append(items, fmt.Sprintf("stale drafts moved aside: %v", moved))
	}
	findings, _ := w.Lint()
	for _, f := range findings {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		items = append(items, fmt.Sprintf("lint %s %s %s", f.Code, loc, f.Text))
	}
	// Only re-run on a week that has findings or new pages since last time.
	if len(items) == 0 {
		return nil
	}
	return items
}

// digestWork: what Auto let through since the last digest; nothing if Auto was off.
func digestWork(p *project.Project, js *JobState, now time.Time) []string {
	since := js.Watermark
	if since.IsZero() || now.Sub(since) > 24*time.Hour {
		since = now.Add(-24 * time.Hour)
	}
	list, err := gates.Digest(p, since)
	if err != nil {
		return nil
	}
	var items []string
	for i, d := range list {
		items = append(items, fmt.Sprintf("%d. %s %s %s", i+1, d.Tool, d.Detail, strings.Join(d.Lines, ",")))
	}
	return items
}

func gitOut(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}
