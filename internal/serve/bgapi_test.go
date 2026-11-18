// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"testing"
	"time"

	"hpk.local/hpk/internal/mcp"
	"hpk.local/hpk/internal/procs"
	"hpk.local/hpk/internal/team"
)

func TestMCPMatchingNeedsArgs(t *testing.T) {
	node := mcp.Server{Name: "x", Kind: "local", Command: "node"}
	if mcpMatches(node, "node server.js") {
		t.Error("a bare program name never matches: it would catch every node process")
	}
	blender := mcp.Server{Name: "blender", Kind: "local", Command: "uvx", Args: []string{"blender-mcp"}}
	if !mcpMatches(blender, `C:\Users\u\.local\bin\blender-mcp.exe`) || mcpMatches(blender, "python other.py") {
		t.Error("args decide")
	}
	roblox := mcp.Server{Name: "roblox", Kind: "local", Command: `C:\tools\rbx-studio-mcp.exe`, Args: []string{"--stdio"}}
	if !mcpMatches(roblox, `C:\tools\rbx-studio-mcp.exe --stdio`) || mcpMatches(roblox, `other.exe --stdio`) {
		t.Error("program name and args together")
	}
}

func TestReusedProcessIDIsNotTheJob(t *testing.T) {
	started := time.Now().Add(-time.Hour)
	p := procs.Proc{PID: 42, At: started.Add(500 * time.Millisecond), Start: "s1"}
	if !sameProcess(p, float64(started.UnixNano())/1e9) {
		t.Error("same start time: the job")
	}
	p.At = time.Now()
	if sameProcess(p, float64(started.UnixNano())/1e9) {
		t.Error("an hour later: another program with the same id")
	}
	a, b := jobID("mcp:x:42", procs.Proc{Start: "s1"}), jobID("mcp:x:42", procs.Proc{Start: "s2"})
	if a == b {
		t.Error("the id names the start time")
	}
}

func TestTeamMembersSeeLessOfBackgroundJobs(t *testing.T) {
	jobs := []bgJob{{ID: "a", Kind: "process", Project: "shop", Cmd: "npm run dev -- --token=abc", Cwd: "/code/shop"},
		{ID: "b", Kind: "process", Project: "secret-proj", Cmd: "x"}, {ID: "c", Kind: "mcp", Cmd: "y"}}
	member := identity{ID: "m", Role: team.Member, Team: true, Projects: []string{"shop"}}
	got := redactJobs(member, jobs)
	if len(got) != 1 || got[0].ID != "a" || got[0].Cmd != "" || got[0].Cwd != "" {
		t.Fatalf("member: %+v", got)
	}
	owner := identity{ID: "o", Role: team.Owner, Team: true}
	if len(redactJobs(owner, jobs)) != 3 {
		t.Fatal("owners see all")
	}
	u := map[string]*mcp.Usage{"x": {Projects: []mcp.Count{{Name: "shop", Root: "/code/shop"}, {Name: "secret-proj"}}, Sessions: []mcp.Count{{Name: "s"}}}}
	r := redactUsage(member, u)["x"]
	if len(r.Projects) != 1 || r.Projects[0].Root != "" || len(r.Sessions) != 0 {
		t.Fatalf("usage: %+v", r)
	}
}
