// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package safepath

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Junction makes link a junction to target (mklink /J needs no administrator).
func Junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
}

func TestJunctionRefused(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(outside, "x.txt"), []byte("x"), 0o644)
	Junction(t, filepath.Join(root, "scripts"), outside)
	fi, err := os.Lstat(filepath.Join(root, "scripts"))
	if err != nil || !IsLink(filepath.Join(root, "scripts"), fi) {
		t.Fatalf("a junction is not seen as a link: %v %v", fi.Mode(), err)
	}
	if _, err := Dir(root, "scripts", true); !errors.Is(err, ErrLink) {
		t.Fatalf("want ErrLink, got %v", err)
	}
	if _, err := Dir(root, "scripts/sub", true); !errors.Is(err, ErrLink) {
		t.Fatalf("want ErrLink, got %v", err)
	}
	if _, _, err := File(root, "scripts/x.txt"); !errors.Is(err, ErrLink) {
		t.Fatalf("want ErrLink, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "sub")); err == nil {
		t.Fatal("made a folder through the junction")
	}
	// a plain folder is not a link
	os.Mkdir(filepath.Join(root, "plain"), 0o755)
	if fi, _ := os.Lstat(filepath.Join(root, "plain")); IsLink(filepath.Join(root, "plain"), fi) {
		t.Fatal("a plain folder is seen as a link")
	}
}
