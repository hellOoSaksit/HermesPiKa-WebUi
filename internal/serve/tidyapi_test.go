// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
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
		"moves":   []map[string]any{{"From": "a.fbx", "To": "../../etc/a.fbx"}},
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
	res, out := tidyDo(t, "POST", ts.URL+"/api/files/tidy", map[string]any{"project": "sample-app", "moves": plan["Moves"]})
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
