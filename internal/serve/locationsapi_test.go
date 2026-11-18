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
	dst := filepath.Join(t.TempDir(), "sample-wiki")

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
	res, _ := post(t, ts.URL+"/api/project/wiki-dir", map[string]string{"project": p.Name, "dir": filepath.Join(t.TempDir(), "w2")}, true)
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

func TestForbiddenLocation(t *testing.T) {
	s, _, p := setup(t)
	other := filepath.Join(s.home, "code", "other-app")
	os.MkdirAll(other, 0o755)
	if _, err := project.Create(s.home, "other-app", other, project.Shared); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	trusted := filepath.Join(s.home, "hpk", "trusted")
	for name, c := range map[string]struct {
		dir  string
		wiki bool
	}{
		"home":          {s.home, true},
		"home root":     {s.home, false},
		"inside home":   {filepath.Join(s.home, "notes"), true},
		"trusted":       {trusted, true},
		"in trusted":    {filepath.Join(trusted, "x"), false},
		"above home":    {filepath.Dir(s.home), true},
		"above home 2":  {filepath.Dir(s.home), false},
		"other root":    {other, true},
		"in other root": {filepath.Join(other, "w"), true},
		"own root":      {p.Root, true},
	} {
		if err := forbiddenLocation(s.home, p, c.dir, c.wiki); err == nil {
			t.Errorf("%s: allowed", name)
		}
	}
	for name, c := range map[string]struct {
		dir  string
		wiki bool
	}{
		"outside wiki":   {outside, true},
		"default wiki":   {filepath.Join(p.Dir, "wiki"), true},
		"in own root":    {filepath.Join(p.Root, "docs", "wiki"), true},
		"root in home":   {filepath.Join(s.home, "code", "new-app"), false},
		"root elsewhere": {outside, false},
	} {
		if err := forbiddenLocation(s.home, p, c.dir, c.wiki); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestStillNeeded(t *testing.T) {
	s, _, p := setup(t)
	other := filepath.Join(s.home, "code", "other-app")
	os.MkdirAll(other, 0o755)
	if _, err := project.Create(s.home, "other-app", other, project.Shared); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if !stillNeeded(s.home, s.home, "") || !stillNeeded(s.home, other, "") {
		t.Error("home and things in it are needed")
	}
	if !stillNeeded(s.home, filepath.Join(other, "sub"), "") {
		t.Error("inside another project's root is needed")
	}
	if stillNeeded(s.home, outside, "") {
		t.Error("unrelated folder is not needed")
	}
	project.SetWikiLocation(s.home, "other-app", outside)
	if !stillNeeded(s.home, outside, "") {
		t.Error("another project's wiki is needed")
	}
	if stillNeeded(s.home, outside, "other-app/wiki") {
		t.Error("except is ignored")
	}
	_ = p
}

func TestWikiAndRootRefuseProtected(t *testing.T) {
	s, ts, p := setup(t)
	for _, c := range []struct{ path, dir string }{
		{"/api/project/wiki-dir", s.home},
		{"/api/project/wiki-dir", filepath.Join(s.home, "hpk", "trusted")},
		{"/api/project/wiki-dir", filepath.Dir(s.home)},
		{"/api/project/root", s.home},
		{"/api/project/root", filepath.Dir(s.home)},
	} {
		if res, _ := post(t, ts.URL+c.path, map[string]string{"project": p.Name, "dir": c.dir}, true); res.StatusCode != 400 {
			t.Errorf("%s %s: %d", c.path, c.dir, res.StatusCode)
		}
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "wiki", "SCHEMA.md")); err != nil {
		t.Error("wiki disturbed")
	}
}
