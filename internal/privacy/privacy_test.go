// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package privacy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

// All names, paths and people here are fictional sample data.
func setup(t *testing.T, mode string) (home string, p *project.Project) {
	t.Helper()
	home = t.TempDir()
	root := filepath.Join(home, "code", "sample-app")
	os.MkdirAll(root, 0o755)
	p, err := project.Create(home, "sample-app", root, mode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wiki.Open(p.WikiDir()).Init(p.Name, assets.WikiTemplates()); err != nil {
		t.Fatal(err)
	}
	return home, p
}

func call(tool string, input map[string]any) Call { return Call{Tool: tool, Input: input} }

func TestGuardMatrix(t *testing.T) {
	sharedFile := map[string]any{"path": `C:\Users\someone\AppData\Local\hermes\hpk-shared\team\lessons\x.md`}
	cases := []struct {
		mode, tool string
		in         map[string]any
		want       Verdict
	}{
		{project.Shared, "write_file", sharedFile, Allow},
		{project.Shared, "send_message", map[string]any{"message": "hi"}, Allow},
		{project.Private, "write_file", sharedFile, Block},
		{project.Private, "terminal", map[string]any{"command": "cp notes.md ~/.hermes/hpk-shared/team/"}, Block},
		{project.Private, "read_file", sharedFile, Allow},
		{project.Private, "search_files", map[string]any{"path": "../hpk-shared"}, Allow},
		{project.Private, "send_message", map[string]any{"target": "telegram", "message": "status"}, Block},
		{project.Private, "mcp__mail__send_email", nil, Block},
		{project.Private, "write_file", map[string]any{"path": "src/app.go"}, Allow},
		{project.Isolated, "read_file", sharedFile, Block},
		{project.Isolated, "react_to_message", nil, Block},
		{project.Isolated, "some_new_tool", map[string]any{"x": []any{"hpk-shared/team"}}, Block},
		{project.Isolated, "read_file", map[string]any{"path": "README.md"}, Allow},
	}
	for _, c := range cases {
		p := &project.Project{Name: "sample-app"}
		p.Privacy.Mode = c.mode
		if got, _ := Decide(p, call(c.tool, c.in)); got != c.want {
			t.Errorf("%s %s %v: got %v want %v", c.mode, c.tool, c.in, got, c.want)
		}
	}
	if v, _ := Decide(nil, call("send_message", nil)); v != Allow {
		t.Error("calls outside hpk projects are not governed")
	}
}

func TestResolveByCwdAndEnv(t *testing.T) {
	home, p := setup(t, project.Private)
	got, err := Resolve(home, filepath.Join(p.Root, "src", "billing"))
	if err != nil || got == nil || got.Name != "sample-app" {
		t.Fatalf("resolve by cwd: %v %v", got, err)
	}
	t.Setenv("HPK_PROJECT", "does-not-exist")
	if _, err := Resolve(home, ""); err == nil {
		t.Fatal("an unknown HPK_PROJECT must be an error so the guard fails closed")
	}
}

func TestOpeningUpNeedsConfirmation(t *testing.T) {
	home, _ := setup(t, project.Private)
	if _, err := project.SetPrivacy(home, "sample-app", project.Isolated, false); err != nil {
		t.Fatalf("closing further needs no confirmation: %v", err)
	}
	if _, err := project.SetPrivacy(home, "sample-app", project.Shared, false); err == nil {
		t.Fatal("opening up must need confirmation")
	}
	p, err := project.SetPrivacy(home, "sample-app", project.Shared, true)
	if err != nil || p.Mode() != project.Shared {
		t.Fatalf("confirmed change: %v %v", p, err)
	}
	reloaded, _ := project.Load(home, "sample-app")
	if reloaded.Mode() != project.Shared {
		t.Fatal("privacy change was not saved")
	}
	audit, _ := os.ReadFile(filepath.Join(reloaded.Dir, "audit.log"))
	if !strings.Contains(string(audit), "isolated → shared") {
		t.Fatalf("audit missing the change:\n%s", audit)
	}
}

func approvedPage(t *testing.T, p *project.Project, target, title, body string) {
	t.Helper()
	w := wiki.Open(p.WikiDir())
	d, err := w.CreateDraft(wiki.DraftInput{Target: target, Title: title, Sources: []string{"src/export/csv.go:31"}, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Approve([]int{d.N}, "reviewer"); err != nil {
		t.Fatal(err)
	}
}

func TestPromoteRedactsAndNeedsAPerson(t *testing.T) {
	home, p := setup(t, project.Shared)
	approvedPage(t, p, "glossary/northwind-trading.md", "Northwind Trading", "A fictional customer.")
	approvedPage(t, p, "lessons/csv-bom.md", "CSV needs a BOM in sample-app",
		"Northwind Trading opens exports in a spreadsheet app. See src/export/csv.go and C:\\work\\sample-app\\out.csv,\nhttps://intranet.example.test/ticket/7 or ask ops@example.test.")
	d, err := Promote(home, p, "lessons/csv-bom.md", "team", assets.WikiTemplates())
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"sample-app", "Northwind", "src/export", `C:\work`, "intranet.example", "ops@example"} {
		if strings.Contains(d.Body+d.Meta.Title, leak) {
			t.Errorf("promoted draft still contains %q:\n%s\n%s", leak, d.Meta.Title, d.Body)
		}
	}
	if d.Meta.Origin != Origin(p) || !d.Meta.Promoted || strings.Contains(d.Meta.Target, "sample-app") {
		t.Fatalf("origin/promoted/target: %+v", d.Meta)
	}
	shared := wiki.Open(filepath.Join(project.SharedDir(home), "team"))
	res, _ := shared.Approve([]int{d.N}, "auto")
	if res[0].Error == "" {
		t.Fatal("Auto must not approve a promotion")
	}
	res, _ = shared.Approve([]int{d.N}, "reviewer")
	if res[0].Done != "approved" {
		t.Fatalf("a person approves: %+v", res)
	}
	page, _ := os.ReadFile(filepath.Join(shared.Dir, filepath.FromSlash(d.Meta.Target)))
	if !strings.Contains(string(page), "origin: "+Origin(p)) {
		t.Fatalf("approved page must keep its origin:\n%s", page)
	}
}

func TestPrivateProjectsCannotPromote(t *testing.T) {
	for _, mode := range []string{project.Private, project.Isolated} {
		home, p := setup(t, mode)
		approvedPage(t, p, "lessons/x.md", "X", "Something learned.")
		if _, err := Promote(home, p, "lessons/x.md", "team", assets.WikiTemplates()); err == nil {
			t.Errorf("%s project promoted", mode)
		}
	}
}

func TestRedactPaths(t *testing.T) {
	got := Redact("see src/bank/client.go, /srv/app/run.sh and ~/notes/x.md", nil)
	if strings.Contains(got, "src") || strings.Contains(got, "srv") || strings.Contains(got, "notes") {
		t.Fatalf("paths left behind: %q", got)
	}
}
