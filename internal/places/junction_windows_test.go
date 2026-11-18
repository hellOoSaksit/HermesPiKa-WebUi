// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package places

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A junction higher up (<home>/s -> elsewhere): EvalSymlinks leaves it alone,
// so only the walk from the shared folder down catches it.
func TestRootThroughJunctionAncestorRefused(t *testing.T) {
	home, elsewhere := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(elsewhere, "x"), 0o755)
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(home, "s"), elsewhere).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
	if _, err := Root(home, isolated(t, home, filepath.Join(home, "s", "x"))); err == nil {
		t.Fatal("a root under a junction is accepted")
	}
}
