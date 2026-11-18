// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hpk.local/hpk/internal/vault"
)

// Action is one thing to do in the browser, by the agent or the person.
type Action struct {
	Tool   string  `json:"tool"` // open snapshot click type fill_secret press scroll back forward reload wait screenshot secrets
	URL    string  `json:"url,omitempty"`
	Ref    string  `json:"ref,omitempty"`  // e12, from the last snapshot
	Text   string  `json:"text,omitempty"` // type: text; click/wait: visible text to find
	Secret string  `json:"secret,omitempty"`
	Key    string  `json:"key,omitempty"`
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	Dir    string  `json:"dir,omitempty"` // up | down
	Submit bool    `json:"submit,omitempty"`
	Ms     int     `json:"ms,omitempty"`
	Note   string  `json:"note,omitempty"`
}

// Result is what the caller gets back. Text is already scrubbed.
type Result struct {
	Text  string `json:"text"`
	Image []byte `json:"-"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

// ErrHuman: the person has the wheel.
var ErrHuman = errors.New("the person is using the browser right now; wait, or ask them to hand it back in the Console")

// Driver hands the browser to the agent or the person.
func (m *Manager) SetDriver(d string) {
	if d != "agent" && d != "human" {
		return
	}
	m.setState(func(s *State) { s.Driver = d; s.Busy = "" })
	if r := m.recorder(); r != nil {
		r.add(Step{By: "human", Tool: "driver", Label: map[string]string{"agent": "handed to the AI", "human": "took over"}[d]})
	}
}

func (m *Manager) recorder() *recorder {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rec
}

// Do runs one action. by is "agent" or "human".
func (m *Manager) Do(ctx context.Context, by string, a Action) (*Result, error) {
	if by == "agent" && m.State().Driver == "human" {
		return nil, ErrHuman
	}
	if a.Tool == "secrets" {
		return m.secretList(), nil
	}
	if !m.State().Running {
		if err := m.Start(ctx); err != nil {
			return nil, err
		}
	}
	m.actMu.Lock()
	defer m.actMu.Unlock()
	if by == "agent" {
		m.setState(func(s *State) { s.Busy = describe(a) })
		defer m.setState(func(s *State) { s.Busy = "" })
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	step := Step{By: by, Tool: a.Tool}
	res, label, err := m.run(ctx, by, a, &step)
	step.Label = m.scrub(label)
	if err != nil {
		step.Error = m.scrub(err.Error())
	}
	st := m.State()
	step.URL, step.Title = m.scrub(st.URL), m.scrub(st.Title)
	if a.Tool != "snapshot" && a.Tool != "wait" && a.Tool != "screenshot" {
		if img, e := m.capture(ctx, 55, 1024); e == nil {
			step.shot = img
		}
	} else if a.Tool == "screenshot" && res != nil {
		step.shot = res.Image
	}
	if r := m.recorder(); r != nil {
		r.add(step)
	}
	m.emit(Event{Kind: "step", Data: step})
	if err != nil {
		return nil, errors.New(m.scrub(err.Error()))
	}
	if res == nil {
		res = &Result{}
	}
	res.URL, res.Title = step.URL, step.Title
	res.Text = m.scrub(res.Text)
	return res, nil
}

func (m *Manager) scrub(s string) string {
	if m.vault == nil {
		return s
	}
	return m.vault.Scrub(s)
}

func describe(a Action) string {
	switch a.Tool {
	case "open":
		return "opening " + host(a.URL)
	case "click":
		return "clicking " + firstOf(a.Ref, a.Text)
	case "type":
		return "typing into " + a.Ref
	case "fill_secret":
		return "filling {{secret:" + a.Secret + "}}"
	}
	return a.Tool
}

func firstOf(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func (m *Manager) run(ctx context.Context, by string, a Action, step *Step) (*Result, string, error) {
	conn, s, err := m.page()
	if err != nil {
		return nil, "", err
	}
	switch a.Tool {
	case "open":
		u, err := url.Parse(strings.TrimSpace(a.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			if err == nil && u.Scheme == "" && u.Host == "" && strings.Contains(a.URL, ".") && !strings.ContainsAny(a.URL, " \"<>") {
				u, err = url.Parse("https://" + strings.TrimSpace(a.URL))
			}
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return nil, a.URL, errors.New("open takes an http(s) address")
			}
		}
		var nav struct{ ErrorText string }
		if err := conn.call(ctx, s, "Page.navigate", map[string]any{"url": u.String()}, &nav); err != nil {
			return nil, u.String(), err
		}
		if nav.ErrorText != "" {
			return nil, u.String(), errors.New(nav.ErrorText)
		}
		m.waitLoad(ctx, 15*time.Second)
		m.mu.Lock()
		m.filled = map[string]string{}
		m.mu.Unlock()
		snap, err := m.snapshot(ctx)
		return &Result{Text: snap}, u.String(), err

	case "snapshot":
		snap, err := m.snapshot(ctx)
		return &Result{Text: snap}, "", err

	case "click":
		var t target
		var err error
		switch {
		case a.Ref != "":
			t, err = m.find(ctx, "ref", a.Ref)
		case a.Text != "":
			if m.hasFill(ctx) {
				return nil, a.Text, errors.New("this page holds a filled secret: click by ref (take a snapshot), not by text")
			}
			t, err = m.find(ctx, "text", a.Text)
		case a.X > 0 || a.Y > 0:
			t = target{X: a.X, Y: a.Y, Label: fmt.Sprintf("(%d, %d)", int(a.X), int(a.Y))}
		default:
			return nil, "", errors.New("click needs ref, text or x/y")
		}
		if err != nil {
			return nil, firstOf(a.Ref, a.Text), err
		}
		step.X, step.Y = t.X, t.Y
		if err := m.mouseClick(ctx, t.X, t.Y); err != nil {
			return nil, t.Label, err
		}
		m.settle(ctx)
		if by == "human" {
			return &Result{}, t.Label, nil
		}
		snap, _ := m.snapshot(ctx)
		return &Result{Text: snap}, t.Label, nil

	case "type":
		if t := strings.TrimSpace(a.Text); strings.HasPrefix(t, "{{secret:") {
			// typing a reference means filling the secret
			return m.run(ctx, by, Action{Tool: "fill_secret", Ref: a.Ref, Secret: t, Submit: a.Submit}, step)
		}
		if a.Ref != "" {
			t, err := m.find(ctx, "focus", a.Ref)
			if err != nil {
				return nil, a.Ref, err
			}
			step.X, step.Y = t.X, t.Y
			label := t.Label
			if by == "human" {
				label = fmt.Sprintf("%s: %d characters", t.Label, len([]rune(a.Text)))
			} else {
				label = fmt.Sprintf("%s: %q", t.Label, a.Text)
			}
			if err := conn.call(ctx, s, "Input.insertText", map[string]any{"text": a.Text}, nil); err != nil {
				return nil, label, err
			}
			if a.Submit {
				m.key(ctx, "Enter")
				m.settle(ctx)
			}
			return &Result{Text: "typed"}, label, nil
		}
		if err := conn.call(ctx, s, "Input.insertText", map[string]any{"text": a.Text}, nil); err != nil {
			return nil, "", err
		}
		return &Result{Text: "typed"}, fmt.Sprintf("%d characters", len([]rune(a.Text))), nil

	case "fill_secret":
		name, part, ok := vault.ParseRef(a.Secret)
		if !ok {
			return nil, a.Secret, errors.New("give a secret name, e.g. github-login or github-login.user")
		}
		label := "{{secret:" + name + map[bool]string{true: ".user", false: ""}[part == "user"] + "}}"
		if a.Ref == "" {
			return nil, label, errors.New("fill_secret needs the ref of the field (from a snapshot)")
		}
		if by != "agent" {
			return nil, label, errors.New("secrets are filled for the AI by name; type your own passwords yourself")
		}
		var loc string
		if err := m.eval(ctx, "location.href", &loc); err != nil {
			return nil, label, err
		}
		if m.vault == nil {
			return nil, label, errors.New("the vault is not open")
		}
		u, err := url.Parse(loc)
		if err != nil || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname()))) {
			return nil, label, errors.New("secrets are filled only on https pages")
		}
		if p := u.Port(); p != "" && p != "443" && !isLoopbackHost(u.Hostname()) {
			return nil, label, errors.New("secrets are filled only on the site's normal https port")
		}
		value, err := m.vault.ForFill(name, part, u.Hostname())
		if err != nil {
			return nil, label, err
		}
		// One step inside the page: check the page is still that site, that the
		// field is the right kind, mask it, then set the value. Nothing types into
		// "whatever has focus", so a page cannot move focus to catch it.
		marker := fmt.Sprintf("%s:%s", name, part)
		val, _ := json.Marshal(value)
		value = ""
		var r struct {
			OK    bool
			Err   string
			Label string
			X, Y  float64
		}
		err = m.eval(ctx, fmt.Sprintf(fillJS, a.Ref, u.Hostname(), u.Scheme+":", part, marker, val), &r)
		val = nil
		if err != nil {
			return nil, label, errors.New("could not fill the field")
		}
		if !r.OK {
			return nil, label, errors.New(r.Err)
		}
		step.X, step.Y = r.X, r.Y
		t := target{Label: r.Label}
		m.mu.Lock()
		m.filled[marker] = name
		m.mu.Unlock()
		if a.Submit {
			m.key(ctx, "Enter")
			m.settle(ctx)
		}
		return &Result{Text: "filled " + label + " into " + t.Label}, label + " → " + t.Label, nil

	case "press":
		k := a.Key
		if k == "" {
			k = a.Text
		}
		if err := m.key(ctx, k); err != nil {
			return nil, k, err
		}
		m.settle(ctx)
		return &Result{Text: "pressed " + k}, k, nil

	case "scroll":
		dy := 600.0
		if a.Dir == "up" {
			dy = -600
		}
		st := m.State()
		x, y := float64(st.Width)/2, float64(st.Height)/2
		if a.X > 0 {
			x, y = a.X, a.Y
		}
		if err := conn.call(ctx, s, "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": x, "y": y, "deltaX": 0, "deltaY": dy}, nil); err != nil {
			return nil, a.Dir, err
		}
		time.Sleep(250 * time.Millisecond)
		if by == "human" {
			return &Result{}, firstOf(a.Dir, "down"), nil
		}
		snap, _ := m.snapshot(ctx)
		return &Result{Text: snap}, firstOf(a.Dir, "down"), nil

	case "back", "forward":
		d := -1
		if a.Tool == "forward" {
			d = 1
		}
		_ = m.eval(ctx, fmt.Sprintf("history.go(%d)", d), nil)
		m.waitLoad(ctx, 10*time.Second)
		snap, _ := m.snapshot(ctx)
		return &Result{Text: snap}, "", nil

	case "reload":
		if err := conn.call(ctx, s, "Page.reload", nil, nil); err != nil {
			return nil, "", err
		}
		m.waitLoad(ctx, 15*time.Second)
		return &Result{Text: "reloaded"}, "", nil

	case "wait":
		if a.Text != "" && m.hasFill(ctx) {
			return nil, a.Text, errors.New("this page holds a filled secret: wait by time (ms), not for text")
		}
		if a.Text != "" {
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				var found bool
				_ = m.eval(ctx, fmt.Sprintf("document.body && document.body.innerText.toLowerCase().includes(%q)", strings.ToLower(a.Text)), &found)
				if found {
					return &Result{Text: "found " + strconv.Quote(a.Text)}, a.Text, nil
				}
				time.Sleep(400 * time.Millisecond)
			}
			return nil, a.Text, fmt.Errorf("%q did not appear within 15 s", a.Text)
		}
		ms := a.Ms
		if ms <= 0 || ms > 15000 {
			ms = 1000
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return &Result{Text: "waited"}, fmt.Sprintf("%d ms", ms), nil

	case "screenshot":
		img, err := m.capture(ctx, 70, 1280)
		if err != nil {
			return nil, "", err
		}
		return &Result{Text: "screenshot of " + m.State().URL, Image: img}, "", nil
	}
	return nil, a.Tool, fmt.Errorf("unknown browser tool %q", a.Tool)
}

func (m *Manager) secretList() *Result {
	if m.vault == nil {
		return &Result{Text: "The vault is not open."}
	}
	names := m.vault.Names()
	if len(names) == 0 {
		return &Result{Text: "No secrets yet. Ask the person to add them in Console > Personal > Vault; you will only ever see their names."}
	}
	var b strings.Builder
	b.WriteString("Secrets you can fill with fill_secret (values are typed by hpk; you never see them):\n")
	for _, n := range names {
		fmt.Fprintf(&b, "- %s (%s) sites: %s", n["name"], n["kind"], strings.Join(n["domains"].([]string), ", "))
		if n["hasUser"].(bool) {
			fmt.Fprintf(&b, " — user name: %s.user", n["name"])
		}
		b.WriteString("\n")
	}
	return &Result{Text: b.String()}
}

type target struct {
	X, Y     float64
	Label    string
	Editable bool
}

// find locates an element (by ref or visible text), scrolls it into view and
// returns its centre. mode "focus" also focuses it and selects its content.
func (m *Manager) find(ctx context.Context, mode, key string) (target, error) {
	var t struct {
		X, Y     float64
		Label    string
		Editable bool
		Err      string
	}
	err := m.eval(ctx, fmt.Sprintf(findJS, mode, key), &t)
	if err != nil {
		return target{}, err
	}
	if t.Err != "" {
		return target{}, errors.New(t.Err)
	}
	return target{X: t.X, Y: t.Y, Label: t.Label, Editable: t.Editable}, nil
}

func (m *Manager) mouseClick(ctx context.Context, x, y float64) error {
	conn, s, err := m.page()
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.clickAt = [2]float64{x, y}
	m.mu.Unlock()
	m.emit(Event{Kind: "click", Data: map[string]float64{"x": x, "y": y}})
	for _, typ := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		p := map[string]any{"type": typ, "x": x, "y": y, "button": "left", "clickCount": 1}
		if typ == "mouseMoved" {
			p["button"] = "none"
		}
		if err := conn.call(ctx, s, "Input.dispatchMouseEvent", p, nil); err != nil {
			return err
		}
	}
	return nil
}

var keys = map[string][3]any{ // key, code, keyCode
	"Enter": {"Enter", "Enter", 13}, "Tab": {"Tab", "Tab", 9}, "Escape": {"Escape", "Escape", 27}, "Backspace": {"Backspace", "Backspace", 8},
	"ArrowDown": {"ArrowDown", "ArrowDown", 40}, "ArrowUp": {"ArrowUp", "ArrowUp", 38}, "ArrowLeft": {"ArrowLeft", "ArrowLeft", 37}, "ArrowRight": {"ArrowRight", "ArrowRight", 39},
	"PageDown": {"PageDown", "PageDown", 34}, "PageUp": {"PageUp", "PageUp", 33}, "Home": {"Home", "Home", 36}, "End": {"End", "End", 35}, "Delete": {"Delete", "Delete", 46}, "Space": {" ", "Space", 32},
}

func (m *Manager) key(ctx context.Context, k string) error {
	conn, s, err := m.page()
	if err != nil {
		return err
	}
	d, ok := keys[k]
	if !ok {
		return fmt.Errorf("key %q: use Enter, Tab, Escape, Backspace, Delete, Space, Arrow…, Page…, Home, End", k)
	}
	for _, typ := range []string{"rawKeyDown", "keyUp"} {
		p := map[string]any{"type": typ, "key": d[0], "code": d[1], "windowsVirtualKeyCode": d[2], "nativeVirtualKeyCode": d[2]}
		if typ == "rawKeyDown" && k == "Enter" {
			p["text"] = "\r"
			p["type"] = "keyDown"
		}
		if err := conn.call(ctx, s, "Input.dispatchKeyEvent", p, nil); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) waitLoad(ctx context.Context, max time.Duration) {
	deadline := time.Now().Add(max)
	time.Sleep(200 * time.Millisecond)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		var rs string
		if m.eval(ctx, "document.readyState", &rs) == nil && rs == "complete" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	m.refreshTitle()
}

// settle gives a click time to start a navigation, then waits for it.
func (m *Manager) settle(ctx context.Context) {
	time.Sleep(450 * time.Millisecond)
	m.waitLoad(ctx, 8*time.Second)
}

func (m *Manager) capture(ctx context.Context, quality, maxW int) ([]byte, error) {
	conn, s, err := m.page()
	if err != nil {
		return nil, err
	}
	var r struct{ Data string }
	if err := conn.call(ctx, s, "Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": quality}, &r); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(r.Data)
}

// snapshot is the page as the agent reads it: numbered elements and text.
func (m *Manager) snapshot(ctx context.Context) (string, error) {
	var out struct {
		URL, Title, Text string
		Count            int
		More             bool
	}
	if err := m.eval(ctx, snapshotJS, &out); err != nil {
		return "", err
	}
	head := fmt.Sprintf("Page: %s\nURL: %s\n", out.Title, out.URL)
	if out.More {
		out.Text += "\n… more below: scroll down, then snapshot again."
	}
	return head + out.Text, nil
}

// MarshalJSON keeps screenshots out of JSON results.
func (r Result) MarshalJSON() ([]byte, error) {
	type plain struct {
		Text     string `json:"text"`
		URL      string `json:"url"`
		Title    string `json:"title"`
		HasImage bool   `json:"hasImage,omitempty"`
		Image    string `json:"image,omitempty"`
	}
	p := plain{Text: r.Text, URL: r.URL, Title: r.Title, HasImage: len(r.Image) > 0}
	if len(r.Image) > 0 {
		p.Image = base64.StdEncoding.EncodeToString(r.Image)
	}
	return json.Marshal(p)
}

func (m *Manager) hasFill(ctx context.Context) bool {
	var has bool
	if err := m.eval(ctx, pageHasFill, &has); err != nil {
		return true // fail closed
	}
	return has
}

func isLoopbackHost(h string) bool {
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || strings.HasSuffix(h, ".localhost")
}
