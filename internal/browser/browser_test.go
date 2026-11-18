// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package browser

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hpk.local/hpk/internal/vault"
)

func TestWSFrameRoundTrip(t *testing.T) {
	if u, ok := devtoolsURL("DevTools listening on ws://127.0.0.1:9222/devtools/browser/abc"); !ok || u != "ws://127.0.0.1:9222/devtools/browser/abc" {
		t.Fatal(u)
	}
	if _, err := wsDial("ws://example.com/x"); err == nil {
		t.Fatal("only this machine's DevTools")
	}
}

// Runs a real Chrome/Edge when one is installed (skipped otherwise, or with HPK_SKIP_BROWSER=1).
func TestRealBrowserSecretsNeverReachTheAgent(t *testing.T) {
	if os.Getenv("HPK_SKIP_BROWSER") == "1" {
		t.Skip("HPK_SKIP_BROWSER=1")
	}
	if _, err := FindExe(); err != nil {
		t.Skip(err)
	}
	var mu sync.Mutex
	got := map[string]string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Sign in</title><h1>Sign in to Harbor</h1>
<form method=post action=/session><label>Email <input name=email></label>
<label>Password <input name=password type=password></label><button>Sign in</button></form>`)
	})
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		got["email"], got["password"] = r.FormValue("email"), r.FormValue("password")
		mu.Unlock()
		fmt.Fprintf(w, `<!doctype html><title>Welcome</title><h1>Welcome %s</h1><p>Your token is %s</p>`, r.FormValue("email"), r.FormValue("password"))
	})
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	srv := httptest.NewUnstartedServer(mux)
	srv.Listener = l
	srv.Start()
	defer srv.Close()
	port := strings.TrimPrefix(srv.URL, "http://127.0.0.1:")

	home := t.TempDir()
	v, err := vault.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	v.SetPIN("", "orchid-garden-42")
	if err := v.Put(vault.Entry{Name: "harbor", Kind: "login", User: "mali@example.com", Value: "S3cret-pass-991", Domains: []string{"127.0.0.1"}}); err != nil {
		t.Fatal(err)
	}
	m := New(home, v)
	defer m.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, err := m.Do(ctx, "agent", Action{Tool: "open", URL: srv.URL + "/login"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Sign in to Harbor") || !strings.Contains(res.Text, "textbox") || !strings.Contains(res.Text, "password") {
		t.Fatalf("snapshot:\n%s", res.Text)
	}
	// DevTools over the pipe: no port another program or OS account could reach
	if os.Getenv("HPK_BROWSER_DEVTOOLS") == "" {
		if st := m.State(); st.DevTools != "pipe" {
			t.Errorf("DevTools over %q, want pipe", st.DevTools)
		}
		if _, err := os.Stat(filepath.Join(Dir(home), "profile", "DevToolsActivePort")); err == nil {
			t.Error("the browser opened a DevTools port")
		}
	}
	ref := func(snap, word string) string {
		for _, l := range strings.Split(snap, "\n") {
			if strings.Contains(strings.ToLower(l), word) && strings.HasPrefix(l, "[e") {
				return l[1:strings.Index(l, "]")]
			}
		}
		t.Fatalf("no %s in\n%s", word, snap)
		return ""
	}
	email, pass := ref(res.Text, "email"), ref(res.Text, "password")
	if _, err := m.Do(ctx, "agent", Action{Tool: "fill_secret", Ref: email, Secret: "{{secret:harbor.user}}"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Do(ctx, "agent", Action{Tool: "type", Ref: pass, Text: "{{secret:harbor}}"}); err != nil {
		t.Fatal(err)
	}
	snap, _ := m.Do(ctx, "agent", Action{Tool: "snapshot"})
	if strings.Contains(snap.Text, "mali@example.com") || strings.Contains(snap.Text, "S3cret") || !strings.Contains(snap.Text, "{{secret:harbor.user}}") {
		t.Fatalf("filled values are hidden from the agent:\n%s", snap.Text)
	}
	if _, err := m.Do(ctx, "agent", Action{Tool: "click", Text: "Sign in"}); err == nil {
		t.Fatal("no text questions on a page with a filled secret")
	}
	if _, err := m.Do(ctx, "agent", Action{Tool: "wait", Text: "guess-a"}); err == nil {
		t.Fatal("wait-for-text cannot test the value")
	}
	if _, err := m.Do(ctx, "agent", Action{Tool: "fill_secret", Ref: email, Secret: "harbor"}); err == nil {
		t.Fatal("a password goes only into a password field")
	}
	if _, err := m.Do(ctx, "human", Action{Tool: "type", Ref: pass, Text: "{{secret:harbor}}"}); err == nil {
		t.Fatal("the Console cannot ask hpk to fill a secret")
	}
	if _, err := m.Do(ctx, "agent", Action{Tool: "click", Ref: ref(snap.Text, "button")}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if got["email"] != "mali@example.com" || got["password"] != "S3cret-pass-991" {
		t.Fatalf("the site got the secrets: %v", got)
	}
	mu.Unlock()
	after, _ := m.Do(ctx, "agent", Action{Tool: "snapshot"})
	if strings.Contains(after.Text, "S3cret") || strings.Contains(after.Text, "mali@example.com") || !strings.Contains(after.Text, "{{secret:harbor}}") {
		t.Fatalf("page text is scrubbed:\n%s", after.Text)
	}
	// the same page on another host name: not an allowed site
	if _, err := m.Do(ctx, "agent", Action{Tool: "open", URL: "http://localhost:" + port + "/login"}); err != nil {
		t.Fatal(err)
	}
	s2, _ := m.Do(ctx, "agent", Action{Tool: "snapshot"})
	if _, err := m.Do(ctx, "agent", Action{Tool: "fill_secret", Ref: ref(s2.Text, "password"), Secret: "harbor"}); err == nil || !strings.Contains(err.Error(), "only be used on") {
		t.Fatalf("other sites are refused: %v", err)
	}
	// the person takes over: the agent waits
	m.SetDriver("human")
	if _, err := m.Do(ctx, "agent", Action{Tool: "snapshot"}); err != ErrHuman {
		t.Fatalf("agent waits for the person: %v", err)
	}
	m.SetDriver("agent")
	shot, err := m.Do(ctx, "agent", Action{Tool: "screenshot"})
	if err != nil || len(shot.Image) < 1000 {
		t.Fatalf("screenshot: %v", err)
	}
	m.Stop()
	runs := Runs(home)
	if len(runs) != 1 || runs[0].Steps < 6 || runs[0].Agent < 6 {
		t.Fatalf("recorded: %+v", runs)
	}
	steps, _ := RunSteps(home, runs[0].ID)
	raw, _ := os.ReadFile(home + "/hpk/browser/runs/" + runs[0].ID + "/steps.jsonl")
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.Contains(l, "S3cret") || strings.Contains(l, "mali@example.com") {
			t.Fatalf("recordings hold no secret values: %s", l)
		}
	}
	if steps[0].Shot == "" {
		t.Fatal("steps have screenshots")
	}
}
