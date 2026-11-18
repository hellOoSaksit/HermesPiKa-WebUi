// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package folders

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

const hdr = "| Folder | What goes here | Extensions |\n|---|---|---|\n"

func TestParseTable(t *testing.T) {
	r := Parse(hdr + "| RAW 3D | งานต้นฉบับ | .blend .max |\n| Export | x | .FBX, obj |\n| Art/Texture | x | png |\n")
	if len(r.Rows) != 3 || len(r.Bad) != 0 {
		t.Fatalf("rows=%d bad=%v", len(r.Rows), r.Bad)
	}
	if !reflect.DeepEqual(r.Rows[1].Exts, []string{".fbx", ".obj"}) || !reflect.DeepEqual(r.Rows[2].Exts, []string{".png"}) {
		t.Fatalf("exts %v %v", r.Rows[1].Exts, r.Rows[2].Exts)
	}
	if f, ok := r.For("ฉาก 1.FBX"); !ok || f != "Export" {
		t.Fatalf("For = %q %v", f, ok)
	}
	if _, ok := r.For("a.tar.gz"); ok {
		t.Fatal("a.tar.gz should not match")
	}
}

func TestParseHandWritten(t *testing.T) {
	in := "Notes\r\n\r\n| Folder | What goes here | Extensions |\r\n|:---|---:|:-:|\r\n| A | x | .a | extra |\r\n| B | y | .b |\r\n"
	r := Parse(in)
	if len(r.Rows) != 2 || len(r.Bad) != 0 {
		t.Fatalf("rows=%d bad=%v", len(r.Rows), r.Bad)
	}
}

func TestParseRejects(t *testing.T) {
	r := Parse(hdr + "| ../out | x | .a |\n| C:\\abs | x | .b |\n| X | x | .a |\n| Y | x | .a |\n")
	if len(r.Rows) != 1 || r.Rows[0].Folder != "X" || len(r.Bad) != 3 {
		t.Fatalf("rows=%v bad=%v", r.Rows, r.Bad)
	}
}

func TestNoSplitIsAnAnswer(t *testing.T) {
	r := Parse(hdr)
	if !r.Answered || len(r.Rows) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestEnsureRefusesEscape(t *testing.T) {
	if _, err := Ensure(t.TempDir(), Rules{Rows: []Rule{{Folder: "../x"}}}); err == nil {
		t.Fatal("want error")
	}
}

func TestTrustAndState(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "orbit-app")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := project.Create(home, "orbit-app", root, "")
	if err != nil {
		t.Fatal(err)
	}
	w := wiki.Open(p.WikiDir())
	if _, err := w.Init(p.Name, assets.WikiTemplates()); err != nil {
		t.Fatal(err)
	}
	if _, st := Load(home, p); st != None {
		t.Fatalf("want none, got %s", st)
	}
	d, err := w.CreateDraft(wiki.DraftInput{Target: Page, Title: "Folders", Sources: []string{"chat"},
		Summary: "where files go", Author: "me", Body: hdr + "| RAW 3D | x | .blend |\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, st := Load(home, p); st != Pending {
		t.Fatalf("want pending, got %s", st)
	}
	res, err := w.Approve([]int{d.N}, "me")
	if err != nil {
		t.Fatal(err)
	}
	if err := AfterApprove(home, p, res); err != nil {
		t.Fatal(err)
	}
	r, st := Load(home, p)
	if st != OK || len(r.Rows) != 1 {
		t.Fatalf("want ok, got %s %+v", st, r)
	}
	if fi, err := os.Stat(filepath.Join(root, "RAW 3D")); err != nil || !fi.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
	page := filepath.Join(p.WikiDir(), "decisions", "folders.md")
	raw, _ := os.ReadFile(page)
	if err := os.WriteFile(page, []byte(strings.Replace(string(raw), "RAW 3D", "Other", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, st := Load(home, p); st != Changed {
		t.Fatalf("want changed, got %s", st)
	}
	// a page that was never approved: no trusted hash exists
	if _, st := Load(t.TempDir(), p); st != Changed {
		t.Fatalf("want changed (no trusted hash), got %s", st)
	}
}
