// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hpk.local/hpk/internal/acp"
	"hpk.local/hpk/internal/modelswitch"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
	"hpk.local/hpk/internal/team"
)

// modelAgent offers three models, records set_model and every prompt text, and
// answers each prompt at once.
type modelAgent struct {
	mu      sync.Mutex
	prompts []string
	sets    []string
	n       int
}

func (a *modelAgent) run(r io.Reader, w io.Writer) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var wmu sync.Mutex
	enc := json.NewEncoder(w)
	send := func(v any) { wmu.Lock(); enc.Encode(v); wmu.Unlock() }
	for sc.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		json.Unmarshal(sc.Bytes(), &m)
		switch m.Method {
		case "initialize":
			send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"protocolVersion": 1}})
		case "session/new":
			a.mu.Lock()
			a.n++
			sid := "h-" + string(rune('0'+a.n))
			a.mu.Unlock()
			send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"sessionId": sid, "models": map[string]any{
				"currentModelId":  "anthropic:m-small",
				"availableModels": []map[string]string{{"modelId": "anthropic:m-small", "name": "small"}, {"modelId": "anthropic:m-big", "name": "big"}, {"modelId": "openai:o-mid", "name": "mid"}},
			}}})
		case "session/set_model":
			var p struct{ SessionID, ModelID string }
			json.Unmarshal(m.Params, &p)
			a.mu.Lock()
			a.sets = append(a.sets, p.SessionID+"="+p.ModelID)
			a.mu.Unlock()
			send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{}})
		case "session/prompt":
			var p struct {
				SessionID string
				Prompt    []struct{ Text string }
			}
			json.Unmarshal(m.Params, &p)
			a.mu.Lock()
			a.prompts = append(a.prompts, p.Prompt[0].Text)
			a.mu.Unlock()
			send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": p.SessionID,
				"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "ok"}}}})
			send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"stopReason": "end_turn"}})
		}
	}
}

func (a *modelAgent) snapshot() ([]string, []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string{}, a.prompts...), append([]string{}, a.sets...)
}

func modelSetup(t *testing.T) (*Server, string, *project.Project, *modelAgent) {
	t.Helper()
	s, ts, p := setup(t)
	agent := &modelAgent{}
	s.acp.launch = func(p *project.Project, h acp.Handlers) (*acp.Client, error) {
		ar, cw := io.Pipe()
		cr, aw := io.Pipe()
		go agent.run(ar, aw)
		return acp.New(cr, cw, h), nil
	}
	// models.dev: small has a 10k window, big 1M; both priced.
	cache := `{"anthropic":{"models":{"m-small":{"cost":{"input":1,"output":5},"limit":{"context":10000}},"m-big":{"cost":{"input":5,"output":25,"cache_write":6.25},"limit":{"context":1000000}}}},"openai":{"models":{"o-mid":{"cost":{"input":2,"output":8},"limit":{"context":400000}}}}}`
	os.WriteFile(filepath.Join(s.home, "models_dev_cache.json"), []byte(cache), 0o644)
	os.MkdirAll(filepath.Join(p.WikiDir(), "decisions"), 0o755)
	os.WriteFile(filepath.Join(p.WikiDir(), "decisions", "auth.md"), []byte("---\ntitle: Auth\n---\n\nUse rotating refresh tokens.\n"), 0o644)
	return s, ts.URL, p, agent
}

func getModels(t *testing.T, url string) modelView {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var v modelView
	json.NewDecoder(res.Body).Decode(&v)
	return v
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 200 && !ok(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !ok() {
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestModelSwitchKeepsMemory(t *testing.T) {
	s, url, p, agent := modelSetup(t)
	res, out := post(t, url+"/api/sessions", map[string]any{"project": "sample-app", "name": "release", "firstMessage": "Plan the release", "start": true}, true)
	if res.StatusCode != 200 {
		t.Fatalf("start: %d %v", res.StatusCode, out)
	}
	id := out["id"].(string)
	waitFor(t, "first turn", func() bool { pr, _ := agent.snapshot(); return len(pr) == 1 && !s.acp.sessions[id].streaming })

	v := getModels(t, url+"/api/sessions/"+id+"/models?project=sample-app&recall=decisions/auth.md,../etc/passwd")
	if !v.Live || v.Current != "anthropic:m-small" || len(v.Options) != 3 || v.State.Brief == 0 || v.State.Recall == 0 {
		t.Fatalf("picker: %+v", v)
	}
	by := map[string]int{}
	for i, o := range v.Options {
		by[o.Model.ID] = i
	}
	big, mid := v.Options[by["anthropic:m-big"]], v.Options[by["openai:o-mid"]]
	if big.Way != "continue" || big.CrossProvider || big.FirstCost <= 0 || !big.Model.Known {
		t.Fatalf("same provider, roomy window: %+v", big)
	}
	if !mid.CrossProvider || v.Options[by["anthropic:m-small"]].Locked != "current" {
		t.Fatalf("cross provider / current: %+v", v.Options)
	}

	// Another provider without confirm is the red line.
	if res, _ := post(t, url+"/api/sessions/"+id+"/model", map[string]any{"project": "sample-app", "model": "openai:o-mid"}, true); res.StatusCode != http.StatusConflict {
		t.Fatalf("unconfirmed cross-provider switch: %d", res.StatusCode)
	}
	if res, out := post(t, url+"/api/sessions/"+id+"/model", map[string]any{"project": "sample-app", "model": "anthropic:m-big", "recall": []string{"decisions/auth.md"}}, true); res.StatusCode != 200 || out["way"] != "continue" {
		t.Fatalf("switch: %d %v", res.StatusCode, out)
	}
	waitFor(t, "switch", func() bool { _, sets := agent.snapshot(); return len(sets) == 1 })
	waitFor(t, "switch recorded", func() bool { x, _ := session.Get(p, id); return len(x.Switches) == 1 })
	if _, sets := agent.snapshot(); !strings.HasSuffix(sets[0], "=anthropic:m-big") {
		t.Fatalf("set_model: %v", sets)
	}
	// The chat shows a divider, and the next message carries the brief and the recalled page.
	msgs, _, _ := s.acp.Transcript(id)
	if last := msgs[len(msgs)-1]; last.T != "switch" || last.From != "anthropic:m-small" || last.To != "anthropic:m-big" {
		t.Fatalf("divider: %+v", last)
	}
	post(t, url+"/api/sessions/"+id+"/prompt", map[string]any{"project": "sample-app", "text": "Go on"}, true)
	waitFor(t, "second turn", func() bool { pr, _ := agent.snapshot(); return len(pr) == 2 })
	pr, _ := agent.snapshot()
	for _, want := range []string{"[hpk] This chat moved from anthropic:m-small to anthropic:m-big", "Session brief", "Use rotating refresh tokens", "Go on"} {
		if !strings.Contains(pr[1], want) {
			t.Errorf("carry-over lacks %q:\n%s", want, pr[1])
		}
	}
	if strings.Contains(pr[1], "passwd") {
		t.Fatal("a path outside the wiki must never be read")
	}
	// Only once.
	waitFor(t, "turn end", func() bool { return !s.acp.sessions[id].streaming })
	post(t, url+"/api/sessions/"+id+"/prompt", map[string]any{"project": "sample-app", "text": "Third"}, true)
	waitFor(t, "third turn", func() bool { pr, _ := agent.snapshot(); return len(pr) == 3 })
	if pr, _ := agent.snapshot(); pr[2] != "Third" {
		t.Fatalf("carry must go once: %q", pr[2])
	}
	raw, _ := os.ReadFile(filepath.Join(p.Dir, "audit.log"))
	if !strings.Contains(string(raw), "model switched") {
		t.Fatal("switch not audited")
	}
}

func TestModelSwitchCompactsWhenFull(t *testing.T) {
	s, url, p, agent := modelSetup(t)
	_, out := post(t, url+"/api/sessions", map[string]any{"project": "sample-app", "name": "big", "firstMessage": "x", "start": true}, true)
	id := out["id"].(string)
	waitFor(t, "first turn", func() bool { pr, _ := agent.snapshot(); return len(pr) == 1 && !s.acp.sessions[id].streaming })
	// Hermes reports 300k tokens in use, on the 1M model: back to small (10k) must compact.
	s.acp.onUpdate("h-1", json.RawMessage(`{"sessionUpdate":"usage_update","size":1000000,"used":300000}`))
	s.acp.mu.Lock()
	s.acp.sessions[id].model = "anthropic:m-big"
	s.acp.mu.Unlock()
	if res, out := post(t, url+"/api/sessions/"+id+"/model", map[string]any{"project": "sample-app", "model": "anthropic:m-small"}, true); res.StatusCode != 200 || out["way"] != "compact" {
		t.Fatalf("switch: %d %v", res.StatusCode, out)
	}
	waitFor(t, "switch", func() bool { _, sets := agent.snapshot(); return len(sets) == 1 })
	if pr, _ := agent.snapshot(); pr[1] != "/compress" {
		t.Fatalf("compaction runs before set_model on the old model: %q", pr)
	}
	// the switch is written to the chat record after set_model; let it land before the temp dir goes
	waitFor(t, "switch recorded", func() bool { x, err := session.Get(p, id); return err == nil && len(x.Switches) == 1 })
	waitFor(t, "switch audited", func() bool {
		raw, _ := os.ReadFile(filepath.Join(p.Dir, "audit.log"))
		return strings.Contains(string(raw), "model switched")
	})
}

func TestModelLocalOnlyAndAskOnce(t *testing.T) {
	s, url, p, agent := modelSetup(t)
	_, out := post(t, url+"/api/sessions", map[string]any{"project": "sample-app", "name": "main", "firstMessage": "x", "start": true}, true)
	id := out["id"].(string)
	waitFor(t, "first turn", func() bool { pr, _ := agent.snapshot(); return len(pr) == 1 && !s.acp.sessions[id].streaming })

	// Ask once: a sub-task starts on the other model; the main chat keeps its own.
	res, sub := post(t, url+"/api/sessions/"+id+"/ask", map[string]any{"project": "sample-app", "model": "anthropic:m-big", "text": "Is this schema sound?"}, true)
	if res.StatusCode != 200 || sub["parent"] != id || sub["ask_once"] != true || sub["model"] != "anthropic:m-big" {
		t.Fatalf("ask once: %d %v", res.StatusCode, sub)
	}
	waitFor(t, "sub-task model", func() bool { _, sets := agent.snapshot(); return len(sets) == 1 })
	if _, sets := agent.snapshot(); sets[0] != "h-2=anthropic:m-big" {
		t.Fatalf("only the sub-task switches: %v", sets)
	}
	if st, _ := s.acp.modelState("sample-app", id); st.model != "anthropic:m-small" {
		t.Fatalf("main chat model changed: %s", st.model)
	}
	if res, _ := post(t, url+"/api/sessions/"+id+"/ask", map[string]any{"project": "sample-app", "model": "openai:o-mid", "text": "?"}, true); res.StatusCode != http.StatusConflict {
		t.Fatalf("cross-provider ask needs confirm: %d", res.StatusCode)
	}

	// local_only: a remote model is locked, even confirmed.
	if err := project.SetField(s.home, p.Name, []string{"privacy", "model_policy"}, "local_only", "test"); err != nil {
		t.Fatal(err)
	}
	v := getModels(t, url+"/api/sessions/"+id+"/models?project=sample-app")
	for _, o := range v.Options {
		if o.Locked == "" {
			t.Fatalf("remote model open on local_only: %+v", o)
		}
	}
	if res, _ := post(t, url+"/api/sessions/"+id+"/model", map[string]any{"project": "sample-app", "model": "anthropic:m-big", "confirm": true}, true); res.StatusCode != http.StatusForbidden {
		t.Fatalf("local_only switch: %d", res.StatusCode)
	}
}

func TestModelAPIAccess(t *testing.T) {
	s, url, p, agent := modelSetup(t)
	t.Setenv("HPK_USER", "max") // the chat is max's
	_, out := post(t, url+"/api/sessions", map[string]any{"project": "sample-app", "name": "maxs", "firstMessage": "x", "start": true}, true)
	id := out["id"].(string)
	waitFor(t, "first turn", func() bool { pr, _ := agent.snapshot(); return len(pr) == 1 && !s.acp.sessions[id].streaming })
	if x, _ := session.Get(p, id); x.Owner != "max" {
		t.Fatalf("owner %q", x.Owner)
	}
	c := &team.Config{Enabled: true}
	c.Add("olivia", "", team.Owner, nil, time.Now())
	mTok, _ := c.Add("max", "", team.Member, nil, time.Now())
	nTok, _ := c.Add("nia", "", team.Member, nil, time.Now())
	if err := c.Save(s.home); err != nil {
		t.Fatal(err)
	}
	base := url + "/api/sessions/" + id
	if code, _ := as(t, http.DefaultClient, "GET", base+"/models?project=sample-app", nTok, nil); code != 403 {
		t.Fatalf("nia must not see max's picker: %d", code)
	}
	if code, _ := as(t, http.DefaultClient, "POST", base+"/model", nTok, map[string]any{"project": "sample-app", "model": "anthropic:m-big"}); code != 403 {
		t.Fatalf("nia must not switch max's chat: %d", code)
	}
	if code, _ := as(t, http.DefaultClient, "POST", base+"/ask", nTok, map[string]any{"project": "sample-app", "model": "anthropic:m-big", "text": "?"}); code != 403 {
		t.Fatalf("nia must not ask from max's chat: %d", code)
	}
	if code, _ := as(t, http.DefaultClient, "GET", base+"/models?project=sample-app", mTok, nil); code != 200 {
		t.Fatalf("max opens his own picker: %d", code)
	}
	if _, sets := agent.snapshot(); len(sets) != 0 {
		t.Fatalf("nothing switched: %v", sets)
	}
	// The manager finds chats by id alone: another project never reaches this one.
	other := &project.Project{Name: "other-app"}
	if err := s.acp.SwitchModel(other, id, modelswitch.Option{Model: modelswitch.Model{ID: "anthropic:m-big"}}, "", "x"); err == nil {
		t.Fatal("a switch through another project must be refused")
	}
	if _, ok := s.acp.modelState("other-app", id); ok {
		t.Fatal("model state through another project must be empty")
	}
}
