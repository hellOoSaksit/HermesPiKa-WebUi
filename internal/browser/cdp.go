// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// cdp is one DevTools connection: commands with ids, events to a handler.
type cdp struct {
	ws      *wsConn
	next    atomic.Int64
	mu      sync.Mutex
	waiting map[int64]chan reply
	onEvent func(method, session string, params json.RawMessage)
	closed  chan struct{}
}

type reply struct {
	Result json.RawMessage
	Err    error
}

type message struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Message string `json:"message"`
		Data    string `json:"data"`
	} `json:"error,omitempty"`
}

func dialCDP(url string, onEvent func(method, session string, params json.RawMessage)) (*cdp, error) {
	ws, err := wsDial(url)
	if err != nil {
		return nil, err
	}
	c := &cdp{ws: ws, waiting: map[int64]chan reply{}, onEvent: onEvent, closed: make(chan struct{})}
	go c.loop()
	return c, nil
}

func (c *cdp) loop() {
	defer close(c.closed)
	for {
		raw, err := c.ws.ReadMessage()
		if err != nil {
			c.mu.Lock()
			for id, ch := range c.waiting {
				ch <- reply{Err: errors.New("the browser closed")}
				delete(c.waiting, id)
			}
			c.mu.Unlock()
			return
		}
		var m message
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch := c.waiting[m.ID]
			delete(c.waiting, m.ID)
			c.mu.Unlock()
			if ch != nil {
				r := reply{Result: m.Result}
				if m.Error != nil {
					r.Err = fmt.Errorf("%s %s", m.Error.Message, m.Error.Data)
				}
				ch <- r
			}
			continue
		}
		if m.Method != "" && c.onEvent != nil {
			c.onEvent(m.Method, m.SessionID, m.Params)
		}
	}
}

// call sends a command (to a page when session is set) and decodes the result into out.
func (c *cdp) call(ctx context.Context, session, method string, params any, out any) error {
	id := c.next.Add(1)
	m := map[string]any{"id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	if session != "" {
		m["sessionId"] = session
	}
	b, _ := json.Marshal(m)
	ch := make(chan reply, 1)
	c.mu.Lock()
	c.waiting[id] = ch
	c.mu.Unlock()
	if err := c.ws.WriteText(b); err != nil {
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
		return err
	}
	select {
	case r := <-ch:
		if r.Err != nil {
			return fmt.Errorf("%s: %w", method, r.Err)
		}
		if out != nil && len(r.Result) > 0 {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
		return fmt.Errorf("%s: %w", method, ctx.Err())
	case <-c.closed:
		return errors.New("the browser closed")
	}
}

func (c *cdp) close() { _ = c.ws.Close() }
