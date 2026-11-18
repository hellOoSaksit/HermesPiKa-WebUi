// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package safepath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestName(t *testing.T) {
	for _, ok := range []string{"Export", "ฉาก.fbx", "a b.txt", "notes~draft.md", "report (2).pdf"} {
		if err := Name(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", ".", "..", ".git", ".env", "x.", "x ", "a:b", "a\x00b", "a\nb", "a\x1bb", "a\b", "a/b", "a*b",
		"CON", "nul.txt", "COM1", "GIT~1", "PROGRA~1", "deploy.pem", "id_ed25519", "credentials", "prod.env", "secrets.yaml"} {
		if Name(bad) == nil {
			t.Errorf("%q allowed", bad)
		}
	}
}

func TestRel(t *testing.T) {
	for _, ok := range []string{"a", "a/b/c.txt", "Export/ฉาก.fbx"} {
		if err := Rel(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/a", "a//b", "a/", "../a", "a/../b", "C:/x", "a/.git/b", "keys/id_rsa", "a/b\nc"} {
		if Rel(bad) == nil {
			t.Errorf("%q allowed", bad)
		}
	}
}

func TestDirAndFile(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(root, "a", "f.txt"), []byte("x"), 0o644)
	if d, err := Dir(root, "a/b", false); err != nil || d != filepath.Join(root, "a", "b") {
		t.Fatalf("%q %v", d, err)
	}
	if _, err := Dir(root, "a/missing", false); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want not-exist, got %v", err)
	}
	if d, err := Dir(root, "a/new/deep", true); err != nil || d != filepath.Join(root, "a", "new", "deep") {
		t.Fatalf("%q %v", d, err)
	}
	if _, err := Dir(root, "a/f.txt", false); err == nil {
		t.Fatal("a file passed as a folder")
	}
	if _, fi, err := File(root, "a/f.txt"); err != nil || fi.Size() != 1 {
		t.Fatalf("%v", err)
	}
	if _, _, err := File(root, "a/b"); err == nil {
		t.Fatal("a folder passed as a file")
	}
}

func TestSymlinkRefused(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(outside, "x.txt"), []byte("x"), 0o644)
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Skip("cannot make a symlink here")
	}
	if _, err := Dir(root, "out", false); !errors.Is(err, ErrLink) {
		t.Fatalf("want ErrLink, got %v", err)
	}
	if _, err := Dir(root, "out/sub", true); !errors.Is(err, ErrLink) {
		t.Fatalf("want ErrLink, got %v", err)
	}
	if _, _, err := File(root, "out/x.txt"); !errors.Is(err, ErrLink) {
		t.Fatalf("want ErrLink, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "sub")); err == nil {
		t.Fatal("made a folder outside the project")
	}
}
