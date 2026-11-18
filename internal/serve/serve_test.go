// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hpk.local/hpk/assets"
)

func TestConsoleAndHealth(t *testing.T) {
	ts := httptest.NewServer(Handler(Options{Console: assets.Console()}))
	defer ts.Close()

	cases := []struct{ path, want string }{
		{"/", "<html"},
		{"/js/app.js", "import"},
		{"/i18n/th.json", "{"},
		{"/api/health", `"status":"ok"`},
	}
	for _, c := range cases {
		res, err := http.Get(ts.URL + c.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !strings.Contains(string(body), c.want) {
			t.Errorf("%s: status %d, body missing %q", c.path, res.StatusCode, c.want)
		}
		if res.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("%s: no CSP header", c.path)
		}
	}
}

func TestRefusesNonLoopback(t *testing.T) {
	err := Run(context.Background(), Options{Addr: "0.0.0.0:0", Console: assets.Console()})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("want loopback refusal, got %v", err)
	}
}
