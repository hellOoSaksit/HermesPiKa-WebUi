// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"hpk.local/hpk/internal/team"
	"hpk.local/hpk/internal/teamlink"
)

// A team server (HTTPS) and a person's own hpk that joins it with an invite code.
func TestJoinTeamServerWithInviteCode(t *testing.T) {
	t.Setenv("HPK_FEATURES", "team") // the join flow is gated off in v1 solo mode
	// ---- the team server ----
	srvHome := t.TempDir()
	c := &team.Config{Enabled: true}
	ownerTok, _ := c.Add("olivia@example.com", "Olivia", team.Owner, nil, time.Now())
	memberTok, _ := c.Add("max@example.com", "Max", team.Member, nil, time.Now())
	if err := c.Save(srvHome); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Options{Home: srvHome})
	tls := httptest.NewTLSServer(srv.Routes(fstest.MapFS{"index.html": {Data: []byte("x")}}))
	defer tls.Close()
	plain := tls.Client()

	if code, _ := as(t, plain, "POST", tls.URL+"/api/team/join-code", memberTok, map[string]any{"id": "max@example.com"}); code != 403 {
		t.Fatalf("a member made an invite code: %d", code)
	}
	code, out := as(t, plain, "POST", tls.URL+"/api/team/join-code", ownerTok, map[string]any{"id": "max@example.com"})
	if code != 200 {
		t.Fatalf("join-code: %d %v", code, out)
	}
	invite := out["code"].(string)

	// A team server joins nothing and keeps no device token itself.
	if code, _ := as(t, plain, "POST", tls.URL+"/api/teamserver/join", ownerTok, map[string]any{"url": tls.URL, "code": invite}); code != 403 {
		t.Fatalf("team server accepted /api/teamserver/join: %d", code)
	}

	// ---- the person's own hpk (solo, loopback) ----
	localHome := t.TempDir()
	loc := NewServer(Options{Home: localHome})
	loc.link.http = tls.Client() // trusts the test certificate; otherwise as normal
	lts := httptest.NewServer(loc.Routes(fstest.MapFS{"index.html": {Data: []byte("x")}}))
	defer lts.Close()
	web := http.DefaultClient

	if code, out := as(t, web, "POST", lts.URL+"/api/teamserver/join", "", map[string]any{"url": "http://team.example.com", "code": invite}); code != 400 {
		t.Fatalf("plain http to another host accepted: %d %v", code, out)
	}
	code, out = as(t, web, "POST", lts.URL+"/api/teamserver/join", "", map[string]any{"url": tls.URL, "code": invite, "device": "Max laptop"})
	if code != 200 || out["role"] != "member" || out["member"] != "max@example.com" {
		t.Fatalf("join: %d %v", code, out)
	}
	// The code was used once.
	if code, _ := as(t, plain, "POST", tls.URL+"/api/join", "", map[string]any{"code": invite, "device": "again"}); code != 401 {
		t.Fatalf("an invite code worked twice: %d", code)
	}
	// The token is in the vault as a system secret, not in teamserver.json, not listed.
	v, _, _ := loc.openPersonal()
	cred, err := teamlink.DecodeCredential(v.System(teamlink.SecretName))
	tok := cred.Token
	if err != nil || !strings.HasPrefix(tok, team.DevicePrefix) {
		t.Fatalf("no device token in the vault: %v", err)
	}
	if cred.URL != tls.URL {
		t.Fatalf("the token is kept with the server it belongs to: %q", cred.URL)
	}
	raw, _ := os.ReadFile(teamlink.File(localHome))
	if bytes.Contains(raw, []byte(tok)) {
		t.Fatal("teamserver.json holds the token")
	}
	if len(v.List()) != 0 || len(v.Names()) != 0 {
		t.Fatal("the device token is listed in the vault (the AI could see its name)")
	}
	if code, out := as(t, web, "GET", lts.URL+"/api/vault", "", nil); code == 200 && strings.Contains(strings.ToLower(fmtAny(out)), "team-server") {
		t.Fatalf("vault page shows the token: %v", out)
	}
	// Server stored only the hash.
	raw, _ = os.ReadFile(team.DevicesFile(srvHome))
	if bytes.Contains(raw, []byte(tok)) || !bytes.Contains(raw, []byte(team.HashToken(tok))) {
		t.Fatal("devices.json must hold only the SHA-256")
	}

	// Status and the proxy to the server.
	if code, out := as(t, web, "GET", lts.URL+"/api/teamserver", "", nil); code != 200 || out["state"] != "ok" || out["role"] != "member" {
		t.Fatalf("status: %d %v", code, out)
	}
	code, out = as(t, web, "GET", lts.URL+"/api/teamserver/remote/me/devices", "", nil)
	if code != 200 || len(out["devices"].([]any)) != 1 || out["current"] == "" {
		t.Fatalf("my devices: %d %v", code, out)
	}
	for _, p := range []string{"GET privacy", "POST mode", "POST team/rotate", "GET browser", "POST team/budget"} {
		m, path, _ := strings.Cut(p, " ")
		if code, _ := as(t, web, m, lts.URL+"/api/teamserver/remote/"+path, "", map[string]any{}); code != 403 && code != 404 {
			t.Fatalf("%s reached the server: %d", p, code)
		}
	}
	// The member's role still applies on the server: a member cannot list every device.
	if code, _ := as(t, web, "GET", lts.URL+"/api/teamserver/remote/team/devices", "", nil); code != 403 {
		t.Fatalf("member listed all devices: %d", code)
	}

	// The owner sees the device (last seen) and revokes it; the machine notices.
	code, out = as(t, plain, "GET", tls.URL+"/api/team/devices", ownerTok, nil)
	devs := out["devices"].([]any)
	if code != 200 || len(devs) != 1 {
		t.Fatalf("owner devices: %d %v", code, out)
	}
	d0 := devs[0].(map[string]any)
	if d0["name"] != "Max laptop" || d0["lastSeen"] == nil {
		t.Fatalf("device row %v", d0)
	}
	if code, _ := as(t, plain, "POST", tls.URL+"/api/team/devices/revoke", ownerTok, map[string]any{"id": d0["id"]}); code != 200 {
		t.Fatal("owner revoke")
	}
	if code, out := as(t, web, "GET", lts.URL+"/api/teamserver", "", nil); code != 200 || out["state"] != "revoked" {
		t.Fatalf("after revoke: %d %v", code, out)
	}

	// Leave, join again with a new code, and leave: the server forgets the device too.
	if code, out := as(t, web, "POST", lts.URL+"/api/teamserver/leave", "", map[string]any{}); code != 200 || out["revokedOnServer"] != false {
		t.Fatalf("leave after revoke: %d %v", code, out)
	}
	_, out = as(t, plain, "POST", tls.URL+"/api/team/join-code", ownerTok, map[string]any{"id": "max@example.com"})
	if code, out := as(t, web, "POST", lts.URL+"/api/teamserver/join", "", map[string]any{"url": tls.URL, "code": out["code"]}); code != 200 {
		t.Fatalf("rejoin: %d %v", code, out)
	}
	if code, _ := as(t, web, "POST", lts.URL+"/api/teamserver/join", "", map[string]any{"url": tls.URL, "code": "AAAAA-AAAAA"}); code != 409 {
		t.Fatal("joined twice")
	}
	if code, out := as(t, web, "POST", lts.URL+"/api/teamserver/leave", "", map[string]any{}); code != 200 || out["revokedOnServer"] != true {
		t.Fatalf("leave: %d %v", code, out)
	}
	if _, err := os.Stat(teamlink.File(localHome)); !os.IsNotExist(err) || v.System(teamlink.SecretName) != "" {
		t.Fatal("leave left the link or the token behind")
	}
	if _, out := as(t, plain, "GET", tls.URL+"/api/team/devices", ownerTok, nil); len(out["devices"].([]any)) != 0 {
		t.Fatalf("server still lists the device: %v", out)
	}
}

func fmtAny(v any) string { b, _ := json.Marshal(v); return string(b) }

// Joining needs HTTPS unless it comes from the server's own machine.
func TestJoinNeedsHTTPSAndAllowlist(t *testing.T) {
	home := t.TempDir()
	c := &team.Config{Enabled: true, AllowIPs: []string{"198.51.100.0/24"}, TrustedProxies: []string{"198.51.100.20"}} // X-Forwarded-Proto counts only from a trusted proxy
	c.Add("olivia@example.com", "", team.Owner, nil, time.Now())
	c.Add("max@example.com", "", team.Member, nil, time.Now())
	if err := c.Save(home); err != nil {
		t.Fatal(err)
	}
	s := NewServer(Options{Home: home})
	h := s.Routes(nil)
	var code string
	s.withDevices(func(d *team.Devices) {
		code, _, _ = d.NewInvite(c, "max@example.com", "olivia@example.com", time.Now())
	})
	do := func(remote string, https bool) int {
		req := httptest.NewRequest("POST", "/api/join", strings.NewReader(`{"code":"`+code+`","device":"x"}`))
		req.RemoteAddr = remote
		req.Header.Set("X-HPK", "1")
		if https {
			req.Header.Set("X-Forwarded-Proto", "https")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := do("203.0.113.9:4000", true); got != 403 {
		t.Fatalf("address outside allow_ips: %d", got)
	}
	if got := do("198.51.100.20:4000", false); got != 403 {
		t.Fatalf("plain HTTP join from the network: %d", got)
	}
	if got := do("198.51.100.20:4000", true); got != 200 {
		t.Fatalf("allowed address over HTTPS: %d", got)
	}
	// Every request, not only joining, obeys the allowlist.
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.RemoteAddr = "203.0.113.9:4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("health from outside allow_ips: %d", rec.Code)
	}
}

func TestJoinWrongCodesAreLimited(t *testing.T) {
	s := NewServer(Options{Home: t.TempDir()})
	now := time.Now()
	for i := 0; i < joinFailMax; i++ {
		if !s.joinAttempt("203.0.113.9", now) {
			t.Fatalf("blocked after %d", i+1)
		}
	}
	if s.joinAttempt("203.0.113.9", now) {
		t.Fatal("not blocked after the limit")
	}
	if !s.joinAttempt("198.51.100.1", now) {
		t.Fatal("another address is blocked")
	}
	if !s.joinAttempt("203.0.113.9", now.Add(joinFailWindow+time.Second)) {
		t.Fatal("still blocked after the window")
	}
	// a good code takes its try back; old addresses are forgotten
	later := now.Add(3 * joinFailWindow)
	if !s.joinAttempt("192.0.2.7", later) {
		t.Fatal("fresh address")
	}
	s.joinWorked("192.0.2.7", later)
	if _, ok := s.devs.fails["192.0.2.7"]; ok {
		t.Fatal("a worked try leaves no entry")
	}
}

// Parallel wrong codes cannot get past the limit: the try is counted before the check.
func TestJoinAttemptsCountedUpFront(t *testing.T) {
	s := NewServer(Options{Home: t.TempDir()})
	now := time.Now()
	var wg sync.WaitGroup
	var mu sync.Mutex
	passed := 0
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.joinAttempt("203.0.113.50", now) {
				mu.Lock()
				passed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if passed != joinFailMax {
		t.Fatalf("%d of 50 parallel tries got through; want %d", passed, joinFailMax)
	}
}

func TestRemovingMemberRevokesDevices(t *testing.T) {
	s, ts, _ := setup(t)
	c := &team.Config{Enabled: true}
	ownerTok, _ := c.Add("olivia", "", team.Owner, nil, time.Now())
	c.Add("max", "", team.Member, nil, time.Now())
	if err := c.Save(s.home); err != nil {
		t.Fatal(err)
	}
	_, out := as(t, http.DefaultClient, "POST", ts.URL+"/api/team/join-code", ownerTok, map[string]any{"id": "max"})
	code, j := as(t, http.DefaultClient, "POST", ts.URL+"/api/join", "", map[string]any{"code": out["code"], "device": "pc"})
	if code != 200 {
		t.Fatalf("join from loopback: %d %v", code, j)
	}
	devTok := j["token"].(string)
	if code, me := as(t, http.DefaultClient, "GET", ts.URL+"/api/me", devTok, nil); code != 200 || me["id"] != "max" || me["device"] == "" {
		t.Fatalf("device sign-in: %d %v", code, me)
	}
	if code, _ := as(t, http.DefaultClient, "POST", ts.URL+"/api/me/devices/revoke", devTok, map[string]any{"id": "dev_other"}); code != 400 {
		t.Fatal("revoked an unknown device")
	}
	if code, _ := as(t, http.DefaultClient, "POST", ts.URL+"/api/team/remove", ownerTok, map[string]any{"id": "max"}); code != 200 {
		t.Fatal("remove")
	}
	if code, _ := as(t, http.DefaultClient, "GET", ts.URL+"/api/snapshot", devTok, nil); code != 401 {
		t.Fatalf("device of a removed member still works: %d", code)
	}
	var n int
	s.withDevices(func(d *team.Devices) { n = len(d.For("")) })
	if n != 0 {
		t.Fatal("devices left after remove")
	}
}
