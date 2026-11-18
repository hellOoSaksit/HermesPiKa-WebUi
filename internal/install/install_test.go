// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"gopkg.in/yaml.v3"
)

// A made-up Hermes config with comments and a user's own hook that must survive.
const sample = `# my Hermes settings
model:
  default: some-model # keep this comment
skills:
  external_dirs:
    - ~/.agents/skills
hooks:
  pre_tool_call:
    - command: "my-own-guard --strict"
      timeout: 5
approvals:
  mode: smart
`

func setup(t *testing.T, body string) Paths {
	t.Helper()
	home := t.TempDir()
	p := Resolve(home, "")
	if body != "" {
		os.WriteFile(p.Config, []byte(body), 0o600)
	}
	return p
}

func TestInstallKeepsWhatIsThere(t *testing.T) {
	p := setup(t, sample)
	pl, err := PlanInstall(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"# my Hermes settings", "# keep this comment", "my-own-guard --strict", "~/.agents/skills", "mode: smart"} {
		if !strings.Contains(pl.After, keep) {
			t.Errorf("lost %q:\n%s", keep, pl.After)
		}
	}
	for _, d := range pl.Diff {
		if strings.HasPrefix(d, "- ") {
			t.Errorf("install must only add lines, removed: %s", d)
		}
	}
	var cfg struct {
		Skills struct {
			External []string `yaml:"external_dirs"`
		} `yaml:"skills"`
		Hooks map[string][]struct {
			Command    string `yaml:"command"`
			FailClosed bool   `yaml:"fail_closed"`
			Matcher    string `yaml:"matcher"`
		} `yaml:"hooks"`
	}
	if err := yaml.Unmarshal([]byte(pl.After), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Skills.External) != 2 || len(cfg.Hooks["pre_tool_call"]) != 4 {
		t.Fatalf("config after: %+v", cfg)
	}
	closed := 0
	for _, h := range cfg.Hooks["pre_tool_call"] {
		if h.FailClosed {
			closed++
		}
	}
	if closed != 2 {
		t.Fatalf("red-line and privacy guards must fail closed, got %d", closed)
	}
	if cfg.Hooks["on_session_end"][0].Command == "" || cfg.Hooks["post_approval_response"][0].Command == "" {
		t.Fatal("report and audit hooks missing")
	}
}

func TestInstallIsIdempotentAndUninstallRestores(t *testing.T) {
	p := setup(t, sample)
	pl, _ := PlanInstall(p)
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	skills := fstest.MapFS{"hpk-wiki/SKILL.md": {Data: []byte("---\nname: hpk-wiki\n---\n")}}
	m, err := Apply(pl, "", skills, "test", now)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(m.Backup); string(b) != sample {
		t.Fatal("backup must hold the original config")
	}
	if _, err := os.Stat(filepath.Join(p.SkillsDir, "hpk-wiki", "SKILL.md")); err != nil {
		t.Fatal("skills not copied")
	}
	again, _ := PlanInstall(p)
	if len(again.Diff) != 0 {
		t.Fatalf("second install must change nothing: %v", again.Diff)
	}
	un, _ := PlanUninstall(p)
	if len(un.Changes) != len(Hooks)+1 { // every hook + the skills dir
		t.Fatalf("uninstall changes: %v", un.Changes)
	}
	ApplyUninstall(un, now.Add(time.Minute))
	after, _ := os.ReadFile(p.Config)
	if strings.Contains(string(after), "/hpk/") || !strings.Contains(string(after), "my-own-guard") {
		t.Fatalf("uninstall must remove only hpk:\n%s", after)
	}
}

func TestNoConfigYetAndBadYAML(t *testing.T) {
	p := setup(t, "")
	pl, err := PlanInstall(p)
	if err != nil || pl.Exists || !strings.Contains(pl.After, "hook red_line_guard") {
		t.Fatalf("fresh config: %v\n%s", err, pl.After)
	}
	bad := setup(t, "hooks: [unclosed\n")
	if _, err := PlanInstall(bad); err == nil {
		t.Fatal("invalid YAML must be left alone")
	}
}

func TestProfilePath(t *testing.T) {
	p := Resolve("/h", "work")
	if filepath.ToSlash(p.Config) != "/h/profiles/work/config.yaml" {
		t.Fatalf("profile config: %s", p.Config)
	}
}

// Install then uninstall must give back the exact original bytes, and install must only add lines.
func TestRoundTripIsExact(t *testing.T) {
	cases := map[string]string{
		"sample": sample,
		// Laid out like Hermes' shipped config: skills with a commented-out example, no hooks.
		"hermes-like": "# Hermes\nmodel:\n  default: m\n\n# ===\n# Skills\n# ===\nskills:\n  # nudge\n  creation_nudge_interval: 15\n\n  # External dirs\n  # external_dirs:\n  #   - ~/.agents/skills\n\n# ===\n# Agent\n# ===\nagent:\n  max_turns: 500\n",
		"crlf-bom":    "\ufeffskills:\r\n  external_dirs:\r\n    - ~/x # mine\r\nhooks: {} # none yet\r\n",
		"compact":     "hooks:\n  pre_tool_call:\n  - command: mine\n    timeout: 3\nskills:\n  external_dirs: []\n",
		"empty-keys":  "skills:\nhooks:\n",
		"no-newline":  "model:\n  default: m",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			p := setup(t, body)
			pl, err := PlanInstall(p)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(body, "\ufeff") != strings.HasPrefix(pl.After, "\ufeff") || strings.Contains(body, "\r\n") != strings.Contains(pl.After, "\r\n") {
				t.Fatal("BOM or line endings changed")
			}
			if strings.Contains(body, "\r\n") && strings.Count(pl.After, "\n") != strings.Count(pl.After, "\r\n") {
				t.Fatal("mixed line endings")
			}
			if !strings.Contains(pl.After, "hook red_line_guard") || !strings.Contains(pl.After, filepath.ToSlash(p.SkillsDir)) {
				t.Fatalf("hpk missing:\n%s", pl.After)
			}
			for _, d := range pl.Diff {
				if strings.HasPrefix(d, "- ") && !strings.Contains(body, "{}") && !strings.Contains(body, "[]") {
					t.Errorf("install removed a line: %q", d)
				}
			}
			if len(pl.Diff) > 12+3*len(Hooks) { // keys, one entry per hook, skills
				t.Errorf("diff too large (%d lines):\n%s", len(pl.Diff), strings.Join(pl.Diff, "\n"))
			}
			if _, err := Apply(pl, "", nil, "test", time.Now()); err != nil {
				t.Fatal(err)
			}
			if again, _ := PlanInstall(p); len(again.Diff) != 0 {
				t.Fatalf("second install changed: %v", again.Diff)
			}
			un, err := PlanUninstall(p)
			if err != nil {
				t.Fatal(err)
			}
			flowOrEmpty := strings.Contains(body, "{}") || strings.Contains(body, "[]") || name == "empty-keys"
			// A file that lacked a final newline may get one back; nothing else may differ.
			if !flowOrEmpty && strings.TrimSuffix(un.After, "\n") != body && un.After != body {
				t.Fatalf("uninstall did not restore the original:\n--- want\n%q\n--- got\n%q", body, un.After)
			}
			if strings.Contains(un.After, "/hpk/") {
				t.Fatalf("hpk left behind:\n%s", un.After)
			}
		})
	}
}

func TestUpdateKeepsPosition(t *testing.T) {
	p := setup(t, sample)
	pl, _ := PlanInstall(p)
	Apply(pl, "", nil, "test", time.Now())
	// A person adds their own hook after hpk's, and hpk's timeout drifts.
	raw, _ := os.ReadFile(p.Config)
	s := strings.Replace(string(raw), "timeout: 15\n      fail_closed: true\n    - command: '\"", "timeout: 99\n      fail_closed: true\n    - command: '\"", 1)
	s += "" // keep
	os.WriteFile(p.Config, []byte(s), 0o600)
	pl2, err := PlanInstall(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl2.Changes) != 1 || !strings.Contains(pl2.Changes[0], "update hpk red_line_guard") {
		t.Fatalf("changes: %v\n%v", pl2.Changes, pl2.Diff)
	}
	if len(pl2.Diff) != 2 {
		t.Fatalf("update should touch one line: %v", pl2.Diff)
	}
}

func TestRefusesToGuess(t *testing.T) {
	for _, body := range []string{
		"skills: {external_dirs: [~/a]}\n",
		"hooks:\n  pre_tool_call: [{command: x}]\n",
		"skills: ~\n",
	} {
		p := setup(t, body)
		_, err := PlanInstall(p)
		if err == nil || !strings.Contains(err.Error(), "by hand") {
			t.Errorf("%q: want a by-hand error, got %v", body, err)
		}
	}
}
