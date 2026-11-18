// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hpk.local/hpk/internal/project"
)

const toolCall = `{"hook_event_name":"pre_tool_call","tool_name":"terminal","tool_input":{"command":"ls"},"session_id":"s1","cwd":"/tmp","profile":"default"}`

func call(name, in string) (string, int) {
	var out bytes.Buffer
	code := Run(name, strings.NewReader(in), &out)
	return out.String(), code
}

func TestNoopAllowsSilently(t *testing.T) {
	if out, code := call("noop", toolCall); out != "" || code != 0 {
		t.Fatalf("want silent allow, got %q code %d", out, code)
	}
}

func TestGuardsFailClosedInHermesDialect(t *testing.T) {
	for _, name := range []string{"red_line_guard", "privacy_guard"} {
		out, code := call(name, `not json`)
		var r Reply
		if err := json.Unmarshal([]byte(out), &r); err != nil || r.Action != "block" || code != 2 {
			t.Errorf("%s: want action=block exit 2, got %q code %d", name, out, code)
		}
	}
}

func TestByteOrderMarkIsIgnored(t *testing.T) {
	if out, code := call("noop", "\xef\xbb\xbf"+toolCall); out != "" || code != 0 {
		t.Fatalf("BOM-prefixed event should parse, got %q code %d", out, code)
	}
}

func TestOtherHooksStepAside(t *testing.T) {
	if out, code := call("board_heartbeat", `not json`); out != "" || code != 0 {
		t.Fatalf("got %q code %d", out, code)
	}
}

func TestAskMapsToHermesApprove(t *testing.T) {
	var out bytes.Buffer
	if code := answer(&out, result{Ask, "push is a red line"}); code != 0 {
		t.Fatalf("ask must not exit 2")
	}
	var r Reply
	_ = json.Unmarshal(out.Bytes(), &r)
	if r.Action != "approve" || r.Message == "" {
		t.Fatalf("got %+v", r)
	}
}

func TestLocalOnlyEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HPK_HOME", home)
	root := filepath.Join(home, "code", "quiet-app")
	os.MkdirAll(root, 0o755)
	if _, err := project.Create(home, "quiet-app", root, "private"); err != nil {
		t.Fatal(err)
	}
	if err := project.SetField(home, "quiet-app", []string{"privacy", "model_policy"}, "local_only", "test"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HPK_PROJECT", "quiet-app")
	read := `{"hook_event_name":"pre_tool_call","tool_name":"read_file","tool_input":{"path":"README.md"},"session_id":"%s","cwd":"` + filepath.ToSlash(root) + `"}`
	api := `{"hook_event_name":"%s","session_id":"%s","extra":{"model":"m","provider":"p","base_url":"%s"}}`

	if _, code := call("privacy_guard", fmt.Sprintf(read, "a")); code != 2 {
		t.Fatal("no route recorded yet: must block")
	}
	call("model_route", fmt.Sprintf(api, "pre_api_request", "a", "http://127.0.0.1:11434/v1"))
	if out, code := call("privacy_guard", fmt.Sprintf(read, "a")); code != 0 {
		t.Fatalf("local route must pass: %s", out)
	}
	call("model_route", fmt.Sprintf(api, "pre_auxiliary_call", "a", "https://api.example.com/v1"))
	if out, code := call("privacy_guard", fmt.Sprintf(read, "a")); code != 2 || !strings.Contains(out, "api.example.com") {
		t.Fatalf("auxiliary call to a remote host must block: %s", out)
	}
	if out, code := call("model_route", `{"hook_event_name":"pre_api_request"}`); out != "" || code != 0 {
		t.Fatalf("model_route is an observer: %q %d", out, code)
	}
}

// Guards write a trace to <home>/hpk/hooks.log; tests must never touch a real Hermes home.
func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "hpk-hook-test")
	os.Setenv("HPK_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// A session that runs inside a Hermes gateway has the gateway's cwd and env;
// the guard must still find its project through the link hpk recorded.
func TestGatewaySessionsResolveByLink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HPK_HOME", home)
	t.Setenv("HPK_PROJECT", "")
	root := filepath.Join(home, "code", "quiet-app")
	os.MkdirAll(root, 0o755)
	p, err := project.Create(home, "quiet-app", root, "private")
	if err != nil {
		t.Fatal(err)
	}
	project.LinkSession(p, "local-gw-1")
	ev := `{"hook_event_name":"pre_tool_call","tool_name":"terminal","tool_input":{"command":"git push"},"session_id":"%s","cwd":"` + filepath.ToSlash(home) + `"}`
	if _, code := call("red_line_guard", fmt.Sprintf(ev, "local-gw-1")); code != 0 {
		t.Fatal("push must ask, not block")
	}
	raw, _ := os.ReadFile(filepath.Join(p.Dir, "decisions.jsonl"))
	if !strings.Contains(string(raw), "local-gw-1") {
		t.Fatalf("the decision must be recorded on the linked project: %q", raw)
	}
	// A private project's content must not be sent out from a gateway session either.
	send := `{"hook_event_name":"pre_tool_call","tool_name":"send_message","tool_input":{"text":"hi"},"session_id":"local-gw-1","cwd":"` + filepath.ToSlash(home) + `"}`
	if _, code := call("privacy_guard", send); code != 2 {
		t.Fatal("privacy must apply to a linked gateway session")
	}
}

// A cron run is linked through the tag in its prompt, then stopped mid-run once it
// passes its job budget (cache reads counted at a tenth).
func TestCronRunLinkAndBudget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HPK_HOME", home)
	t.Setenv("HPK_PROJECT", "")
	root := filepath.Join(home, "code", "quiet-app")
	os.MkdirAll(root, 0o755)
	p, err := project.Create(home, "quiet-app", root, "private")
	if err != nil {
		t.Fatal(err)
	}
	project.SetField(home, p.Name, []string{"budget", "nightly_max_tokens"}, 1000, "test")
	llm := `{"hook_event_name":"pre_llm_call","session_id":"cron-1","extra":{"user_message":"Nightly work...\n\n[hpk-job quiet-app/nightly]\n"}}`
	call("session_link", llm)
	if name, job := project.LinkOf(home, "cron-1"); name != "quiet-app" || job != "nightly" {
		t.Fatalf("link: %q %q", name, job)
	}
	tool := `{"hook_event_name":"pre_tool_call","tool_name":"read_file","tool_input":{"path":"README.md"},"session_id":"cron-1","cwd":"` + filepath.ToSlash(home) + `"}`
	usage := `{"hook_event_name":"post_api_request","session_id":"cron-1","extra":{"model":"m","provider":"p","base_url":"https://api.example.com","usage":{"input_tokens":%d,"output_tokens":100,"cache_read_tokens":%d}}}`
	call("model_usage", fmt.Sprintf(usage, 100, 5000)) // weighted 100+100+500 = 700
	if _, code := call("red_line_guard", tool); code != 0 {
		t.Fatal("under budget: tools run")
	}
	call("model_usage", fmt.Sprintf(usage, 200, 2000)) // weighted 700+200+100+200 = 1200
	if out, code := call("red_line_guard", tool); code != 2 || !strings.Contains(out, "over its budget") {
		t.Fatalf("over budget must stop tools: %s", out)
	}
}
