// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"hpk.local/hpk/internal/folders"
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
	res, out = tidyDo(t, "POST", ts.URL+"/api/files/tidy/undo", map[string]any{"project": "sample-app"})
	if res.StatusCode != 200 || len(out["done"].([]any)) != 1 {
		t.Fatalf("%d %v", res.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(p.Root, "a.fbx")); err != nil {
		t.Fatal(err)
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
