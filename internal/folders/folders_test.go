// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package folders

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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

// A rule may only name a folder upload and the file browser would accept.
func TestParseRejectsNamesUploadRefuses(t *testing.T) {
	body := "| Folder | What goes here | Extensions |\n|---|---|---|\n" +
		"| .git/hooks | x | .a |\n| GIT~1 | x | .b |\n| CON | x | .c |\n| keys/credentials | x | .d |\n| bad\x1bname | x | .e |\n| tail. | x | .f |\n| Export/ | models | .fbx |\n"
	r := Parse(body)
	if len(r.Bad) != 6 || len(r.Rows) != 1 || r.Rows[0].Folder != "Export" {
		t.Fatalf("rows=%v bad=%v", r.Rows, r.Bad)
	}
}

// While the AI is isolated, a hash it writes into <home>/hpk/trusted does not
// make a page it changed "approved": hpk reads its own copy in the private
// folder. And approval creates folders only under a root the AI was given.
func TestIsolatedTrustIgnoresTheAIsCopy(t *testing.T) {
	home, priv := t.TempDir(), t.TempDir()
	cfg := filepath.Join(t.TempDir(), "isolation.json")
	t.Setenv("HPK_ISOLATION_FILE", cfg)
	os.WriteFile(cfg, []byte(`{"mode":"user","user":"hpk-ai","home":`+strconv.Quote(home)+`,"private":`+strconv.Quote(priv)+`}`), 0o600)
	root := filepath.Join(t.TempDir(), "orbit-app") // not shared with the AI
	os.MkdirAll(root, 0o755)
	p, err := project.Create(home, "orbit-app", root, "")
	if err != nil {
		t.Fatal(err)
	}
	w := wiki.Open(p.WikiDir())
	w.Init(p.Name, assets.WikiTemplates())
	d, _ := w.CreateDraft(wiki.DraftInput{Target: Page, Title: "Folders", Sources: []string{"chat"}, Summary: "s", Author: "me", Body: hdr + "| Export | x | .fbx |\n"})
	res, _ := w.Approve([]int{d.N}, "me")
	if err := AfterApprove(home, p, res); err == nil {
		t.Fatal("folders created under a root the AI was not given")
	}
	if _, err := os.Stat(filepath.Join(root, "Export")); err == nil {
		t.Fatal("made a folder there")
	}
	if _, st := Load(home, p); st != OK {
		t.Fatalf("want ok, got %s", st)
	}
	page := filepath.Join(p.WikiDir(), "decisions", "folders.md")
	forged := []byte(strings.Replace(string(must(os.ReadFile(page))), "Export", "scripts", 1))
	os.WriteFile(page, forged, 0o644)
	os.WriteFile(TrustFile(home, p.Name), []byte(sum(forged)+"\n"), 0o644)
	if _, st := Load(home, p); st != Changed {
		t.Fatalf("the AI's own hash was believed: %s", st)
	}
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
