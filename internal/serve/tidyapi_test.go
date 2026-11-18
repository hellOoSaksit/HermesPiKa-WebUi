// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"hpk.local/hpk/internal/folders"
	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

func tidyDo(t *testing.T, method, url string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, url, rd)
	req.Header.Set("X-HPK", "1")
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res, out
}

func TestTidyAPIRejectsForeignMoves(t *testing.T) {
	s, ts, p := setup(t)
	approveRules(t, s.home, p)
	os.WriteFile(filepath.Join(p.Root, "a.fbx"), []byte("x"), 0o644)
	res, out := tidyDo(t, "POST", ts.URL+"/api/files/tidy", map[string]any{
		"project": "sample-app",
		"moves":   []map[string]any{{"from": "a.fbx", "to": "../../etc/a.fbx"}},
	})
	if res.StatusCode != 400 {
		t.Fatalf("%d %v", res.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(p.Root, "a.fbx")); err != nil {
		t.Fatal("file moved")
	}
}

func TestTidyAPIPlanApplyUndo(t *testing.T) {
	s, ts, p := setup(t)
	if res, out := tidyDo(t, "GET", ts.URL+"/api/files/tidy?project=sample-app", nil); res.StatusCode != 409 || out["state"] != "none" {
		t.Fatalf("%d %v", res.StatusCode, out)
	}
	approveRules(t, s.home, p)
	os.WriteFile(filepath.Join(p.Root, "a.fbx"), []byte("x"), 0o644)
	res, plan := tidyDo(t, "GET", ts.URL+"/api/files/tidy?project=sample-app", nil)
	if res.StatusCode != 200 || plan["canUndo"] != false {
		t.Fatalf("%d %v", res.StatusCode, plan)
	}
	res, out := tidyDo(t, "POST", ts.URL+"/api/files/tidy", map[string]any{"project": "sample-app", "moves": plan["moves"]})
	if res.StatusCode != 200 || len(out["done"].([]any)) != 1 {
		t.Fatalf("%d %v", res.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(p.Root, "Export", "a.fbx")); err != nil {
		t.Fatal(err)
	}
	if _, plan = tidyDo(t, "GET", ts.URL+"/api/files/tidy?project=sample-app", nil); plan["canUndo"] != true {
		t.Fatalf("%v", plan)
	}
	// the record is in the person's private folder, not beside project.yaml
	if _, err := os.Stat(folders.UndoFile(isolate.PrivateDir(s.home), "sample-app")); err != nil {
		t.Fatal(err)
	}
	// undo needs the list the person saw: without it nothing moves
	if res, out = tidyDo(t, "POST", ts.URL+"/api/files/tidy/undo", map[string]any{"project": "sample-app"}); res.StatusCode != 400 {
		t.Fatalf("%d %v", res.StatusCode, out)
	}
	res, list := tidyDo(t, "GET", ts.URL+"/api/files/tidy/undo?project=sample-app", nil)
	if res.StatusCode != 200 || len(list["moves"].([]any)) != 1 {
		t.Fatalf("%d %v", res.StatusCode, list)
	}
	res, out = tidyDo(t, "POST", ts.URL+"/api/files/tidy/undo", map[string]any{"project": "sample-app", "at": list["at"]})
	if res.StatusCode != 200 || len(out["done"].([]any)) != 1 {
		t.Fatalf("%d %v", res.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(p.Root, "a.fbx")); err != nil {
		t.Fatal(err)
	}
}

// An undo record the AI writes beside project.yaml (its old place) does nothing.
func TestTidyUndoIgnoresPlantedRecord(t *testing.T) {
	s, ts, p := setup(t)
	approveRules(t, s.home, p)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "startup.js"), []byte("x"), 0o644)
	planted := `{"at":"2026-01-01T00:00:00Z","root":` + strconv.Quote(p.Root) + `,"moves":[{"from":"startup.js","to":"x.js","size":1,"mtime":"2026-01-01T00:00:00Z"}]}`
	os.WriteFile(filepath.Join(p.Dir, "tidy-last.json"), []byte(planted), 0o644)
	if res, plan := tidyDo(t, "GET", ts.URL+"/api/files/tidy?project=sample-app", nil); res.StatusCode != 200 || plan["canUndo"] != false {
		t.Fatalf("%d %v", res.StatusCode, plan)
	}
	if res, out := tidyDo(t, "GET", ts.URL+"/api/files/tidy/undo?project=sample-app", nil); res.StatusCode != 409 {
		t.Fatalf("%d %v", res.StatusCode, out)
	}
}

// A project.yaml root the AI pointed at a folder it was not given is refused
// for upload, list and tidy while the AI is isolated.
func TestRootOutsideSharedFoldersRefused(t *testing.T) {
	s, ts, p := setup(t)
	approveRules(t, s.home, p)
	cfg := filepath.Join(t.TempDir(), "isolation.json")
	raw, _ := json.Marshal(map[string]any{"mode": "user", "user": "hpk-ai", "home": s.home, "private": t.TempDir(), "granted": []string{s.home}})
	os.WriteFile(cfg, raw, 0o600)
	t.Setenv("HPK_ISOLATION_FILE", cfg)
	// the project's own folder is inside the home: fine
	if res, out := tidyDo(t, "GET", ts.URL+"/api/files/list?project=sample-app", nil); res.StatusCode != 200 {
		t.Fatalf("%d %v", res.StatusCode, out)
	}
	elsewhere := t.TempDir()
	os.WriteFile(filepath.Join(elsewhere, "a.fbx"), []byte("x"), 0o644)
	if err := project.SetField(s.home, p.Name, []string{"root"}, elsewhere, "the AI edits project.yaml"); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"/api/files/list?project=sample-app", "/api/files/tidy?project=sample-app"} {
		if res, out := tidyDo(t, "GET", ts.URL+u, nil); res.StatusCode != 400 {
			t.Fatalf("%s: %d %v", u, res.StatusCode, out)
		}
	}
	b, ct := multipartBody([]part{{"b.fbx", "x"}})
	req, _ := http.NewRequest("POST", ts.URL+"/api/files/upload?project=sample-app", b)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-HPK", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("upload: %d", res.StatusCode)
	}
	if ents, _ := os.ReadDir(elsewhere); len(ents) != 1 {
		t.Fatal("wrote into a folder the AI named")
	}
}

func TestRulesRouteStates(t *testing.T) {
	s, ts, p := setup(t)
	get := func() map[string]any {
		t.Helper()
		res, out := tidyDo(t, "GET", ts.URL+"/api/files/rules?project=sample-app", nil)
		if res.StatusCode != 200 {
			t.Fatalf("%d %v", res.StatusCode, out)
		}
		return out
	}
	if out := get(); out["state"] != "none" || len(out["rows"].([]any)) != 0 {
		t.Fatalf("%v", out)
	}
	w := wiki.Open(p.WikiDir())
	d, err := w.CreateDraft(wiki.DraftInput{Target: folders.Page, Title: "Folders", Sources: []string{"chat"}, Summary: "where files go", Author: "me", Body: folderTable})
	if err != nil {
		t.Fatal(err)
	}
	if out := get(); out["state"] != "pending" {
		t.Fatalf("%v", out)
	}
	res, err := w.Approve([]int{d.N}, "me")
	if err != nil {
		t.Fatal(err)
	}
	if err := folders.AfterApprove(s.home, p, res); err != nil {
		t.Fatal(err)
	}
	out := get()
	if out["state"] != "ok" {
		t.Fatalf("%v", out)
	}
	rows := out["rows"].([]any)
	row := rows[0].(map[string]any)
	if len(rows) != 1 || row["folder"] != "Export" || row["what"] != "models" || row["exts"].([]any)[0] != ".fbx" {
		t.Fatalf("%v", out)
	}
	page := filepath.Join(p.WikiDir(), filepath.FromSlash(folders.Page))
	raw, _ := os.ReadFile(page)
	os.WriteFile(page, append(raw, []byte("| Bad | x |\n")...), 0o644)
	if out := get(); out["state"] != "changed" || len(out["bad"].([]any)) != 1 {
		t.Fatalf("%v", out)
	}
}
