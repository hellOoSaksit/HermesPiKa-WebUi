// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package relocate moves a folder tree (a project's wiki) to another place
// without ever leaving the person with half of it: copy, check, swap, clean up.
package relocate

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// OldSuffix is added to the source while the swap is in progress.
const OldSuffix = ".hpk-old"

// CleanupError: the move itself succeeded (the new place is complete and
// recorded) but the old copy could not be removed; it is still at Path.
type CleanupError struct {
	Path string
	Err  error
}

func (e *CleanupError) Error() string {
	return fmt.Sprintf("moved, but the old copy is still at %s: %v", e.Path, e.Err)
}
func (e *CleanupError) Unwrap() error { return e.Err }

// removeAll is os.RemoveAll; tests swap it.
var removeAll = os.RemoveAll

// Move moves the folder src to dst (see MoveWith).
func Move(src, dst string) error { return MoveWith(src, dst, nil) }

// MoveWith copies src to dst, checks the copy (file count and sizes), renames
// src to src.hpk-old, calls commit (which records the new location), then
// removes src.hpk-old. Before the swap any failure removes the partial dst and
// leaves src untouched; when commit fails src is put back. Links are refused.
// When only the final removal of src.hpk-old fails, the move stands and a
// *CleanupError is returned: callers must treat that as success with a warning.
func MoveWith(src, dst string, commit func() error) error {
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	if !filepath.IsAbs(src) || !filepath.IsAbs(dst) {
		return errors.New("give full folder paths")
	}
	if fi, err := os.Lstat(src); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a folder", src)
	}
	if inside(src, dst) || inside(dst, src) {
		return errors.New("the new place cannot be inside the old one, or the reverse")
	}
	old := src + OldSuffix
	if _, err := os.Lstat(old); err == nil {
		return fmt.Errorf("%s is in the way; remove it first", old)
	}
	created := false
	ents, err := os.ReadDir(dst)
	switch {
	case err == nil:
		if len(ents) > 0 {
			return fmt.Errorf("%s is not empty", dst)
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		created = true
	default:
		return err
	}
	clean := func() {
		if created {
			_ = os.RemoveAll(dst)
			return
		}
		left, _ := os.ReadDir(dst)
		for _, e := range left {
			_ = os.RemoveAll(filepath.Join(dst, e.Name()))
		}
	}
	nSrc, err := copyTree(src, dst)
	if err != nil {
		clean()
		return err
	}
	nDst, err := count(dst)
	if err == nil && nDst != nSrc {
		err = fmt.Errorf("the copy differs from the original (%d of %d files, or sizes)", nDst.files, nSrc.files)
	}
	if err != nil {
		clean()
		return err
	}
	if err := os.Rename(src, old); err != nil {
		clean()
		return err
	}
	if commit != nil {
		if err := commit(); err != nil {
			if rerr := os.Rename(old, src); rerr != nil {
				return fmt.Errorf("%w (the original is at %s)", err, old)
			}
			clean()
			return err
		}
	}
	if err := removeAll(old); err != nil {
		return &CleanupError{Path: old, Err: err}
	}
	return nil
}

type tally struct{ files, bytes int64 }

func count(dir string) (tally, error) {
	var t tally
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			fi, err := d.Info()
			if err != nil {
				return err
			}
			t.files++
			t.bytes += fi.Size()
		}
		return nil
	})
	return t, err
}

func copyTree(src, dst string) (tally, error) {
	var t tally
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		if d.Type()&fs.ModeSymlink != 0 || (!d.IsDir() && !d.Type().IsRegular()) {
			return fmt.Errorf("%s is a link or special file; links are not moved", p)
		}
		if d.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		n, err := copyFile(p, to)
		t.files++
		t.bytes += n
		return err
	})
	return t, err
}

func copyFile(from, to string) (int64, error) {
	in, err := os.Open(from)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return 0, err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm()|0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
}

// inside reports whether p is dir or below it.
func inside(dir, p string) bool {
	if runtime.GOOS == "windows" {
		dir, p = strings.ToLower(dir), strings.ToLower(p)
	}
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
