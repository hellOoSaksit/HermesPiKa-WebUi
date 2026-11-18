// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"os"
	"path/filepath"
	"testing"

	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

func TestWikiDirAPI(t *testing.T) {
	s, ts, p := setup(t)
	dst := filepath.Join(s.home, "elsewhere", "sample-wiki")

	if res, _ := post(t, ts.URL+"/api/project/wiki-dir", map[string]string{"project": p.Name, "dir": "relative/dir"}, true); res.StatusCode != 400 {
		t.Errorf("relative dir: %d", res.StatusCode)
	}

	other := filepath.Join(s.home, "code", "other-app")
	os.MkdirAll(other, 0o755)
	op, err := project.Create(s.home, "other-app", other, project.Shared)
	if err != nil {
		t.Fatal(err)
	}
	if res, _ := post(t, ts.URL+"/api/project/wiki-dir", map[string]string{"project": p.Name, "dir": filepath.Join(op.WikiDir(), "inner")}, true); res.StatusCode != 400 {
		t.Errorf("inside another wiki: %d", res.StatusCode)
	}

	res, out := post(t, ts.URL+"/api/project/wiki-dir", map[string]string{"project": p.Name, "dir": dst}, true)
	if res.StatusCode != 200 || out["ok"] != true || out["dir"] != dst {
		t.Fatalf("move: %d %v", res.StatusCode, out)
	}
	np, _ := project.Load(s.home, p.Name)
	if np.WikiDir() != dst || !wiki.Open(np.WikiDir()).Exists() {
		t.Errorf("wiki not at %s (%s)", dst, np.WikiDir())
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "wiki")); err == nil {
		t.Error("old wiki left")
	}
}

func TestWikiDirBusy(t *testing.T) {
	s, ts, p := setup(t)
	s.acp.mu.Lock()
	s.acp.sessions["x"] = &liveSession{project: p.Name, hpkID: "x", streaming: true}
	s.acp.mu.Unlock()
	res, _ := post(t, ts.URL+"/api/project/wiki-dir", map[string]string{"project": p.Name, "dir": filepath.Join(s.home, "w2")}, true)
	if res.StatusCode != 409 {
		t.Errorf("busy: %d", res.StatusCode)
	}
}

func TestRootAPI(t *testing.T) {
	s, ts, p := setup(t)
	if res, _ := post(t, ts.URL+"/api/project/root", map[string]string{"project": p.Name, "dir": filepath.Join(s.home, "nope")}, true); res.StatusCode != 400 {
		t.Errorf("missing dir: %d", res.StatusCode)
	}
	nd := filepath.Join(s.home, "code", "moved-app")
	os.MkdirAll(nd, 0o755)
	res, out := post(t, ts.URL+"/api/project/root", map[string]string{"project": p.Name, "dir": nd}, true)
	if res.StatusCode != 200 || out["ok"] != true {
		t.Fatalf("root: %d %v", res.StatusCode, out)
	}
	np, _ := project.Load(s.home, p.Name)
	if np.Root != nd {
		t.Errorf("root %s", np.Root)
	}
}
