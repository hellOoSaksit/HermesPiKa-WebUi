// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckPlaces(t *testing.T) {
	blender := Spec{Name: "blender", Command: "uvx", Args: []string{"blender-mcp"}}
	web := Spec{Name: "docs", URL: "https://mcp.example.com/mcp"}
	if err := Check(blender, false); err != nil {
		t.Fatalf("a local program is fine on your machine: %v", err)
	}
	if err := Check(blender, true); err == nil || !strings.Contains(err.Error(), "own machine") {
		t.Fatalf("the server runs no programs: %v", err)
	}
	if err := Check(web, true); err != nil {
		t.Fatalf("a web MCP is fine on the server: %v", err)
	}
	for _, u := range []string{"http://mcp.example.com/mcp", "https://localhost:3845/mcp", "https://127.0.0.1/mcp", "https://[::1]/mcp", "https://studio.localhost/mcp"} {
		if Check(Spec{Name: "x", URL: u}, true) == nil {
			t.Errorf("server refuses %s", u)
		}
	}
	if Check(Spec{Name: "figma", URL: "http://127.0.0.1:3845/mcp"}, false) != nil {
		t.Error("a desktop app's local URL is fine on your machine")
	}
}

func TestCheckRefusesBadInput(t *testing.T) {
	bad := []Spec{
		{Name: "Bad Name", URL: "https://a.example.com"},
		{Name: "both", URL: "https://a.example.com", Command: "uvx"},
		{Name: "none"},
		{Name: "ftp", URL: "ftp://a.example.com"},
		{Name: "flag", Command: "--evil"},
		{Name: "shell", Command: "uvx; rm -rf /"},
		{Name: "nl", Command: "uvx", Args: []string{"a\nb"}},
		{Name: "env", Command: "uvx", Env: map[string]string{"bad key": "v"}},
		{Name: "tok", Command: "uvx", Token: "abc"},
		{Name: "webenv", URL: "https://a.example.com", Env: map[string]string{"K": "v"}},
	}
	for _, s := range bad {
		if Check(s, false) == nil {
			t.Errorf("refused: %+v", s)
		}
	}
}

func TestListHidesSecrets(t *testing.T) {
	dir := t.TempDir()
	cfg := `model: x
mcp_servers:
  roblox:
    command: rbx-studio-mcp
    args: ["--stdio"]
    env:
      ROBLOX_KEY: very-secret-value
  github:
    url: https://api.githubcopilot.com/mcp/
    headers:
      Authorization: Bearer ${MCP_GITHUB_API_KEY}
    enabled: false
`
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600)
	list, err := List(dir, true)
	if err != nil || len(list) != 2 {
		t.Fatalf("%v %+v", err, list)
	}
	gh, rb := list[0], list[1]
	if gh.Kind != "url" || gh.Auth != "token" || gh.Enabled || gh.Blocked != "" {
		t.Fatalf("github: %+v", gh)
	}
	if rb.Kind != "local" || !rb.Enabled || rb.Blocked == "" || len(rb.Env) != 1 || rb.Env[0] != "ROBLOX_KEY" {
		t.Fatalf("roblox: %+v", rb)
	}
	if out, _ := List(dir, false); out[1].Blocked != "" {
		t.Fatal("nothing is blocked on your own machine")
	}
	if l, err := List(t.TempDir(), false); err != nil || len(l) != 0 {
		t.Fatal("no config is an empty list")
	}
}

func TestParseHermesOutput(t *testing.T) {
	ok := parse(`  Connecting to 'deep'...

  ✓ Connected! Found 2 tool(s) from 'deep':

    ask_question                          Ask any question about a repository.
    read_wiki_structure                   Get a list of documentation topics.

  Enable all 2 tools? [Y/n/select]:
  ✓ Saved 'deep' to /x/config.yaml (2/2 tools enabled)`)
	if !ok.Connected || !ok.Saved || !ok.Enabled || len(ok.Tools) != 2 || ok.Tools[0] != "ask_question" {
		t.Fatalf("%+v", ok)
	}
	bad := parse(`  ✗ Failed to connect: Connecting to MCP server 'gh' timed out after 10s
  Save config anyway (you can test later)? [y/N]:   ✓ Saved 'gh' to config (disabled)`)
	if bad.Connected || !bad.Saved || bad.Enabled || !strings.Contains(bad.Message, "Saved") && !strings.Contains(bad.Message, "timed out") {
		t.Fatalf("%+v", bad)
	}
	if got := redact("token abc123456 here", Spec{Token: "abc123456"}); strings.Contains(got, "abc123456") {
		t.Fatal("tokens are removed from output")
	}
}

func TestServerRefusesLocalAndPrivateAddresses(t *testing.T) {
	for _, u := range []string{"https://127.1/mcp", "https://0x7f.1/mcp", "https://2130706433/mcp", "https://10.0.0.5/mcp", "https://192.168.1.2/mcp",
		"https://169.254.169.254/latest", "https://[fc00::1]/mcp", "https://100.64.1.1/mcp", "https://0.0.0.0/mcp", "https://db.internal/mcp"} {
		if Check(Spec{Name: "x", URL: u}, true) == nil {
			t.Errorf("server must refuse %s", u)
		}
	}
	if Check(Spec{Name: "x", URL: "https://93.184.215.14/mcp"}, true) != nil {
		t.Error("a public address is fine")
	}
	if got := SafeURL("https://user:pw@mcp.test/mcp?key=abc#x"); got != "https://mcp.test/mcp" {
		t.Errorf("SafeURL: %s", got)
	}
}
