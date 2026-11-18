// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package install adds hpk to a Hermes profile and takes it out again
// (docs/DESIGN.md §15). It changes only what it lists: the skills folder in
// skills.external_dirs and hpk's entries under hooks. config.yaml is edited as a
// text at fixed line positions (every other line kept byte for byte), backed up first, and shown as a diff
// before anything is written. Changing Hermes config is a red line: the CLI
// writes only after a person says yes.
package install

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Hook is one hpk entry under hooks.<event>.
type Hook struct {
	Event      string
	Name       string // hpk hook <name>
	FailClosed bool
	Timeout    int
	Matcher    string
}

// Hooks hpk registers. Guards that protect people fail closed (deny when they break).
var Hooks = []Hook{
	{Event: "pre_tool_call", Name: "red_line_guard", FailClosed: true, Timeout: 15},
	{Event: "pre_tool_call", Name: "privacy_guard", FailClosed: true, Timeout: 15},
	{Event: "pre_tool_call", Name: "board_guard", Timeout: 15, Matcher: "^(write_file|patch)$"},
	{Event: "post_tool_call", Name: "board_heartbeat", Timeout: 15},
	{Event: "on_session_end", Name: "child_report", Timeout: 15},
	{Event: "post_approval_response", Name: "approval_audit", Timeout: 15},
	// Observers: where each request goes, so privacy_guard can enforce model_policy: local_only.
	{Event: "pre_api_request", Name: "model_route", Timeout: 15},
	{Event: "pre_auxiliary_call", Name: "model_route", Timeout: 15},
	// Observers: real token use per session, for the Console and budgets.
	{Event: "post_api_request", Name: "model_usage", Timeout: 15},
	{Event: "post_auxiliary_call", Name: "model_usage", Timeout: 15},
	// Links a cron run to its project and job from the tag in hpk's prompt.
	{Event: "pre_llm_call", Name: "session_link", Timeout: 15},
	{Event: "post_compaction", Name: "compaction_note", Timeout: 15},
}

// Paths of an install.
type Paths struct {
	Home      string // Hermes home
	Profile   string // "" = the default profile
	Config    string // config.yaml being edited
	Bin       string // where the hpk binary is copied
	SkillsDir string // where hpk's skills are copied (added to skills.external_dirs)
	Manifest  string
}

// Resolve works out the paths for a home and profile.
func Resolve(home, profile string) Paths {
	cfg := filepath.Join(home, "config.yaml")
	if profile != "" {
		cfg = filepath.Join(home, "profiles", profile, "config.yaml")
	}
	exe := "hpk"
	if runtime.GOOS == "windows" {
		exe = "hpk.exe"
	}
	return Paths{Home: home, Profile: profile, Config: cfg, Bin: filepath.Join(home, "hpk", "bin", exe),
		SkillsDir: filepath.Join(home, "hpk", "skills"), Manifest: filepath.Join(home, "hpk", "install.json")}
}

func (p Paths) command(name string) string {
	return fmt.Sprintf("%q hook %s", filepath.ToSlash(p.Bin), name)
}

// isOurs recognises an hpk hook entry by its command.
func isOurs(cmd string) bool {
	c := strings.ToLower(filepath.ToSlash(cmd))
	return strings.Contains(c, "/hpk/bin/hpk") && strings.Contains(c, " hook ")
}

// Plan is what an install or uninstall would change.
type Plan struct {
	Paths   Paths    `json:"paths"`
	Before  string   `json:"-"`
	After   string   `json:"-"`
	Diff    []string `json:"diff"`
	Changes []string `json:"changes"`
	Exists  bool     `json:"config_exists"`
}

// Manifest records what an install did, so uninstall and doctor can check it.
type Manifest struct {
	Version   string    `json:"version"`
	At        time.Time `json:"at"`
	Config    string    `json:"config"`
	Backup    string    `json:"backup"`
	Bin       string    `json:"bin"`
	SkillsDir string    `json:"skills_dir"`
	Hooks     []string  `json:"hooks"`
}

// Apply writes a plan: backup, binary, skills, config, manifest. Nothing is
// written when the plan changes nothing.
func Apply(pl Plan, self string, skills fs.FS, version string, now time.Time) (Manifest, error) {
	m := Manifest{Version: version, At: now.UTC(), Config: pl.Paths.Config, Bin: pl.Paths.Bin, SkillsDir: pl.Paths.SkillsDir}
	if err := os.MkdirAll(filepath.Dir(pl.Paths.Bin), 0o755); err != nil {
		return m, err
	}
	if self != "" && !sameFile(self, pl.Paths.Bin) && !sameBytes(self, pl.Paths.Bin) {
		if err := copyFile(self, pl.Paths.Bin, 0o755); err != nil {
			return m, fmt.Errorf("copy hpk to %s: %w", pl.Paths.Bin, err)
		}
	}
	if skills != nil {
		if err := copyTree(skills, pl.Paths.SkillsDir, pl.Paths.Bin); err != nil {
			return m, err
		}
	}
	if pl.Before != pl.After {
		if pl.Exists {
			m.Backup = pl.Paths.Config + ".hpk-backup-" + now.Format("20060102-150405")
			if err := os.WriteFile(m.Backup, []byte(pl.Before), 0o600); err != nil {
				return m, err
			}
		}
		if err := os.MkdirAll(filepath.Dir(pl.Paths.Config), 0o755); err != nil {
			return m, err
		}
		tmp := pl.Paths.Config + ".hpk-tmp"
		if err := os.WriteFile(tmp, []byte(pl.After), 0o600); err != nil {
			return m, err
		}
		if err := os.Rename(tmp, pl.Paths.Config); err != nil {
			return m, err
		}
	}
	for _, h := range Hooks {
		m.Hooks = append(m.Hooks, h.Event+":"+h.Name)
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	return m, os.WriteFile(pl.Paths.Manifest, raw, 0o644)
}

// ApplyUninstall writes the cleaned config (with a backup) and removes hpk's skills copy.
// The binary stays (it may be the one running); projects and wikis are never touched.
func ApplyUninstall(pl Plan, now time.Time) (string, error) {
	backup := ""
	if pl.Before != pl.After && pl.Exists {
		backup = pl.Paths.Config + ".hpk-backup-" + now.Format("20060102-150405")
		if err := os.WriteFile(backup, []byte(pl.Before), 0o600); err != nil {
			return "", err
		}
		if err := os.WriteFile(pl.Paths.Config, []byte(pl.After), 0o600); err != nil {
			return backup, err
		}
	}
	_ = os.RemoveAll(pl.Paths.SkillsDir)
	_ = os.Remove(pl.Paths.Manifest)
	return backup, nil
}

// lineDiff is a small line-based diff (LCS) good enough for a config file review.
func lineDiff(a, b string) []string {
	a = strings.TrimPrefix(strings.ReplaceAll(a, "\r\n", "\n"), "\ufeff")
	b = strings.TrimPrefix(strings.ReplaceAll(b, "\r\n", "\n"), "\ufeff")
	x := strings.Split(strings.TrimRight(a, "\n"), "\n")
	y := strings.Split(strings.TrimRight(b, "\n"), "\n")
	if a == "" {
		x = nil
	}
	n, m := len(x), len(y)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && x[i] == y[j]:
			i++
			j++
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			out = append(out, "+ "+y[j])
			j++
		default:
			out = append(out, "- "+x[i])
			i++
		}
	}
	return out
}

// ---- files ----

func sameFile(a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}

// sameBytes avoids replacing an identical binary: Hermes would then ask to approve the hooks again.
func sameBytes(a, b string) bool {
	x, err1 := os.ReadFile(a)
	y, err2 := os.ReadFile(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// binNote tells the agent where hpk is, since it is usually not on PATH.
func binNote(raw []byte, bin string) []byte {
	s := string(raw)
	b := filepath.ToSlash(bin)
	note := "\n> **hpk on this machine:** `\"" + b + "\"`. When `hpk` is not on PATH, run that path wherever this skill says `hpk` (in PowerShell: `& \"" + b + "\" ...`).\n"
	if strings.HasPrefix(s, "---") {
		if i := strings.Index(s[3:], "\n---"); i >= 0 {
			end := 3 + i + 4
			if nl := strings.IndexByte(s[end:], 10); nl >= 0 {
				end += nl + 1
			}
			return []byte(s[:end] + note + s[end:])
		}
	}
	return []byte(note + s)
}

func copyTree(src fs.FS, dst, bin string) error {
	var files []string
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		raw, err := fs.ReadFile(src, f)
		if err != nil {
			return err
		}
		if path.Base(f) == "SKILL.md" && bin != "" {
			raw = binNote(raw, bin)
		}
		out := filepath.Join(dst, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}
