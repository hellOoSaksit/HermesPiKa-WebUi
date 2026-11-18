// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// In solo mode a program on this machine cannot change anything: changes need
// the cookie a browser gets from hpk's one-time link.
func TestConsoleKeyGuardsChanges(t *testing.T) {
	home := t.TempDir()
	s := NewServer(Options{Home: home})
	ts := httptest.NewServer(s.Routes(fstest.MapFS{"index.html": {Data: []byte("x")}}))
	defer ts.Close()
	s.keys = newConsoleKeys(home)
	s.keys.addr = strings.TrimPrefix(ts.URL, "http://")
	link := s.keys.fresh()
	if raw, _ := os.ReadFile(LinkFile(home)); strings.TrimSpace(string(raw)) != link {
		t.Fatal("the link is kept for hpk open")
	}
	post := func(c *http.Client, path string) int {
		req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(`{"mode":"auto"}`))
		req.Header.Set("X-HPK", "1")
		req.Header.Set("Content-Type", "application/json")
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	program := &http.Client{}
	if code := post(program, "/api/mode"); code != http.StatusUnauthorized {
		t.Fatalf("a program without the key changes the mode: %d", code)
	}
	if res, _ := program.Get(ts.URL + "/api/console-key"); res.StatusCode != 200 {
		t.Fatal("reading still works")
	}
	jar, _ := cookiejar.New(nil)
	person := &http.Client{Jar: jar}
	if res, err := person.Get(link); err != nil || res.StatusCode != 200 {
		t.Fatalf("open the link: %v %v", err, res)
	}
	if code := post(person, "/api/console-key/forget"); code != 200 {
		t.Fatalf("the person's browser may change things: %d", code)
	}
	if code := post(person, "/api/console-key/forget"); code != http.StatusUnauthorized {
		t.Fatal("signed out")
	}
	if res, _ := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(link); res.StatusCode != http.StatusForbidden {
		t.Fatal("a link works once")
	}
}
