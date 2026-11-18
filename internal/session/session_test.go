// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

// Fictional sample project in a temporary git repository.
func setup(t *testing.T, template string) *project.Project {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, "code", "sample-app")
	os.MkdirAll(root, 0o755)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	p, err := project.Create(home, "sample-app", root, project.Shared)
	if err != nil {
		t.Fatal(err)
	}
	if template != "" {
		f, _ := os.OpenFile(filepath.Join(p.Dir, "project.yaml"), os.O_APPEND|os.O_WRONLY, 0o644)
		f.WriteString("session:\n  title_template: \"" + template + "\"\n  ask_on_start: [version]\n  max_children: 2\n")
		f.Close()
	}
	wiki.Open(p.WikiDir()).Init(p.Name, assets.WikiTemplates())
	return p
}

func TestTreeTitlesWorktreesAndLimit(t *testing.T) {
	p := setup(t, "Version {version} {name}")
	if _, err := New(p, NewOptions{Name: "release"}); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("missing template value must be asked for: %v", err)
	}
	main, err := New(p, NewOptions{Name: "release", Vars: map[string]string{"version": "1.4.0"}})
	if err != nil || main.Num != "1" || main.Title != "Version 1.4.0 release" || main.Worktree != "" {
		t.Fatalf("main: %+v %v", main, err)
	}
	sub, err := New(p, NewOptions{Parent: "1", Name: "auth › api-tests", Worktree: true, Task: "Write contract tests."})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Num != "1.1" || sub.Title != "Version 1.4.0 release › auth › api-tests" || sub.Branch != "hpk/1-1-auth-api-tests" {
		t.Fatalf("sub: %+v", sub)
	}
	if fi, err := os.Stat(sub.Worktree); err != nil || !fi.IsDir() {
		t.Fatalf("worktree not created: %v", err)
	}
	brief, _ := os.ReadFile(sub.Brief)
	for _, want := range []string{"Write contract tests.", "Red lines", "hpk session report 1.1", "Question:"} {
		if !strings.Contains(string(brief), want) {
			t.Errorf("brief missing %q", want)
		}
	}
	New(p, NewOptions{Parent: "1", Name: "billing"})
	if _, err := New(p, NewOptions{Parent: "1", Name: "third"}); err == nil {
		t.Fatal("max_children (2) must stop a third running sub-task")
	}
	SetStatus(p, "1.1", Done)
	if s, err := New(p, NewOptions{Parent: "1", Name: "third"}); err != nil || s.Num != "1.3" {
		t.Fatalf("after one finished a new sub-task fits: %+v %v", s, err)
	}
	list, _ := List(p)
	var nums []string
	for _, s := range list {
		nums = append(nums, s.Num)
	}
	if strings.Join(nums, ",") != "1,1.1,1.2,1.3" {
		t.Fatalf("lineage order: %v", nums)
	}
}

func TestReportFormat(t *testing.T) {
	ok := "Done: wrote 38 contract tests\nDecided: keep fixtures\nLessons: clock skew matters\nOpen: none\nQuestion: none"
	if r, err := ParseReport(ok); err != nil || r.Done == "" || r.Question != "" {
		t.Fatalf("valid report: %+v %v", r, err)
	}
	if r, err := ParseReport("ทำแล้ว: เขียนเทสต์\nคำถาม: เก็บ fixture เดิมไหม"); err != nil || r.Question == "" {
		t.Fatalf("Thai labels: %+v %v", r, err)
	}
	bad := map[string]string{
		"no done":   "Decided: x",
		"unlabeled": "just some text",
		"too long":  "Done: x\n" + strings.Repeat("more\n", 15),
		"secret":    "Done: set api_key = abcdefgh1234",
	}
	for name, txt := range bad {
		if _, err := ParseReport(txt); err == nil {
			t.Errorf("%s: want refusal", name)
		}
	}
}

func TestReportGoesToMainChatAndWiki(t *testing.T) {
	p := setup(t, "")
	New(p, NewOptions{Name: "release"})
	sub, _ := New(p, NewOptions{Parent: "1", Name: "docs", Worktree: true})
	s, _, err := File(p, sub.Num, "Done: wrote the guide\nQuestion: keep the old screenshots?", false)
	if err != nil || s.Status != Waiting || !s.Reported {
		t.Fatalf("question report: %+v %v", s, err)
	}
	s, _, err = File(p, sub.Num, "Done: replaced screenshots\nQuestion: none", true)
	if err != nil || s.Status != Done {
		t.Fatalf("final report: %+v %v", s, err)
	}
	items, _ := Inbox(p, "1")
	if len(items) != 2 || items[0].Kind != "question" || !items[0].Pending || items[1].Kind != "summary" {
		t.Fatalf("inbox: %+v", items)
	}
	raws, _ := filepath.Glob(filepath.Join(p.WikiDir(), "raw", "sessions", "1-1-*.md"))
	if len(raws) != 2 {
		t.Fatalf("raw session notes: %v", raws)
	}
}

func TestAttachRenameAndAutoTitle(t *testing.T) {
	p := setup(t, "")
	New(p, NewOptions{Name: "release"})
	Attach(p, "1", "hermes-abc")
	s, err := Get(p, "hermes-abc")
	if err != nil || s.Status != Running {
		t.Fatalf("lookup by Hermes id: %+v %v", s, err)
	}
	var synced string
	Rename(p, "1", "v1.4 release", func(id, title string) error { synced = id + "=" + title; return nil })
	if synced != "hermes-abc=v1.4 release" {
		t.Fatalf("rename must sync to Hermes: %q", synced)
	}
	s, _ = Get(p, "1")
	if _, change := AutoTitle(p, s, "Untitled chat about tests"); change {
		t.Fatal("a title set by hpk or the person is never overwritten by auto-naming")
	}
}

func TestRecordSwitch(t *testing.T) {
	p := setup(t, "")
	New(p, NewOptions{Name: "release"})
	if _, err := RecordSwitch(p, "1", Switch{From: "anthropic:sonnet", To: "anthropic:opus", Way: "continue", Fill: 0.3}); err != nil {
		t.Fatal(err)
	}
	RecordSwitch(p, "1", Switch{From: "anthropic:opus", To: "anthropic:sonnet", Way: "compact"})
	s, _ := Get(p, "1")
	if s.Model != "anthropic:sonnet" || len(s.Switches) != 2 || s.Switches[0].To != "anthropic:opus" || s.Switches[0].At.IsZero() {
		t.Fatalf("switches: %+v", s)
	}
	if _, err := RecordSwitch(p, "1", Switch{}); err == nil {
		t.Fatal("a switch without a model is refused")
	}
}
