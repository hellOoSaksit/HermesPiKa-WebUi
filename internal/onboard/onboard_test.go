// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package onboard

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/scan"
)

// A fictional sample repository.
func sampleRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "sample-app")
	files := map[string]string{
		"README.md":                "# Sample app\n\nSee [setup](docs/setup.md) and [old](docs/gone.md). Code lives in `src/main.go` and `src/missing.go`.\n",
		"docs/setup.md":            "# Setup\n\nRun the tests with go test.\n",
		"docs/adr/0001-storage.md": "# Use SQLite\n\nBecause it is one file.\n",
		"notes/2026-11-01.md":      "Meeting notes.\n",
		"AGENTS.md":                "- Always run the tests before you push any change.\n",
		"CLAUDE.md":                "- Always run the tests before you push any change.\n",
		"src/main.go":              "package main\n\nfunc main() {}\n",
		"tools/gen.sh":             "echo hi\n",
		".env":                     "API_KEY=do-not-read\n",
		"config/secrets.json":      "{}\n",
		"deploy/id_rsa":            "not a real key\n",
		"go.mod":                   "module sample\n\ngo 1.27\n",
		"node_modules/x/README.md": "should be ignored\n",
	}
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestScanNeverOpensSecrets(t *testing.T) {
	r, err := scan.Run(sampleRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	skipped := map[string]bool{}
	for _, s := range r.Skipped {
		skipped[s.Path] = true
	}
	for _, p := range []string{".env", "config/secrets.json", "deploy/id_rsa"} {
		if !skipped[p] {
			t.Errorf("%s should be skipped as a secret", p)
		}
	}
	for _, s := range r.Sources {
		if strings.HasPrefix(s.Path, "node_modules/") || s.Path == ".env" {
			t.Errorf("%s must not be a source", s.Path)
		}
	}
	if r.TestCmd != "go test ./..." {
		t.Errorf("test command: %q", r.TestCmd)
	}
}

func TestPickDepthsAndExclude(t *testing.T) {
	r, _ := scan.Run(sampleRepo(t))
	light, _ := Pick(r, Light, nil)
	for _, s := range light {
		if !(s.Kind == "readme" || s.Kind == "docs") {
			t.Errorf("light picked %s (%s)", s.Path, s.Kind)
		}
	}
	full, _ := Pick(r, Full, nil)
	for _, s := range full {
		if s.Kind == "control" {
			t.Errorf("agent instruction file %s must never be ingested", s.Path)
		}
	}
	cut, excluded := Pick(r, Full, []string{"notes/**", "docs/adr/"})
	if len(cut) != len(full)-2 || len(excluded) != 2 {
		t.Errorf("exclude: kept %d of %d, excluded %v", len(cut), len(full), excluded)
	}
	if skip, _ := Pick(r, Skip, nil); len(skip) != 0 {
		t.Error("skip reads nothing")
	}
}

func TestAddQueueAndBudget(t *testing.T) {
	SetTemplates(assets.WikiTemplates())
	home := t.TempDir()
	root := sampleRepo(t)
	if _, err := exec.LookPath("git"); err == nil {
		exec.Command("git", "-C", root, "init", "-q").Run()
		exec.Command("git", "-C", root, "-c", "user.name=t", "-c", "user.email=t@t", "add", ".").Run()
		exec.Command("git", "-C", root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init").Run()
	}
	r, _ := scan.Run(root)

	if _, _, err := Add(home, Options{Root: r.Root, Name: "sample-app", Depth: Full, Privacy: project.Private, Budget: 5}, r); err == nil {
		t.Fatal("a plan over budget must be refused")
	}
	p, est, err := Add(home, Options{Root: r.Root, Name: "sample-app", Depth: Full, Privacy: project.Private}, r)
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode() != project.Private || len(est.Sources) == 0 {
		t.Fatalf("project %+v, %d sources", p, len(est.Sources))
	}
	raw, _ := os.ReadFile(filepath.Join(p.Dir, "project.yaml"))
	for _, want := range []string{"mode: private", "test: go test ./...", "depth: full", "delivery: console"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("project.yaml missing %q:\n%s", want, raw)
		}
	}

	// Walk the queue.
	seen := 0
	for {
		it, _, err := Next(p)
		if err != nil {
			t.Fatal(err)
		}
		if it == nil {
			break
		}
		if _, err := Done(p, it.Path, 0, false); err != nil {
			t.Fatal(err)
		}
		seen++
	}
	st, _ := LoadState(p)
	if seen != len(est.Sources) || st.Step != "organise" {
		t.Fatalf("walked %d of %d, step %s", seen, len(est.Sources), st.Step)
	}

	// Budget stop: shrink the budget and re-open one item.
	st.Budget, st.UsedTokens = 1, 1
	st.Queue[0].Status = "pending"
	SaveState(p, st)
	if _, _, err := Next(p); !errors.Is(err, ErrBudget) {
		t.Fatalf("want ErrBudget, got %v", err)
	}

	// Change detection.
	os.WriteFile(filepath.Join(root, "docs", "setup.md"), []byte("# Setup\n\nChanged.\n"), 0o644)
	ch, _ := Changed(p)
	if len(ch) != 1 || ch[0] != "docs/setup.md" {
		t.Fatalf("changed: %v", ch)
	}
}

func TestHealth(t *testing.T) {
	r, _ := scan.Run(sampleRepo(t))
	kinds := map[string]int{}
	for _, f := range Health(r) {
		kinds[f.Kind]++
	}
	for _, k := range []string{"broken-link", "missing-file", "duplicate-rule", "undocumented"} {
		if kinds[k] == 0 {
			t.Errorf("health missed %s: %v", k, kinds)
		}
	}
	if kinds["no-readme"] != 0 {
		t.Error("README exists")
	}
}

func TestWriteRawNotes(t *testing.T) {
	SetTemplates(assets.WikiTemplates())
	home := t.TempDir()
	r, _ := scan.Run(sampleRepo(t))
	p, _, err := Add(home, Options{Root: r.Root, Name: "sample-app", Depth: Full, Privacy: project.Private}, r)
	if err != nil {
		t.Fatal(err)
	}
	it, _, _ := Next(p)
	if _, err := WriteRaw(p, "not/queued.md", []byte("- x")); err == nil {
		t.Fatal("a path outside the queue must be refused")
	}
	if _, err := WriteRaw(p, it.Path, []byte("- key AKIAIOSFODNN7EXAMPLE (a:1)")); err == nil {
		t.Fatal("secrets must be refused")
	}
	dst, err := WriteRaw(p, it.Path, []byte("- Notes are stored in SQLite (README.md:3)\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(dst)
	if !strings.HasPrefix(string(raw), "---\ntitle: "+it.Path) || !strings.Contains(string(raw), "SQLite") || !strings.HasPrefix(dst, p.WikiDir()) {
		t.Fatalf("raw page %s:\n%s", dst, raw)
	}
}
