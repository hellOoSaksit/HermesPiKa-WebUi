// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package privacy enforces the project privacy modes (docs/DESIGN.md §6) at the
// moment an agent calls a tool, and promotes pages to the shared wiki (§6.2).
package privacy

import (
	"fmt"
	"os"
	"strings"

	"hpk.local/hpk/internal/project"
)

// Verdict mirrors the hook verdicts without importing the hook package.
type Verdict int

const (
	Allow Verdict = iota
	Block
)

// Call is the part of a Hermes pre_tool_call event the guard reads.
type Call struct {
	Tool  string
	Input map[string]any
	Cwd   string
}

// Tools that write files or run arbitrary code (they could write anywhere).
var writeTools = map[string]bool{"write_file": true, "patch": true, "terminal": true, "execute_code": true, "process_manage": true}

// Tools that read files.
var readTools = map[string]bool{"read_file": true, "search_files": true}

// Tools that send content off this machine (verified tool names, Hermes v0.23.1).
var outbound = map[string]bool{"send_message": true, "discord": true, "discord_admin": true, "react_to_message": true}

var outboundWords = []string{"send", "post", "email", "mail", "message", "slack", "telegram", "discord", "line_", "webhook", "tweet", "publish"}

// The shared wiki lives in a folder of this name; matching the name catches
// absolute, relative and ~ paths alike.
const sharedMarker = "hpk-shared"

// Decide returns the verdict for one tool call by the agent working on project p.
// A nil project means the call is not in a project hpk governs.
func Decide(p *project.Project, c Call) (Verdict, string) {
	if p == nil || p.Mode() == project.Shared {
		return Allow, ""
	}
	mode := p.Mode()
	touchesShared := mentions(c.Input, sharedMarker)

	if isOutbound(c.Tool) {
		return Block, fmt.Sprintf("%s is a %s project: its content stays on this machine (tool %s sends it out). Use HPK Console instead", p.Name, mode, c.Tool)
	}
	if touchesShared {
		switch {
		case writeTools[c.Tool]:
			return Block, fmt.Sprintf("%s is a %s project: it never writes to the shared wiki", p.Name, mode)
		case mode == project.Isolated:
			return Block, fmt.Sprintf("%s is isolated: it neither reads nor writes the shared wiki", p.Name)
		case readTools[c.Tool]:
			return Allow, "" // private projects may read shared knowledge
		default:
			// An unknown tool pointing at the shared wiki: refuse rather than guess.
			return Block, fmt.Sprintf("%s is a %s project: tool %s may not touch the shared wiki", p.Name, mode, c.Tool)
		}
	}
	return Allow, ""
}

func isOutbound(tool string) bool {
	if outbound[tool] {
		return true
	}
	if strings.HasPrefix(tool, "mcp__") {
		t := strings.ToLower(tool)
		for _, w := range outboundWords {
			if strings.Contains(t, w) {
				return true
			}
		}
	}
	return false
}

// mentions reports whether any string inside v contains marker (case-insensitive).
func mentions(v any, marker string) bool {
	switch x := v.(type) {
	case string:
		return strings.Contains(strings.ToLower(strings.ReplaceAll(x, `\`, "/")), marker)
	case map[string]any:
		for _, e := range x {
			if mentions(e, marker) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if mentions(e, marker) {
				return true
			}
		}
	}
	return false
}

// Resolve finds the project for a call: $HPK_PROJECT (set by hpk when it starts a
// session), else the project hpk linked the Hermes session to (sessions may run
// inside a gateway, whose cwd and environment are not the project's), else the
// project whose root contains the working directory.
// A project that is named but cannot be loaded is an error, so the guard fails closed.
func Resolve(home, cwd string) (*project.Project, error) { return ResolveSession(home, cwd, "") }

// ResolveSession is Resolve with the Hermes session id of the event.
func ResolveSession(home, cwd, session string) (*project.Project, error) {
	if name := os.Getenv("HPK_PROJECT"); name != "" {
		return project.Load(home, name)
	}
	if name := project.BySession(home, session); name != "" {
		return project.Load(home, name)
	}
	if cwd == "" {
		return nil, nil
	}
	return project.FindByDir(home, cwd)
}
