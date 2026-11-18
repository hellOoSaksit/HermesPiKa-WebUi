// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package team

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func teamOf(t *testing.T) *Config {
	t.Helper()
	c := &Config{Enabled: true, Auth: "token"}
	if _, err := c.Add("olivia@example.com", "Olivia", Owner, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Add("max@example.com", "Max", Member, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestInviteJoinOnce(t *testing.T) {
	home := t.TempDir()
	c := teamOf(t)
	d, err := LoadDevices(home)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, _, err := d.NewInvite(c, "nobody@example.com", "olivia@example.com", now); err == nil {
		t.Fatal("invite for someone not in the team")
	}
	code, exp, err := d.NewInvite(c, "max@example.com", "olivia@example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Sub(now) != InviteTTL || len(code) != 11 || code[5] != '-' {
		t.Fatalf("code %q expires %v", code, exp)
	}
	raw, _ := os.ReadFile(DevicesFile(home))
	if strings.Contains(string(raw), code) {
		t.Fatal("devices.json holds the code itself")
	}
	// Typed in lower case without the dash still works.
	dev, token, err := d.Join(c, strings.ToLower(strings.ReplaceAll(code, "-", " ")), "Max's laptop", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, DevicePrefix) || dev.Member != "max@example.com" || dev.Name != "Max's laptop" {
		t.Fatalf("dev %+v token %q", dev, token)
	}
	raw, _ = os.ReadFile(DevicesFile(home))
	if strings.Contains(string(raw), token) || !strings.Contains(string(raw), HashToken(token)) {
		t.Fatal("devices.json must hold only the token's SHA-256")
	}
	if _, _, err := d.Join(c, code, "again", now.Add(2*time.Minute)); err == nil {
		t.Fatal("a code works twice")
	}
	// The token signs in as Max and records last seen.
	m, got, ok := d.ByToken(c, token, "203.0.113.5", now.Add(3*time.Minute))
	if !ok || m.ID != "max@example.com" || got.ID != dev.ID {
		t.Fatal("device token does not sign in")
	}
	if ls := d.For("max@example.com"); len(ls) != 1 || ls[0].LastIP != "203.0.113.5" || ls[0].LastSeen.IsZero() {
		t.Fatalf("list %+v", ls)
	}
	// Reload from disk: same device.
	d2, _ := LoadDevices(home)
	if _, _, ok := d2.ByToken(c, token, "", now); !ok {
		t.Fatal("device lost on reload")
	}
}

func TestInviteExpires(t *testing.T) {
	c := teamOf(t)
	d, _ := LoadDevices(t.TempDir())
	now := time.Now()
	code, _, _ := d.NewInvite(c, "max@example.com", "olivia@example.com", now)
	if _, _, err := d.Join(c, code, "late", now.Add(InviteTTL+time.Second)); err == nil {
		t.Fatal("an expired code works")
	}
	if _, _, err := d.Join(c, "ABCDE-FGHJK", "guess", now); err == nil {
		t.Fatal("a made-up code works")
	}
}

func TestDeviceRevoke(t *testing.T) {
	c := teamOf(t)
	d, _ := LoadDevices(t.TempDir())
	now := time.Now()
	join := func(who, name string) (Device, string) {
		code, _, err := d.NewInvite(c, who, "olivia@example.com", now)
		if err != nil {
			t.Fatal(err)
		}
		dev, tok, err := d.Join(c, code, name, now)
		if err != nil {
			t.Fatal(err)
		}
		return dev, tok
	}
	laptop, tl := join("max@example.com", "laptop")
	desk, td := join("max@example.com", "desk")
	_, to := join("olivia@example.com", "olivia-pc")
	if n := len(d.For("max@example.com")); n != 2 {
		t.Fatalf("max has %d devices", n)
	}
	if n := len(d.For("")); n != 3 {
		t.Fatalf("all: %d", n)
	}
	// Max cannot revoke Olivia's device through his own list.
	if err := d.Revoke(d.For("olivia@example.com")[0].ID, "max@example.com"); err == nil {
		t.Fatal("revoked someone else's device")
	}
	if err := d.Revoke(laptop.ID, "max@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := d.ByToken(c, tl, "", now); ok {
		t.Fatal("revoked device still works")
	}
	if _, _, ok := d.ByToken(c, td, "", now); !ok {
		t.Fatal("revoking one device stopped another")
	}
	// Out of the team: their devices stop, even before cleanup.
	if err := c.Remove("max@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := d.ByToken(c, td, "", now); ok {
		t.Fatal("device of a removed member works")
	}
	_ = d.RevokeMember("max@example.com")
	if n := len(d.For("")); n != 1 {
		t.Fatalf("after RevokeMember: %d", n)
	}
	if _, _, ok := d.ByToken(c, to, "", now); !ok {
		t.Fatal("olivia's device lost")
	}
	_ = desk
	// A personal token is not a device token.
	if _, _, ok := d.ByToken(c, "hpk_abc", "", now); ok {
		t.Fatal("personal token accepted as device token")
	}
}

func TestAllowIPs(t *testing.T) {
	c := &Config{}
	if !c.IPAllowed(net.ParseIP("198.51.100.1")) {
		t.Fatal("empty allowlist must allow everyone")
	}
	c.AllowIPs = []string{"203.0.113.0/24", "198.51.100.7"}
	for ip, want := range map[string]bool{"203.0.113.9": true, "198.51.100.7": true, "198.51.100.8": false, "127.0.0.1": true, "::1": true} {
		if got := c.IPAllowed(net.ParseIP(ip)); got != want {
			t.Errorf("%s: %v", ip, got)
		}
	}
	c2 := &Config{Enabled: true, Auth: "token", AllowIPs: []string{"not-an-ip"}, Members: []Person{{ID: "o", Role: Owner}}}
	if err := c2.check(); err == nil {
		t.Fatal("bad allow_ips accepted")
	}
}
