// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"bufio"
	"crypto/tls"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
	"hpk.local/hpk/internal/team"
)

// DNS rebinding: a page on evil.example that resolves to 127.0.0.1 sends
// Host: evil.example. Without team mode only this machine's names are answered.
func TestSoloAnswersOnlyLoopbackNames(t *testing.T) {
	_, ts, _ := setup(t)
	try := func(host string) int {
		req, _ := http.NewRequest("GET", ts.URL+"/api/snapshot", nil)
		req.Host = host
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	port := ts.URL[strings.LastIndex(ts.URL, ":"):]
	for _, h := range []string{"evil.example" + port, "evil.example", "127.0.0.1.nip.io" + port, "192.168.1.5" + port} {
		if code := try(h); code != http.StatusMisdirectedRequest {
			t.Errorf("Host %s answered %d", h, code)
		}
	}
	for _, h := range []string{"127.0.0.1" + port, "localhost" + port, "localhost", "[::1]" + port} {
		if code := try(h); code != 200 {
			t.Errorf("Host %s refused: %d", h, code)
		}
	}
}

// A chat id from another project, or someone else's unshared chat, is out of reach
// for reading, writing, cancelling, starting and renaming.
func TestChatAccessFailsClosed(t *testing.T) {
	s, ts, p := setup(t)
	root := filepath.Join(s.home, "code", "secret-app")
	os.MkdirAll(root, 0o755)
	other, err := project.Create(s.home, "secret-app", root, project.Shared)
	if err != nil {
		t.Fatal(err)
	}
	c := &team.Config{Enabled: true}
	c.Add("olivia", "", team.Owner, nil, time.Now())
	mTok, _ := c.Add("max", "", team.Member, []string{"sample-app"}, time.Now())
	nTok, _ := c.Add("nia", "", team.Member, nil, time.Now())
	c.Save(s.home)
	hidden, _ := session.New(other, session.NewOptions{Name: "hidden", Owner: "nia"})
	mine, _ := session.New(p, session.NewOptions{Name: "maxs", Owner: "max"})

	base := ts.URL + "/api/sessions/"
	// max sees sample-app only; nia's chat lives in secret-app
	if code, _ := as(t, http.DefaultClient, "GET", base+hidden.ID+"/chat?project=sample-app", mTok, nil); code != 404 {
		t.Fatalf("a chat of another project, asked through a visible one: %d", code)
	}
	for _, path := range []string{"/prompt", "/cancel", "/start", "/rename", "/share"} {
		if code, _ := as(t, http.DefaultClient, "POST", base+hidden.ID+path, mTok, map[string]any{"project": "sample-app", "text": "hi", "title": "x", "with": []string{"max"}}); code != 404 {
			t.Errorf("%s on another project's chat: %d", path, code)
		}
	}
	// nia sees every project, but max's chat is not shared with her
	for _, path := range []string{"/prompt", "/cancel", "/rename"} {
		if code, _ := as(t, http.DefaultClient, "POST", base+mine.ID+path, nTok, map[string]any{"project": "sample-app", "text": "hi", "title": "x"}); code != 403 {
			t.Errorf("%s on someone else's chat: %d", path, code)
		}
	}
	if code, _ := as(t, http.DefaultClient, "GET", base+mine.ID+"/chat?project=sample-app", nTok, nil); code != 403 {
		t.Fatal("reading someone else's chat")
	}
	if code, _ := as(t, http.DefaultClient, "POST", base+mine.ID+"/rename", mTok, map[string]any{"project": "sample-app", "title": "renamed"}); code != 200 {
		t.Fatal("the owner renames their chat")
	}
}

// A red-line permit is answered only through its own project.
func TestDecideStaysInItsProject(t *testing.T) {
	s, _, _ := setup(t)
	pm := &permit{id: "perm-9", project: "secret-app", decision: make(chan string, 1)}
	s.acp.mu.Lock()
	s.acp.permits["perm-9"] = pm
	s.acp.mu.Unlock()
	if err := s.acp.Decide("perm-9", "sample-app", "approve"); err == nil {
		t.Fatal("approved from another project")
	}
	if err := s.acp.Decide("perm-9", "secret-app", "deny"); err != nil || <-pm.decision != "deny" {
		t.Fatalf("its own project: %v", err)
	}
}

// Rotating a token, or removing and re-adding a member, ends their Console sign-ins.
func TestRotateEndsSignIns(t *testing.T) {
	s, ts, _ := setup(t)
	c := &team.Config{Enabled: true}
	c.Add("olivia", "", team.Owner, nil, time.Now())
	tok, _ := c.Add("max", "", team.Member, nil, time.Now())
	c.Save(s.home)
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	if code, _ := as(t, browser, "POST", ts.URL+"/api/login", "", map[string]any{"token": tok}); code != 200 {
		t.Fatal("login")
	}
	if code, _ := as(t, browser, "GET", ts.URL+"/api/snapshot", "", nil); code != 200 {
		t.Fatal("signed in")
	}
	c, _ = team.Load(s.home)
	if _, err := c.Rotate("max"); err != nil {
		t.Fatal(err)
	}
	c.Save(s.home)
	if code, _ := as(t, browser, "GET", ts.URL+"/api/snapshot", "", nil); code != 401 {
		t.Fatalf("the old sign-in still works after a rotate: %d", code)
	}
	// removed and added again under the same id: the old cookie does not come back
	tok2, _ := c.Rotate("max")
	c.Save(s.home)
	jar2, _ := cookiejar.New(nil)
	b2 := &http.Client{Jar: jar2}
	as(t, b2, "POST", ts.URL+"/api/login", "", map[string]any{"token": tok2})
	c.Remove("max")
	c.Add("max", "", team.Member, nil, time.Now())
	c.Save(s.home)
	if code, _ := as(t, b2, "GET", ts.URL+"/api/snapshot", "", nil); code != 401 {
		t.Fatalf("a removed member's sign-in survives re-adding: %d", code)
	}
}

// An open event stream ends once its person can no longer sign in.
func TestEventStreamEndsOnRevoke(t *testing.T) {
	old := eventsTick
	eventsTick = 50 * time.Millisecond
	t.Cleanup(func() { eventsTick = old })
	s, ts, _ := setup(t)
	c := &team.Config{Enabled: true}
	c.Add("olivia", "", team.Owner, nil, time.Now())
	tok, _ := c.Add("max", "", team.Member, nil, time.Now())
	c.Save(s.home)
	req, _ := http.NewRequest("GET", ts.URL+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("open: %v %v", err, res)
	}
	defer res.Body.Close()
	c, _ = team.Load(s.home)
	c.Rotate("max")
	c.Save(s.home)
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream stayed open for a token that no longer works")
	}
}

// X-Forwarded-Proto counts only from a trusted proxy.
func TestViaHTTPSTrustsOnlyProxies(t *testing.T) {
	s, _, _ := setup(t)
	c := &team.Config{Enabled: true}
	c.Add("olivia", "", team.Owner, nil, time.Now())
	c.Save(s.home)
	r := httptest.NewRequest("GET", "/api/me", nil)
	r.RemoteAddr = "203.0.113.4:5555"
	r.Header.Set("X-Forwarded-Proto", "https")
	if s.viaHTTPS(r) {
		t.Fatal("anyone can claim https")
	}
	c.TrustedProxies = []string{"203.0.113.4"}
	c.Save(s.home)
	if !s.viaHTTPS(r) {
		t.Fatal("a trusted proxy's https")
	}
	r2 := httptest.NewRequest("GET", "/api/me", nil)
	r2.TLS = &tls.ConnectionState{}
	if !s.viaHTTPS(r2) {
		t.Fatal("real TLS")
	}
}
