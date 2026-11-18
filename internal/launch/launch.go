// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package launch is what makes hpk one program: `hpk` with no arguments starts
// the daemon once per machine (the listening port is the lock), opens the
// Console in the default browser, and `hpk stop` ends it from anywhere. The
// Console stays a web app, so this works the same on every OS; only opening a
// URL and start-at-login (autostart.go) differ per OS.
package launch

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultAddr is where `hpk` and `hpk serve` listen unless told otherwise.
const DefaultAddr = "127.0.0.1:7878"

// DataDir is hpk's own per-user folder (not Hermes'): the run file, the
// Hermes that hpk installs for a person, logs of start-at-login.
// HPK_DATA_DIR overrides it (tests, portable installs).
func DataDir() string {
	if d := os.Getenv("HPK_DATA_DIR"); d != "" {
		return d
	}
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "hpk")
		}
	case "darwin":
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, "Library", "Application Support", "hpk")
		}
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "hpk")
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".local", "share", "hpk")
	}
	return filepath.Join(os.TempDir(), "hpk")
}

// Run describes a running daemon. The file is readable only by its owner: the
// key lets `hpk stop` ask the daemon to end, which nothing else may do.
type Run struct {
	PID     int    `json:"pid"`
	Addr    string `json:"addr"`
	URL     string `json:"url"`
	Key     string `json:"key"`
	Started int64  `json:"started"`
	Version string `json:"version"`
}

// RunFile is where the daemon on addr records itself (one file per port).
func RunFile(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		port = "7878"
	}
	return filepath.Join(DataDir(), "run", "serve-"+port+".json")
}

// NewKey makes the stop key for a new run file.
func NewKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WriteRun records a running daemon; the returned func removes the file.
func WriteRun(r Run) (func(), error) {
	f := RunFile(r.Addr)
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return func() {}, err
	}
	raw, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(f, raw, 0o600); err != nil {
		return func() {}, err
	}
	return func() {
		// Only remove our own record (a second daemon on another home may have replaced it).
		if cur, err := ReadRun(r.Addr); err == nil && cur.Key == r.Key {
			_ = os.Remove(f)
		}
	}, nil
}

// ReadRun reads the run file for addr.
func ReadRun(addr string) (Run, error) {
	var r Run
	raw, err := os.ReadFile(RunFile(addr))
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(raw, &r)
	return r, err
}

// Runs lists every run file (hpk stop with no --addr stops them all).
func Runs() []Run {
	es, _ := os.ReadDir(filepath.Join(DataDir(), "run"))
	var out []Run
	for _, e := range es {
		if !strings.HasPrefix(e.Name(), "serve-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(DataDir(), "run", e.Name()))
		var r Run
		if err == nil && json.Unmarshal(raw, &r) == nil && r.Addr != "" {
			out = append(out, r)
		}
	}
	return out
}

// State of the address `hpk` wants to use.
type State int

const (
	Free      State = iota // nothing listens: start the daemon
	HPK                    // an hpk daemon answers: just open the Console
	Something              // another program holds the port
)

// Probe asks addr whether hpk is there.
func Probe(ctx context.Context, addr string) State {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	for _, scheme := range []string{"http", "https"} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+addr+"/api/health", nil)
		res, err := probeClient.Do(req)
		if err != nil {
			continue
		}
		var h struct {
			Status  string `json:"status"`
			Version string `json:"version"`
		}
		err = json.NewDecoder(res.Body).Decode(&h)
		res.Body.Close()
		if err == nil && h.Status == "ok" && h.Version != "" {
			return HPK
		}
		return Something
	}
	c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return Free
	}
	c.Close()
	return Something
}

var probeClient = &http.Client{Timeout: 1500 * time.Millisecond}

// Wait polls until hpk answers on addr (after another `hpk` won the race).
func Wait(ctx context.Context, addr string, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if Probe(ctx, addr) == HPK {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
	return false
}

// ConsoleURL is the page `hpk` opens.
func ConsoleURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr + "/#/overview"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/#/overview"
}

// Stop asks the daemon in r to end (it stops its built-in browser and chats).
func Stop(ctx context.Context, r Run) error {
	u := "http://" + r.Addr + "/api/shutdown"
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader([]byte("{}")))
	req.Header.Set("X-HPK", "1")
	req.Header.Set("X-HPK-Stop-Key", r.Key)
	req.Header.Set("Content-Type", "application/json")
	res, err := probeClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("hpk on %s refused to stop (HTTP %d)", r.Addr, res.StatusCode)
	}
	end := time.Now().Add(8 * time.Second)
	for time.Now().Before(end) {
		if Probe(ctx, r.Addr) != HPK {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("hpk on " + r.Addr + " is still answering")
}

// OpenCommand is how this OS opens a URL in the default browser (or in the
// installed app, when the browser hands its own URLs to it).
func OpenCommand(goos, url string) (string, []string) {
	switch goos {
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		return "open", []string{url}
	default:
		return "xdg-open", []string{url}
	}
}

// Open opens url without waiting for the browser.
func Open(url string) error {
	if os.Getenv("HPK_NO_BROWSER") != "" {
		return nil
	}
	name, args := OpenCommand(runtime.GOOS, url)
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// LinkFile is where `hpk serve` keeps the one-time Console link (the Console
// key: a browser that opens it gets the cookie that lets it make changes).
func LinkFile(hermesHome string) string { return filepath.Join(hermesHome, "hpk", "console-link") }

// ConsoleLink reads that link. Only a loopback http link is accepted; a link
// older than since (left by an earlier run) is ignored.
func ConsoleLink(hermesHome string, since time.Time) (string, bool) {
	f := LinkFile(hermesHome)
	fi, err := os.Stat(f)
	if err != nil || fi.ModTime().Before(since) {
		return "", false
	}
	raw, err := os.ReadFile(f)
	if err != nil {
		return "", false
	}
	link := strings.TrimSpace(string(raw))
	for _, p := range []string{"http://127.0.0.1:", "http://localhost:", "http://[::1]:"} {
		if strings.HasPrefix(link, p) && !strings.ContainsAny(link, " \t\r\n\"") {
			return link, true
		}
	}
	return "", false
}

// WaitLink waits up to d for serve to write a link newer than since.
func WaitLink(hermesHome string, since time.Time, d time.Duration) (string, bool) {
	end := time.Now().Add(d)
	for {
		if l, ok := ConsoleLink(hermesHome, since); ok {
			return l, true
		}
		if time.Now().After(end) {
			return "", false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
