// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/acp"
	"hpk.local/hpk/internal/board"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
	"hpk.local/hpk/internal/wiki"
)

// fakeAgent: answers ACP, streams a reply, asks permission for "git push", and
// ends the turn with the option the client chose. No model, no Hermes.
func fakeAgent(r io.Reader, w io.Writer) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var mu sync.Mutex
	enc := json.NewEncoder(w)
	send := func(v any) { mu.Lock(); enc.Encode(v); mu.Unlock() }
	perm := make(chan string, 1)
	for sc.Scan() {
		var m map[string]json.RawMessage
		json.Unmarshal(sc.Bytes(), &m)
		var method string
		json.Unmarshal(m["method"], &method)
		id := m["id"]
		switch {
		case method == "initialize":
			send(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"protocolVersion": 1}})
		case method == "session/new":
			send(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"sessionId": "h-1"}})
		case method == "session/prompt":
			go func(id json.RawMessage) {
				up := func(u map[string]any) {
					send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "h-1", "update": u}})
				}
				up(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Tests pass. "}})
				up(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Pushing now."}})
				send(map[string]any{"jsonrpc": "2.0", "id": 77, "method": "session/request_permission", "params": map[string]any{
					"sessionId": "h-1", "toolCall": map[string]any{"title": "Push the branch", "rawInput": map[string]string{"command": "git push origin feat/x"}},
					"options": []map[string]string{{"optionId": "allow_once", "name": "Allow once", "kind": "allow_once"}, {"optionId": "deny", "name": "Deny", "kind": "reject_once"}}}})
				choice := <-perm
				send(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"stopReason": "end_turn:" + choice}})
			}(id)
		case method == "" && string(id) == "77":
			var r struct {
				Outcome struct{ OptionID string } `json:"outcome"`
			}
			json.Unmarshal(m["result"], &r)
			perm <- r.Outcome.OptionID
		}
	}
}

func fakeLauncher(p *project.Project, h acp.Handlers) (*acp.Client, error) {
	ar, cw := io.Pipe()
	cr, aw := io.Pipe()
	go fakeAgent(ar, aw)
	return acp.New(cr, cw, h), nil
}

// Fictional sample project.
func setup(t *testing.T) (*Server, *httptest.Server, *project.Project) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	root := filepath.Join(home, "code", "sample-app")
	os.MkdirAll(root, 0o755)
	for _, a := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"}} {
		exec.Command("git", append([]string{"-C", root}, a...)...).Run()
	}
	p, err := project.Create(home, "sample-app", root, project.Shared)
	if err != nil {
		t.Fatal(err)
	}
	w := wiki.Open(p.WikiDir())
	w.Init(p.Name, assets.WikiTemplates())
	w.CreateDraft(wiki.DraftInput{Target: "lessons/csv.md", Title: "CSV needs a BOM", Sources: []string{"src/csv.go:3"}, Body: "Spreadsheets need it."})
	s := NewServer(Options{Home: home, Launcher: fakeLauncher})
	ts := httptest.NewServer(s.Routes(assets.Console()))
	t.Cleanup(func() { ts.Close(); s.acp.Close() })
	t.Setenv("HPK_USER", "reviewer")
	return s, ts, p
}

func post(t *testing.T, url string, body any, header bool) (*http.Response, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if header {
		req.Header.Set("X-HPK", "1")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res, out
}

func snapshot(t *testing.T, ts *httptest.Server) Snapshot {
	t.Helper()
	res, err := http.Get(ts.URL + "/api/snapshot?project=sample-app")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var snap Snapshot
	json.NewDecoder(res.Body).Decode(&snap)
	return snap
}

func TestSnapshotShapes(t *testing.T) {
	_, ts, p := setup(t)
	session.New(p, session.NewOptions{Name: "release"})
	db, _ := board.Open(p)
	db.ClaimWork(board.Scope{Task: "export", Paths: []string{"api/**"}}, "reviewer", "", false)
	db.Close()
	snap := snapshot(t, ts)
	if !snap.Live || snap.ProjectID != "sample-app" || len(snap.Projects) != 1 {
		t.Fatalf("snapshot head: %+v", snap)
	}
	if len(snap.Sessions) != 1 || snap.Sessions[0].Num != "1" || snap.Sessions[0].Status != "queued" {
		t.Fatalf("sessions: %+v", snap.Sessions)
	}
	if len(snap.Claims) != 1 || len(snap.Drafts) != 1 || snap.Drafts[0].Path != "lessons/csv.md" {
		t.Fatalf("claims/drafts: %+v %+v", snap.Claims, snap.Drafts)
	}
	if len(snap.Approvals) != 1 || snap.Approvals[0].Kind != "draft" {
		t.Fatalf("approvals: %+v", snap.Approvals)
	}
}

func TestWritesNeedHeaderAndSameOrigin(t *testing.T) {
	_, ts, _ := setup(t)
	if res, _ := post(t, ts.URL+"/api/mode", map[string]any{"project": "sample-app", "value": "auto"}, false); res.StatusCode != 403 {
		t.Fatalf("no X-HPK header must be refused, got %d", res.StatusCode)
	}
	raw, _ := json.Marshal(map[string]any{"project": "sample-app", "value": "auto"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/mode", bytes.NewReader(raw))
	req.Header.Set("X-HPK", "1")
	req.Header.Set("Origin", "https://evil.example.test")
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != 403 {
		t.Fatalf("foreign origin must be refused, got %d", res.StatusCode)
	}
	if res, _ := post(t, ts.URL+"/api/mode", map[string]any{"project": "sample-app", "value": "auto", "minutes": 60}, true); res.StatusCode != 200 {
		t.Fatalf("mode change: %d", res.StatusCode)
	}
	if snap := snapshot(t, ts); snap.Mode.Value != "auto" || snap.Mode.AutoUntil == nil {
		t.Fatalf("mode: %+v", snap.Mode)
	}
}

func TestApproveDraftThroughAPI(t *testing.T) {
	_, ts, p := setup(t)
	res, out := post(t, ts.URL+"/api/wiki/drafts", map[string]any{"project": "sample-app", "nums": []int{1}, "decision": "approve"}, true)
	if res.StatusCode != 200 {
		t.Fatalf("approve: %d %v", res.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(p.WikiDir(), "lessons", "csv.md")); err != nil {
		t.Fatal("page not written")
	}
	snap := snapshot(t, ts)
	if len(snap.Drafts) != 0 || len(snap.WikiTree) != 1 || snap.WikiTree[0].Pages[0].Title != "CSV needs a BOM" {
		t.Fatalf("after approve: %+v %+v", snap.Drafts, snap.WikiTree)
	}
	pg, _ := http.Get(ts.URL + "/api/wiki/page?project=sample-app&path=lessons/csv.md")
	var page map[string]any
	json.NewDecoder(pg.Body).Decode(&page)
	if page["markdown"] != "Spreadsheets need it.\n" {
		t.Fatalf("page body: %v", page)
	}
	bad, _ := http.Get(ts.URL + "/api/wiki/page?project=sample-app&path=../../project.yaml")
	if bad.StatusCode == 200 {
		t.Fatal("path traversal must be refused")
	}
}

func TestPermissionRoundTrip(t *testing.T) {
	s, ts, p := setup(t)
	res, out := post(t, ts.URL+"/api/sessions", map[string]any{"project": "sample-app", "name": "release", "start": true, "firstMessage": "Ship it"}, true)
	if res.StatusCode != 200 {
		t.Fatalf("start: %d %v", res.StatusCode, out)
	}
	id := out["id"].(string)
	// Wait for the held permission to show up as an approval.
	var perm string
	for i := 0; i < 100 && perm == ""; i++ {
		for _, a := range snapshot(t, ts).Approvals {
			if a.Kind == "permission" {
				perm = a.ID
				if a.RedLine != "push" || a.Session != id {
					t.Fatalf("permission approval: %+v", a)
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if perm == "" {
		t.Fatal("permission never reached the Console")
	}
	if sess, _ := session.Get(p, id); sess.Status != session.Waiting {
		t.Fatalf("session must wait for a person: %s", sess.Status)
	}
	if res, _ := post(t, ts.URL+"/api/approvals/"+perm, map[string]any{"project": "sample-app", "decision": "deny"}, true); res.StatusCode != 200 {
		t.Fatalf("decide: %d", res.StatusCode)
	}
	for i := 0; i < 100; i++ {
		msgs, live, streaming := s.acp.Transcript(id)
		if live && !streaming && len(msgs) >= 3 {
			var kinds []string
			for _, m := range msgs {
				kinds = append(kinds, m.T)
				if m.T == "permit" && m.Decision != "deny" {
					t.Fatalf("permit decision: %+v", m)
				}
				if m.T == "msg" && m.Who == "agent" && m.Text != "Tests pass. Pushing now." {
					t.Fatalf("streamed text: %q", m.Text)
				}
			}
			if strings.Join(kinds, ",") != "msg,msg,permit" {
				t.Fatalf("transcript: %v", kinds)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	msgs, live, streaming := s.acp.Transcript(id)
	t.Fatalf("turn did not finish: live=%v streaming=%v msgs=%+v", live, streaming, msgs)
}

func TestSSE(t *testing.T) {
	s, ts, _ := setup(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	r := bufio.NewReader(res.Body)
	line, _ := r.ReadString('\n')
	if line != "event: hello\n" {
		t.Fatalf("first event: %q", line)
	}
	r.ReadString('\n')
	r.ReadString('\n')
	go func() { time.Sleep(50 * time.Millisecond); s.hub.send("changed", "sample-app") }()
	line, _ = r.ReadString('\n')
	if line != "event: changed\n" {
		t.Fatalf("event: %q", line)
	}
}
