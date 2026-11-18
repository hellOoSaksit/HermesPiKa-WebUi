// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package gates

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"hpk.local/hpk/internal/project"
)

func yamlUnmarshal(b []byte, v any) error { return yaml.Unmarshal(b, v) }

// Verdict for one tool call.
type Verdict int

const (
	Allow Verdict = iota
	AskHuman
)

// Call is the part of a pre_tool_call event the gate reads.
type Call struct {
	Tool    string
	Input   map[string]any
	Cwd     string
	Session string
}

// Tools that change things.
var writing = map[string]bool{"write_file": true, "patch": true, "terminal": true, "execute_code": true, "process_manage": true,
	"delegate_task": true, "cronjob_manage": true, "skill_manage": true, "memory": true}

// Tools that only read; anything not listed here or in writing counts as writing in Manual mode.
var readOnly = map[string]bool{"read_file": true, "search_files": true, "web_search": true, "web_extract": true, "browser_snapshot": true,
	"vision_analyze": true, "session_search": true, "skills_list": true, "skill_view": true, "todo": true, "clarify": true}

// Result explains a verdict.
type Result struct {
	Verdict Verdict `json:"-"`
	Ask     bool    `json:"ask"`
	Mode    string  `json:"mode"`
	Hits    []Hit   `json:"red_lines,omitempty"`
	Reason  string  `json:"reason,omitempty"`
}

// Decide applies red lines (every mode) and then the mode. p may be nil for calls
// outside any hpk project: red lines still apply there, with the Ask mode.
func Decide(p *project.Project, c Call, now time.Time) Result {
	mode, extra, delivery := Ask, []string(nil), ""
	if p != nil {
		mode = ForSession(p, c.Session, now)
		extra, delivery = projectExtras(p)
	}
	r := Result{Mode: mode, Hits: Check(c.Tool, c.Input, extra, delivery)}
	if len(r.Hits) > 0 {
		var parts []string
		for _, h := range r.Hits {
			parts = append(parts, h.Line+": "+h.Detail)
		}
		r.Verdict, r.Ask = AskHuman, true
		r.Reason = "Red line (" + mode + " mode cannot pass it) — " + strings.Join(parts, " · ")
		return r
	}
	switch mode {
	case Manual:
		// unknown tools and MCP tools may write too: Manual asks for them
		if writing[c.Tool] || !readOnly[c.Tool] {
			r.Verdict, r.Ask, r.Reason = AskHuman, true, "Manual mode: every write and command asks ("+c.Tool+")"
		}
	case Ask:
		if p != nil && (c.Tool == "write_file" || c.Tool == "patch") {
			target := firstString(c.Input, "path", "file_path", "file")
			// p.Dir is hpk's own record of the project (gates, audit): never a free write
			if target != "" && !inside(abs(c.Cwd, target), p.Root) {
				r.Verdict, r.Ask, r.Reason = AskHuman, true, "Ask mode: writing outside the project ("+target+")"
			}
		}
	}
	return r
}

// projectExtras reads gates.red_lines_extra and delivery from project.yaml.
func projectExtras(p *project.Project) ([]string, string) {
	raw, err := os.ReadFile(filepath.Join(p.Dir, "project.yaml"))
	if err != nil {
		return nil, ""
	}
	var f struct {
		Gates struct {
			Extra []string `yaml:"red_lines_extra"`
		} `yaml:"gates"`
		Delivery string `yaml:"delivery"`
	}
	_ = yaml.Unmarshal(raw, &f)
	return f.Gates.Extra, f.Delivery
}

func abs(cwd, p string) string {
	if filepath.IsAbs(p) || strings.HasPrefix(p, "~") || cwd == "" {
		return p
	}
	return filepath.Join(cwd, p)
}

func inside(p, root string) bool {
	if root == "" {
		return false
	}
	n := func(s string) string {
		s = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(s)), "/")
		if runtime.GOOS == "windows" {
			s = strings.ToLower(s)
		}
		return s
	}
	a, b := n(p), n(root)
	return a == b || strings.HasPrefix(a, b+"/")
}

// Summary is a one-line description for audit and the digest.
func Summary(c Call) string {
	if s := firstString(c.Input, "command", "code", "path", "file_path", "target"); s != "" {
		if r := []rune(s); len(r) > 140 {
			s = string(r[:139]) + "…"
		}
		return s
	}
	return fmt.Sprintf("%v", c.Tool)
}
