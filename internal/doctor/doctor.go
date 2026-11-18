// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package doctor checks the Hermes install, hpk's hooks and skills, and every
// project. It only reads. The CLI (hpk doctor) and the Console share it.
package doctor

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/board"
	"hpk.local/hpk/internal/gates"
	"hpk.local/hpk/internal/hermes"
	"hpk.local/hpk/internal/install"
	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

// Check is one line of the report.
type Check struct {
	Level  string `json:"level"` // ok | warn | fail | info
	Name   string `json:"name"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// Run checks everything hpk depends on (docs/DESIGN.md §15). Read-only.
// serveURL is probed for a running hpk serve; empty skips that check.
func Run(ctx context.Context, h, serveURL string) []Check {
	var cs []Check
	add := func(level, name, detail, fix string) { cs = append(cs, Check{level, name, detail, fix}) }

	info := hermes.Detect(ctx)
	if info.Error != "" {
		add("fail", "hermes", info.Error, "install Hermes Agent and make sure `hermes` is on PATH")
	} else {
		add("ok", "hermes", info.Version, "")
		if info.ACP != "" {
			add("ok", "hermes-acp", info.ACP, "")
		} else {
			add("warn", "hermes-acp", "not found: sessions cannot be started from the Console", "reinstall Hermes Agent with ACP support")
		}
	}
	if _, err := exec.LookPath("git"); err != nil {
		add("fail", "git", "not found: wikis, worktrees and the board need it", "install git")
	} else {
		add("ok", "git", "found", "")
	}

	paths := install.Resolve(h, "")
	pl, err := install.PlanInstall(paths)
	switch {
	case err != nil:
		add("fail", "config.yaml", err.Error(), "")
	case len(pl.Diff) == 0:
		add("ok", "hooks and skills", "registered in "+paths.Config, "")
	default:
		add("warn", "hooks and skills", "not registered (or out of date) in "+paths.Config, "hpk install")
	}
	if _, err := os.Stat(paths.Bin); err != nil {
		add("warn", "hpk binary", "no copy at "+paths.Bin+" (hooks point there)", "hpk install")
	}
	missing := 0
	fs.WalkDir(assets.Skills(), ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if _, err := os.Stat(filepath.Join(paths.SkillsDir, filepath.FromSlash(p))); err != nil {
				missing++
			}
		}
		return nil
	})
	if missing > 0 {
		add("warn", "skills", fmt.Sprintf("%d hpk skill files missing in %s", missing, paths.SkillsDir), "hpk install")
	} else {
		add("ok", "skills", paths.SkillsDir, "")
	}
	if allowed := hookConsent(h); allowed {
		add("ok", "hook consent", "Hermes has allowed hpk's hooks", "")
	} else {
		add("info", "hook consent", "Hermes will ask you once to allow each hpk hook the first time it runs", "run `hermes` in a terminal once, or set hooks_auto_accept: true")
	}

	// who the AI runs as: the person's account (warn) or its own (docs/isolation.md)
	for _, f := range isolate.Findings(h) {
		add(f.Level, f.Name, f.Detail, f.Fix)
	}

	list, _ := project.List(h)
	if len(list) == 0 {
		add("info", "projects", "none yet", "hpk project add <folder>")
	}
	for _, p := range list {
		var problems []string
		if !wiki.Open(p.WikiDir()).Exists() {
			problems = append(problems, "no wiki")
		}
		if db, err := board.Open(p); err != nil {
			problems = append(problems, "board.db: "+err.Error())
		} else {
			db.Close()
		}
		if _, err := os.Stat(p.Root); err != nil {
			problems = append(problems, "root folder missing")
		}
		if len(problems) == 0 {
			add("ok", "project "+p.Name, p.Mode()+" · "+gates.Current(p, time.Now()).Mode, "")
		} else {
			add("warn", "project "+p.Name, strings.Join(problems, "; "), "")
		}
		if p.Privacy.ModelPolicy == privacy.LocalOnly {
			if prof, r, err := privacy.CheckActiveProfile(h, p); err != nil {
				add("warn", "local only: "+p.Name, err.Error(), "")
			} else {
				add("ok", "local only: "+p.Name, "profile "+prof+" uses "+r.Where()+"; every session is checked when it calls a tool", "")
			}
		}
	}

	if serveURL != "" {
		c := http.Client{Timeout: time.Second}
		if res, err := c.Get(serveURL + "/api/health"); err == nil {
			res.Body.Close()
			add("ok", "hpk serve", "running on "+serveURL, "")
		} else {
			add("info", "hpk serve", "not running", "hpk serve")
		}
	}
	return cs
}

// Failed reports whether any check failed.
func Failed(cs []Check) bool {
	for _, x := range cs {
		if x.Level == "fail" {
			return true
		}
	}
	return false
}

// hookConsent reads Hermes' allowlist of approved hook commands (read-only).
func hookConsent(home string) bool {
	raw, err := os.ReadFile(filepath.Join(home, "shell-hooks-allowlist.json"))
	if err != nil {
		return false
	}
	s := strings.ReplaceAll(string(raw), `\\`, "/")
	for _, hk := range install.Hooks {
		if !strings.Contains(s, " hook "+hk.Name) {
			return false
		}
	}
	return true
}
