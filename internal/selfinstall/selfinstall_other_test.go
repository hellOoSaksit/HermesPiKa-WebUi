// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build !windows

package selfinstall

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A clean account has no shell profile at all: addPath must create the one for
// the login shell, and only that one.
func TestAddPathCreatesLoginShellProfile(t *testing.T) {
	cases := []struct{ shell, want string }{
		{"/bin/zsh", ".zshrc"},
		{"/usr/bin/zsh", ".zshrc"},
		{"/bin/bash", map[bool]string{true: ".bash_profile", false: ".bashrc"}[runtime.GOOS == "darwin"]},
		{"/usr/bin/fish", ".profile"},
		{"", ".profile"},
	}
	for _, c := range cases {
		t.Run(c.shell, func(t *testing.T) {
			d := sandbox(t)
			home := filepath.Join(d, "home")
			t.Setenv("SHELL", c.shell)
			added, err := addPath(PathDirs()...)
			if err != nil || !added {
				t.Fatalf("addPath = %v, %v", added, err)
			}
			b, err := os.ReadFile(filepath.Join(home, c.want))
			if err != nil {
				t.Fatalf("expected %s: %v", c.want, err)
			}
			if !strings.Contains(string(b), profileLine(PathDirs())) {
				t.Fatalf("PATH line missing:\n%s", b)
			}
			if len(pathFiles) != 1 || pathFiles[0] != filepath.Join(home, c.want) {
				t.Fatalf("pathFiles = %v", pathFiles)
			}
			for _, other := range []string{".profile", ".zshrc", ".bashrc", ".bash_profile"} {
				if other == c.want {
					continue
				}
				if _, err := os.Stat(filepath.Join(home, other)); err == nil {
					t.Fatalf("must not also create %s", other)
				}
			}
			// Again: once only.
			if added, _ := addPath(PathDirs()...); added {
				t.Fatal("second addPath must change nothing")
			}
			// And uninstall removes the line again.
			if removed, err := removePath(PathDirs()...); err != nil || !removed {
				t.Fatalf("removePath = %v, %v", removed, err)
			}
		})
	}
}

// With an existing profile, nothing new is created.
func TestAddPathKeepsExistingProfilesOnly(t *testing.T) {
	d := sandbox(t)
	home := filepath.Join(d, "home")
	t.Setenv("SHELL", "/bin/zsh")
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if added, err := addPath(PathDirs()...); err != nil || !added {
		t.Fatalf("addPath = %v, %v", added, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); err == nil {
		t.Fatal("must not create .zshrc when another profile exists")
	}
}
