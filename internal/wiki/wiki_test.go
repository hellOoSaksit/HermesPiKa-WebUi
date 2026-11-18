// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package wiki

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hpk.local/hpk/assets"
)

// Sample data is fictional (PRODUCT.md: no real projects, people or paths).
func newWiki(t *testing.T) *Wiki {
	t.Helper()
	w := Open(filepath.Join(t.TempDir(), "wiki"))
	clock := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	w.Now = func() time.Time { return clock }
	if _, err := w.Init("sample-app", assets.WikiTemplates()); err != nil {
		t.Fatal(err)
	}
	return w
}

func draft(t *testing.T, w *Wiki, target, title string) Draft {
	t.Helper()
	d, err := w.CreateDraft(DraftInput{Target: target, Title: title, Sources: []string{"src/export.go:42"},
		Summary: "why CSV uses UTF-8 with BOM", Author: "s-01", Body: "Spreadsheet apps read UTF-8 CSV correctly only with a BOM.\n\nSeen in session s-01."})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestInitIsIdempotent(t *testing.T) {
	w := newWiki(t)
	for _, f := range []string{"SCHEMA.md", "index.md", "log.md", "lessons/.gitkeep", "_drafts/stale/.gitkeep"} {
		if _, err := os.Stat(w.path(f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	created, err := w.Init("sample-app", assets.WikiTemplates())
	if err != nil || len(created) != 0 {
		t.Fatalf("second init should change nothing: %v %v", created, err)
	}
	log, _ := os.ReadFile(w.path("log.md"))
	if strings.Count(string(log), "· init") != 1 {
		t.Errorf("init logged more than once:\n%s", log)
	}
}

func TestDraftApproveFlow(t *testing.T) {
	w := newWiki(t)
	d := draft(t, w, "lessons/csv-encoding", "CSV encoding")
	if d.N != 1 || d.Meta.Target != "lessons/csv-encoding.md" || d.Meta.Kind != "new" {
		t.Fatalf("unexpected draft %+v", d.Meta)
	}
	res, err := w.Approve([]int{1}, "reviewer")
	if err != nil || res[0].Done != "approved" {
		t.Fatalf("approve: %+v %v", res, err)
	}
	page, err := Parse(mustRead(t, w.path("lessons/csv-encoding.md")))
	if err != nil || page.Meta.Target != "" || page.Meta.Summary != "" || len(page.Meta.Sources) != 1 {
		t.Fatalf("draft-only fields must be stripped, sources kept: %+v", page.Meta)
	}
	idx, _ := w.Index()
	if len(idx) != 1 || idx[0].Path != "lessons/csv-encoding.md" || idx[0].Summary == "" {
		t.Fatalf("index not updated: %+v", idx)
	}
	if ds, _ := w.Drafts(); len(ds) != 0 {
		t.Fatalf("draft should be gone, have %d", len(ds))
	}
	// An edit of the same page reuses its title and is numbered 2, never 1 again.
	d2, err := w.CreateDraft(DraftInput{Target: "lessons/csv-encoding.md", Sources: []string{"commit abc123"}, Body: "Updated body."})
	if err != nil || d2.N != 2 || d2.Meta.Kind != "edit" || d2.Meta.Title != "CSV encoding" {
		t.Fatalf("edit draft: %+v %v", d2.Meta, err)
	}
	if findings, _ := w.Lint(); hasError(findings) {
		t.Fatalf("clean wiki should lint without errors: %+v", findings)
	}
}

func TestDraftRefusals(t *testing.T) {
	w := newWiki(t)
	cases := map[string]DraftInput{
		"no source":      {Target: "lessons/a.md", Title: "A", Body: "x"},
		"secret":         {Target: "lessons/a.md", Title: "A", Sources: []string{"x"}, Body: "api_key = abcdef123456"},
		"schema":         {Target: "SCHEMA.md", Title: "A", Sources: []string{"x"}, Body: "x"},
		"outside":        {Target: "../etc/passwd", Title: "A", Sources: []string{"x"}, Body: "x"},
		"no area":        {Target: "notes/a.md", Title: "A", Sources: []string{"x"}, Body: "x"},
		"raw":            {Target: "raw/a.md", Title: "A", Sources: []string{"x"}, Body: "x"},
		"too long":       {Target: "lessons/a.md", Title: "A", Sources: []string{"x"}, Body: strings.Repeat("word ", 2000)},
		"empty":          {Target: "lessons/a.md", Title: "A", Sources: []string{"x"}, Body: "  "},
		"bad confidence": {Target: "lessons/a.md", Title: "A", Sources: []string{"x"}, Body: "x", Confidence: "sure"},
	}
	for name, in := range cases {
		if _, err := w.CreateDraft(in); err == nil {
			t.Errorf("%s: want refusal", name)
		}
	}
}

func TestAutoMayOnlyApproveSomeAreas(t *testing.T) {
	w := newWiki(t)
	draft(t, w, "lessons/a.md", "Lesson")
	draft(t, w, "decisions/b.md", "Decision")
	res, err := w.Approve([]int{1, 2}, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Done != "approved" || res[1].Error == "" {
		t.Fatalf("auto must approve lessons but not decisions: %+v", res)
	}
}

func TestDiscardKeepsTheFile(t *testing.T) {
	w := newWiki(t)
	d := draft(t, w, "lessons/a.md", "Lesson")
	if _, err := w.Discard([]int{d.N}, "reviewer", "duplicate"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.path("_drafts/discarded/" + filepath.Base(d.File))); err != nil {
		t.Fatal("discarded draft should be kept in _drafts/discarded/")
	}
}

func TestStaleAfter14Days(t *testing.T) {
	w := newWiki(t)
	draft(t, w, "lessons/a.md", "Lesson")
	later := w.Now().Add(15 * 24 * time.Hour)
	w.Now = func() time.Time { return later }
	moved, err := w.MoveStale()
	if err != nil || len(moved) != 1 {
		t.Fatalf("moved %v err %v", moved, err)
	}
}

func TestLintFindsProblems(t *testing.T) {
	w := newWiki(t)
	os.WriteFile(w.path("lessons/orphan.md"), []byte("---\ntitle: Orphan\ntype: lesson\nupdated: 2026-11-17\nconfidence: low\n---\n\nAlways run the tests first.\n"), 0o644)
	findings, err := w.Lint()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"no-source": false, "not-indexed": false, "rule": false}
	for _, f := range findings {
		if _, ok := want[f.Code]; ok {
			want[f.Code] = true
		}
		if f.Code == "rule" && f.Line != 8 {
			t.Errorf("rule line: got %d want 8", f.Line)
		}
	}
	for code, seen := range want {
		if !seen {
			t.Errorf("lint missed %s", code)
		}
	}
}

func TestSearch(t *testing.T) {
	w := newWiki(t)
	draft(t, w, "lessons/csv-encoding.md", "CSV encoding")
	w.Approve([]int{1}, "reviewer")
	hits, err := w.Search("csv bom", 5)
	if err != nil || len(hits) != 1 || hits[0].File != "lessons/csv-encoding.md" {
		t.Fatalf("search: %+v %v", hits, err)
	}
}

func TestThaiRuleAndTokens(t *testing.T) {
	if len(RuleLines("ห้ามแก้ไฟล์นี้")) != 1 || len(RuleLines("บทเรียนเรื่องการเข้ารหัส")) != 0 {
		t.Fatal("Thai rule detection")
	}
	if Tokens("abcd") != 1 || Tokens("กขคง") != 2 {
		t.Fatalf("token estimate: %d %d", Tokens("abcd"), Tokens("กขคง"))
	}
}

func TestGitHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	w := newWiki(t)
	draft(t, w, "lessons/a.md", "Lesson")
	w.Approve([]int{1}, "reviewer")
	out, err := w.git("log", "--oneline")
	if err != nil || strings.Count(strings.TrimSpace(out), "\n") < 2 {
		t.Fatalf("want init, draft and approve commits:\n%s %v", out, err)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func hasError(fs []Finding) bool {
	for _, f := range fs {
		if f.Level == "error" {
			return true
		}
	}
	return false
}

func TestDraftDropsByteOrderMark(t *testing.T) {
	w := newWiki(t)
	d, err := w.CreateDraft(DraftInput{Target: "lessons/bom.md", Title: "BOM", Sources: []string{"x"}, Body: "\ufeffBody text."})
	if err != nil || strings.HasPrefix(d.Body, "\ufeff") {
		t.Fatalf("body keeps BOM: %q %v", d.Body, err)
	}
}

func TestWriteRawStaysInRaw(t *testing.T) {
	w := newWiki(t)
	for _, bad := range []string{"notes.txt", "", "/"} {
		if _, err := w.WriteRaw(bad, []byte("x")); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	dst, err := w.WriteRaw(`sessions\2026-11-17.md`, []byte("summary"))
	if err != nil || !strings.HasPrefix(dst, filepath.Join(w.Dir, "raw", "sessions")) {
		t.Fatalf("%s %v", dst, err)
	}
	if p, _ := w.WriteRaw("../../x.md", []byte("y")); !strings.HasPrefix(p, filepath.Join(w.Dir, "raw")) {
		t.Fatalf("escaped raw/: %s", p)
	}
}

func TestEditDraftBodyKeepsFrontMatter(t *testing.T) {
	w := newWiki(t)
	d, err := w.CreateDraft(DraftInput{Target: "lessons/csv.md", Title: "CSV needs a BOM", Sources: []string{"src/csv.go:3"}, Body: "Spreadsheets at shop-api need it."})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.EditDraftBody(d.N, "password: hunter2", "reviewer"); err == nil {
		t.Fatal("secrets must be refused")
	}
	if err := w.EditDraftBody(d.N, "Spreadsheets need it.", "reviewer"); err != nil {
		t.Fatal(err)
	}
	ds, _ := w.Drafts()
	if len(ds) != 1 || ds[0].Meta.Title != "CSV needs a BOM" || strings.TrimSpace(ds[0].Body) != "Spreadsheets need it." {
		t.Fatalf("draft after edit: %+v", ds)
	}
}
