// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/gates"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

// A fictional project in a temporary git repo.
func setup(t *testing.T) (string, *project.Project) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	root := filepath.Join(home, "code", "sample-app")
	os.MkdirAll(root, 0o755)
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	p, err := project.Create(home, "sample-app", root, project.Shared)
	if err != nil {
		t.Fatal(err)
	}
	wiki.Open(p.WikiDir()).Init(p.Name, assets.WikiTemplates())
	return home, p
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s", args, out)
	}
}

func TestPrepSleepsWhenNothingChanged(t *testing.T) {
	home, p := setup(t)
	now := time.Now()
	r1, err := Prep(home, p, "nightly", now)
	if err != nil || !r1.Work || !strings.Contains(r1.Output, "commit ") {
		t.Fatalf("first run sees the history: %+v %v", r1, err)
	}
	if strings.Contains(r1.Output, now.Format("2006-01-02")) {
		t.Fatal("output must not carry timestamps (Hermes compares exact bytes)")
	}
	// While the run is going, the next tick repeats the output: no second model run.
	r2, _ := Prep(home, p, "nightly", now.Add(time.Minute))
	if r2.Work || r2.Output != r1.Output || !strings.HasPrefix(r2.Why, "locked") {
		t.Fatalf("locked tick must repeat: %+v", r2)
	}
	if err := Done(p, "nightly", now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Quiet night: same bytes again, so Hermes keeps the model asleep.
	r3, _ := Prep(home, p, "nightly", now.Add(24*time.Hour))
	if r3.Work || r3.Output != r1.Output {
		t.Fatalf("quiet night must repeat the last output: %+v", r3)
	}
	// New commit: new output, the model runs.
	git(t, p.Root, "commit", "-q", "--allow-empty", "-m", "add export")
	r4, _ := Prep(home, p, "nightly", now.Add(25*time.Hour))
	if !r4.Work || !strings.Contains(r4.Output, "add export") || strings.Contains(r4.Output, " init") {
		t.Fatalf("only new work is listed: %+v", r4)
	}
}

func TestBudgetPausesUntilAllowed(t *testing.T) {
	home, p := setup(t)
	SetHermesID(p, "nightly", "job123")
	os.MkdirAll(filepath.Join(home, "cron"), 0o755)
	write := func(tokens int) {
		rec, _ := json.Marshal(map[string]any{"ts": time.Now().UTC().Format(time.RFC3339Nano), "job_id": "job123", "total_tokens": tokens, "model": "cheap"})
		f, _ := os.OpenFile(filepath.Join(home, "cron", "usage_audit.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		f.Write(append(rec, '\n'))
		f.WriteString(`{"ts":"x","job_id":"someone-elses-job","total_tokens":999999}` + "\n")
		f.Close()
	}
	write(5_000)
	if u, _ := Account(home, p); len(u) != 1 || u[0].Tokens != 5000 {
		t.Fatalf("only this project's jobs are counted: %+v", u)
	}
	if u, _ := Account(home, p); len(u) != 0 {
		t.Fatal("lines are read once")
	}
	write(90_000) // over the 60k nightly budget
	r, _ := Prep(home, p, "nightly", time.Now())
	if r.Work || !strings.HasPrefix(r.Why, "paused") {
		t.Fatalf("over budget must pause: %+v", r)
	}
	Allow(p, "nightly")
	if r, _ := Prep(home, p, "nightly", time.Now()); !r.Work {
		t.Fatalf("allowed job runs again: %+v", r)
	}
	if TokensSince(p, time.Now().Add(-time.Hour)) != 95_000 {
		t.Fatalf("history: %d", TokensSince(p, time.Now().Add(-time.Hour)))
	}
}

func TestDigestOnlyWhenAutoActed(t *testing.T) {
	home, p := setup(t)
	if r, _ := Prep(home, p, "digest", time.Now()); r.Work {
		t.Fatal("no Auto decisions: no digest run")
	}
	gates.Record(p, gates.Decision{At: time.Now().UTC(), Kind: "auto-allow", Tool: "terminal", Detail: "go test ./...", By: "auto"})
	r, _ := Prep(home, p, "digest", time.Now())
	if !r.Work || !strings.Contains(r.Output, "1. terminal go test") {
		t.Fatalf("digest: %+v", r)
	}
}

func TestPlanRespectsPrivacy(t *testing.T) {
	home, p := setup(t)
	j, _ := JobByName(p, "nightly")
	prompt, err := Prompt(p, j, assets.CronPrompts(), "Thai")
	if err != nil || !strings.Contains(prompt, "hpk cron done nightly --project sample-app") {
		t.Fatalf("prompt: %v\n%s", err, prompt)
	}
	pl := MakePlan(home, p, j, prompt, "/usr/local/bin/hpk", "telegram", "")
	if pl.Deliver != "telegram" || !contains(pl.Args, "--monitor-script") || !strings.Contains(pl.ScriptBody, `"cron", "prep", "nightly"`) || !strings.Contains(pl.ScriptBody, "sys.stdout.buffer.write") {
		t.Fatalf("shared plan: %+v", pl)
	}
	project.SetPrivacy(home, p.Name, project.Private, false)
	p2, _ := project.Load(home, p.Name)
	if pl := MakePlan(home, p2, j, prompt, "hpk", "telegram", ""); pl.Deliver != "local" {
		t.Fatalf("private projects deliver locally only: %s", pl.Deliver)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

var _ = fmt.Sprint

// A run stopped mid-way never calls `hpk cron done`; once Hermes logs it, the lock goes.
func TestLoggedRunReleasesLock(t *testing.T) {
	home, p := setup(t)
	SetHermesID(p, "nightly", "job123")
	os.MkdirAll(filepath.Join(home, "cron"), 0o755)
	start := time.Now()
	if r, _ := Prep(home, p, "nightly", start); !r.Work {
		t.Fatalf("first prep should find work: %+v", r)
	}
	if r, _ := Prep(home, p, "nightly", start.Add(time.Minute)); r.Work || !strings.HasPrefix(r.Why, "locked") {
		t.Fatalf("a running job is locked: %+v", r)
	}
	rec, _ := json.Marshal(map[string]any{"ts": start.Add(2 * time.Minute).UTC().Format(time.RFC3339Nano), "job_id": "job123", "total_tokens": 100, "error": "stopped"})
	os.WriteFile(filepath.Join(home, "cron", "usage_audit.jsonl"), append(rec, '\n'), 0o644)
	if r, _ := Prep(home, p, "nightly", start.Add(3*time.Minute)); strings.HasPrefix(r.Why, "locked") {
		t.Fatalf("the logged run must release the lock: %+v", r)
	}
}
