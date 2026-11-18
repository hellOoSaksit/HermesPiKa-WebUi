// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package acp is a minimal Agent Client Protocol client: JSON-RPC 2.0, one JSON
// message per line over the agent's stdin/stdout. hpk uses it to drive
// `hermes-acp` (docs/DESIGN.md §9, verified in docs/m0-findings.md): hpk is the
// client, so permission requests of the sessions it starts come back to hpk.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// PermissionOption is one choice the agent offers (allow_once, reject_once, …).
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// PermissionRequest is session/request_permission from the agent.
type PermissionRequest struct {
	SessionID string             `json:"sessionId"`
	ToolCall  json.RawMessage    `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// Handlers receive what the agent sends on its own.
type Handlers struct {
	// Update gets every session/update notification (message chunks, tool calls, titles).
	Update func(sessionID string, update json.RawMessage)
	// Permission decides a request; return an optionId, or "" to cancel (= deny).
	// A nil handler denies everything: hpk fails closed.
	Permission func(PermissionRequest) string
}

type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("acp error %d: %s", e.Code, e.Message) }

// Client talks to one agent process.
type Client struct {
	w       io.Writer
	wmu     sync.Mutex
	h       Handlers
	mu      sync.Mutex
	nextID  int
	pending map[string]chan message
	done    chan struct{}
	err     error
	cmd     *exec.Cmd
}

// New wraps an agent's stdio. Start reading with go c.Serve(r) (Start does both).
func New(r io.Reader, w io.Writer, h Handlers) *Client {
	c := &Client{w: w, h: h, pending: map[string]chan message{}, done: make(chan struct{})}
	go c.serve(r)
	return c
}

// Start launches an agent command (e.g. hermes-acp) and connects to it.
func Start(ctx context.Context, h Handlers, name string, args ...string) (*Client, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := New(out, in, h)
	c.cmd = cmd
	return c, nil
}

// Close stops the agent process if Start launched it.
func (c *Client) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	return nil
}

func (c *Client) serve(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		var m message
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue // agents may log non-JSON lines; ignore them
		}
		switch {
		case m.Method != "" && m.ID != nil:
			go c.answer(m) // a request from the agent
		case m.Method != "":
			if m.Method == "session/update" && c.h.Update != nil {
				var p struct {
					SessionID string          `json:"sessionId"`
					Update    json.RawMessage `json:"update"`
				}
				if json.Unmarshal(m.Params, &p) == nil {
					c.h.Update(p.SessionID, p.Update)
				}
			}
		case m.ID != nil:
			c.mu.Lock()
			ch := c.pending[string(*m.ID)]
			delete(c.pending, string(*m.ID))
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
	c.mu.Lock()
	c.err = sc.Err()
	if c.err == nil {
		c.err = io.EOF
	}
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.done)
}

// answer handles requests the agent makes of the client.
func (c *Client) answer(m message) {
	switch m.Method {
	case "session/request_permission":
		var req PermissionRequest
		_ = json.Unmarshal(m.Params, &req)
		choice := ""
		if c.h.Permission != nil {
			choice = c.h.Permission(req)
		}
		var outcome any = map[string]string{"outcome": "cancelled"}
		if choice != "" {
			outcome = map[string]string{"outcome": "selected", "optionId": choice}
		}
		c.reply(m.ID, map[string]any{"outcome": outcome}, nil)
	default:
		// hpk does not offer file or terminal access to the agent: the agent uses its own tools.
		c.reply(m.ID, nil, &rpcError{Code: -32601, Message: "method not supported by hpk: " + m.Method})
	}
}

func (c *Client) reply(id *json.RawMessage, result any, e *rpcError) {
	m := map[string]any{"jsonrpc": "2.0", "id": id}
	if e != nil {
		m["error"] = e
	} else {
		m["result"] = result
	}
	c.send(m)
}

func (c *Client) send(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.w.Write(append(raw, '\n'))
	return err
}

// Call sends a request and waits for its result.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	c.nextID++
	id := json.RawMessage(fmt.Sprint(c.nextID))
	ch := make(chan message, 1)
	c.pending[string(id)] = ch
	c.mu.Unlock()
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, string(id))
		c.mu.Unlock()
		return ctx.Err()
	case m, ok := <-ch:
		if !ok {
			return fmt.Errorf("agent closed the connection during %s: %w", method, c.err)
		}
		if m.Error != nil {
			return m.Error
		}
		if result != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	}
}

// Notify sends a notification (no answer expected).
func (c *Client) Notify(method string, params any) error {
	return c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// ---- typed calls ----

// InitResult is what the agent says about itself.
type InitResult struct {
	ProtocolVersion   int             `json:"protocolVersion"`
	AgentCapabilities json.RawMessage `json:"agentCapabilities,omitempty"`
	AgentInfo         json.RawMessage `json:"agentInfo,omitempty"`
	AuthMethods       json.RawMessage `json:"authMethods,omitempty"`
}

// Initialize negotiates protocol version 1. hpk offers no fs or terminal capability.
func (c *Client) Initialize(ctx context.Context) (InitResult, error) {
	var r InitResult
	err := c.Call(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false},
		"clientInfo":         map[string]string{"name": "hpk", "version": "0"},
	}, &r)
	return r, err
}

// ModelInfo is one model the agent offers a session (ACP SessionModelState).
type ModelInfo struct {
	ModelID     string `json:"modelId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Models is what session/new says about models: the ones on offer and the current one.
// Hermes names them provider:model.
type Models struct {
	Available []ModelInfo `json:"availableModels"`
	Current   string      `json:"currentModelId"`
}

// NewSession opens a session working in cwd (a worktree for sub-tasks).
func (c *Client) NewSession(ctx context.Context, cwd string) (string, error) {
	id, _, err := c.NewSessionModels(ctx, cwd)
	return id, err
}

// NewSessionModels opens a session and returns the models the agent offers it.
func (c *Client) NewSessionModels(ctx context.Context, cwd string) (string, Models, error) {
	var r struct {
		SessionID string  `json:"sessionId"`
		Models    *Models `json:"models"`
	}
	if err := c.Call(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}, &r); err != nil {
		return "", Models{}, err
	}
	if r.SessionID == "" {
		return "", Models{}, errors.New("agent returned no sessionId")
	}
	var m Models
	if r.Models != nil {
		m = *r.Models
	}
	return r.SessionID, m, nil
}

// SetModel switches a session's model (session/set_model). Hermes rebuilds the
// agent on the new model and keeps the session's history; the session must be
// idle. It never writes Hermes' config.
func (c *Client) SetModel(ctx context.Context, sessionID, modelID string) error {
	return c.Call(ctx, "session/set_model", map[string]string{"sessionId": sessionID, "modelId": modelID}, nil)
}

// Prompt sends text and waits until the turn ends; it returns the stop reason.
// Text such as "/queue …" or "/steer …" is passed through as Hermes slash commands.
func (c *Client) Prompt(ctx context.Context, sessionID, text string) (string, error) {
	var r struct {
		StopReason string `json:"stopReason"`
	}
	err := c.Call(ctx, "session/prompt", map[string]any{"sessionId": sessionID, "prompt": []map[string]string{{"type": "text", "text": text}}}, &r)
	return r.StopReason, err
}

// Cancel asks the agent to stop the current turn.
func (c *Client) Cancel(sessionID string) error {
	return c.Notify("session/cancel", map[string]string{"sessionId": sessionID})
}

// StartCmd connects to an agent command the caller prepared (env, dir).
func StartCmd(cmd *exec.Cmd, h Handlers) (*Client, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := New(out, in, h)
	c.cmd = cmd
	return c, nil
}

// Done is closed when the agent's output ends.
func (c *Client) Done() <-chan struct{} { return c.done }
