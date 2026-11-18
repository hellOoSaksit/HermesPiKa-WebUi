// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hpk.local/hpk/internal/acp"
	"hpk.local/hpk/internal/project"
)

func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET %s: %d", url, res.StatusCode)
	}
	json.NewDecoder(res.Body).Decode(out)
}

func TestGatesPage(t *testing.T) {
	_, ts, p := setup(t)
	var g struct {
		RedLines []struct {
			ID, Key, Text string
			Locked        bool
		}
		Audit []map[string]any
	}
	getJSON(t, ts.URL+"/api/gates?project=sample-app", &g)
	if len(g.RedLines) != 8 || !g.RedLines[0].Locked {
		t.Fatalf("built-in lines: %+v", g.RedLines)
	}
	if res, _ := post(t, ts.URL+"/api/gates/redlines", map[string]any{"project": "sample-app", "add": "re:^terraform apply"}, true); res.StatusCode != 200 {
		t.Fatalf("add: %d", res.StatusCode)
	}
	getJSON(t, ts.URL+"/api/gates?project=sample-app", &g)
	if len(g.RedLines) != 9 || g.RedLines[8].Text != "re:^terraform apply" || g.RedLines[8].Locked {
		t.Fatalf("after add: %+v", g.RedLines)
	}
	if len(g.Audit) == 0 || !strings.Contains(g.Audit[0]["target"].(string), "red lines") {
		t.Fatalf("audit should show the change: %+v", g.Audit)
	}
	post(t, ts.URL+"/api/mode", map[string]any{"project": "sample-app", "value": "manual"}, true)
	getJSON(t, ts.URL+"/api/gates?project=sample-app", &g)
	modeRows := 0
	for _, a := range g.Audit {
		if strings.Contains(a["target"].(string), "manual") {
			modeRows++
		}
	}
	if modeRows != 1 {
		t.Fatalf("a mode change must show once, got %d: %+v", modeRows, g.Audit)
	}
	if res, _ := post(t, ts.URL+"/api/gates/redlines", map[string]any{"project": "sample-app", "remove": "r-push"}, true); res.StatusCode == 200 {
		t.Fatal("a built-in red line was removed")
	}
	post(t, ts.URL+"/api/gates/redlines", map[string]any{"project": "sample-app", "remove": "x-0"}, true)
	var extra []string
	project.Field(p, []string{"gates", "red_lines_extra"}, &extra)
	if len(extra) != 0 {
		t.Fatalf("remove: %v", extra)
	}
}

func TestPrivacyNeedsConfirmToOpenUp(t *testing.T) {
	s, ts, _ := setup(t)
	post(t, ts.URL+"/api/privacy", map[string]any{"project": "sample-app", "mode": "isolated"}, true)
	if res, _ := post(t, ts.URL+"/api/privacy", map[string]any{"project": "sample-app", "mode": "shared"}, true); res.StatusCode == 200 {
		t.Fatal("opening up without confirm must fail")
	}
	post(t, ts.URL+"/api/privacy", map[string]any{"project": "sample-app", "mode": "shared", "confirm": true, "modelPolicy": "local_only"}, true)
	p, _ := project.Load(s.home, "sample-app")
	if p.Mode() != "shared" || p.Privacy.ModelPolicy != "local_only" {
		t.Fatalf("privacy: %s %s", p.Mode(), p.Privacy.ModelPolicy)
	}
}

func TestJobsAndSettings(t *testing.T) {
	s, ts, p := setup(t)
	var j struct {
		Jobs []struct {
			Key        string
			Installed  bool
			Paused     bool
			LastStatus string
		}
		InstallCmd string
	}
	getJSON(t, ts.URL+"/api/jobs?project=sample-app", &j)
	if len(j.Jobs) != 3 || j.Jobs[0].Installed || j.Jobs[0].LastStatus != "none" || !strings.Contains(j.InstallCmd, "sample-app") {
		t.Fatalf("jobs: %+v", j)
	}
	post(t, ts.URL+"/api/jobs/nightly/pause", map[string]any{"project": "sample-app", "paused": true}, true)
	getJSON(t, ts.URL+"/api/jobs?project=sample-app", &j)
	if !j.Jobs[0].Paused {
		t.Fatal("pause did not stick")
	}
	if res, _ := post(t, ts.URL+"/api/project/settings", map[string]any{"project": "sample-app", "key": "mode", "value": "yolo"}, true); res.StatusCode == 200 {
		t.Fatal("bad mode accepted")
	}
	if res, _ := post(t, ts.URL+"/api/project/settings", map[string]any{"project": "sample-app", "key": "model", "value": "x"}, true); res.StatusCode == 200 {
		t.Fatal("model is a red line; the Console must not change it")
	}
	post(t, ts.URL+"/api/project/settings", map[string]any{"project": "sample-app", "key": "nightlyMax", "value": 5000}, true)
	var n int
	project.Field(p, []string{"budget", "nightly_max_tokens"}, &n)
	if n != 5000 {
		t.Fatalf("nightly budget: %d", n)
	}
	_ = s
}

func TestAddProjectFromConsole(t *testing.T) {
	s, ts, _ := setup(t)
	root := filepath.Join(s.home, "code", "new-app")
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("# New app\nSee [setup](docs/setup.md).\n"), 0o644)
	os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=do-not-read\n"), 0o600)
	res, out := post(t, ts.URL+"/api/projects/scan", map[string]any{"path": root, "depth": "full"}, true)
	if res.StatusCode != 200 {
		t.Fatalf("scan: %d %v", res.StatusCode, out)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "do-not-read") || !strings.Contains(string(raw), ".env") {
		t.Fatalf("secrets must be listed as skipped, never read: %s", raw)
	}
	if h, _ := out["health"].([]any); len(h) == 0 {
		t.Fatalf("broken link should be found: %s", raw)
	}
	if res, _ := post(t, ts.URL+"/api/projects", map[string]any{"path": root, "depth": "light", "privacy": "private"}, true); res.StatusCode == 200 {
		t.Fatal("add without consent must fail")
	}
	res, out = post(t, ts.URL+"/api/projects", map[string]any{"path": root, "depth": "light", "privacy": "private", "consent": true}, true)
	if res.StatusCode != 200 || out["name"] != "new-app" {
		t.Fatalf("add: %d %v", res.StatusCode, out)
	}
	p, err := project.Load(s.home, "new-app")
	if err != nil || p.Mode() != "private" {
		t.Fatalf("project: %v %v", p, err)
	}
}

func TestLocalOnlyRefusesRemoteSessions(t *testing.T) {
	s, ts, p := setup(t)
	t.Setenv("HERMES_HOME", "")
	os.WriteFile(filepath.Join(s.home, "config.yaml"), []byte("model:\n  default: big\n  base_url: https://api.example.com/v1\n"), 0o644)
	project.SetField(s.home, p.Name, []string{"privacy", "model_policy"}, "local_only", "test")
	launched := false
	s.acp.launch = func(p *project.Project, h acp.Handlers) (*acp.Client, error) {
		launched = true
		return fakeLauncher(p, h)
	}
	res, out := post(t, ts.URL+"/api/sessions", map[string]any{"project": "sample-app", "name": "try", "start": true, "firstMessage": "hi"}, true)
	if res.StatusCode == 200 || launched || !strings.Contains(fmt.Sprint(out["error"]), "api.example.com") {
		t.Fatalf("a remote model must not start: %d launched=%v %v", res.StatusCode, launched, out)
	}
	if r := snapshot(t, ts).Projects[0].ModelRoute; r == nil || r.Local || r.Where != "api.example.com" {
		t.Fatalf("snapshot route: %+v", r)
	}
	os.WriteFile(filepath.Join(s.home, "config.yaml"), []byte("model:\n  default: qwen\n  base_url: http://127.0.0.1:11434/v1\n"), 0o644)
	res, out = post(t, ts.URL+"/api/sessions", map[string]any{"project": "sample-app", "name": "try2", "start": true, "firstMessage": "hi"}, true)
	if res.StatusCode != 200 || !launched {
		t.Fatalf("a local model may start: %d %v", res.StatusCode, out)
	}
}
