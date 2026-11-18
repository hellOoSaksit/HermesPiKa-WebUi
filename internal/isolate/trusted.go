// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package isolate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hpk.local/hpk/internal/safepath"
)

// ReadTrusted reads name from OwnTrustedDir: the copy the AI cannot change.
func ReadTrusted(home, name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(OwnTrustedDir(home), name))
}

// WriteTrusted writes name to OwnTrustedDir and, for the AI's side, to
// TrustedDir (one write when they are the same folder). Each is a new file
// renamed into place, so a file the AI made there earlier is replaced, not
// written through.
func WriteTrusted(home, name string, data []byte) error {
	own, shared := OwnTrustedDir(home), TrustedDir(home)
	dirs := []string{own}
	if !samePath(own, shared) {
		dirs = append(dirs, shared)
	}
	for _, d := range dirs {
		if samePath(d, shared) {
			if err := sharedGuard(home); err != nil {
				return err
			}
		}
		if err := writeNew(d, name, data); err != nil {
			return err
		}
	}
	return nil
}

// sharedGuard refuses when <home>/hpk or <home>/hpk/trusted is a link or a
// junction. The AI can rename and recreate <home>/hpk, so it could make
// "trusted" lead anywhere; hpk, running as the person, must then not write,
// copy or change permissions there. A missing folder is fine (hpk makes it).
func sharedGuard(home string) error {
	for _, p := range []string{filepath.Join(home, "hpk"), TrustedDir(home)} {
		fi, err := os.Lstat(p)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if safepath.IsLink(p, fi) {
			return fmt.Errorf("%s is a link or junction (the AI may have planted it); hpk does not write through it. Remove it, then run hpk isolate status", p)
		}
		if !fi.IsDir() {
			return fmt.Errorf("%s is not a folder", p)
		}
	}
	return nil
}

func writeNew(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-"+name+"-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(dir, name))
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// copyTrusted copies the plain files of one trusted folder into another,
// replacing what is there (links and folders are left out). A missing from is
// nothing to copy.
func copyTrusted(from, to string) error {
	ents, err := os.ReadDir(from)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range ents {
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			return err
		}
		if err := writeNew(to, e.Name(), raw); err != nil {
			return err
		}
	}
	return nil
}
