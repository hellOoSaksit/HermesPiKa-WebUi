// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package mcp lists and manages the MCP servers of a Hermes profile.
//
// Hermes stays the only owner of the setting: hpk reads mcp_servers from
// config.yaml (names, URLs, commands; env values are never returned) and makes
// every change with `hermes mcp add / remove / test`, answering its questions on
// stdin. A token goes to Hermes the same way and ends up in Hermes' .env as
// MCP_<NAME>_API_KEY; hpk never stores, logs or echoes it.
//
// Two places, two rules:
//   - local (hpk on your own machine): web MCPs (a URL) and local programs
//     (a command, e.g. Blender or Roblox Studio through their MCP bridge);
//   - server (team mode, or HPK_MCP_URL_ONLY=1): web MCPs only. A server must
//     not start programs for the team, and a URL on the server's own loopback
//     would be a program on the server, so that is refused too.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"hpk.local/hpk/internal/isolate"
)

// Server is one entry of mcp_servers, safe to show: env values and headers are
// reduced to names.
type Server struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // url | local
	URL     string   `json:"url,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`  // names only
	Auth    string   `json:"auth,omitempty"` // token | oauth | ""
	Enabled bool     `json:"enabled"`
	Tools   []string `json:"tools,omitempty"`   // include list; empty = all
	Blocked string   `json:"blocked,omitempty"` // why this place will not run it
}

// Spec is what a person asks to add.
type Spec struct {
	Name    string            `json:"name"`
	URL     string            `json:"url"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Token   string            `json:"token"`
	Timeout int               `json:"timeout"` // seconds to connect and list tools
}

// Result of add or test: what Hermes said, reduced to a line and the tools.
type Result struct {
	Connected bool     `json:"connected"`
	Saved     bool     `json:"saved"`
	Enabled   bool     `json:"enabled"`
	Tools     []string `json:"tools,omitempty"`
	Message   string   `json:"message,omitempty"`
}

var (
	nameRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
	envKeyRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
	cmdRE    = regexp.MustCompile(`^[A-Za-z0-9._/\\:@+ -]{1,260}$`)
	toolRE   = regexp.MustCompile(`^\s{2,}([A-Za-z0-9_.:-]{2,80})\s{2,}\S`)
)

// ServerMode: the place only connects web MCPs.
func ServerMode(team bool) bool {
	return team || os.Getenv("HPK_MCP_URL_ONLY") == "1"
}

// Dir is the Hermes home of a profile: Hermes itself points HERMES_HOME at
// <home>/profiles/<name> for a named profile.
func Dir(home, profile string) string {
	if profile != "" && profile != "default" {
		return filepath.Join(home, "profiles", profile)
	}
	return home
}

func configPath(dir string) string { return filepath.Join(dir, "config.yaml") }

func hermes(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := isolate.CommandContext(ctx, "hermes", append([]string{"mcp"}, args...)...)
	cmd.Env = append(os.Environ(), "HERMES_HOME="+dir)
	return cmd
}

// List reads mcp_servers from the profile's config.yaml.
func List(dir string, server bool) ([]Server, error) {
	raw, err := os.ReadFile(configPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return []Server{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c struct {
		MCP map[string]struct {
			URL     string            `yaml:"url"`
			Command string            `yaml:"command"`
			Args    []string          `yaml:"args"`
			Env     map[string]string `yaml:"env"`
			Headers map[string]string `yaml:"headers"`
			Auth    string            `yaml:"auth"`
			Enabled *bool             `yaml:"enabled"`
			Tools   struct {
				Include []string `yaml:"include"`
			} `yaml:"tools"`
		} `yaml:"mcp_servers"`
	}
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("config.yaml: %w", err)
	}
	out := []Server{}
	for name, m := range c.MCP {
		s := Server{Name: name, URL: m.URL, Command: m.Command, Args: m.Args, Enabled: m.Enabled == nil || *m.Enabled, Tools: m.Tools.Include}
		s.Kind = "url"
		if m.URL == "" {
			s.Kind = "local"
		}
		for k := range m.Env {
			s.Env = append(s.Env, k)
		}
		sort.Strings(s.Env)
		switch {
		case m.Auth == "oauth":
			s.Auth = "oauth"
		case len(m.Headers) > 0:
			s.Auth = "token"
		}
		if server {
			s.Blocked = serverRefusal(s.Kind, s.URL)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// serverRefusal explains why a server will not connect this MCP ("" = fine).
func serverRefusal(kind, raw string) string {
	if kind == "local" {
		return "local programs run only on a person's own machine; on the server use a web MCP (a URL)"
	}
	return hostRefusal(raw)
}

// Check validates a spec for this place. It never contacts anything.
func Check(s Spec, server bool) error {
	if !nameRE.MatchString(s.Name) {
		return fmt.Errorf("%q: use a short name of a-z, 0-9, - and _ (e.g. blender)", s.Name)
	}
	if (s.URL == "") == (s.Command == "") {
		return errors.New("give either a URL (web MCP) or a command (local program)")
	}
	if s.URL != "" {
		u, err := url.Parse(s.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.ContainsAny(s.URL, " \r\n") {
			return fmt.Errorf("%q is not an http(s) URL", s.URL)
		}
		if len(s.Env) > 0 {
			return errors.New("environment variables are for local programs; a web MCP takes a token")
		}
		if server {
			if why := serverRefusal("url", s.URL); why != "" {
				return errors.New(why)
			}
			if why := resolveRefusal(s.URL); why != "" {
				return errors.New(why)
			}
			if u.Scheme != "https" {
				return errors.New("on the server a web MCP must use https")
			}
		}
	} else {
		if server {
			return errors.New(serverRefusal("local", ""))
		}
		if !cmdRE.MatchString(s.Command) || strings.HasPrefix(s.Command, "-") {
			return fmt.Errorf("%q is not a program name or path", s.Command)
		}
		if s.Token != "" {
			return errors.New("a token is for web MCPs; give a local program its key as an environment variable")
		}
		if len(s.Args) > 40 {
			return errors.New("too many arguments")
		}
		for _, a := range s.Args {
			if len(a) > 500 || strings.ContainsAny(a, "\r\n\x00") {
				return errors.New("an argument is too long or has a line break")
			}
		}
		for k, v := range s.Env {
			if !envKeyRE.MatchString(k) || strings.ContainsAny(v, "\r\n\x00") || len(v) > 4096 {
				return fmt.Errorf("environment variable %q: use NAME=value on one line", k)
			}
		}
	}
	if strings.ContainsAny(s.Token, "\r\n") || len(s.Token) > 8192 {
		return errors.New("paste one token")
	}
	return nil
}

// Add connects a new MCP (or reconnects an existing one) through `hermes mcp add`.
// Hermes probes it and lists its tools; if it does not answer, the setting is
// still saved, switched off, so "Test" can bring it in later.
func Add(ctx context.Context, dir string, s Spec, server bool) (*Result, error) {
	if err := Check(s, server); err != nil {
		return nil, err
	}
	if s.Token != "" && !CanSendTokens {
		return nil, ErrNeedsTerminal
	}
	existing, _ := List(dir, server)
	var had *Server
	for i := range existing {
		if existing[i].Name == s.Name {
			had = &existing[i]
		}
	}
	timeout := s.Timeout
	if timeout <= 0 || timeout > 120 {
		timeout = 20
	}
	args := []string{"add", s.Name, "--connect-timeout", fmt.Sprint(timeout)}
	if s.URL != "" {
		args = append(args, "--url", s.URL)
	} else {
		keys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// Hermes takes env values only on its command line, so they are visible in
		// the process list for the seconds `hermes mcp add` runs (and are kept in
		// Hermes' config.yaml, as Hermes does). For a key, write ${NAME} here and
		// put NAME=value in Hermes' .env: Hermes resolves it when it connects.
		if len(keys) > 0 {
			args = append(args, "--env")
			for _, k := range keys {
				args = append(args, k+"="+s.Env[k])
			}
		}
		args = append(args, "--command", s.Command)
		if len(s.Args) > 0 {
			args = append(append(args, "--args"), s.Args...) // last: Hermes takes the rest as-is
		}
	}
	// Answers, in the order Hermes asks them.
	var in strings.Builder
	if had != nil {
		in.WriteString("y\n") // overwrite
	}
	if s.URL != "" {
		if s.Token != "" || (had != nil && had.Auth == "token") {
			in.WriteString("y\n") // needs authentication
			if s.Token != "" && (had == nil || had.Auth != "token") {
				in.WriteString(strings.TrimSpace(s.Token) + "\n")
			}
		} else {
			in.WriteString("n\n")
		}
	}
	in.WriteString("y\n") // enable all tools / save anyway
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout+60)*time.Second)
	defer cancel()
	cmd, err := hermesCmd(hermes(ctx, dir, args...), s.Token != "")
	if err != nil {
		return nil, err
	}
	cmd.Stdin = strings.NewReader(in.String())
	out, runErr := cmd.CombinedOutput()
	text := redact(string(out), s)
	res := parse(text)
	if runErr != nil && !res.Saved {
		return res, fmt.Errorf("hermes mcp add: %s", lastLine(text, runErr))
	}
	return res, nil
}

// Remove deletes an MCP from the profile.
func Remove(ctx context.Context, dir, name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("%q is not an MCP name", name)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := hermes(ctx, dir, "remove", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("hermes mcp remove: %s", lastLine(string(out), err))
	}
	return nil
}

// Test connects to a configured MCP and lists its tools.
func Test(ctx context.Context, dir, name string) (*Result, error) {
	if !nameRE.MatchString(name) {
		return nil, fmt.Errorf("%q is not an MCP name", name)
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	out, err := hermes(ctx, dir, "test", name).CombinedOutput()
	res := parse(string(out))
	if err != nil && !res.Connected {
		res.Message = lastLine(string(out), err)
	}
	return res, nil
}

// parse reads Hermes' human output: tool rows, ✓ / ✗ lines.
func parse(out string) *Result {
	r := &Result{}
	for _, l := range strings.Split(out, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.Contains(t, "install out of sync"):
		case strings.Contains(t, "✓ Saved"):
			// may follow a question on the same line ("Save config anyway? [y/N]: ✓ Saved ...")
			t = t[strings.Index(t, "✓ Saved"):]
			r.Saved = true
			r.Enabled = !strings.Contains(t, "(disabled)")
			if r.Enabled || r.Message == "" {
				r.Message = strings.TrimPrefix(t, "✓ ")
			}
		case strings.HasPrefix(t, "✓ Connected") || strings.Contains(t, "Found ") && strings.Contains(t, "tool"):
			r.Connected = true
		case strings.HasPrefix(t, "✗"):
			r.Message = strings.TrimSpace(strings.TrimPrefix(t, "✗"))
		default:
			if m := toolRE.FindStringSubmatch(l); m != nil {
				r.Tools = append(r.Tools, m[1])
				r.Connected = true
			}
		}
	}
	if r.Saved && !r.Enabled {
		r.Connected = false
	}
	return r
}

// redact removes anything the person typed as a secret from Hermes' output.
func redact(out string, s Spec) string {
	if t := strings.TrimSpace(s.Token); t != "" {
		out = strings.ReplaceAll(out, t, "•••")
	}
	for _, v := range s.Env {
		if len(v) >= 6 {
			out = strings.ReplaceAll(out, v, "•••")
		}
	}
	return out
}

func lastLine(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l != "" && !strings.Contains(l, "install out of sync") && !strings.HasPrefix(l, "Fix the issue") {
			return strings.TrimSpace(strings.TrimPrefix(l, "✗"))
		}
	}
	if err != nil {
		return err.Error()
	}
	return "no output"
}

// Enable switches on an MCP that was saved off (it did not answer when it was
// added): it is added again with the same setting, which makes Hermes probe it
// and save it on. Env values are read here and handed back to Hermes only.
func Enable(ctx context.Context, dir, name string, server bool) (*Result, error) {
	raw, err := os.ReadFile(configPath(dir))
	if err != nil {
		return nil, err
	}
	var c struct {
		MCP map[string]struct {
			URL     string            `yaml:"url"`
			Command string            `yaml:"command"`
			Args    []string          `yaml:"args"`
			Env     map[string]string `yaml:"env"`
			Timeout float64           `yaml:"connect_timeout"`
		} `yaml:"mcp_servers"`
	}
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	m, ok := c.MCP[name]
	if !ok {
		return nil, fmt.Errorf("no MCP named %q", name)
	}
	return Add(ctx, dir, Spec{Name: name, URL: m.URL, Command: m.Command, Args: m.Args, Env: m.Env, Timeout: int(m.Timeout)}, server)
}
