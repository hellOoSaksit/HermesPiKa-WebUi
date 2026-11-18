// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/team"
)

type dirsOut struct {
	Path   string `json:"path"`
	Parent string `json:"parent"`
	Dirs   []struct{ Name, Path string }
	Places []struct{ Label, Path string }
}

func getDirs(t *testing.T, base, path string) (int, dirsOut) {
	t.Helper()
	res, err := http.Get(base + "/api/fs/dirs?path=" + url.QueryEscape(path))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out dirsOut
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestDirsNeedsConsoleKey(t *testing.T) {
	home := t.TempDir()
	s := NewServer(Options{Home: home})
	ts := httptest.NewServer(s.Routes(fstest.MapFS{"index.html": {Data: []byte("x")}}))
	defer ts.Close()
	s.keys = newConsoleKeys(home)
	res, err := http.Get(ts.URL + "/api/fs/dirs?path=" + url.QueryEscape(home))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 || res.Header.Get("X-HPK-Need-Key") == "" {
		t.Fatalf("no key: %d %v", res.StatusCode, res.Header)
	}
}

func TestDirsListsFoldersOnly(t *testing.T) {
	_, ts, _ := setup(t)
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "a"), 0o755)
	os.MkdirAll(filepath.Join(d, "B"), 0o755)
	os.MkdirAll(filepath.Join(d, ".hid"), 0o755)
	os.WriteFile(filepath.Join(d, "b.txt"), []byte("x"), 0o644)
	code, out := getDirs(t, ts.URL, d)
	if code != 200 || len(out.Dirs) != 2 || out.Dirs[0].Name != "a" || out.Dirs[1].Name != "B" {
		t.Fatalf("dirs: %d %+v", code, out.Dirs)
	}
	if out.Parent != filepath.Dir(d) {
		t.Errorf("parent %q", out.Parent)
	}
	if code, out := getDirs(t, ts.URL, ""); code != 200 || len(out.Places) == 0 || len(out.Dirs) != 0 {
		t.Errorf("places: %d %+v", code, out)
	}
	if code, _ := getDirs(t, ts.URL, "relative"); code != 400 {
		t.Errorf("relative: %d", code)
	}
	if code, _ := getDirs(t, ts.URL, filepath.Join(d, "b.txt")); code != 400 {
		t.Errorf("file: %d", code)
	}
}

func TestDirsHidesPrivate(t *testing.T) {
	s, ts, _ := setup(t)
	os.MkdirAll(isolate.PrivateDir(s.home), 0o755)
	os.MkdirAll(filepath.Join(s.home, "code", "x"), 0o755)
	_, out := getDirs(t, ts.URL, s.home)
	names := ""
	for _, d := range out.Dirs {
		names += d.Name + ","
	}
	if strings.Contains(names, "hpk,") || !strings.Contains(names, "code,") {
		t.Errorf("home lists: %s", names)
	}
	os.MkdirAll(filepath.Join(s.home, "hpk", "trusted"), 0o755)
	os.MkdirAll(filepath.Join(s.home, "hpk", "other"), 0o755)
	if code, _ := getDirs(t, ts.URL, filepath.Join(s.home, "hpk", "trusted")); code != 403 {
		t.Errorf("trusted opened: %d", code)
	}
}

func TestDirsTeamMode404(t *testing.T) {
	s, ts, _ := setup(t)
	c := &team.Config{Enabled: true}
	tok, _ := c.Add("olivia", "", team.Owner, nil, time.Now())
	if err := c.Save(s.home); err != nil {
		t.Fatal(err)
	}
	code, _ := as(t, http.DefaultClient, "GET", ts.URL+"/api/fs/dirs?path="+url.QueryEscape(s.home), tok, nil)
	if code != 404 {
		t.Fatalf("team mode: %d", code)
	}
}
