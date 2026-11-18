// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"testing"
	"time"

	"hpk.local/hpk/internal/gates"
)

type gitModel struct {
	Live     bool   `json:"live"`
	Head     string `json:"head"`
	Commits  []any  `json:"commits"`
	Branches []struct {
		Name string `json:"name"`
		Tip  string `json:"tip"`
	} `json:"branches"`
	Undo []struct {
		ID string `json:"id"`
	} `json:"undo"`
}

func TestGitAPI(t *testing.T) {
	_, ts, p := setup(t)
	var m gitModel
	getJSON(t, ts.URL+"/api/git?project=sample-app", &m)
	if !m.Live || m.Head == "" || len(m.Commits) != 1 || len(m.Branches) != 1 {
		t.Fatalf("the repository is read: %+v", m)
	}

	// writes need the Console's header
	if res, _ := post(t, ts.URL+"/api/git/action", map[string]any{"project": "sample-app", "action": "branch", "name": "feat/a"}, false); res.StatusCode == 200 {
		t.Fatal("a POST without X-HPK is refused")
	}
	if res, out := post(t, ts.URL+"/api/git/action", map[string]any{"project": "sample-app", "action": "branch", "name": "feat/a"}, true); res.StatusCode != 200 {
		t.Fatalf("branch: %d %v", res.StatusCode, out)
	}
	getJSON(t, ts.URL+"/api/git?project=sample-app", &m)
	if len(m.Branches) != 2 {
		t.Fatalf("the new branch shows: %+v", m.Branches)
	}

	// git's refusal comes back with the commands that ran
	res, out := post(t, ts.URL+"/api/git/action", map[string]any{"project": "sample-app", "action": "branch", "name": "feat/a"}, true)
	if res.StatusCode != 400 || out["error"] == nil {
		t.Fatalf("a duplicate branch is refused: %d %v", res.StatusCode, out)
	}

	// deleting is a red line: recorded as a person's decision, with an undo point
	if res, out := post(t, ts.URL+"/api/git/action", map[string]any{"project": "sample-app", "action": "delete", "branch": "feat/a"}, true); res.StatusCode != 200 {
		t.Fatalf("delete: %d %v", res.StatusCode, out)
	}
	ds, _ := gates.Decisions(p, time.Now().Add(-time.Hour))
	found := false
	for _, d := range ds {
		if d.Tool == "git" && d.Outcome == "approved" && len(d.Lines) == 1 && d.Lines[0] == "delete" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the red line is recorded: %+v", ds)
	}
	getJSON(t, ts.URL+"/api/git?project=sample-app", &m)
	if len(m.Branches) != 1 || len(m.Undo) != 1 {
		t.Fatalf("deleted, undo point kept: %+v", m)
	}
	if res, out := post(t, ts.URL+"/api/git/action", map[string]any{"project": "sample-app", "action": "undo", "undo": m.Undo[0].ID}, true); res.StatusCode != 200 {
		t.Fatalf("undo: %d %v", res.StatusCode, out)
	}
	getJSON(t, ts.URL+"/api/git?project=sample-app", &m)
	if len(m.Branches) != 2 {
		t.Fatalf("undo brings the branch back: %+v", m.Branches)
	}
}
