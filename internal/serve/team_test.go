// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
	"hpk.local/hpk/internal/team"
)

// as sends a request with a client (cookies) and an optional bearer token.
func as(t *testing.T, c *http.Client, method, url, token string, body any) (int, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, url, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HPK", "1")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestTeamModeRoles(t *testing.T) {
	s, ts, p := setup(t)
	root := filepath.Join(s.home, "code", "other-app")
	os.MkdirAll(root, 0o755)
	project.Create(s.home, "other-app", root, project.Shared)

	c := &team.Config{Enabled: true}
	ownerTok, _ := c.Add("olivia", "", team.Owner, nil, time.Now())
	memberTok, _ := c.Add("max", "", team.Member, []string{"sample-app"}, time.Now())
	viewerTok, _ := c.Add("vic", "", team.Viewer, nil, time.Now())
	if err := c.Save(s.home); err != nil {
		t.Fatal(err)
	}
	plain := http.DefaultClient

	if code, _ := as(t, plain, "GET", ts.URL+"/api/snapshot", "", nil); code != 401 {
		t.Fatalf("team mode needs sign-in: %d", code)
	}
	if code, _ := as(t, plain, "GET", ts.URL+"/api/health", "", nil); code != 200 {
		t.Fatal("health stays public")
	}
	// Sign in with a token: a cookie, then the API works without the token.
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	if code, _ := as(t, browser, "POST", ts.URL+"/api/login", "", map[string]any{"token": "hpk_wrong"}); code != 401 {
		t.Fatal("a wrong token is refused")
	}
	if code, me := as(t, browser, "POST", ts.URL+"/api/login", "", map[string]any{"token": memberTok}); code != 200 || me["role"] != "member" {
		t.Fatalf("login: %d %v", code, me)
	}
	var snap Snapshot
	req, _ := http.NewRequest("GET", ts.URL+"/api/snapshot", nil)
	res, _ := browser.Do(req)
	json.NewDecoder(res.Body).Decode(&snap)
	res.Body.Close()
	if len(snap.Projects) != 1 || snap.Projects[0].Name != "sample-app" || snap.Me.ID != "max" {
		t.Fatalf("a member limited to sample-app sees only it: %+v %+v", snap.Projects, snap.Me)
	}
	if code, _ := as(t, browser, "GET", ts.URL+"/api/gates?project=other-app", "", nil); code != 404 {
		t.Fatal("projects outside the member's list do not exist for them")
	}
	if code, _ := as(t, browser, "POST", ts.URL+"/api/privacy", "", map[string]any{"project": "sample-app", "mode": "isolated"}); code != 403 {
		t.Fatal("members cannot change privacy")
	}
	if code, _ := as(t, browser, "POST", ts.URL+"/api/mode", "", map[string]any{"project": "sample-app", "value": "auto"}); code != 403 {
		t.Fatal("members cannot change the mode (red-line power)")
	}
	if code, _ := as(t, plain, "POST", ts.URL+"/api/sessions", viewerTok, map[string]any{"project": "sample-app", "name": "x"}); code != 403 {
		t.Fatal("viewers cannot start work")
	}
	if code, out := as(t, browser, "POST", ts.URL+"/api/sessions", "", map[string]any{"project": "sample-app", "name": "mine"}); code != 200 {
		t.Fatalf("members start chats: %d %v", code, out)
	}
	// Someone else's chat is not readable by a member; the owner role can.
	other, _ := session.New(p, session.NewOptions{Name: "olivias", Owner: "olivia"})
	if code, _ := as(t, browser, "GET", ts.URL+"/api/sessions/"+other.ID+"/chat?project=sample-app", "", nil); code != 403 {
		t.Fatal("a chat belongs to its owner")
	}
	if code, _ := as(t, plain, "GET", ts.URL+"/api/sessions/"+other.ID+"/chat?project=sample-app", ownerTok, nil); code != 200 {
		t.Fatal("owners can read every chat")
	}
	// Owners manage people; tokens come back once.
	code, out := as(t, plain, "POST", ts.URL+"/api/team/invite", ownerTok, map[string]any{"id": "nia", "role": "maintainer"})
	if code != 200 || out["token"] == "" {
		t.Fatalf("invite: %d %v", code, out)
	}
	if code, _ := as(t, browser, "POST", ts.URL+"/api/team/invite", "", map[string]any{"id": "x", "role": "owner"}); code != 403 {
		t.Fatal("members cannot add people")
	}
	if code, _ := as(t, plain, "POST", ts.URL+"/api/team/remove", ownerTok, map[string]any{"id": "olivia"}); code != 400 {
		t.Fatal("you cannot remove yourself")
	}
	// Signing out ends the cookie.
	as(t, browser, "POST", ts.URL+"/api/logout", "", nil)
	if code, _ := as(t, browser, "GET", ts.URL+"/api/snapshot", "", nil); code != 401 {
		t.Fatal("after logout the cookie no longer works")
	}
}

func TestHeaderModeTrustsOnlyTheProxy(t *testing.T) {
	s, ts, _ := setup(t)
	c := &team.Config{Enabled: true, Auth: "header", Header: "X-Forwarded-Email", TrustedProxies: []string{"10.9.9.9"}}
	c.Members = []team.Person{{ID: "olivia@example.com", Role: team.Owner}}
	if err := c.Save(s.home); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/snapshot", nil)
	req.Header.Set("X-Forwarded-Email", "olivia@example.com")
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("the header from a client that is not the proxy is ignored: %d", res.StatusCode)
	}
	c.TrustedProxies = []string{"127.0.0.0/8", "::1"}
	c.Save(s.home)
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("from the proxy: %d", res.StatusCode)
	}
}

func TestNetworkNeedsTeamAndTLS(t *testing.T) {
	home := t.TempDir()
	ctx := t.Context()
	if err := Run(ctx, Options{Addr: "0.0.0.0:0", Home: home}); err == nil || !bytes.Contains([]byte(err.Error()), []byte("team mode")) {
		t.Fatalf("solo mode stays on loopback: %v", err)
	}
	c := &team.Config{Enabled: true}
	c.Add("olivia", "", team.Owner, nil, time.Now())
	c.Save(home)
	if err := Run(ctx, Options{Addr: "0.0.0.0:0", Home: home}); err == nil || !bytes.Contains([]byte(err.Error()), []byte("tls")) {
		t.Fatalf("team mode on a network needs TLS: %v", err)
	}
}

func TestPersonalMoneyCap(t *testing.T) {
	s, ts, p := setup(t)
	os.WriteFile(filepath.Join(s.home, "models_dev_cache.json"), []byte(`{"anthropic":{"models":{"m-big":{"cost":{"input":2,"output":10}}}}}`), 0o644)
	c := &team.Config{Enabled: true}
	ownerTok, _ := c.Add("olivia", "", team.Owner, nil, time.Now())
	maxTok, _ := c.Add("max", "", team.Member, nil, time.Now())
	c.Save(s.home)
	// Max ran a chat that cost $3.
	project.LinkOwned(p, "local-max-1", "max")
	privacy.RecordUsage(s.home, "local-max-1", privacy.Route{Provider: "anthropic", BaseURL: "https://api.anthropic.com", Model: "m-big"}, map[string]any{"input_tokens": 1e6, "output_tokens": 1e5})
	if code, _ := as(t, http.DefaultClient, "POST", ts.URL+"/api/team/budget", maxTok, map[string]any{"id": "max", "daily": 100}); code != 403 {
		t.Fatal("members cannot set budgets")
	}
	if code, _ := as(t, http.DefaultClient, "POST", ts.URL+"/api/team/budget", ownerTok, map[string]any{"id": "max", "daily": 2.5}); code != 200 {
		t.Fatal("owners set budgets")
	}
	code, out := as(t, http.DefaultClient, "POST", ts.URL+"/api/sessions", maxTok, map[string]any{"project": "sample-app", "name": "more", "start": true, "firstMessage": "hi"})
	if code != 409 || !strings.Contains(fmt.Sprint(out["error"]), "max spent $3.00 today") {
		t.Fatalf("max is at the cap: %d %v", code, out)
	}
	var team1 map[string]any
	req, _ := http.NewRequest("GET", ts.URL+"/api/team", nil)
	req.Header.Set("Authorization", "Bearer "+ownerTok)
	res, _ := http.DefaultClient.Do(req)
	json.NewDecoder(res.Body).Decode(&team1)
	res.Body.Close()
	if !strings.Contains(fmt.Sprint(team1["members"]), "spentToday:3") {
		t.Fatalf("the team page shows what each person spent: %v", team1["members"])
	}
}

func TestShareChat(t *testing.T) {
	s, ts, p := setup(t)
	c := &team.Config{Enabled: true}
	oTok, _ := c.Add("olivia", "", team.Owner, nil, time.Now())
	mTok, _ := c.Add("max", "", team.Member, nil, time.Now())
	nTok, _ := c.Add("nia", "", team.Member, nil, time.Now())
	c.Save(s.home)
	chat, _ := session.New(p, session.NewOptions{Name: "maxs", Owner: "max"})
	url := ts.URL + "/api/sessions/" + chat.ID
	if code, _ := as(t, http.DefaultClient, "GET", url+"/chat?project=sample-app", nTok, nil); code != 403 {
		t.Fatal("nia cannot read max's chat yet")
	}
	if code, _ := as(t, http.DefaultClient, "POST", url+"/share", nTok, map[string]any{"project": "sample-app", "with": []string{"nia"}}); code != 403 {
		t.Fatal("only the chat's owner shares it")
	}
	if code, _ := as(t, http.DefaultClient, "POST", url+"/share", mTok, map[string]any{"project": "sample-app", "with": []string{"stranger"}}); code != 400 {
		t.Fatal("share only with team members")
	}
	if code, _ := as(t, http.DefaultClient, "POST", url+"/share", mTok, map[string]any{"project": "sample-app", "with": []string{"nia"}}); code != 200 {
		t.Fatal("max shares with nia")
	}
	if code, _ := as(t, http.DefaultClient, "GET", url+"/chat?project=sample-app", nTok, nil); code != 200 {
		t.Fatal("nia reads the shared chat")
	}
	if code, _ := as(t, http.DefaultClient, "POST", url+"/share", oTok, map[string]any{"project": "sample-app", "with": []string{}}); code != 200 {
		t.Fatal("a Console owner can unshare")
	}
	if code, _ := as(t, http.DefaultClient, "GET", url+"/chat?project=sample-app", nTok, nil); code != 403 {
		t.Fatal("unshared again")
	}
}

func TestPersonalSharedWiki(t *testing.T) {
	s, ts, p := setup(t)
	if privacy.UserTarget("Max.Doe@example.com") != "users/max.doe-example.com" {
		t.Fatalf("slug: %s", privacy.UserTarget("Max.Doe@example.com"))
	}
	// An approved page to share.
	os.MkdirAll(filepath.Join(p.WikiDir(), "lessons"), 0o755)
	os.WriteFile(filepath.Join(p.WikiDir(), "lessons", "retry.md"), []byte("---\ntitle: Retry with backoff\ntype: lesson\nsources: [src/net.go:9]\nupdated: 2026-11-18\nconfidence: high\n---\n\nRetry three times with backoff.\n"), 0o644)
	c := &team.Config{Enabled: true}
	c.Add("olivia", "", team.Owner, nil, time.Now())
	mTok, _ := c.Add("max@example.com", "", team.Member, nil, time.Now())
	c.Save(s.home)
	code, out := as(t, http.DefaultClient, "POST", ts.URL+"/api/wiki/promote", mTok, map[string]any{"project": "sample-app", "page": "lessons/retry.md", "to": "mine"})
	if code != 200 || out["to"] != "users/max-example.com" {
		t.Fatalf("promote to my wiki: %d %v", code, out)
	}
	var list []map[string]any
	req, _ := http.NewRequest("GET", ts.URL+"/api/promotions?project=sample-app", nil)
	req.Header.Set("Authorization", "Bearer "+mTok)
	res, _ := http.DefaultClient.Do(req)
	json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list) != 1 || list[0]["wiki"] != "mine" {
		t.Fatalf("my promotion is listed: %v", list)
	}
	n := list[0]["n"]
	if code, _ := as(t, http.DefaultClient, "POST", ts.URL+"/api/promotions", mTok, map[string]any{"project": "sample-app", "wiki": "team", "n": n, "decision": "approve"}); code != 403 {
		t.Fatal("members do not decide team promotions")
	}
	if code, out := as(t, http.DefaultClient, "POST", ts.URL+"/api/promotions", mTok, map[string]any{"project": "sample-app", "wiki": "mine", "n": n, "decision": "approve"}); code != 200 {
		t.Fatalf("a person approves their own: %d %v", code, out)
	}
	if m, _ := filepath.Glob(filepath.Join(s.home, "hpk-shared", "users", "max-example.com", "lessons", "*-retry.md")); len(m) != 1 {
		t.Fatalf("approved into the personal shared wiki: %v", m)
	}
}

// Behind a reverse proxy that terminates TLS and rewrites Host.
func TestBehindProxyHeaders(t *testing.T) {
	s, ts, _ := setup(t)
	c := &team.Config{Enabled: true, TrustedProxies: []string{"127.0.0.1"}} // the proxy in front (X-Forwarded-Proto counts only from it)
	tok, _ := c.Add("olivia", "", team.Owner, nil, time.Now())
	c.Save(s.home)
	login := func(proto string) string {
		req, _ := http.NewRequest("POST", ts.URL+"/api/login", bytes.NewReader([]byte(`{"token":"`+tok+`"}`)))
		req.Header.Set("X-HPK", "1")
		req.Header.Set("Content-Type", "application/json")
		if proto != "" {
			req.Header.Set("X-Forwarded-Proto", proto)
		}
		res, _ := http.DefaultClient.Do(req)
		res.Body.Close()
		return res.Header.Get("Set-Cookie")
	}
	if ck := login("https"); !strings.Contains(ck, "Secure") {
		t.Fatalf("TLS at the proxy: the cookie is Secure: %s", ck)
	}
	if ck := login(""); strings.Contains(ck, "Secure") {
		t.Fatalf("plain HTTP on loopback: no Secure flag, or the browser drops it: %s", ck)
	}
	post := func(origin, fwdHost string) int {
		req, _ := http.NewRequest("POST", ts.URL+"/api/notices/read", bytes.NewReader([]byte(`{"project":"sample-app"}`)))
		req.Header.Set("X-HPK", "1")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Origin", origin)
		if fwdHost != "" {
			req.Header.Set("X-Forwarded-Host", fwdHost)
		}
		res, _ := http.DefaultClient.Do(req)
		res.Body.Close()
		return res.StatusCode
	}
	if code := post("https://hpk.example.com", "hpk.example.com"); code != 200 {
		t.Fatalf("same origin as the proxy's public host: %d", code)
	}
	if code := post("https://evil.example.com", "hpk.example.com"); code != 403 {
		t.Fatalf("another site: %d", code)
	}
}

func TestAccountsRules(t *testing.T) {
	s, ts, _ := setup(t)
	c := &team.Config{Enabled: true}
	oTok, _ := c.Add("olivia", "", team.Owner, nil, time.Now())
	mTok, _ := c.Add("max", "", team.Member, nil, time.Now())
	c.Save(s.home)
	if code, _ := as(t, http.DefaultClient, "POST", ts.URL+"/api/accounts", mTok, map[string]any{"action": "remove", "provider": "openrouter", "target": "1"}); code != 403 {
		t.Fatal("only owners manage accounts")
	}
	if code, _ := as(t, http.DefaultClient, "POST", ts.URL+"/api/accounts", oTok, map[string]any{"action": "remove", "provider": "x; rm -rf /", "target": "1"}); code != 400 {
		t.Fatal("provider ids are checked before anything runs")
	}
	// A key over plain HTTP from another machine is refused before Hermes is called.
	req, _ := http.NewRequest("POST", ts.URL+"/api/accounts", bytes.NewReader([]byte(`{"action":"add","provider":"openrouter","key":"sk-test"}`)))
	req.Host = "hpk.example.com:7878"
	req.Header.Set("X-HPK", "1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+oTok)
	res, _ := http.DefaultClient.Do(req)
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != 403 || !strings.Contains(fmt.Sprint(out["error"]), "HTTPS") {
		t.Fatalf("keys only over HTTPS or on this machine: %d %v", res.StatusCode, out)
	}
	if strings.Contains(fmt.Sprint(out), "sk-test") {
		t.Fatal("the key must never come back")
	}
}
