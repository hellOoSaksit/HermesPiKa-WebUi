// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package places

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hpk.local/hpk/internal/project"
)

// isolated writes an isolation config for home and returns a project rooted at root.
func isolated(t *testing.T, home, root string) *project.Project {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "isolation.json")
	t.Setenv("HPK_ISOLATION_FILE", cfg)
	raw, _ := json.Marshal(map[string]any{"mode": "user", "user": "hpk-ai", "home": home, "private": t.TempDir(), "granted": []string{home}})
	os.WriteFile(cfg, raw, 0o600)
	return &project.Project{Name: "orbit-app", Root: root, Dir: filepath.Join(home, "hpk", "projects", "orbit-app")}
}

func TestRootInsideSharedFolder(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "code", "orbit-app")
	os.MkdirAll(root, 0o755)
	if _, err := Root(home, isolated(t, home, root)); err != nil {
		t.Fatal(err)
	}
	if _, err := Root(home, isolated(t, home, t.TempDir())); err == nil {
		t.Fatal("a root the AI was not given is accepted")
	}
}

// A symlink higher up (<home>/s -> elsewhere) passes a check on the written
// path; the resolved path is checked too.
func TestRootThroughSymlinkAncestorRefused(t *testing.T) {
	home, elsewhere := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(elsewhere, "x"), 0o755)
	if err := os.Symlink(elsewhere, filepath.Join(home, "s")); err != nil {
		t.Skip("cannot make a symlink here")
	}
	if _, err := Root(home, isolated(t, home, filepath.Join(home, "s", "x"))); err == nil {
		t.Fatal("a root under a symlinked folder that leads out of the shared home is accepted")
	}
}
