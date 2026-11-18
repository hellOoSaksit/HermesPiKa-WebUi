// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package folders

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
}

// The AI replaces the rule folder with a junction to a folder outside the
// project (say, the Startup folder): a tidy must not move a file into it.
func TestTidyRefusesJunctionedRuleFolder(t *testing.T) {
	root, dir, outside := t.TempDir(), t.TempDir(), t.TempDir()
	rules := Rules{Answered: true, Rows: []Rule{{Folder: "scripts", Exts: []string{".js"}}}}
	put(t, root, "evil.js", "x")
	junction(t, filepath.Join(root, "scripts"), outside)
	pl, err := PlanTidy(area(root, dir), rules)
	if err != nil || len(pl.Moves) != 1 {
		t.Fatalf("%+v %v", pl, err)
	}
	done, skipped, _ := ApplyTidy(area(root, dir), pl.Moves)
	if len(done) != 0 || len(skipped) != 1 {
		t.Fatalf("%v %v", done, skipped)
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatal("moved a file through the junction")
	}
	if !exists(root, "evil.js") {
		t.Fatal("the file is gone")
	}
}

// Files behind a junction are not planned (nor moved out from there).
func TestPlanDoesNotEnterJunction(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	put(t, outside, "far.fbx", "x")
	junction(t, filepath.Join(root, "link"), outside)
	pl, err := PlanTidy(area(root, ""), exportRules)
	if err != nil || len(pl.Moves) != 0 {
		t.Fatalf("%+v %v", pl, err)
	}
}

func TestEnsureRefusesJunction(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	junction(t, filepath.Join(root, "Export"), outside)
	rules := Rules{Answered: true, Rows: []Rule{{Folder: "Export/sub", Exts: []string{".fbx"}}}}
	if _, err := Ensure(root, rules); err == nil {
		t.Fatal("made a folder through a junction")
	}
	if _, err := os.Stat(filepath.Join(outside, "sub")); err == nil {
		t.Fatal("made a folder outside the project")
	}
}
