// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"net/http"
	"testing"
	"time"

	"hpk.local/hpk/internal/team"
)

func TestFeatureTeamGateInSoloMode(t *testing.T) {
	t.Setenv("HPK_FEATURES", "")
	_, ts, _ := setup(t)
	c := http.DefaultClient
	for _, r := range [][2]string{
		{"POST", "/api/join"}, {"POST", "/api/teamserver/join"}, {"POST", "/api/team/join-code"},
		{"GET", "/api/team"}, {"GET", "/api/team/devices"}, {"POST", "/api/team/devices/revoke"},
		{"POST", "/api/team/invite"}, {"POST", "/api/team/role"}, {"POST", "/api/team/remove"},
		{"POST", "/api/team/budget"}, {"POST", "/api/team/rotate"},
	} {
		code, out := as(t, c, r[0], ts.URL+r[1], "", map[string]any{})
		if code != 404 || out["error"] != "team features come in a later version" {
			t.Errorf("%s %s: %d %v", r[0], r[1], code, out)
		}
	}
	for _, r := range [][2]string{{"POST", "/api/teamserver/leave"}, {"GET", "/api/teamserver"}} {
		if code, _ := as(t, c, r[0], ts.URL+r[1], "", map[string]any{}); code == 404 {
			t.Errorf("%s %s must stay open", r[0], r[1])
		}
	}
	_, snap := as(t, c, "GET", ts.URL+"/api/snapshot", "", nil)
	if f, _ := snap["features"].(map[string]any); f == nil || f["team"] != false {
		t.Errorf("snapshot features: %v", snap["features"])
	}
}

func TestFeatureTeamEnvTurnsItBackOn(t *testing.T) {
	t.Setenv("HPK_FEATURES", "team")
	_, ts, _ := setup(t)
	if code, _ := as(t, http.DefaultClient, "GET", ts.URL+"/api/team", "", nil); code != 200 {
		t.Fatalf("HPK_FEATURES=team brings /api/team back: %d", code)
	}
	_, snap := as(t, http.DefaultClient, "GET", ts.URL+"/api/snapshot", "", nil)
	if f, _ := snap["features"].(map[string]any); f == nil || f["team"] != true {
		t.Errorf("snapshot features: %v", snap["features"])
	}
}

func TestFeatureTeamModeServerUnaffected(t *testing.T) {
	t.Setenv("HPK_FEATURES", "")
	s, ts, _ := setup(t)
	c := &team.Config{Enabled: true}
	tok, _ := c.Add("olivia", "", team.Owner, nil, time.Now())
	if err := c.Save(s.home); err != nil {
		t.Fatal(err)
	}
	if code, _ := as(t, http.DefaultClient, "GET", ts.URL+"/api/team", tok, nil); code != 200 {
		t.Fatalf("team mode is unaffected: %d", code)
	}
}
