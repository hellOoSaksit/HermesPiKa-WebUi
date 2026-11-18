// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsagesAndSkills(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	if _, _, ok := SplitTool("terminal"); ok {
		t.Fatal("not an MCP tool")
	}
	s, tool, ok := SplitTool("mcp__roblox__run_code")
	if !ok || s != "roblox" || tool != "run_code" {
		t.Fatal("split")
	}
	Record(home, Use{At: now.Add(-48 * time.Hour), Server: "roblox", Tool: "run_code", Session: "s1", Project: "obby", Root: "/code/obby"})
	Record(home, Use{At: now.Add(-1 * time.Hour), Server: "roblox", Tool: "insert_model", Session: "s2", Project: "obby", Root: "/code/obby"})
	Record(home, Use{At: now, Server: "roblox", Tool: "run_code", Session: "s2", Project: "kart", Root: "/code/kart"})
	Record(home, Use{At: now, Server: "roblox", Tool: "run_code", Blocked: true})
	skills := filepath.Join(home, "skills", "level-builder")
	os.MkdirAll(skills, 0o755)
	os.WriteFile(filepath.Join(skills, "SKILL.md"), []byte("Use mcp__roblox__insert_model to place parts; never mcp__blender__render here.\n"), 0o644)

	u := Usages(home, []string{filepath.Join(home, "skills")}, now)
	r := u["roblox"]
	if r == nil || r.Calls != 3 || r.Blocked != 1 || len(r.Projects) != 2 || r.Projects[0].Name != "kart" || r.Projects[1].Calls != 2 {
		t.Fatalf("%+v", r)
	}
	if r.Tools[0].Name != "run_code" || r.Tools[0].Calls != 2 || len(r.Sessions) != 2 {
		t.Fatalf("tools/sessions %+v %+v", r.Tools, r.Sessions)
	}
	if r.Days[13] != 2 || r.Days[11] != 1 {
		t.Fatalf("days %v", r.Days)
	}
	if len(r.Skills) != 1 || r.Skills[0].Name != "level-builder" || len(u["blender"].Skills) != 1 {
		t.Fatalf("skills %+v", r.Skills)
	}
}
