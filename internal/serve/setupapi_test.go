// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hpk.local/hpk/assets"
)

func TestShutdownNeedsTheStopKey(t *testing.T) {
	s := NewServer(Options{Home: t.TempDir()})
	ts := httptest.NewServer(s.Routes(assets.Console()))
	defer ts.Close()
	defer s.acp.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.life.stop, s.life.stopKey = cancel, "k123"

	try := func(key string) int {
		req, _ := http.NewRequest("POST", ts.URL+"/api/shutdown", strings.NewReader("{}"))
		req.Header.Set("X-HPK", "1")
		if key != "" {
			req.Header.Set("X-HPK-Stop-Key", key)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if c := try(""); c != http.StatusForbidden {
		t.Fatalf("no key: %d", c)
	}
	if c := try("nope"); c != http.StatusForbidden {
		t.Fatalf("wrong key: %d", c)
	}
	if ctx.Err() != nil {
		t.Fatal("stopped without the key")
	}
	if c := try("k123"); c != http.StatusOK {
		t.Fatalf("right key: %d", c)
	}
	<-ctx.Done() // Run's context is cancelled: serve ends and stops its browser
}

func TestSetupNeedsConsentAndThisMachine(t *testing.T) {
	t.Setenv("HPK_DATA_DIR", t.TempDir())
	s := NewServer(Options{Home: t.TempDir()})
	defer s.acp.Close()
	h := s.Routes(assets.Console())

	do := func(method, path, body, remote string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = remote
		req.Host = "127.0.0.1:7878" // solo mode answers only to loopback names
		req.Header.Set("X-HPK", "1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var out map[string]any
		raw, _ := io.ReadAll(rec.Body)
		_ = json.Unmarshal(raw, &out)
		return rec.Code, out
	}
	if c, _ := do("GET", "/api/setup", "", "192.0.2.7:5000"); c != http.StatusForbidden {
		t.Fatalf("another machine read setup: %d", c)
	}
	c, out := do("GET", "/api/setup", "", "127.0.0.1:5000")
	if c != http.StatusOK {
		t.Fatalf("setup: %d %v", c, out)
	}
	offer, _ := out["offer"].(map[string]any)
	if offer["version"] != "v0.23.1" {
		t.Fatalf("offer: %v", out["offer"])
	}
	if c, _ := do("POST", "/api/setup/hermes", `{}`, "127.0.0.1:5000"); c != http.StatusBadRequest {
		t.Fatalf("install without consent: %d", c)
	}
	if c, _ := do("POST", "/api/setup/install", `{"consent":false}`, "127.0.0.1:5000"); c != http.StatusBadRequest {
		t.Fatalf("hpk install without consent: %d", c)
	}
}

func TestPWAFilesServed(t *testing.T) {
	s := NewServer(Options{Home: t.TempDir()})
	defer s.acp.Close()
	ts := httptest.NewServer(s.Routes(assets.Console()))
	defer ts.Close()
	for path, ctype := range map[string]string{"/manifest.webmanifest": "application/manifest+json", "/sw.js": "javascript", "/offline.html": "text/html", "/icons/icon-192.png": "image/png", "/icons/icon.svg": "image/svg+xml"} {
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Type"), ctype) {
			t.Fatalf("%s: %d %s", path, res.StatusCode, res.Header.Get("Content-Type"))
		}
		// The worker and manifest load under the same strict policy as the Console.
		if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
			t.Fatalf("%s: CSP %q", path, csp)
		}
	}
}
