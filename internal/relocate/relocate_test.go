// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package relocate

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMoveTree(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "wiki"), filepath.Join(base, "new", "wiki")
	files := []string{"SCHEMA.md", ".git/HEAD", "decisions/บันทึก.md", "lessons/a/b/c.md"}
	for _, f := range files {
		write(t, filepath.Join(src, f), "x"+f)
	}
	if err := Move(src, dst); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dst, f))
		if err != nil || string(b) != "x"+f {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := os.Stat(src); err == nil {
		t.Error("src still there")
	}
	if _, err := os.Stat(src + OldSuffix); err == nil {
		t.Error("old copy left")
	}
}

func TestMoveKeepsSourceOnFailure(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "wiki"), filepath.Join(base, "dst")
	write(t, filepath.Join(src, "a.md"), "a")
	write(t, filepath.Join(dst, "other.md"), "o")
	if err := Move(src, dst); err == nil {
		t.Fatal("moved into a non-empty folder")
	}
	if b, err := os.ReadFile(filepath.Join(src, "a.md")); err != nil || string(b) != "a" {
		t.Error("src changed")
	}
	if _, err := os.Stat(src + OldSuffix); err == nil {
		t.Error("old copy exists")
	}
	if _, err := os.Stat(filepath.Join(dst, "other.md")); err != nil {
		t.Error("dst content touched")
	}
	if err := Move(src, filepath.Join(src, "sub")); err == nil {
		t.Error("moved into itself")
	}
}

func TestMoveCommitFailureRestores(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "wiki"), filepath.Join(base, "dst")
	write(t, filepath.Join(src, "a.md"), "a")
	if err := MoveWith(src, dst, func() error { return os.ErrPermission }); err == nil {
		t.Fatal("no error")
	}
	if _, err := os.Stat(filepath.Join(src, "a.md")); err != nil {
		t.Error("src not restored")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("dst left behind")
	}
}
