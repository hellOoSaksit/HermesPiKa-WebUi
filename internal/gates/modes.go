// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package gates

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hpk.local/hpk/internal/project"
)

// Modes (§11.1).
const (
	Manual = "manual" // read, draft and log only; every write and command asks
	Ask    = "ask"    // work freely inside the project; decision points and red lines ask
	Auto   = "auto"   // Hermes' smart approvals decide; only red lines ask
)

// ModeState is <project>/gates.json.
type ModeState struct {
	Mode      string    `json:"mode"`
	AutoUntil time.Time `json:"auto_until,omitempty"` // Auto falls back to Ask after this
	Session   string    `json:"session,omitempty"`    // Auto limited to one session
	SetBy     string    `json:"set_by,omitempty"`
	SetAt     time.Time `json:"set_at"`
}

func modePath(p *project.Project) string { return filepath.Join(p.Dir, "gates.json") }

// Current returns the effective mode now: an expired Auto reads as Ask.
func Current(p *project.Project, now time.Time) ModeState {
	st := ModeState{Mode: defaultMode(p)}
	if raw, err := os.ReadFile(modePath(p)); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	if st.Mode == Auto && !st.AutoUntil.IsZero() && now.After(st.AutoUntil) {
		st = ModeState{Mode: Ask, SetBy: "expired auto", SetAt: st.AutoUntil}
	}
	return st
}

// ForSession is the mode that applies to one session (Auto may be scoped to a session).
func ForSession(p *project.Project, session string, now time.Time) string {
	st := Current(p, now)
	if st.Mode == Auto && st.Session != "" && st.Session != session {
		return Ask
	}
	return st.Mode
}

// SetMode changes the mode. Auto may carry a duration and a session scope.
func SetMode(p *project.Project, mode string, d time.Duration, session, by string, now time.Time) (ModeState, error) {
	if mode != Manual && mode != Ask && mode != Auto {
		return ModeState{}, fmt.Errorf("mode must be manual, ask or auto, not %q", mode)
	}
	st := ModeState{Mode: mode, SetBy: by, SetAt: now.UTC()}
	if mode == Auto {
		if d > 0 {
			st.AutoUntil = now.Add(d).UTC()
		}
		st.Session = session
	}
	raw, _ := json.MarshalIndent(st, "", "  ")
	if err := os.WriteFile(modePath(p), raw, 0o644); err != nil {
		return st, err
	}
	detail := mode
	if !st.AutoUntil.IsZero() {
		detail += " until " + st.AutoUntil.Format(time.RFC3339)
	}
	if session != "" {
		detail += " for session " + session
	}
	return st, Record(p, Decision{At: now.UTC(), Kind: "mode", Mode: mode, By: by, Detail: detail})
}

func defaultMode(p *project.Project) string {
	raw, err := os.ReadFile(filepath.Join(p.Dir, "project.yaml"))
	if err != nil {
		return Ask
	}
	var f project.File
	if err := yamlUnmarshal(raw, &f); err == nil {
		switch f.Gates.DefaultMode {
		case Manual, Ask, Auto:
			return f.Gates.DefaultMode
		}
	}
	return Ask
}

// ---- audit (§11.4) ----

// Decision is one line of <project>/decisions.jsonl, mirrored readably in audit.log.
type Decision struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"` // ask | auto-allow | allow | mode | human
	Mode    string    `json:"mode,omitempty"`
	Lines   []string  `json:"lines,omitempty"` // red lines crossed
	Tool    string    `json:"tool,omitempty"`
	Detail  string    `json:"detail,omitempty"`
	Session string    `json:"session,omitempty"`
	By      string    `json:"by,omitempty"`      // person, "auto" or "hpk"
	Outcome string    `json:"outcome,omitempty"` // for human decisions: approved | denied
}

// Record appends a decision; it never rewrites earlier ones.
func Record(p *project.Project, d Decision) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(p.Dir, "decisions.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(append(raw, '\n'))
	f.Close()
	if err != nil {
		return err
	}
	who := d.By
	if who == "" {
		who = "hpk"
	}
	return project.Audit(p.Dir, "["+who+"] "+d.Kind, strings.TrimSpace(strings.Join(d.Lines, ",")+" "+d.Tool+" "+d.Detail))
}

// Decisions reads decisions since a time, oldest first.
func Decisions(p *project.Project, since time.Time) ([]Decision, error) {
	f, err := os.Open(filepath.Join(p.Dir, "decisions.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Decision
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var d Decision
		if json.Unmarshal(sc.Bytes(), &d) == nil && !d.At.Before(since) {
			out = append(out, d)
		}
	}
	return out, sc.Err()
}

// Digest is the evening list of what Auto let through (§11.4), numbered so a
// person can answer "revert 3,7".
func Digest(p *project.Project, since time.Time) ([]Decision, error) {
	all, err := Decisions(p, since)
	if err != nil {
		return nil, err
	}
	var out []Decision
	for _, d := range all {
		if d.Kind == "auto-allow" {
			out = append(out, d)
		}
	}
	return out, nil
}
