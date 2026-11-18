// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"
)

// fakeAgent speaks just enough ACP to test the client: it answers initialize and
// session/new, and on session/prompt it streams an update, asks permission, and
// reports what the client chose in its stop reason.
func fakeAgent(r io.Reader, w io.Writer) {
	sc := bufio.NewScanner(r)
	enc := json.NewEncoder(w)
	var mu sync.Mutex
	send := func(v any) { mu.Lock(); enc.Encode(v); mu.Unlock() }
	permReplies := make(chan json.RawMessage, 1)
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
			send(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"sessionId": "sess-1"}})
		case method == "session/prompt":
			go func(id json.RawMessage) {
				send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "sess-1", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "hello"}}}})
				send(map[string]any{"jsonrpc": "2.0", "id": 900, "method": "session/request_permission", "params": map[string]any{
					"sessionId": "sess-1", "toolCall": map[string]string{"title": "git push"},
					"options": []map[string]string{{"optionId": "allow", "name": "Allow", "kind": "allow_once"}, {"optionId": "deny", "name": "Deny", "kind": "reject_once"}}}})
				reply := <-permReplies
				var r struct {
					Outcome struct{ Outcome, OptionID string } `json:"outcome"`
				}
				json.Unmarshal(reply, &r)
				send(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"stopReason": r.Outcome.Outcome + ":" + r.Outcome.OptionID}})
			}(id)
		case method == "" && string(id) == "900":
			permReplies <- m["result"]
		}
	}
}

func connect(t *testing.T, h Handlers) *Client {
	t.Helper()
	ar, cw := io.Pipe()
	cr, aw := io.Pipe()
	go fakeAgent(ar, aw)
	t.Cleanup(func() { cw.Close(); aw.Close() })
	return New(cr, cw, h)
}

func TestHandshakeSessionPromptAndPermission(t *testing.T) {
	var got []string
	var mu sync.Mutex
	c := connect(t, Handlers{
		Update: func(sid string, u json.RawMessage) { mu.Lock(); got = append(got, sid+":"+string(u)); mu.Unlock() },
		Permission: func(r PermissionRequest) string {
			if len(r.Options) != 2 {
				t.Errorf("options: %+v", r.Options)
			}
			return "deny"
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if ir, err := c.Initialize(ctx); err != nil || ir.ProtocolVersion != 1 {
		t.Fatalf("initialize: %+v %v", ir, err)
	}
	sid, err := c.NewSession(ctx, "/tmp/work")
	if err != nil || sid != "sess-1" {
		t.Fatalf("new session: %q %v", sid, err)
	}
	stop, err := c.Prompt(ctx, sid, "do the thing")
	if err != nil || stop != "selected:deny" {
		t.Fatalf("prompt: %q %v", stop, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("updates: %v", got)
	}
}

func TestNoPermissionHandlerDenies(t *testing.T) {
	c := connect(t, Handlers{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.Initialize(ctx)
	sid, _ := c.NewSession(ctx, "/tmp")
	if stop, _ := c.Prompt(ctx, sid, "x"); stop != "cancelled:" {
		t.Fatalf("a missing handler must cancel (deny), got %q", stop)
	}
}
