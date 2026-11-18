// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package mcp

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Use is one MCP tool call, written by hpk's pre_tool_call hook to
// <home>/hpk/mcp-use.jsonl. Only names: never the tool's input or output.
type Use struct {
	At      time.Time `json:"at"`
	Server  string    `json:"server"`
	Tool    string    `json:"tool"`
	Session string    `json:"session,omitempty"` // Hermes session id
	Project string    `json:"project,omitempty"`
	Root    string    `json:"root,omitempty"` // the project's repository folder
	Blocked bool      `json:"blocked,omitempty"`
}

const useMax = 1 << 20

func usePath(home string) string { return filepath.Join(home, "hpk", "mcp-use.jsonl") }

// SplitTool reads Hermes' MCP tool name: mcp__<server>__<tool>.
func SplitTool(name string) (server, tool string, ok bool) {
	rest, found := strings.CutPrefix(name, "mcp__")
	if !found {
		return "", "", false
	}
	server, tool, ok = strings.Cut(rest, "__")
	return server, tool, ok && server != "" && tool != ""
}

// Record appends one call. Errors are ignored: recording must never stop a tool.
func Record(home string, u Use) {
	p := usePath(home)
	if fi, err := os.Stat(p); err == nil && fi.Size() > useMax {
		_ = os.Rename(p, p+".1")
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(u)
	f.Write(append(b, '\n'))
}

// Count is a name with how often and how lately.
type Count struct {
	Name  string    `json:"name"`
	Calls int       `json:"calls"`
	Last  time.Time `json:"last"`
	Root  string    `json:"root,omitempty"`
	Repo  string    `json:"repo,omitempty"`
}

// Usage is what one MCP is used by.
type Usage struct {
	Calls    int       `json:"calls"`
	Blocked  int       `json:"blocked"`
	Last     time.Time `json:"last,omitempty"`
	Tools    []Count   `json:"tools"`
	Projects []Count   `json:"projects"`
	Sessions []Count   `json:"sessions"`
	Skills   []Skill   `json:"skills"`
	Days     []int     `json:"days"` // calls per day, oldest first (14 days)
}

// Skill names a skill whose instructions mention the MCP.
type Skill struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func bump(m map[string]*Count, k string, at time.Time) *Count {
	c := m[k]
	if c == nil {
		c = &Count{Name: k}
		m[k] = c
	}
	c.Calls++
	if at.After(c.Last) {
		c.Last = at
	}
	return c
}

func sorted(m map[string]*Count, n int) []Count {
	out := make([]Count, 0, len(m))
	for _, c := range m {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Usages reads the call log (both files) and the skills, per MCP name.
func Usages(home string, skillDirs []string, now time.Time) map[string]*Usage {
	type acc struct {
		u                         *Usage
		tools, projects, sessions map[string]*Count
	}
	all := map[string]*acc{}
	get := func(s string) *acc {
		a := all[s]
		if a == nil {
			a = &acc{u: &Usage{Days: make([]int, 14)}, tools: map[string]*Count{}, projects: map[string]*Count{}, sessions: map[string]*Count{}}
			all[s] = a
		}
		return a
	}
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -13)
	for _, p := range []string{usePath(home) + ".1", usePath(home)} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 64<<10)
		for sc.Scan() {
			var u Use
			if json.Unmarshal(sc.Bytes(), &u) != nil || u.Server == "" {
				continue
			}
			a := get(u.Server)
			if u.Blocked {
				a.u.Blocked++
				continue
			}
			a.u.Calls++
			if u.At.After(a.u.Last) {
				a.u.Last = u.At
			}
			if d := int(u.At.In(now.Location()).Sub(day0).Hours() / 24); d >= 0 && d < 14 {
				a.u.Days[d]++
			}
			bump(a.tools, u.Tool, u.At)
			if u.Project != "" {
				bump(a.projects, u.Project, u.At).Root = u.Root
			}
			if u.Session != "" {
				bump(a.sessions, u.Session, u.At)
			}
		}
		f.Close()
	}
	out := map[string]*Usage{}
	for s, a := range all {
		a.u.Tools, a.u.Projects, a.u.Sessions = sorted(a.tools, 40), sorted(a.projects, 20), sorted(a.sessions, 12)
		out[s] = a.u
	}
	for name, skills := range skillsMentioning(skillDirs) {
		if out[name] == nil {
			out[name] = &Usage{Days: make([]int, 14)}
		}
		out[name].Skills = skills
	}
	for _, u := range out {
		if u.Tools == nil {
			u.Tools, u.Projects, u.Sessions = []Count{}, []Count{}, []Count{}
		}
		if u.Skills == nil {
			u.Skills = []Skill{}
		}
	}
	return out
}

var mentionRE = regexp.MustCompile(`mcp__([a-z0-9_-]+?)__`)

// skillsMentioning finds SKILL.md files that name an MCP tool (mcp__<server>__…).
func skillsMentioning(dirs []string) map[string][]Skill {
	out := map[string][]Skill{}
	seen := map[string]bool{}
	for _, d := range dirs {
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if e.IsDir() && (e.Name() == "node_modules" || strings.HasPrefix(e.Name(), ".")) && p != d {
				return filepath.SkipDir
			}
			if e.IsDir() || e.Name() != "SKILL.md" || seen[p] {
				return nil
			}
			seen[p] = true
			raw, err := os.ReadFile(p)
			if err != nil || len(raw) > 512<<10 {
				return nil
			}
			name := filepath.Base(filepath.Dir(p))
			got := map[string]bool{}
			for _, m := range mentionRE.FindAllStringSubmatch(string(raw), -1) {
				if !got[m[1]] {
					got[m[1]] = true
					out[m[1]] = append(out[m[1]], Skill{Name: name, Path: p})
				}
			}
			return nil
		})
	}
	return out
}
