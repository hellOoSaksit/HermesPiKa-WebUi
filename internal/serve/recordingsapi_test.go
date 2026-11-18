// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"hpk.local/hpk/internal/browser"
	"hpk.local/hpk/internal/isolate"
)

func TestRecordingsDirRules(t *testing.T) {
	s, ts, p := setup(t)
	t.Setenv("HPK_DATA_DIR", t.TempDir())
	priv := filepath.Join(t.TempDir(), "private")
	cfg := filepath.Join(t.TempDir(), "isolation.json")
	t.Setenv("HPK_ISOLATION_FILE", cfg)
	raw, _ := json.Marshal(map[string]string{"mode": "docker", "home": s.home, "private": priv})
	os.WriteFile(cfg, raw, 0o600)

	url := ts.URL + "/api/browser/recordings-dir"
	for name, dir := range map[string]string{
		"under the Hermes home": filepath.Join(s.home, "elsewhere", "runs"),
		"under a project root":  filepath.Join(p.Root, "recordings"),
		"holding the home":      filepath.Dir(s.home),
		"relative":              "runs",
	} {
		if res, _ := post(t, url, map[string]string{"dir": dir}, true); res.StatusCode != 400 {
			t.Errorf("%s: %d", name, res.StatusCode)
		}
	}

	oldRuns := browser.RunsDir(s.home)
	id := "20260101-120000-abcdef"
	os.MkdirAll(filepath.Join(oldRuns, id), 0o700)
	os.WriteFile(filepath.Join(oldRuns, id, "run.json"), []byte(`{"id":"`+id+`"}`), 0o600)

	_, m, _ := s.openPersonal()
	m.MarkRunning(true)
	dst := filepath.Join(t.TempDir(), "my-recordings")
	if res, _ := post(t, url, map[string]string{"dir": dst}, true); res.StatusCode != 409 {
		t.Errorf("running: %d", res.StatusCode)
	}
	m.MarkRunning(false)

	res, out := post(t, url, map[string]string{"dir": dst}, true)
	if res.StatusCode != 200 || out["ok"] != true || out["dir"] != dst {
		t.Fatalf("move: %d %v", res.StatusCode, out)
	}
	if browser.RunsDir(s.home) != dst {
		t.Errorf("RunsDir %s", browser.RunsDir(s.home))
	}
	if _, err := os.Stat(filepath.Join(dst, id, "run.json")); err != nil {
		t.Errorf("recording not moved: %v", err)
	}
	if _, err := os.Stat(oldRuns); err == nil {
		t.Error("old recordings left")
	}

	res, out = post(t, url, map[string]string{"dir": ""}, true)
	if res.StatusCode != 200 || browser.RunsDir(s.home) != filepath.Join(priv, "browser", "runs") {
		t.Fatalf("reset: %d %v %s", res.StatusCode, out, browser.RunsDir(s.home))
	}
	if _, err := os.Stat(filepath.Join(priv, "browser", "runs", id, "run.json")); err != nil {
		t.Errorf("recording not back: %v", err)
	}
}

// Isolated, a recordings folder that cannot be made private is not chosen:
// the error comes back and the setting stays as it was.
func TestRecordingsDirMakePrivateFailureSurfaces(t *testing.T) {
	s, ts, _ := setup(t)
	t.Setenv("HPK_DATA_DIR", t.TempDir())
	cfg := filepath.Join(t.TempDir(), "isolation.json")
	t.Setenv("HPK_ISOLATION_FILE", cfg)
	raw, _ := json.Marshal(map[string]string{"mode": "docker", "home": s.home, "private": filepath.Join(t.TempDir(), "private")})
	os.WriteFile(cfg, raw, 0o600)
	var asked string
	makePrivate = func(dir string) error { asked = dir; return errors.New("access denied") }
	t.Cleanup(func() { makePrivate = isolate.MakePrivate })
	before := browser.RunsDir(s.home)
	dst := filepath.Join(t.TempDir(), "my-recordings")
	res, out := post(t, ts.URL+"/api/browser/recordings-dir", map[string]string{"dir": dst}, true)
	if res.StatusCode != 500 || asked != dst {
		t.Fatalf("%d %v (asked %q)", res.StatusCode, out, asked)
	}
	if browser.RunsDir(s.home) != before {
		t.Fatalf("the setting was saved: %s", browser.RunsDir(s.home))
	}
}
