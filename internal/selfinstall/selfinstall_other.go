// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build !windows

package selfinstall

import (
	"os"
	"path/filepath"
	"strings"
)

func profiles() []string {
	h, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(h, ".profile"), filepath.Join(h, ".zshrc"), filepath.Join(h, ".bashrc")}
}

// addPath appends the PATH line to each profile that exists and lacks it.
func addPath(dirs ...string) (bool, error) {
	line := profileLine(dirs)
	added := false
	for _, p := range profiles() {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := string(b)
		if strings.Contains(s, line) {
			continue
		}
		if strings.Contains(s, marker) { // an older line without every dir: replace it
			var keep []string
			for _, l := range strings.Split(s, "\n") {
				if !strings.HasSuffix(strings.TrimSpace(l), marker) {
					keep = append(keep, l)
				}
			}
			s = strings.Join(keep, "\n")
		}
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

func removePath(_ ...string) (bool, error) {
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
