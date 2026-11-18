// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build !windows

package selfinstall

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func profiles() []string {
	h, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(h, ".profile"), filepath.Join(h, ".zshrc"), filepath.Join(h, ".bashrc"), filepath.Join(h, ".bash_profile")}
}

// loginProfile is the file to create when the person has none of the profiles:
// the one their login shell (SHELL) reads. A fresh macOS account has none, and
// its shell is zsh.
func loginProfile() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch filepath.Base(os.Getenv("SHELL")) {
	case "zsh":
		return filepath.Join(h, ".zshrc")
	case "bash":
		if runtime.GOOS == "darwin" {
			return filepath.Join(h, ".bash_profile")
		}
		return filepath.Join(h, ".bashrc")
	}
	return filepath.Join(h, ".profile")
}

// addPath appends the PATH line to each profile that exists and lacks it. When
// none of the profiles exists it creates the one for the login shell, so a
// new account still gets hpk and hermes on PATH. The files it changed are
// recorded in pathFiles.
func addPath(dirs ...string) (bool, error) {
	line := profileLine(dirs)
	added := false
	pathFiles = nil
	targets := profiles()
	anyExists := false
	for _, p := range targets {
		if _, err := os.Stat(p); err == nil {
			anyExists = true
			break
		}
	}
	if !anyExists {
		if lp := loginProfile(); lp != "" {
			targets = []string{lp}
		}
	}
	for _, p := range targets {
		b, err := os.ReadFile(p)
		if err != nil && !(!anyExists && os.IsNotExist(err)) {
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
		pathFiles = append(pathFiles, p)
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
