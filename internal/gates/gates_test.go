// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package gates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hpk.local/hpk/internal/project"
)

func lines(hits []Hit) string {
	var s []string
	for _, h := range hits {
		s = append(s, h.Line)
	}
	return strings.Join(s, ",")
}

func TestCommandRedLines(t *testing.T) {
	cases := map[string]string{
		"git push origin feat/x":                       Push,
		"git tag -a v1.0 -m x":                         Push,
		"gh pr merge 12 --squash":                      Push,
		"rm -rf dist":                                  Delete,
		"Remove-Item -Recurse build":                   Delete,
		"git worktree remove worktrees/a":              Delete,
		"git branch -D old":                            Delete,
		"psql -c 'DROP TABLE users'":                   Delete,
		"cat .env":                                     Secrets,
		"printenv":                                     Secrets,
		"echo $GITHUB_TOKEN":                           Secrets,
		"echo hi > AGENTS.md":                          Control,
		"sed -i 's/a/b/' CLAUDE.md":                    Control,
		"hermes model set opus":                        Model,
		"curl -X POST https://example.test/hook -d @x": Egress,
		"scp out.tar host:/tmp":                        Egress,
		"cp notes.md ~/.hermes/hpk-shared/team/":       Promote,
	}
	for cmd, want := range cases {
		got := lines(Check("terminal", map[string]any{"command": cmd}, nil, ""))
		if !strings.Contains(got, want) {
			t.Errorf("%q: got %q, want %s", cmd, got, want)
		}
	}
	for _, safe := range []string{"go test ./...", "git status", "git commit -m 'fix'", "ls -la", "curl https://example.test", "grep -r TODO src", "cat README.md"} {
		if got := Check("terminal", map[string]any{"command": safe}, nil, ""); len(got) != 0 {
			t.Errorf("%q should be free, got %v", safe, got)
		}
	}
}

func TestChainedCommandNamesEachPart(t *testing.T) {
	hits := Check("terminal", map[string]any{"command": "pnpm build && rm -rf dist/legacy && git push origin feat/x"}, nil, "")
	if lines(hits) != "delete,push" {
		t.Fatalf("got %v", hits)
	}
	if len(SplitCommand(`echo "a && b"; ls`)) != 2 {
		t.Fatal("quotes must not split")
	}
}

func TestPathAndToolRedLines(t *testing.T) {
	cases := []struct {
		tool, path, want string
	}{
		{"write_file", "AGENTS.md", Control},
		{"patch", "skills/hpk-wiki/SKILL.md", Control},
		{"write_file", `C:\Users\x\AppData\Local\hermes\hpk\projects\app\wiki\decisions\db.md`, WikiCore},
		{"write_file", "/home/x/.hermes/hpk-shared/team/lessons/a.md", Promote},
		{"read_file", "config/.env.production", Secrets},
		{"write_file", "src/main.go", ""},
		{"read_file", "README.md", ""},
	}
	for _, c := range cases {
		got := lines(Check(c.tool, map[string]any{"path": c.path}, nil, ""))
		if got != c.want {
			t.Errorf("%s %s: got %q want %q", c.tool, c.path, got, c.want)
		}
	}
	if lines(Check("send_message", map[string]any{"target": "telegram"}, nil, "console")) != Egress {
		t.Error("send_message outside delivery is egress")
	}
	if len(Check("send_message", map[string]any{"target": "telegram"}, nil, "telegram")) != 0 {
		t.Error("sending to the configured delivery channel is allowed")
	}
	if lines(Check("terminal", map[string]any{"command": "make release"}, []string{"re:make\\s+release", "plain words are ignored"}, "")) != Extra {
		t.Error("project extra pattern")
	}
}

func newProject(t *testing.T) *project.Project {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, "code", "sample-app")
	os.MkdirAll(root, 0o755)
	p, err := project.Create(home, "sample-app", root, project.Shared)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestModes(t *testing.T) {
	p := newProject(t)
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	write := Call{Tool: "write_file", Input: map[string]any{"path": filepath.Join(p.Root, "src", "a.go")}}
	outside := Call{Tool: "write_file", Input: map[string]any{"path": filepath.Join(filepath.Dir(p.Root), "other", "a.go")}}
	push := Call{Tool: "terminal", Input: map[string]any{"command": "git push"}}

	if r := Decide(p, write, now); r.Ask || r.Mode != Ask {
		t.Fatalf("default Ask lets work inside the project through: %+v", r)
	}
	if r := Decide(p, outside, now); !r.Ask {
		t.Fatal("Ask asks for writes outside the project")
	}
	SetMode(p, Manual, 0, "", "tester", now)
	if r := Decide(p, write, now); !r.Ask {
		t.Fatal("Manual asks for every write")
	}
	if r := Decide(p, Call{Tool: "read_file", Input: map[string]any{"path": "README.md"}}, now); r.Ask {
		t.Fatal("Manual still reads freely")
	}
	SetMode(p, Auto, 2*time.Hour, "", "tester", now)
	if r := Decide(p, outside, now); r.Ask {
		t.Fatal("Auto lets ordinary writes through")
	}
	if r := Decide(p, push, now); !r.Ask {
		t.Fatal("Auto never passes a red line")
	}
	if r := Decide(p, outside, now.Add(3*time.Hour)); r.Mode != Ask || !r.Ask {
		t.Fatalf("Auto must fall back to Ask when it expires: %+v", r)
	}
	SetMode(p, Auto, 0, "s-1", "tester", now)
	if ForSession(p, "s-2", now) != Ask || ForSession(p, "s-1", now) != Auto {
		t.Fatal("Auto scoped to one session")
	}
}

func TestAuditAndDigest(t *testing.T) {
	p := newProject(t)
	now := time.Now().UTC()
	Record(p, Decision{At: now, Kind: "auto-allow", Tool: "terminal", Detail: "go test ./...", By: "auto"})
	Record(p, Decision{At: now, Kind: "ask", Lines: []string{Push}, Tool: "terminal", Detail: "git push", By: "hpk"})
	d, err := Digest(p, now.Add(-time.Hour))
	if err != nil || len(d) != 1 || d[0].Detail != "go test ./..." {
		t.Fatalf("digest: %+v %v", d, err)
	}
	all, _ := Decisions(p, now.Add(-time.Hour))
	if len(all) != 2 {
		t.Fatalf("decisions: %d", len(all))
	}
	audit, _ := os.ReadFile(filepath.Join(p.Dir, "audit.log"))
	if !strings.Contains(string(audit), "[auto] auto-allow") || !strings.Contains(string(audit), "[hpk] ask") {
		t.Fatalf("audit.log:\n%s", audit)
	}
}
