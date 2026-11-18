// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package isolate runs Hermes (the AI) under an OS identity of its own, so it
// cannot read what belongs to the person: the vault and its key, the built-in
// browser's profile and recordings, the Console key (one-time link and
// sessions). The person's hpk (vault, browser, Console) stays in the person's
// account. docs/isolation.md is the design.
//
// Two modes:
//
//   - "user": a dedicated local account (Windows: a standard user, hpk-ai).
//     Every `hermes` hpk starts goes through a relay (`hpk ai-relay`) that
//     starts it as that account; the account's password is kept for the person
//     only (DPAPI in the private folder).
//   - "docker": Hermes in a container (the docker/hermes image) with the
//     Hermes home and the project folders mounted; the relay is `docker exec`.
//
// What crosses the boundary: the Hermes home (config, sessions, hpk projects,
// wikis, board) and the project folders, read/write for both; the hpk hooks
// (run by Hermes, as the AI) read that home; the hpk-browser MCP reaches
// `hpk serve` over loopback with the agent key from <home>/hpk/browser/agent.json.
// Nothing else.
//
// The settings live in the person's own config folder (isolation.json), which
// the AI's account cannot read or write; hpk never takes them from the shared
// Hermes home, where the AI could plant them.
package isolate

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Modes.
const (
	ModeUser   = "user"
	ModeDocker = "docker"
)

// DefaultUser is the name of the AI's local account.
const DefaultUser = "hpk-ai"

// Config is isolation.json (the person's own config folder).
type Config struct {
	Mode      string    `json:"mode"`                // user | docker
	User      string    `json:"user,omitempty"`      // user: the AI's account
	Home      string    `json:"home"`                // the Hermes home shared with the AI
	Private   string    `json:"private"`             // the person's private hpk folder (vault, browser, Console key)
	Bin       string    `json:"bin,omitempty"`       // user: the hpk the AI runs (read-only for it)
	Container string    `json:"container,omitempty"` // docker: the container name
	Compose   string    `json:"compose,omitempty"`   // docker: the compose file hpk wrote
	Mounts    []Mount   `json:"mounts,omitempty"`    // docker: host folder -> container folder
	Granted   []string  `json:"granted,omitempty"`   // user: folders the AI's account was given (for teardown)
	ReadOnly  []string  `json:"readOnly,omitempty"`  // user: program folders in the home it may run but not change
	Created   time.Time `json:"created"`
}

// Mount maps a host folder into the container.
type Mount struct {
	Host      string `json:"host"`
	Container string `json:"container"`
}

// ConfigPath is isolation.json in the person's config folder
// (%APPDATA%\hpk on Windows). HPK_ISOLATION_FILE overrides it (tests).
func ConfigPath() string {
	if p := os.Getenv("HPK_ISOLATION_FILE"); p != "" {
		return p
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "hpk", "isolation.json")
}

// Load reads isolation.json; ok is false when the AI is not isolated.
func Load() (*Config, bool) {
	p := ConfigPath()
	if p == "" {
		return nil, false
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var c Config
	if json.Unmarshal(raw, &c) != nil || (c.Mode != ModeUser && c.Mode != ModeDocker) || c.Home == "" || c.Private == "" {
		return nil, false
	}
	return &c, true
}

// Save writes isolation.json, readable by this account only.
func Save(c *Config) error {
	p := ConfigPath()
	if p == "" {
		return errors.New("no config folder for this account")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, raw, 0o600)
}

// Remove deletes isolation.json.
func Remove() error {
	err := os.Remove(ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// For returns the isolation settings that apply to this Hermes home.
func For(home string) (*Config, bool) {
	c, ok := Load()
	if !ok || !samePath(c.Home, home) {
		return nil, false
	}
	return c, true
}

// PrivateDir is where the person's private hpk files live for this home: the
// vault, the browser profile and recordings, the Console key. Without
// isolation it is <home>/hpk, as before; isolated, a folder only the person's
// account can read.
func PrivateDir(home string) string {
	if c, ok := For(home); ok {
		return c.Private
	}
	return filepath.Join(home, "hpk")
}

// Private names, relative to PrivateDir: they move out of the Hermes home when
// the AI is isolated, and back when it is not.
var Private = []string{"vault/vault.bin", "vault/key.dpapi", "vault/key", "browser/profile", "browser/runs", "console-sessions.json"}

// Active reports whether hpk on this machine runs Hermes isolated.
func Active() bool { _, ok := Load(); return ok }

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Command is exec.Command for Hermes programs (hermes, hermes-acp): isolated,
// it runs `hpk ai-relay -- name args…`, which starts the program as the AI's
// account (or in its container) with the same working folder, the variables
// Forward names, and the same stdin/stdout/stderr. Callers set Dir, Env and
// pipes as usual.
func Command(name string, args ...string) *exec.Cmd {
	return CommandContext(context.Background(), name, args...)
}

// CommandContext is Command with a context (exec.CommandContext).
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	if !Active() {
		return exec.CommandContext(ctx, name, args...)
	}
	self, err := os.Executable()
	if err != nil {
		return exec.CommandContext(ctx, name, args...)
	}
	return exec.CommandContext(ctx, self, append([]string{"ai-relay", "--", name}, args...)...)
}

// Forward is what crosses to the AI's process from the caller's environment:
// where Hermes and hpk find their home and project, and nothing of the
// person's own (no tokens, no profile folders).
var Forward = []string{"PATH", "HERMES_HOME", "HPK_HOME", "HPK_PROJECT", "WIKI_PATH", "HPK_SESSION", "NO_COLOR", "PYTHONIOENCODING", "PYTHONUTF8", "LANG", "LC_ALL", "TERM", "COLUMNS"}

// Spec is one program to run as the AI (passed to `hpk ai-exec`).
type Spec struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
	Dir  string   `json:"dir,omitempty"`
	Env  []string `json:"env,omitempty"`
}

// progBase is the last part of a program path, split on / and \ on every OS
// (a Windows path can reach a Linux container through the relay).
func progBase(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// Allowed programs: the relay runs Hermes, nothing else.
func Allowed(name string) bool {
	b := strings.ToLower(progBase(name))
	b = strings.TrimSuffix(b, ".exe")
	b = strings.TrimSuffix(b, ".cmd")
	return b == "hermes" || b == "hermes-acp" || b == "hpk"
}

// NewSpec builds the spec for name/args from this process's folder and
// environment, with the Hermes home forced to home.
func NewSpec(home, name string, args []string) Spec {
	s := Spec{Name: name, Args: args}
	s.Dir, _ = os.Getwd()
	for _, k := range Forward {
		if v, ok := os.LookupEnv(k); ok && k != "HERMES_HOME" && k != "HPK_HOME" {
			s.Env = append(s.Env, k+"="+v)
		}
	}
	s.Env = append(s.Env, "HERMES_HOME="+home, "HPK_HOME="+home)
	return s
}

// Encode/Decode carry a Spec on a command line.
func (s Spec) Encode() string {
	raw, _ := json.Marshal(s)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// WriteSpecFile carries a spec too long for a command line (Windows starts
// another account's program with at most 1024 characters, and PATH alone is
// often longer): a one-time file in the shared home, passed as @file, which
// ai-exec reads and deletes. It holds no secrets: only what Forward lets cross.
func WriteSpecFile(home string, s Spec) (string, error) {
	dir := filepath.Join(home, "hpk", "relay")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	f := filepath.Join(dir, "spec-"+hex.EncodeToString(b))
	return f, os.WriteFile(f, []byte(s.Encode()), 0o600)
}

// ReadSpecArg decodes ai-exec's argument: the spec itself, or @file from
// WriteSpecFile (read once, then deleted).
func ReadSpecArg(arg string) (Spec, error) {
	if f, ok := strings.CutPrefix(arg, "@"); ok {
		if filepath.Base(filepath.Dir(f)) != "relay" || !strings.HasPrefix(filepath.Base(f), "spec-") {
			return Spec{}, errors.New("ai-exec: bad spec file")
		}
		raw, err := os.ReadFile(f)
		_ = os.Remove(f)
		if err != nil {
			return Spec{}, errors.New("ai-exec: bad spec file")
		}
		arg = strings.TrimSpace(string(raw))
	}
	return Decode(arg)
}

func Decode(arg string) (Spec, error) {
	var s Spec
	raw, err := base64.RawURLEncoding.DecodeString(arg)
	if err != nil {
		return s, errors.New("ai-exec: bad spec")
	}
	if err := json.Unmarshal(raw, &s); err != nil || s.Name == "" {
		return s, errors.New("ai-exec: bad spec")
	}
	return s, nil
}

// Exec runs a spec in this process's account with this process's stdio and
// returns its exit code. It is `hpk ai-exec`, started as the AI by the relay.
func Exec(s Spec) int {
	if !Allowed(s.Name) {
		os.Stderr.WriteString("hpk ai-exec: only Hermes programs run here\n")
		return 126
	}
	for _, kv := range s.Env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			os.Setenv(k, v)
		}
	}
	path, err := exec.LookPath(s.Name)
	if err != nil {
		os.Stderr.WriteString("hpk ai-exec: " + err.Error() + "\n")
		return 127
	}
	cmd := exec.Command(path, s.Args...)
	cmd.Dir = s.Dir
	if fi, err := os.Stat(s.Dir); s.Dir != "" && (err != nil || !fi.IsDir()) {
		cmd.Dir = "" // a folder of the person's the AI cannot enter: stay in the Hermes home the relay started in
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		os.Stderr.WriteString("hpk ai-exec: " + err.Error() + "\n")
		return 127
	}
	return 0
}

// errContainer: folders are mounted into the container, not granted to an account.
var errContainer = errors.New("not supported while the AI runs in a container")

// Grant lets the AI's account read and write dir (a project folder or wiki the
// person moved) and records it for teardown. Windows with a dedicated account
// only; the container mode and other systems report why not.
func Grant(c *Config, dir string) error {
	if c.Mode == ModeDocker {
		return errContainer
	}
	if err := grantDir(c, dir); err != nil {
		return err
	}
	for _, g := range c.Granted {
		if g == dir {
			return Save(c)
		}
	}
	c.Granted = append(c.Granted, dir)
	return Save(c)
}

// Ungrant takes the AI's account off dir again.
func Ungrant(c *Config, dir string) error {
	if c.Mode == ModeDocker {
		return errContainer
	}
	if err := ungrantDir(c, dir); err != nil {
		return err
	}
	var kept []string
	for _, g := range c.Granted {
		if g != dir {
			kept = append(kept, g)
		}
	}
	c.Granted = kept
	return Save(c)
}
