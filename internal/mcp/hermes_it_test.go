// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package mcp

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Runs against a real Hermes (e.g. inside the hpk-hermes container):
//
//	HPK_IT_HERMES=1 HPK_IT_URL=https://mcp.deepwiki.com/mcp go test ./internal/mcp -run Real
//
// It uses a throw-away HERMES_HOME, so no real profile is touched.
func TestRealHermes(t *testing.T) {
	if os.Getenv("HPK_IT_HERMES") != "1" {
		t.Skip("set HPK_IT_HERMES=1 to run against a real hermes")
	}
	if _, err := exec.LookPath("hermes"); err != nil {
		t.Skip("hermes not installed")
	}
	dir := t.TempDir()
	ctx := context.Background()
	if u := os.Getenv("HPK_IT_URL"); u != "" {
		res, err := Add(ctx, dir, Spec{Name: "web", URL: u, Timeout: 30}, true)
		if err != nil || !res.Saved || !res.Enabled || len(res.Tools) == 0 {
			t.Fatalf("web MCP: %v %+v", err, res)
		}
		// again, with the same name: Hermes asks to overwrite first
		if res, err = Add(ctx, dir, Spec{Name: "web", URL: u, Timeout: 30}, true); err != nil || !res.Enabled {
			t.Fatalf("re-add: %v %+v", err, res)
		}
		if res, _ := Test(ctx, dir, "web"); !res.Connected || len(res.Tools) == 0 {
			t.Fatalf("test: %+v", res)
		}
	}
	// a token for an MCP that does not answer: saved switched off, token in .env only
	res, err := Add(ctx, dir, Spec{Name: "tok", URL: "https://127.0.0.2:9/mcp", Token: "it-secret-123456", Timeout: 3}, false)
	if err != nil || !res.Saved || res.Enabled {
		t.Fatalf("token MCP: %v %+v", err, res)
	}
	cfg, _ := os.ReadFile(configPath(dir))
	env, _ := os.ReadFile(dir + "/.env")
	if strings.Contains(string(cfg), "it-secret") || !strings.Contains(string(env), "it-secret-123456") || strings.Contains(res.Message, "it-secret") {
		t.Fatal("the token goes to Hermes' .env only")
	}
	// a local program that is not installed
	if res, err = Add(ctx, dir, Spec{Name: "blender", Command: "no-such-mcp-program", Args: []string{"--stdio"}, Env: map[string]string{"A_KEY": "v"}, Timeout: 5}, false); err != nil || !res.Saved || res.Enabled {
		t.Fatalf("local: %v %+v", err, res)
	}
	list, err := List(dir, false)
	if err != nil || len(list) < 2 {
		t.Fatalf("list: %v %+v", err, list)
	}
	for _, s := range list {
		if s.Name == "tok" && s.Auth != "token" {
			t.Fatalf("token auth is visible: %+v", s)
		}
		if s.Name == "blender" && (s.Kind != "local" || len(s.Args) != 1 || s.Args[0] != "--stdio" || len(s.Env) != 1) {
			t.Fatalf("local: %+v", s)
		}
	}
	if err := Remove(ctx, dir, "blender"); err != nil {
		t.Fatal(err)
	}
	if l, _ := List(dir, false); len(l) != len(list)-1 {
		t.Fatal("removed")
	}
}
