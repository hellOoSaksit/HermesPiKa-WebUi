// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package team

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRolesAndActions(t *testing.T) {
	cases := []struct {
		role, action string
		want         bool
	}{
		{Viewer, View, true}, {Viewer, Work, false},
		{Member, Work, true}, {Member, ApproveDraft, true}, {Member, ApproveCore, false}, {Member, RedLine, false},
		{Maintainer, ApproveCore, true}, {Maintainer, RedLine, true}, {Maintainer, Admin, false},
		{Owner, Admin, true}, {Owner, People, true}, {Owner, "made-up", false},
	}
	for _, c := range cases {
		if got := Can(c.role, c.action); got != c.want {
			t.Errorf("Can(%s, %s) = %v", c.role, c.action, got)
		}
	}
}

func TestTokensAndOwners(t *testing.T) {
	home := t.TempDir()
	if c, err := Load(home); c != nil || err != nil {
		t.Fatal("no team.yaml is solo mode")
	}
	c := &Config{Enabled: true}
	tok, err := c.Add("alice@example.com", "Alice", Owner, nil, time.Now())
	if err != nil || !strings.HasPrefix(tok, "hpk_") {
		t.Fatalf("owner token: %q %v", tok, err)
	}
	bobTok, _ := c.Add("bob", "", Member, []string{"sample-app"}, time.Now())
	if err := c.Save(home); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(File(home))
	if strings.Contains(string(raw), tok) || !strings.Contains(string(raw), HashToken(tok)) {
		t.Fatal("only the token hash may be stored")
	}
	c, _ = Load(home)
	if m, ok := c.ByToken(bobTok); !ok || m.ID != "bob" || !m.Sees("sample-app") || m.Sees("other") {
		t.Fatalf("bob: %+v %v", m, ok)
	}
	if _, ok := c.ByToken("hpk_nope"); ok {
		t.Fatal("unknown token")
	}
	if err := c.Remove("alice@example.com"); err == nil {
		t.Fatal("the last owner cannot be removed")
	}
	if err := c.SetRole("alice@example.com", Member); err == nil {
		t.Fatal("the last owner keeps the role")
	}
	newTok, _ := c.Rotate("bob")
	if _, ok := c.ByToken(bobTok); ok {
		t.Fatal("a rotated token stops working")
	}
	if _, ok := c.ByToken(newTok); !ok {
		t.Fatal("the new token works")
	}
	if _, err := c.Add("eve; rm -rf", "", Member, nil, time.Now()); err == nil {
		t.Fatal("ids are emails or user names")
	}
	h := &Config{Enabled: true, Auth: "header"}
	h.Members = []Person{{ID: "a", Role: Owner}}
	if err := h.Save(home); err == nil {
		t.Fatal("header mode needs a header and trusted proxies")
	}
}

func TestWideProxies(t *testing.T) {
	c := &Config{TrustedProxies: []string{"10.77.0.10", "10.77.0.0/24", "10.77.0.11/32", "::1", "fd00::/64"}}
	if got := strings.Join(c.WideProxies(), ","); got != "10.77.0.0/24,fd00::/64" {
		t.Fatalf("wide: %s", got)
	}
}
