// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package folders

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
	pl, err := PlanTidy(root, exportRules)
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
	pl, err := PlanTidy(root, exportRules)
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
	pl, _ := PlanTidy(root, exportRules)
	done, skipped, err := ApplyTidy(root, dir, pl.Moves)
	if err != nil || len(done) != 1 || len(skipped) != 0 {
		t.Fatalf("%v %v %v", done, skipped, err)
	}
	if exists(root, "a.fbx") || !exists(root, "Export/a.fbx") {
		t.Fatal("file not moved")
	}
	if _, err := os.Stat(filepath.Join(dir, "tidy-last.json")); err != nil {
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
	pl, _ := PlanTidy(root, exportRules)
	if _, _, err := ApplyTidy(root, dir, pl.Moves); err != nil {
		t.Fatal(err)
	}
	if st := git(t, root, "status", "--porcelain"); !strings.HasPrefix(st, "R ") {
		t.Fatalf("want a staged rename, got %q", st)
	}
}

func TestApplySkipsChanged(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	put(t, root, "a.fbx", "mesh")
	pl, _ := PlanTidy(root, exportRules)
	put(t, root, "a.fbx", "mesh, changed")
	done, skipped, err := ApplyTidy(root, dir, pl.Moves)
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
	pl, _ := PlanTidy(root, exportRules)
	done, _, err := ApplyTidy(root, dir, pl.Moves)
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
	pl, _ := PlanTidy(root, exportRules)
	done, skipped, _ := ApplyTidy(root, dir, pl.Moves)
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
	pl, _ := PlanTidy(root, exportRules)
	if done, _, err := ApplyTidy(root, dir, pl.Moves); err != nil || len(done) != 2 {
		t.Fatalf("%v %v", done, err)
	}
	put(t, root, "Export/b.fbx", "two, edited")
	done, skipped, err := UndoTidy(root, dir)
	if err != nil || !reflect.DeepEqual(done, []string{"a.fbx"}) || !reflect.DeepEqual(skipped, []string{"Export/b.fbx"}) {
		t.Fatalf("%v %v %v", done, skipped, err)
	}
	if !exists(root, "a.fbx") || exists(root, "Export/a.fbx") || !exists(root, "Export/b.fbx") {
		t.Fatal("layout wrong after undo")
	}
	if _, err := os.Stat(filepath.Join(dir, "tidy-last.json")); !os.IsNotExist(err) {
		t.Fatalf("tidy-last.json should be gone: %v", err)
	}
	if _, _, err := UndoTidy(root, dir); err == nil {
		t.Fatal("undo with nothing to undo should say so")
	}
}
