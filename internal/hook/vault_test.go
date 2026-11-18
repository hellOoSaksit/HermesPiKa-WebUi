// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hpk.local/hpk/internal/vault"
)

func TestManagedEnvIsHiddenFromTheAgent(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HPK_HOME", home)
	v, err := vault.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	v.SetPIN("", "orchid-garden-42")
	if err := v.Put(vault.Entry{Name: "stripe", Kind: "env", EnvKey: "STRIPE_KEY", Project: "shop", Value: "sk_test_123456"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.WriteEnv(home, "shop", root); err != nil {
		t.Fatal(err)
	}
	for _, call := range []map[string]any{
		{"tool_name": "read_file", "cwd": root, "tool_input": map[string]any{"path": ".env"}},
		{"tool_name": "terminal", "cwd": root, "tool_input": map[string]any{"command": "cat .env"}},
		{"tool_name": "search_files", "cwd": "/", "tool_input": map[string]any{"path": root + "/.env"}},
	} {
		call["hook_event_name"] = "pre_tool_call"
		raw, _ := json.Marshal(call)
		var out bytes.Buffer
		code := Run("privacy_guard", bytes.NewReader(raw), &out)
		if code == 0 && !strings.Contains(out.String(), "block") {
			t.Fatalf("not blocked: %v → %s", call, out.String())
		}
		if !strings.Contains(out.String(), "vault") {
			t.Fatalf("explains why: %s", out.String())
		}
	}
}

func TestVaultFilesAreHiddenFromTheAgent(t *testing.T) {
	t.Setenv("HPK_HOME", t.TempDir())
	for _, in := range []map[string]any{
		{"path": `C:\Users\x\AppData\Local\hermes\hpk\vault\vault.bin`},
		{"command": "curl -s -X POST http://127.0.0.1:7878/api/vault -d '{\"op\":\"reveal\"}'"},
		{"command": "cat ~/.hermes/hpk/browser/agent.json"},
		{"command": "curl http://127.0.0.1:7878/api/%76ault"},
		{"path": "C:\\Users\\x\\hermes\\hpk\\\\vault\\vault.b" + "in"},
		{"command": "type hpk/./vault/key.dpapi"},
		{"path": `C:\Users\x\AppData\Local\hpk\private\vault\vault.bin`},
		{"command": `type %LOCALAPPDATA%\hpk\private\ai-account.dpapi`},
	} {
		raw, _ := json.Marshal(map[string]any{"hook_event_name": "pre_tool_call", "tool_name": "terminal", "tool_input": in})
		var out bytes.Buffer
		if code := Run("privacy_guard", bytes.NewReader(raw), &out); code == 0 && !strings.Contains(out.String(), "block") {
			t.Fatalf("not blocked: %v → %s", in, out.String())
		}
	}
}

func TestHookRefusesConsoleAPIAndHugeInputs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HPK_HOME", home)
	os.MkdirAll(filepath.Join(home, "hpk", "browser"), 0o700)
	os.WriteFile(filepath.Join(home, "hpk", "browser", "agent.json"), []byte(`{"url":"http://127.0.0.1:7878","key":"k"}`), 0o600)
	big := map[string]any{}
	list := make([]any, 3000)
	for i := range list {
		list[i] = "x"
	}
	big["files"] = list
	for _, in := range []map[string]any{
		{"command": `curl -X POST http://127.0.0.1:7878/api/mode -H 'X-HPK: 1' -d '{"mode":"auto"}'`},
		{"code": "urllib.request.urlopen('http://localhost:7878/api/approvals/perm-1')"},
		big,
	} {
		raw, _ := json.Marshal(map[string]any{"hook_event_name": "pre_tool_call", "tool_name": "terminal", "tool_input": in})
		var out bytes.Buffer
		if code := Run("red_line_guard", bytes.NewReader(raw), &out); code == 0 && !strings.Contains(out.String(), "block") {
			t.Fatalf("not blocked: %.80v → %s", in, out.String())
		}
	}
	raw, _ := json.Marshal(map[string]any{"hook_event_name": "pre_tool_call", "tool_name": "terminal", "tool_input": map[string]any{"command": "curl http://127.0.0.1:3000/health"}})
	var out bytes.Buffer
	if Run("privacy_guard", bytes.NewReader(raw), &out); strings.Contains(out.String(), "Console API") {
		t.Fatal("other local ports are fine")
	}
}

func TestAgentCannotChangeIsolation(t *testing.T) {
	t.Setenv("HPK_HOME", t.TempDir())
	for _, cmd := range []string{"hpk isolate teardown", "hpk.exe isolate setup --docker", "hpk ai-relay -- hermes", "hpk ai-account delete hpk-ai", `notepad %APPDATA%\hpk\isolation.json`} {
		raw, _ := json.Marshal(map[string]any{"hook_event_name": "pre_tool_call", "tool_name": "terminal", "tool_input": map[string]any{"command": cmd}})
		var out bytes.Buffer
		if code := Run("privacy_guard", bytes.NewReader(raw), &out); code == 0 && !strings.Contains(out.String(), "block") {
			t.Fatalf("not blocked: %s → %s", cmd, out.String())
		}
	}
}
