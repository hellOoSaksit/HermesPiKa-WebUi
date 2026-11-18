// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package folders

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var exportRules = Rules{Answered: true, Rows: []Rule{{Folder: "Export", Exts: []string{".fbx"}}}}

func put(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(root, rel string) bool {
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=me", "-c", "user.email=me@example.com"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestPlanTidy(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"a.fbx", "Export/b.fbx", "Export/sub/c.fbx", "node_modules/x.fbx", ".cache/y.fbx", "readme.md"} {
		put(t, root, f, "x")
	}
	pl, err := PlanTidy(area(root, ""), exportRules)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Moves) != 1 || pl.Moves[0].From != "a.fbx" || pl.Moves[0].To != "Export/a.fbx" || pl.Moves[0].Size != 1 {
		t.Fatalf("moves = %+v", pl.Moves)
	}
	if !reflect.DeepEqual(pl.Homeless, []string{"readme.md"}) {
		t.Fatalf("homeless = %v", pl.Homeless)
	}
}

func TestPlanSkipsIgnored(t *testing.T) {
	needGit(t)
	root := t.TempDir()
	git(t, root, "init", "-q")
	put(t, root, ".gitignore", "build/\n")
	put(t, root, "build/z.fbx", "x")
	put(t, root, "ฉาก.fbx", "x")
	pl, err := PlanTidy(area(root, ""), exportRules)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Moves) != 1 || pl.Moves[0].From != "ฉาก.fbx" {
		t.Fatalf("moves = %+v", pl.Moves)
	}
}

func TestApplyNoGit(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	put(t, root, "a.fbx", "mesh")
	pl, _ := PlanTidy(area(root, ""), exportRules)
	done, skipped, err := ApplyTidy(area(root, dir), pl.Moves)
	if err != nil || len(done) != 1 || len(skipped) != 0 {
		t.Fatalf("%v %v %v", done, skipped, err)
	}
	if exists(root, "a.fbx") || !exists(root, "Export/a.fbx") {
		t.Fatal("file not moved")
	}
	if _, err := os.Stat(UndoFile(dir, "p")); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "audit.log")); !strings.Contains(string(raw), "a.fbx") {
		t.Fatalf("audit: %q", raw)
	}
}

func TestApplyGitMv(t *testing.T) {
	needGit(t)
	root, dir := t.TempDir(), t.TempDir()
	git(t, root, "init", "-q")
	put(t, root, "a.fbx", "mesh")
	git(t, root, "add", "a.fbx")
	git(t, root, "commit", "-q", "-m", "one")
	pl, _ := PlanTidy(area(root, ""), exportRules)
	if _, _, err := ApplyTidy(area(root, dir), pl.Moves); err != nil {
		t.Fatal(err)
	}
	if st := git(t, root, "status", "--porcelain"); !strings.HasPrefix(st, "R ") {
		t.Fatalf("want a staged rename, got %q", st)
	}
}

func TestApplySkipsChanged(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	put(t, root, "a.fbx", "mesh")
	pl, _ := PlanTidy(area(root, ""), exportRules)
	put(t, root, "a.fbx", "mesh, changed")
	done, skipped, err := ApplyTidy(area(root, dir), pl.Moves)
	if err != nil || len(done) != 0 || !reflect.DeepEqual(skipped, []string{"a.fbx"}) {
		t.Fatalf("%v %v %v", done, skipped, err)
	}
	if !exists(root, "a.fbx") {
		t.Fatal("file moved")
	}
}

func TestApplyClash(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	put(t, root, "a.fbx", "new")
	put(t, root, "Export/a.fbx", "old")
	pl, _ := PlanTidy(area(root, ""), exportRules)
	done, _, err := ApplyTidy(area(root, dir), pl.Moves)
	if err != nil || len(done) != 1 || done[0].To != "Export/a (2).fbx" {
		t.Fatalf("%v %v", done, err)
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "Export", "a.fbx")); string(raw) != "old" {
		t.Fatalf("replaced: %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "Export", "a (2).fbx")); string(raw) != "new" {
		t.Fatalf("moved: %q", raw)
	}
}

func TestApplyRefusesLinkedFolder(t *testing.T) {
	root, dir, outside := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "Export")); err != nil {
		t.Skip("cannot make a symlink here")
	}
	put(t, root, "a.fbx", "mesh")
	pl, _ := PlanTidy(area(root, ""), exportRules)
	done, skipped, _ := ApplyTidy(area(root, dir), pl.Moves)
	if len(done) != 0 || len(skipped) != 1 || !exists(root, "a.fbx") {
		t.Fatalf("%v %v", done, skipped)
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatal("moved outside the project")
	}
}

func TestUndo(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	put(t, root, "a.fbx", "one")
	put(t, root, "b.fbx", "two")
	pl, _ := PlanTidy(area(root, ""), exportRules)
	if done, _, err := ApplyTidy(area(root, dir), pl.Moves); err != nil || len(done) != 2 {
		t.Fatalf("%v %v", done, err)
	}
	put(t, root, "Export/b.fbx", "two, edited")
	u, err := ReadUndo(area(root, dir))
	if err != nil || len(u.Moves) != 2 {
		t.Fatalf("%v %v", u, err)
	}
	done, skipped, err := UndoTidy(area(root, dir), u.At)
	if err != nil || !reflect.DeepEqual(done, []string{"a.fbx"}) || !reflect.DeepEqual(skipped, []string{"Export/b.fbx"}) {
		t.Fatalf("%v %v %v", done, skipped, err)
	}
	if !exists(root, "a.fbx") || exists(root, "Export/a.fbx") || !exists(root, "Export/b.fbx") {
		t.Fatal("layout wrong after undo")
	}
	if _, err := os.Stat(UndoFile(dir, "p")); !os.IsNotExist(err) {
		t.Fatalf("the undo record should be gone: %v", err)
	}
	if _, _, err := UndoTidy(area(root, dir), u.At); err == nil {
		t.Fatal("undo with nothing to undo should say so")
	}
}

func area(root, dir string) Area {
	if dir == "" {
		return Area{Root: root, Record: filepath.Join(root, "..", "none.json")}
	}
	return Area{Root: root, Record: UndoFile(dir, "p"), Audit: dir}
}

func TestUndoNeedsTheSameRoundAndRoot(t *testing.T) {
	root, other, dir := t.TempDir(), t.TempDir(), t.TempDir()
	put(t, root, "a.fbx", "one")
	pl, _ := PlanTidy(area(root, dir), exportRules)
	if _, _, err := ApplyTidy(area(root, dir), pl.Moves); err != nil {
		t.Fatal(err)
	}
	u, err := ReadUndo(area(root, dir))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UndoTidy(area(root, dir), u.At.Add(time.Second)); err == nil {
		t.Fatal("undo of a round the person did not see")
	}
	// a record made for another folder is refused there
	if _, err := ReadUndo(Area{Root: other, Record: UndoFile(dir, "p")}); err == nil {
		t.Fatal("undo record used for another folder")
	}
	if !exists(root, "Export/a.fbx") {
		t.Fatal("moved")
	}
}

func TestUndoRefusesBadPaths(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	put(t, root, "Export/a.fbx", "x")
	fi, _ := os.Stat(filepath.Join(root, "Export", "a.fbx"))
	a := area(root, dir)
	for _, from := range []string{`..\evil.fbx`, "../evil.fbx", "x\nfake audit line.fbx", ".git/hooks/a.fbx", "node_modules/a.fbx", "id_rsa"} {
		raw, _ := json.Marshal(lastRound{At: time.Unix(1, 0).UTC(), Root: root, Moves: []Move{{From: from, To: "Export/a.fbx", Size: fi.Size(), MTime: fi.ModTime()}}})
		os.MkdirAll(filepath.Dir(a.Record), 0o755)
		os.WriteFile(a.Record, raw, 0o600)
		done, skipped, err := UndoTidy(a, time.Unix(1, 0).UTC())
		if err != nil || len(done) != 0 || len(skipped) != 1 {
			t.Fatalf("%q: %v %v %v", from, done, skipped, err)
		}
	}
	if !exists(root, "Export/a.fbx") {
		t.Fatal("moved")
	}
}

func TestPlanSkipsWikiAndOtherProjects(t *testing.T) {
	root := t.TempDir()
	put(t, root, "wiki/notes.fbx", "x")
	put(t, root, "other/b.fbx", "x")
	put(t, root, "a.fbx", "x")
	a := Area{Root: root, Skip: []string{filepath.Join(root, "wiki"), filepath.Join(root, "other"), t.TempDir()}}
	pl, err := PlanTidy(a, exportRules)
	if err != nil || len(pl.Moves) != 1 || pl.Moves[0].From != "a.fbx" {
		t.Fatalf("%+v %v", pl, err)
	}
	// a move into a skipped folder is refused too
	rules := Rules{Answered: true, Rows: []Rule{{Folder: "wiki", Exts: []string{".fbx"}}}}
	if pl, _ := PlanTidy(a, rules); len(pl.Moves) != 0 {
		t.Fatalf("moves into the wiki: %+v", pl.Moves)
	}
	if done, _, _ := ApplyTidy(Area{Root: root, Skip: a.Skip, Record: filepath.Join(t.TempDir(), "r.json")}, []Move{{From: "wiki/notes.fbx", To: "Export/notes.fbx"}}); len(done) != 0 {
		t.Fatal("moved a wiki page")
	}
}
