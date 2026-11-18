// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build !windows

package selfinstall

import (
	"os"
	"path/filepath"
	"strings"
)

const marker = "# added by hpk"

func profiles() []string {
	h, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(h, ".profile"), filepath.Join(h, ".zshrc"), filepath.Join(h, ".bashrc")}
}

// addPath appends the PATH line to each profile that exists and lacks it.
func addPath(dir string) (bool, error) {
	line := `export PATH="` + dir + `:$PATH" ` + marker
	added := false
	for _, p := range profiles() {
		b, err := os.ReadFile(p)
		if err != nil || strings.Contains(string(b), marker) {
			continue
		}
		s := string(b)
		if s != "" && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		if err := os.WriteFile(p, []byte(s+line+"\n"), 0o644); err != nil {
			return added, err
		}
		added = true
	}
	return added, nil
}

func removePath(dir string) (bool, error) {
	removed := false
	for _, p := range profiles() {
		b, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(b), marker) {
			continue
		}
		var keep []string
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasSuffix(strings.TrimSpace(l), marker) {
				continue
			}
			keep = append(keep, l)
		}
		if err := os.WriteFile(p, []byte(strings.Join(keep, "\n")), 0o644); err != nil {
			return removed, err
		}
		removed = true
	}
	return removed, nil
}

func addShortcut(string) (bool, error) { return false, nil }
func removeShortcut() error            { return nil }
