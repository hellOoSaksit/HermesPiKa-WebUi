// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package gates decides when an agent must stop and ask a person
// (docs/DESIGN.md §11): red lines that hold in every mode, and the
// Manual / Ask / Auto modes for everything else. Decisions are audited.
package gates

import (
	"path"
	"regexp"
	"strings"
)

// Red line keys match the Console's i18n keys (redlines.<key>).
const (
	Push     = "push"     // push / tag / open or merge a PR into a main branch
	Delete   = "delete"   // delete files, folders, worktrees, branches, databases, wiki pages
	Secrets  = "secrets"  // read or show .env and other secret files
	Control  = "control"  // change files that steer agents
	Model    = "model"    // change model mid-chat / spend over budget
	WikiCore = "wikicore" // write decisions/ architecture/ people/ directly
	Promote  = "promote"  // write to the shared wiki / change privacy
	Egress   = "egress"   // send data off this machine outside the delivery channel
	Extra    = "extra"    // a project's own red line (gates.red_lines_extra)
)

// Hit is one red line a call crosses.
type Hit struct {
	Line   string `json:"line"`
	Detail string `json:"detail"` // the command part or path that crossed it
}

// Part is one command of a chained shell line.
type Part struct {
	Cmd string `json:"cmd"`
	Op  string `json:"op,omitempty"` // && || ; | that follows it
}

// SplitCommand splits on && || ; | outside quotes (same rules as the Console).
func SplitCommand(line string) []Part {
	var parts []Part
	var cur strings.Builder
	var q byte
	flush := func(op string) {
		if s := strings.TrimSpace(cur.String()); s != "" {
			parts = append(parts, Part{Cmd: s, Op: op})
		}
		cur.Reset()
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if q != 0 {
			cur.WriteByte(c)
			if c == q {
				q = 0
			}
			continue
		}
		switch {
		case c == '"' || c == '\'':
			q = c
			cur.WriteByte(c)
		case i+1 < len(line) && (line[i:i+2] == "&&" || line[i:i+2] == "||"):
			flush(line[i : i+2])
			i++
		case c == ';' || c == '|' || c == '\n':
			flush(string(c))
		default:
			cur.WriteByte(c)
		}
	}
	flush("")
	return parts
}

type rule struct {
	line string
	re   *regexp.Regexp
}

// Command rules. Ordered: the first that matches names the line.
var cmdRules = []rule{
	{Secrets, regexp.MustCompile(`(?i)(^|\s)(cat|type|less|more|head|tail|bat|get-content|gc|sed\s+-n|awk|grep|rg|findstr)\b[^|;&]*(\.env\b|\.pem\b|\.key\b|id_(rsa|ed25519|ecdsa)|credentials|secrets?/|\.npmrc|\.netrc|\.git-credentials)|^\s*(printenv|env|set|get-childitem\s+env:|gci\s+env:|dir\s+env:)\s*$|\$\{?[A-Z_]*(SECRET|TOKEN|PASSWORD|API_KEY)\b|\$env:[A-Z_]*(SECRET|TOKEN|PASSWORD|KEY)\b`)},
	{Push, regexp.MustCompile(`(?i)\bgit\s+(push|tag\s+-?[a-z]|merge)\b|\bgh\s+(pr\s+(create|merge)|release\s+create)\b|\bglab\s+mr\s+(create|merge)\b|\bnpm\s+publish\b|\bcargo\s+publish\b|\bdocker\s+push\b`)},
	{Delete, regexp.MustCompile(`(?i)(^|\s)(rm|rmdir|del|erase|rd|remove-item|ri|unlink|shred)\s|\bgit\s+(branch\s+-[dD]\b|worktree\s+remove|clean\s+-[a-z]*f|reset\s+--hard|stash\s+(drop|clear)|push\s+[^|;&]*--delete|rm\b)|\b(drop|truncate)\s+(table|database|schema)\b|\bdelete\s+from\b|\bdropdb\b|\bhermes\s+sessions\s+(delete|prune)|\bshutil\.rmtree\b|\bos\.(remove|unlink|rmdir|removedirs)\s*\(|\bfs\.(rm|rmdir|unlink)(sync)?\s*\(|\[(system\.)?io\.(directory|file)\]::delete`)},
	{Model, regexp.MustCompile(`(?i)\bhermes\s+(model\b|config\s+set\s+(model|provider)\b|auth\s+(priority|add|remove|reset)\b)`)},
	{Control, regexp.MustCompile(`(?i)(>|>>|tee\s|sed\s+-i|set-content|add-content|out-file|cp\s|copy\s|mv\s|move\s)[^|;&]*(agents\.md|claude\.md|soul\.md|gemini\.md|\.cursorrules|skill\.md|project\.yaml|schema\.md|config\.yaml|\.hermes[\\/])`)},
	{Promote, regexp.MustCompile(`(?i)hpk-shared|\bhpk\s+(wiki\s+promote|project\s+privacy)\b`)},
	{Egress, regexp.MustCompile(`(?i)\b(curl|wget|http|https|invoke-webrequest|iwr|invoke-restmethod|irm)\b[^|;&]*(\s-d\b|\s--data|\s-F\b|\s--form|\s-T\b|--upload-file|-X\s*(POST|PUT|PATCH)|-Method\s+(Post|Put|Patch)|-InFile|-Body|\s--json\b|\s-d\s*@|\s--data-\w+)|\b(curl|wget|invoke-webrequest|iwr|invoke-restmethod|irm)\b[^|;&]*https?://\S*\?\S*(\$|\x60)|\b(scp|sftp|rsync|nc|ncat|ftp)\s`)},
}

// Paths whose change steers agents (§11.2 #4).
var controlPath = regexp.MustCompile(`(?i)(^|/)(agents\.md|claude\.md|soul\.md|gemini\.md|\.cursorrules|copilot-instructions\.md|project\.yaml|schema\.md)$|(^|/)skills/.+/skill\.md$|(^|/)\.hermes/|(^|/)hermes/(config\.yaml|profiles/)|(^|/)hpk/(team\.yaml|devices\.json|teamserver\.json|vault/|browser/)|(^|/)hpk/projects/[^/]+/(gates\.json|decisions\.jsonl|audit\.log|project\.yaml)$`)

var secretPath = regexp.MustCompile(`(?i)(^|/)(\.env(\..*)?|.*\.(pem|key|p12|pfx)|id_(rsa|dsa|ecdsa|ed25519)|\.npmrc|\.netrc|\.git-credentials|credentials(\.json)?)$|(^|/)secrets?/`)

var wikiCorePath = regexp.MustCompile(`(?i)/wiki/(decisions|architecture|people)/[^/]+\.md$`)

var outboundTools = map[string]bool{"send_message": true, "discord": true, "discord_admin": true, "react_to_message": true}

// Check returns every red line a tool call crosses. extra are a project's own
// patterns ("re:<regexp>" entries in gates.red_lines_extra); delivery is the
// project's configured channel, which is not egress.
func Check(tool string, input map[string]any, extra []string, delivery string) []Hit {
	var hits []Hit
	add := func(line, detail string) {
		for _, h := range hits {
			if h.Line == line && h.Detail == detail {
				return
			}
		}
		hits = append(hits, Hit{Line: line, Detail: detail})
	}
	switch tool {
	case "terminal", "execute_code", "process_manage":
		cmd := firstString(input, "command", "code", "cmd", "script")
		for _, p := range SplitCommand(cmd) {
			forms := candidates(p.Cmd)
			for _, r := range cmdRules {
				for _, f := range forms {
					if r.re.MatchString(f) {
						add(r.line, p.Cmd)
						break
					}
				}
			}
			for _, x := range extraRules(extra) {
				for _, f := range forms {
					if x.MatchString(f) {
						add(Extra, p.Cmd)
						break
					}
				}
			}
		}
	case "write_file", "patch":
		p := normPath(firstString(input, "path", "file_path", "file"))
		switch {
		case strings.Contains(strings.ToLower(p), "hpk-shared"): // before control: it sits under .hermes/
			add(Promote, p)
		case controlPath.MatchString(p):
			add(Control, p)
		case wikiCorePath.MatchString(p):
			add(WikiCore, p)
		case secretPath.MatchString(p):
			add(Secrets, p)
		}
	case "read_file", "search_files":
		if p := normPath(firstString(input, "path", "file_path", "file")); secretPath.MatchString(p) {
			add(Secrets, p)
		}
	case "skill_manage":
		add(Control, "skill_manage")
	default:
		if strings.HasPrefix(tool, "mcp__") {
			checkMCP(tool, input, add)
		}
		if outboundTools[tool] || (strings.HasPrefix(tool, "mcp__") && regexp.MustCompile(`(?i)send|post|email|mail|message|slack|telegram|discord|webhook|publish`).MatchString(tool)) {
			target := firstString(input, "target", "channel", "to", "platform")
			if delivery == "" || delivery == "console" || !strings.EqualFold(target, delivery) {
				add(Egress, tool+" "+target)
			}
		}
	}
	return hits
}

func extraRules(extra []string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, e := range extra {
		if pat, ok := strings.CutPrefix(strings.TrimSpace(e), "re:"); ok {
			if re, err := regexp.Compile(strings.TrimSpace(pat)); err == nil {
				out = append(out, re)
			}
		}
	}
	return out
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func slash(p string) string { return path.Clean(strings.ReplaceAll(p, `\`, "/")) }

var (
	mcpPushRE   = regexp.MustCompile(`(?i)push|merge|release|publish|deploy`)
	mcpDeleteRE = regexp.MustCompile(`(?i)delete|remove|drop|destroy|purge|truncate`)
)

// checkMCP applies the path and action red lines to an MCP tool: its name
// (mcp__<server>__<tool>) says what it does, its string inputs name files.
func checkMCP(tool string, input map[string]any, add func(line, detail string)) {
	name := tool[strings.LastIndex(tool, "__")+2:]
	if mcpPushRE.MatchString(name) {
		add(Push, tool)
	}
	if mcpDeleteRE.MatchString(name) {
		add(Delete, tool)
	}
	for _, s := range inputStrings(input) {
		if len(s) > 400 || strings.ContainsRune(s, '\n') {
			continue
		}
		p := normPath(s)
		switch {
		case controlPath.MatchString(p):
			add(Control, tool+" "+s)
		case secretPath.MatchString(p):
			add(Secrets, tool+" "+s)
		}
	}
}

// inputStrings collects the string values of a tool input (all of them).
func inputStrings(in map[string]any) []string {
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case []any:
			for _, y := range x {
				walk(y)
			}
		case map[string]any:
			for _, y := range x {
				walk(y)
			}
		}
	}
	walk(in)
	return out
}
