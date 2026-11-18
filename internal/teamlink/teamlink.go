// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package teamlink connects a person's own hpk to their team's server
// (docs/plans/connected-mode.md §12). The person enters the server's URL and a
// one-time invite code; hpk exchanges the code for a device token and keeps
// that token in the local vault (as a system secret the AI never sees). This
// file, <hermes home>/hpk/teamserver.json, holds only where and who — never the
// token.
package teamlink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SecretName is the vault name of the device token.
const SecretName = "hpk.team-server"

// Link is what this machine knows about its team server.
type Link struct {
	URL      string    `json:"url"`
	Member   string    `json:"member"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	DeviceID string    `json:"deviceId"`
	Device   string    `json:"device"`
	Joined   time.Time `json:"joined"`
}

// File is where the link lives.
func File(home string) string { return filepath.Join(home, "hpk", "teamserver.json") }

// Load reads the link; (nil, nil) when this machine has not joined a server.
func Load(home string) (*Link, error) {
	raw, err := os.ReadFile(File(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l Link
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("teamserver.json: %w", err)
	}
	// the same rules as when joining: https only (plain http for this machine)
	u, err := CheckURL(l.URL)
	if err != nil {
		return nil, fmt.Errorf("teamserver.json: %w", err)
	}
	l.URL = u
	return &l, nil
}

// Save writes the link (0600).
func (l *Link) Save(home string) error {
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	f := File(home)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	return os.WriteFile(f, raw, 0o600)
}

// Remove forgets the link.
func Remove(home string) error {
	err := os.Remove(File(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// CheckURL cleans a server URL. Only HTTPS, except this machine (tests and a
// server on the same computer).
func CheckURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("give the server's address, like https://team.example.com")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("give only the server's address, like https://team.example.com")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !loopbackHost(u.Hostname()) {
			return "", errors.New("a team server must use HTTPS (https://…); plain http only for this machine")
		}
	default:
		return "", errors.New("a team server address starts with https://")
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.EscapedPath(), "/"), nil
}

func loopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// Client talks to the team server for this machine.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

// DefaultHTTP checks certificates as usual, never follows redirects (a
// redirect could carry the token to another host) and gives up after 15s.
func DefaultHTTP() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Do sends a request to path on the server ("/api/…") with the device token.
func (c Client) Do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-HPK", "1")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	h := c.HTTP
	if h == nil {
		h = DefaultHTTP()
	}
	return h.Do(req)
}

// JSON sends a request and decodes a JSON answer; a non-2xx answer is an error
// with the server's message.
func (c Client) JSON(ctx context.Context, method, path string, body, out any) error {
	res, err := c.Do(ctx, method, path, body)
	if err != nil {
		return fmt.Errorf("cannot reach the team server: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = res.Status
		}
		return &ServerError{Code: res.StatusCode, Msg: e.Error}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// ServerError is an answer from the server that is not a success.
type ServerError struct {
	Code int
	Msg  string
}

func (e *ServerError) Error() string { return "team server: " + e.Msg }

// Joined is the server's answer to a join.
type Joined struct {
	Token    string `json:"token"`
	DeviceID string `json:"deviceId"`
	Device   string `json:"device"`
	Member   string `json:"member"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

// Join exchanges an invite code for a device token. The caller stores the
// token in the vault and the returned Link in teamserver.json.
func Join(ctx context.Context, h *http.Client, serverURL, code, device string) (*Link, string, error) {
	base, err := CheckURL(serverURL)
	if err != nil {
		return nil, "", err
	}
	var j Joined
	c := Client{Base: base, HTTP: h}
	if err := c.JSON(ctx, http.MethodPost, "/api/join", map[string]string{"code": code, "device": device}, &j); err != nil {
		return nil, "", err
	}
	if !strings.HasPrefix(j.Token, "hpkd_") || j.Member == "" {
		return nil, "", errors.New("the team server gave an answer hpk does not understand")
	}
	return &Link{URL: base, Member: j.Member, Name: j.Name, Role: j.Role, DeviceID: j.DeviceID, Device: j.Device, Joined: time.Now().UTC()}, j.Token, nil
}
