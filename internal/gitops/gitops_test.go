// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hpk.local/hpk/internal/project"
)

// A fictional repo: main with two commits, a feature branch, a bare "origin".
func setup(t *testing.T) (*project.Project, func(args ...string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	root := filepath.Join(home, "code", "harbor-api")
	bare := filepath.Join(home, "remote.git")
	os.MkdirAll(root, 0o755)
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	exec.Command("git", "init", "-q", "--bare", bare).Run()
	run(root, "init", "-q", "-b", "main")
	run(root, "config", "user.name", "t")
	run(root, "config", "user.email", "t@example.com")
	write := func(f, s string) { os.WriteFile(filepath.Join(root, f), []byte(s), 0o644) }
	write("routes.ts", "a\nb\nc\n")
	run(root, "add", ".")
	run(root, "commit", "-qm", "Start")
	run(root, "remote", "add", "origin", bare)
	run(root, "push", "-q", "-u", "origin", "main")
	run(root, "switch", "-q", "-c", "feat")
	write("export.ts", "export\n")
	run(root, "add", ".")
	run(root, "commit", "-qm", "Add export")
	write("export.ts", "export v2\n")
	run(root, "commit", "-qam", "wip")
	run(root, "switch", "-q", "main")
	write("routes.ts", "a\nB\nc\n")
	run(root, "commit", "-qam", "Fix routes")
	p, err := project.Create(home, "harbor-api", root, project.Shared)
	if err != nil {
		t.Fatal(err)
	}
	return p, func(args ...string) string { return run(root, args...) }
}

func do(t *testing.T, p *project.Project, r Request) *Result {
	t.Helper()
	res, err := Do(context.Background(), p, r, "tester")
	if err != nil {
		t.Fatalf("%s: %v (ran %v)", r.Action, err, res.Commands)
	}
	return res
}

func tip(run func(...string) string, b string) string { return run("rev-parse", "--short=7", b) }

func TestLoadSeesHistoryBranchesAndBase(t *testing.T) {
	p, _ := setup(t)
	m, err := Load(context.Background(), p, "tester")
	if err != nil || !m.Live {
		t.Fatalf("load: %v %+v", err, m)
	}
	if m.Head != "main" || len(m.Commits) != 4 || len(m.Branches) != 2 {
		t.Fatalf("model: head %s, %d commits, %d branches", m.Head, len(m.Commits), len(m.Branches))
	}
	var base, feat *Branch
	for _, b := range m.Branches {
		if b.Name == "main" {
			base = b
		} else {
			feat = b
		}
	}
	if !base.Base || base.Remote == "" || feat.Remote != "" {
		t.Fatalf("branches: %+v %+v", base, feat)
	}
	if len(m.Commits[0].Files) == 0 {
		t.Fatal("file stats are read")
	}
}

func TestRebaseInteractiveAndUndo(t *testing.T) {
	p, run := setup(t)
	before := tip(run, "feat")
	do(t, p, Request{Action: "rebase", Branch: "feat", Onto: "main"})
	if got := run("merge-base", "feat", "main"); !strings.HasPrefix(run("rev-parse", "main"), got[:7]) {
		t.Fatal("feat is on top of main after rebase")
	}
	if run("symbolic-ref", "--short", "HEAD") != "main" {
		t.Fatal("the person's checkout stays on main (the rebase ran in a throw-away worktree)")
	}
	// squash "wip" into the commit above, reword it
	log := strings.Fields(run("log", "--format=%h", "main..feat"))
	m, _ := Load(context.Background(), p, "tester")
	_ = m
	do(t, p, Request{Action: "interactive", Branch: "feat", Onto: "main", Steps: []Step{
		{SHA: log[1], Action: "reword", Msg: "Export bookings"},
		{SHA: log[0], Action: "fixup"},
	}})
	if n := run("rev-list", "--count", "main..feat"); n != "1" {
		t.Fatalf("one commit after squash, got %s", n)
	}
	if msg := run("log", "-1", "--format=%s", "feat"); msg != "Export bookings" {
		t.Fatalf("reworded: %q", msg)
	}
	if out := run("show", "feat:export.ts"); out != "export v2" {
		t.Fatalf("the fixup's change is kept: %q", out)
	}
	if m, _ := Load(context.Background(), p, "tester"); len(m.Commits) != 3 {
		t.Fatalf("rewritten commits (kept only by undo points) are not drawn: %d commits", len(m.Commits))
	}
	// undo twice: back to before the rebase
	undo := loadUndo(p)
	if len(undo) != 2 {
		t.Fatalf("an undo point per rewrite: %d", len(undo))
	}
	do(t, p, Request{Action: "undo", Undo: undo[1].ID})
	if tip(run, "feat") != before {
		t.Fatal("undo restores the branch")
	}
}

func TestConflictResolveContinue(t *testing.T) {
	p, run := setup(t)
	run("switch", "-q", "feat")
	os.WriteFile(filepath.Join(p.Root, "routes.ts"), []byte("a\nX\nc\n"), 0o644)
	run("commit", "-qam", "Change routes too")
	run("switch", "-q", "main")
	res := do(t, p, Request{Action: "rebase", Branch: "feat", Onto: "main"})
	if !res.Conflict {
		t.Fatalf("expected a conflict: %+v", res)
	}
	m, _ := Load(context.Background(), p, "tester")
	if m.InProgress == nil || m.InProgress.Kind != "rebase" || m.InProgress.Files[0].Path != "routes.ts" {
		t.Fatalf("conflict is visible: %+v", m.InProgress)
	}
	if !strings.Contains(m.InProgress.Files[0].Ours, "B") || !strings.Contains(m.InProgress.Files[0].Theirs, "X") {
		t.Fatalf("both sides: %+v", m.InProgress.Files[0])
	}
	if _, err := Do(context.Background(), p, Request{Action: "continue"}, "t"); err == nil {
		t.Fatal("continue needs every file resolved")
	}
	do(t, p, Request{Action: "resolve", File: "routes.ts", Side: "theirs"})
	do(t, p, Request{Action: "continue"})
	if m, _ := Load(context.Background(), p, "t"); m.InProgress != nil {
		t.Fatal("finished")
	}
	if out := run("show", "feat:routes.ts"); !strings.Contains(out, "X") {
		t.Fatalf("resolved with the incoming side: %q", out)
	}
}

func TestSafety(t *testing.T) {
	p, run := setup(t)
	os.WriteFile(filepath.Join(p.Root, "routes.ts"), []byte("dirty\n"), 0o644)
	if _, err := Do(context.Background(), p, Request{Action: "checkout", Branch: "feat"}, "t"); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("a dirty checkout is not touched: %v", err)
	}
	run("checkout", "--", "routes.ts")
	if _, err := Do(context.Background(), p, Request{Action: "branch", Name: "bad name"}, "t"); err == nil {
		t.Fatal("invalid branch names are refused")
	}
	if _, err := Do(context.Background(), p, Request{Action: "cherry", SHA: "; rm -rf /"}, "t"); err == nil {
		t.Fatal("commit ids are checked")
	}
	do(t, p, Request{Action: "delete", Branch: "feat"})
	undo := loadUndo(p)
	do(t, p, Request{Action: "undo", Undo: undo[0].ID})
	if tip(run, "feat") == "" {
		t.Fatal("a deleted branch comes back with undo")
	}
	// push to the bare remote, then a rebase force-pushes with lease
	do(t, p, Request{Action: "push", Branch: "feat"})
	m, _ := Load(context.Background(), p, "t")
	if RedLine(Request{Action: "rebase", Branch: "feat"}, m) != "push" {
		t.Fatal("rebasing a pushed branch is a red line")
	}
	res := do(t, p, Request{Action: "rebase", Branch: "feat", Onto: "main"})
	if !strings.Contains(strings.Join(res.Commands, ";"), "--force-with-lease") || run("rev-parse", "origin/feat") != run("rev-parse", "feat") {
		t.Fatalf("pushed with lease: %v", res.Commands)
	}
}

func TestOptionLikeNamesAreRefused(t *testing.T) {
	p, _ := setup(t)
	for _, r := range []Request{
		{Action: "push", Branch: "--receive-pack=calc"},
		{Action: "rebase", Branch: "feat", Onto: "--exec=calc"},
		{Action: "merge", From: "--strategy=evil", Into: "main"},
		{Action: "pr", Branch: "feat", Into: "--base=x"},
		{Action: "branch", Name: "-x"},
	} {
		if _, err := Do(context.Background(), p, r, "t"); err == nil || !strings.Contains(err.Error(), "not a branch name") {
			t.Errorf("%s %+v: %v", r.Action, r, err)
		}
	}
}
