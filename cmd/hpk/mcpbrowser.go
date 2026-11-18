// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/serve"
)

// `hpk mcp-browser` is an MCP server (stdio) that gives the agent hpk's
// built-in browser. It holds no browser and no secrets: every call goes to the
// running `hpk serve` on this machine (address and key in agent.json), which
// drives the browser the person watches in the Console.
func mcpBrowser(stdin io.Reader, stdout io.Writer) int {
	type rpc struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}
	out := json.NewEncoder(stdout)
	reply := func(id json.RawMessage, result any) {
		out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	fail := func(id json.RawMessage, code int, msg string) {
		out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
	}
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var req rpc
		if json.Unmarshal(line, &req) != nil {
			fail(nil, -32700, "parse error")
			continue
		}
		if len(req.ID) == 0 { // a notification
			continue
		}
		switch req.Method {
		case "initialize":
			reply(req.ID, map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "hpk-browser", "version": "1"},
				"instructions": "hpk's built-in browser. The person watches every step live and can take over. " +
					"Work from snapshot refs ([e12]). Never ask for or type passwords, tokens or user names: call secrets to see the names, " +
					"then fill_secret with {{secret:name}} (or name.user); hpk types the value only on the sites the person allowed and you never see it.",
			})
		case "ping":
			reply(req.ID, map[string]any{})
		case "tools/list":
			reply(req.ID, map[string]any{"tools": browserTools()})
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			json.Unmarshal(req.Params, &p)
			reply(req.ID, callBrowser(p.Name, p.Arguments))
		default:
			fail(req.ID, -32601, "method not found: "+req.Method)
		}
	}
	return 0
}

func browserTools() []map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	tool := func(name, desc string, props map[string]any, req ...string) map[string]any {
		if props == nil {
			props = map[string]any{}
		}
		schema := map[string]any{"type": "object", "properties": props}
		if len(req) > 0 {
			schema["required"] = req
		}
		return map[string]any{"name": name, "description": desc, "inputSchema": schema}
	}
	ref := str("element ref from the last snapshot, e.g. e12")
	return []map[string]any{
		tool("open", "Open a web address in the built-in browser and return a snapshot of the page.", map[string]any{"url": str("http(s) address")}, "url"),
		tool("snapshot", "Read the current page: numbered elements ([e12] button \"Sign in\") and text. Refs change when the page changes.", nil),
		tool("click", "Click an element by ref (preferred) or by its visible text.", map[string]any{"ref": ref, "text": str("visible text, when there is no ref")}),
		tool("type", "Type text into a field (replaces its content). Never type secrets: use fill_secret.", map[string]any{"ref": ref, "text": str("text to type"), "submit": map[string]any{"type": "boolean", "description": "press Enter after"}}, "ref", "text"),
		tool("fill_secret", "Fill a field with a secret from the person's vault by name, e.g. {{secret:github-login}} or {{secret:github-login.user}} for the user name. hpk types it; you never see the value. Only works on the sites the secret allows.", map[string]any{"ref": ref, "secret": str("{{secret:name}} or {{secret:name.user}}"), "submit": map[string]any{"type": "boolean"}}, "ref", "secret"),
		tool("secrets", "List the names of the person's secrets and the sites each may be used on (never the values).", nil),
		tool("press", "Press a key: Enter, Tab, Escape, Backspace, Delete, Space, ArrowDown/Up/Left/Right, PageDown/PageUp, Home, End.", map[string]any{"key": str("key name")}, "key"),
		tool("scroll", "Scroll the page and return a new snapshot.", map[string]any{"dir": map[string]any{"type": "string", "enum": []string{"down", "up"}}}),
		tool("back", "Go back one page.", nil),
		tool("forward", "Go forward one page.", nil),
		tool("reload", "Reload the page.", nil),
		tool("wait", "Wait for text to appear on the page (up to 15 s), or for ms milliseconds.", map[string]any{"text": str("text to wait for"), "ms": map[string]any{"type": "integer"}}),
		tool("screenshot", "See the page as an image. Fields hpk filled from the vault are masked.", nil),
	}
}

func callBrowser(name string, args map[string]any) map[string]any {
	text := func(s string, isErr bool) map[string]any {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": s}}, "isError": isErr}
	}
	raw, err := os.ReadFile(serve.AgentFile(project.Home()))
	if err != nil {
		return text("hpk's browser is not available: start the HPK Console on this machine (hpk serve), then try again.", true)
	}
	var cfg struct{ URL, Key string }
	if json.Unmarshal(raw, &cfg) != nil || !strings.HasPrefix(cfg.URL, "http://127.0.0.1:") && !strings.HasPrefix(cfg.URL, "http://[::1]:") && !strings.HasPrefix(cfg.URL, "http://localhost:") {
		return text("hpk's browser: agent.json is damaged; restart hpk serve.", true)
	}
	body := map[string]any{"tool": name}
	for k, v := range args {
		body[k] = v
	}
	b, _ := json.Marshal(body)
	target := cfg.URL
	if h := os.Getenv("HPK_AGENT_HOST"); h == "host.docker.internal" {
		// Hermes in hpk's container (hpk isolate setup --docker): the person's
		// hpk serve is on the host; the Host header stays loopback for its check
		if u, err := url.Parse(cfg.URL); err == nil {
			target = "http://" + h + ":" + u.Port()
		}
	}
	req, _ := http.NewRequest(http.MethodPost, target+"/api/browser/agent", bytes.NewReader(b))
	if u, err := url.Parse(cfg.URL); err == nil {
		req.Host = u.Host
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HPK", "1")
	req.Header.Set("X-HPK-Agent-Key", cfg.Key)
	res, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return text("hpk's browser is not answering (is hpk serve running?): "+err.Error(), true)
	}
	defer res.Body.Close()
	var r struct {
		Text, URL, Title, Image, Error string
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&r); err != nil {
		return text(fmt.Sprintf("hpk's browser: bad answer (%s)", res.Status), true)
	}
	if r.Error != "" {
		return text(r.Error, true)
	}
	content := []map[string]any{{"type": "text", "text": r.Text}}
	if r.Image != "" {
		content = append(content, map[string]any{"type": "image", "data": r.Image, "mimeType": "image/jpeg"})
	}
	return map[string]any{"content": content, "isError": false}
}
