// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import "testing"

// A file that fails part-way through still reports the files saved before it.
func TestUploadPartialReportsSaved(t *testing.T) {
	_, ts, _ := setup(t)
	b, ct := multipartBody([]part{{"one.txt", "a"}, {"bad\x1bname.txt", "b"}})
	res, out := upload(t, ts.URL, b, ct)
	if res.StatusCode != 400 || out["error"] == nil {
		t.Fatalf("%d %v", res.StatusCode, out)
	}
	if fs, _ := out["files"].([]any); len(fs) != 1 || fs[0].(map[string]any)["name"] != "one.txt" {
		t.Fatalf("files = %v", out["files"])
	}
}
