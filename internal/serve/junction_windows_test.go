// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package serve

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
}

// The AI (Modify on the project) swaps the rule folder for a junction to a
// folder outside the project: upload, list and file reads must not follow it.
func TestJunctionInProjectNotFollowed(t *testing.T) {
	s, ts, p := setup(t)
	approveRules(t, s.home, p) // Export holds .fbx
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "private.txt"), []byte("secret"), 0o644)
	os.RemoveAll(filepath.Join(p.Root, "Export"))
	junction(t, filepath.Join(p.Root, "Export"), outside)

	if res, out := uploadOne(t, ts.URL, "evil.fbx", "x"); res.StatusCode != 400 {
		t.Fatalf("upload: %d %v", res.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.fbx")); err == nil {
		t.Fatal("uploaded through the junction")
	}
	if res, out := tidyDo(t, "GET", ts.URL+"/api/files/list?project=sample-app&path=Export", nil); res.StatusCode != 400 {
		t.Fatalf("list: %d %v", res.StatusCode, out)
	}
	// the root listing does not show the junction either
	_, out := tidyDo(t, "GET", ts.URL+"/api/files/list?project=sample-app", nil)
	for _, e := range out["entries"].([]any) {
		if e.(map[string]any)["name"] == "Export" {
			t.Fatal("the junction is listed")
		}
	}
	res, _ := tidyDo(t, "GET", ts.URL+"/api/files?project=sample-app&path=Export/private.txt", nil)
	if res.StatusCode != 404 {
		t.Fatalf("read through the junction: %d", res.StatusCode)
	}
}
