// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package browser is hpk's built-in browser: a Chromium (Chrome or Edge) that
// hpk starts on this machine and drives over the DevTools protocol. The agent
// uses it through `hpk mcp-browser`; the person watches it live in the Console
// and can take over at any time.
//
// Secrets never reach the agent: it asks to fill {{secret:name}} and hpk types
// the value itself, only on the sites the secret allows; everything the agent
// reads back (page text, URLs, titles) is scrubbed by the vault, and fields hpk
// filled are masked before any screenshot. Every action is recorded for the
// person (steps + screenshots, kept 30 days), never for the wiki.
package browser

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/procs"
	"hpk.local/hpk/internal/vault"
)

// Event goes to the Console (live view, steps, state).
type Event struct {
	Kind  string `json:"kind"` // frame | step | state
	Data  any    `json:"data"`
	Frame []byte `json:"-"`
}

// State is what the Console shows above the live view.
type State struct {
	Running  bool   `json:"running"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Driver   string `json:"driver"` // agent | human
	Run      string `json:"run,omitempty"`
	Busy     string `json:"busy,omitempty"` // what the agent is doing now
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Exe      string `json:"exe,omitempty"`
	Loading  bool   `json:"loading"`
	DevTools string `json:"devtools,omitempty"` // pipe | tcp: how hpk talks to the browser
}

// Manager owns the one browser of this hpk.
type Manager struct {
	home  string
	vault *vault.Vault

	mu      sync.Mutex
	cmd     *exec.Cmd
	conn    *cdp
	session string // page session id
	target  string
	state   State
	frame   []byte
	subs    map[chan Event]bool
	rec     *recorder
	filled  map[string]string // data-hpk-secret marker -> secret name
	actMu   sync.Mutex        // one action at a time
	exited  chan struct{}     // closed when the started browser process ends
	clickAt [2]float64
}

func New(home string, v *vault.Vault) *Manager {
	return &Manager{home: home, vault: v, subs: map[chan Event]bool{}, filled: map[string]string{}, state: State{Driver: "agent", Width: 1280, Height: 800}}
}

// Subscribe receives events until cancel is called.
func (m *Manager) Subscribe() (chan Event, func()) {
	ch := make(chan Event, 16)
	m.mu.Lock()
	m.subs[ch] = true
	m.mu.Unlock()
	return ch, func() { m.mu.Lock(); delete(m.subs, ch); m.mu.Unlock() }
}

func (m *Manager) emit(e Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.subs {
		select {
		case ch <- e:
		default: // slow viewer: drop a frame
		}
	}
}

func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *Manager) Frame() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.frame
}

func (m *Manager) setState(f func(*State)) {
	m.mu.Lock()
	f(&m.state)
	if m.vault != nil {
		m.state.URL, m.state.Title, m.state.Busy = m.vault.Scrub(m.state.URL), m.vault.Scrub(m.state.Title), m.vault.Scrub(m.state.Busy)
	}
	st := m.state
	m.mu.Unlock()
	m.emit(Event{Kind: "state", Data: st})
}

// FindExe looks for Chrome, Edge or Chromium. HPK_BROWSER overrides.
func FindExe() (string, error) {
	if p := os.Getenv("HPK_BROWSER"); p != "" {
		return p, nil
	}
	var list []string
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if base == "" {
				continue
			}
			list = append(list, filepath.Join(base, `Google\Chrome\Application\chrome.exe`), filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`), filepath.Join(base, `Chromium\Application\chrome.exe`))
		}
	case "darwin":
		list = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge", "/Applications/Chromium.app/Contents/MacOS/Chromium"}
	default:
		for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge"} {
			if p, err := exec.LookPath(n); err == nil {
				list = append(list, p)
			}
		}
	}
	for _, p := range list {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("no Chrome, Edge or Chromium found; install one or set HPK_BROWSER to its path")
}

// Dir holds the browser's profile and recordings: <home>/hpk/browser, or the
// person's private folder when the AI is isolated (docs/isolation.md).
func Dir(home string) string { return filepath.Join(isolate.PrivateDir(home), "browser") }

// Start launches the browser (headless, its own profile under Dir).
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.conn != nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	exe, err := FindExe()
	if err != nil {
		return err
	}
	dir := Dir(m.home)
	os.MkdirAll(filepath.Join(dir, "profile"), 0o700)
	pruneRuns(dir, 30*24*time.Hour)
	st := m.State()
	profile := filepath.Join(dir, "profile")
	var cmd *exec.Cmd
	kill := func() {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	// Never attach to a socket hpk did not open itself: a file in the profile
	// could point at a fake browser that would receive the secrets. A browser
	// left running on this profile (hpk restarted) is closed first.
	stopStale(profile)
	_ = os.Remove(filepath.Join(profile, "DevToolsActivePort"))
	noPasswordSaving(profile)
	args := []string{"--headless=new", "--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check", "--disable-sync",
		"--disable-background-networking", "--password-store=basic", "--disable-features=Translate,AutofillServerCommunication",
		fmt.Sprintf("--window-size=%d,%d", st.Width, st.Height)}
	// DevTools over a pipe only hpk holds: a TCP port on 127.0.0.1 could be
	// reached by every program and every OS account on this machine,
	// including the AI's own account when it is isolated (docs/isolation.md).
	var conn *cdp
	how := os.Getenv("HPK_BROWSER_DEVTOOLS") // pipe | tcp; empty: pipe, TCP only as a fallback
	if how != "tcp" {
		cmd, conn, err = m.startPipe(ctx, exe, args)
		switch {
		case err == nil:
			how = "pipe"
		case how == "pipe" || isolate.Active():
			return fmt.Errorf("the browser did not answer on its DevTools pipe (%v); with the AI isolated hpk does not open a DevTools port instead", err)
		default:
			log.Printf("browser: DevTools pipe failed (%v); using a 127.0.0.1 port instead", err)
			stopStale(profile)
		}
	}
	if conn == nil {
		how = "tcp"
		cmd = exec.Command(exe, append(append([]string{"--remote-debugging-port=0", "--remote-debugging-address=127.0.0.1"}, args...), "about:blank")...)
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start %s: %w", filepath.Base(exe), err)
		}
		found := make(chan string, 1)
		go func() {
			sc := bufio.NewScanner(stderr)
			for sc.Scan() {
				if u, ok := devtoolsURL(sc.Text()); ok {
					found <- u
				}
			}
		}()
		var wsURL string
		select {
		case wsURL = <-found:
		case <-time.After(20 * time.Second):
			kill()
			return errors.New("the browser did not open its DevTools socket (another browser may hold hpk's profile; close it and try again)")
		case <-ctx.Done():
			kill()
			return ctx.Err()
		}
		if conn, err = dialCDP(wsURL, m.onEvent); err != nil {
			kill()
			return err
		}
	}
	var targets struct {
		TargetInfos []struct{ TargetID, Type string } `json:"targetInfos"`
	}
	if err := conn.call(ctx, "", "Target.getTargets", nil, &targets); err != nil {
		kill()
		return err
	}
	target := ""
	for _, t := range targets.TargetInfos {
		if t.Type == "page" {
			target = t.TargetID
			break
		}
	}
	if target == "" {
		var r struct{ TargetID string }
		if err := conn.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &r); err != nil {
			return err
		}
		target = r.TargetID
	}
	var att struct{ SessionID string }
	if err := conn.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true}, &att); err != nil {
		kill()
		return err
	}
	// popups open in the same tab: one tab keeps the live view and the agent in step
	_ = conn.call(ctx, "", "Target.setDiscoverTargets", map[string]any{"discover": true}, nil)
	s := att.SessionID
	for _, c := range []struct {
		m string
		p any
	}{{"Page.enable", nil}, {"Runtime.enable", nil}, {"Page.setLifecycleEventsEnabled", map[string]any{"enabled": true}},
		{"Emulation.setDeviceMetricsOverride", map[string]any{"width": st.Width, "height": st.Height, "deviceScaleFactor": 1, "mobile": false}},
		{"Page.startScreencast", map[string]any{"format": "jpeg", "quality": 62, "maxWidth": st.Width, "maxHeight": st.Height, "everyNthFrame": 1}}} {
		if err := conn.call(ctx, s, c.m, c.p, nil); err != nil {
			kill()
			return err
		}
	}
	rec, err := newRecorder(dir)
	if err != nil {
		kill()
		return err
	}
	m.mu.Lock()
	m.cmd, m.conn, m.session, m.target, m.rec = cmd, conn, s, target, rec
	m.filled = map[string]string{}
	m.mu.Unlock()
	m.setState(func(s *State) {
		s.Running, s.Exe, s.Run, s.URL, s.Title, s.DevTools = true, filepath.Base(exe), rec.id, "about:blank", "", how
	})
	exited := make(chan struct{})
	m.mu.Lock()
	m.exited = exited
	m.mu.Unlock()
	go func() {
		if cmd != nil {
			_ = cmd.Wait()
		}
		close(exited)
		<-conn.closed
		m.mu.Lock()
		was := m.conn == conn
		if was {
			m.conn, m.cmd, m.session = nil, nil, ""
		}
		m.mu.Unlock()
		if was {
			rec.close()
			m.setState(func(s *State) { s.Running, s.Busy, s.Loading = false, "", false })
		}
	}()
	return nil
}

// startPipe starts the browser with --remote-debugging-pipe and checks that
// it answers there.
func (m *Manager) startPipe(ctx context.Context, exe string, args []string) (*exec.Cmd, *cdp, error) {
	d, err := newDevtoolsPipes()
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(exe)
	extra, err := attachPipes(cmd, d)
	if err != nil {
		d.closeAll()
		return nil, nil, err
	}
	cmd.Args = append(append(append([]string{exe}, extra...), args...), "about:blank")
	if err := cmd.Start(); err != nil {
		d.closeAll()
		return nil, nil, fmt.Errorf("start %s: %w", filepath.Base(exe), err)
	}
	d.afterStart()
	conn := newCDP(newPipeConn(d.ourIn, d.ourOut), m.onEvent)
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := conn.call(pctx, "", "Browser.getVersion", nil, nil); err != nil {
		conn.close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, nil, err
	}
	return cmd, conn, nil
}

// Stop closes the browser and ends the recording.
func (m *Manager) Stop() {
	m.mu.Lock()
	cmd, conn, rec, exited := m.cmd, m.conn, m.rec, m.exited
	m.cmd, m.conn, m.session = nil, nil, ""
	m.mu.Unlock()
	if conn != nil {
		_ = conn.call(context.Background(), "", "Browser.close", nil, nil)
		conn.close()
	}
	// wait for the browser to let go of its profile; kill it if it does not
	if cmd != nil && cmd.Process != nil && exited != nil {
		select {
		case <-exited:
		case <-time.After(4 * time.Second):
			_ = cmd.Process.Kill()
			select {
			case <-exited:
			case <-time.After(3 * time.Second):
			}
		}
	}
	// Chrome's helper processes (network, storage) outlive the main one for a
	// moment and still hold files in the profile
	if cmd != nil {
		waitHelpers(filepath.Join(Dir(m.home), "profile"), 3*time.Second)
	}
	if rec != nil {
		rec.close()
	}
	m.setState(func(s *State) { s.Running, s.Busy, s.Loading = false, "", false })
}

// waitHelpers waits for every process on the profile to end, then kills what is left.
func waitHelpers(profile string, d time.Duration) {
	needle := "--user-data-dir=" + profile
	left := func() []procs.Proc {
		list, err := procs.List(context.Background())
		if err != nil {
			return nil
		}
		var out []procs.Proc
		for _, p := range list {
			if strings.Contains(p.Cmd, needle) {
				out = append(out, p)
			}
		}
		return out
	}
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if len(left()) == 0 {
			return
		}
	}
	for _, p := range left() {
		_ = procs.Kill(context.Background(), p.PID, p.Start)
	}
	time.Sleep(300 * time.Millisecond)
}

func (m *Manager) onEvent(method, session string, params json.RawMessage) {
	switch method {
	case "Page.screencastFrame":
		var f struct {
			Data      string `json:"data"`
			SessionID int    `json:"sessionId"`
		}
		if json.Unmarshal(params, &f) != nil {
			return
		}
		m.mu.Lock()
		conn, s := m.conn, m.session
		m.mu.Unlock()
		if conn != nil {
			go conn.call(context.Background(), s, "Page.screencastFrameAck", map[string]any{"sessionId": f.SessionID}, nil)
		}
		img, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			return
		}
		m.mu.Lock()
		m.frame = img
		m.mu.Unlock()
		m.emit(Event{Kind: "frame", Frame: img})
	case "Page.frameNavigated":
		var p struct {
			Frame struct {
				ParentID string `json:"parentId"`
				URL      string `json:"url"`
			} `json:"frame"`
		}
		if json.Unmarshal(params, &p) == nil && p.Frame.ParentID == "" {
			m.setState(func(s *State) { s.URL = p.Frame.URL })
		}
	case "Page.lifecycleEvent":
		var p struct{ Name string }
		if json.Unmarshal(params, &p) == nil {
			switch p.Name {
			case "init":
				m.setState(func(s *State) { s.Loading = true })
			case "load", "networkAlmostIdle":
				m.setState(func(s *State) { s.Loading = false })
				go m.refreshTitle()
			}
		}
	case "Target.targetCreated":
		// a popup: bring its address into the one tab
		var p struct {
			TargetInfo struct {
				TargetID, Type, URL, OpenerID string
			} `json:"targetInfo"`
		}
		// every new tab is closed (one tab keeps the person watching what runs);
		// one a page opened has its address brought into the tab, recorded as the page's doing
		if json.Unmarshal(params, &p) == nil && p.TargetInfo.Type == "page" && p.TargetInfo.TargetID != m.target {
			m.mu.Lock()
			conn := m.conn
			m.mu.Unlock()
			if conn != nil {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					time.Sleep(400 * time.Millisecond)
					var info struct {
						TargetInfo struct{ URL string } `json:"targetInfo"`
					}
					_ = conn.call(ctx, "", "Target.getTargetInfo", map[string]any{"targetId": p.TargetInfo.TargetID}, &info)
					_ = conn.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": p.TargetInfo.TargetID}, nil)
					if u := info.TargetInfo.URL; strings.HasPrefix(u, "http") && p.TargetInfo.OpenerID != "" {
						_, _ = m.Do(ctx, "page", Action{Tool: "open", URL: u, Note: "popup"})
					}
				}()
			}
		}
	}
}

func (m *Manager) refreshTitle() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var title string
	if err := m.eval(ctx, "document.title", &title); err == nil {
		m.setState(func(s *State) { s.Title = title })
	}
}

func (m *Manager) page() (*cdp, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn == nil {
		return nil, "", errors.New("the browser is not running")
	}
	return m.conn, m.session, nil
}

// eval runs an expression in the page and decodes its JSON-able value.
func (m *Manager) eval(ctx context.Context, expr string, out any) error {
	conn, s, err := m.page()
	if err != nil {
		return err
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := conn.call(ctx, s, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true}, &r); err != nil {
		return err
	}
	if r.ExceptionDetails != nil {
		msg := r.ExceptionDetails.Exception.Description
		if msg == "" {
			msg = r.ExceptionDetails.Text
		}
		if i := strings.Index(msg, "\n"); i > 0 {
			msg = msg[:i]
		}
		return errors.New(strings.TrimPrefix(msg, "Error: "))
	}
	if out != nil && len(r.Result.Value) > 0 {
		return json.Unmarshal(r.Result.Value, out)
	}
	return nil
}

func host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// activePort reads the DevTools address a running browser wrote into its profile.
func activePort(profile string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
	if err != nil {
		return "", false
	}
	lines := strings.Fields(string(raw))
	if len(lines) < 2 {
		return "", false
	}
	port := strings.TrimSpace(lines[0])
	path := strings.TrimSpace(lines[1])
	if port == "" || !strings.HasPrefix(path, "/devtools/browser/") {
		return "", false
	}
	return "ws://127.0.0.1:" + port + path, true
}

// stopStale ends browsers still running on hpk's profile (from an earlier hpk).
func stopStale(profile string) {
	list, err := procs.List(context.Background())
	if err != nil {
		return
	}
	needle := "--user-data-dir=" + profile
	for _, p := range list {
		if strings.Contains(p.Cmd, needle) && !strings.Contains(p.Cmd, "--type=") {
			_ = procs.Kill(context.Background(), p.PID, p.Start)
		}
	}
	time.Sleep(300 * time.Millisecond)
}

// noPasswordSaving turns off Chrome's own password saving for hpk's profile:
// passwords live in the vault, not in the browser.
func noPasswordSaving(profile string) {
	pref := filepath.Join(profile, "Default", "Preferences")
	if _, err := os.Stat(pref); err == nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(pref), 0o700)
	_ = os.WriteFile(pref, []byte(`{"credentials_enable_service":false,"profile":{"password_manager_enabled":false},"autofill":{"profile_enabled":false,"credit_card_enabled":false}}`), 0o600)
}
