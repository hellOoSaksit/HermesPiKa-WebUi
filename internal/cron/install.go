// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package cron

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/project"
)

// Plan is what installing a job does in Hermes.
type Plan struct {
	Job        string   `json:"job"`
	ScriptPath string   `json:"script_path"` // the monitor script, under <home>/scripts/
	ScriptBody string   `json:"script_body"`
	Args       []string `json:"args"` // hermes cron create …
	Deliver    string   `json:"deliver"`
}

// MakePlan builds the hermes cron create call for one job. The monitor script is a
// tiny Python file (Hermes runs non-.sh scripts with its own Python on every OS)
// that only calls `hpk cron prep`.
func MakePlan(home string, p *project.Project, j Job, prompt, hpkPath, delivery, model string) Plan {
	script := filepath.Join(home, "scripts", fmt.Sprintf("hpk-%s-%s.py", p.Name, j.Name))
	body := fmt.Sprintf(`# Written by hpk. 0 tokens: prints what changed for the %s job of %s.
# Unchanged output means Hermes skips the model run (monitor mode).
import subprocess, sys
r = subprocess.run([%q, "cron", "prep", %q, "--project", %q], capture_output=True)
sys.stdout.buffer.write(r.stdout)  # bytes as hpk wrote them (UTF-8), no code-page decoding
sys.exit(0)
`, j.Name, p.Name, hpkPath, j.Name, p.Name)
	// Private and isolated projects deliver only locally (§6.3).
	if p.Mode() != project.Shared || delivery == "" || delivery == "console" {
		delivery = "local"
	}
	args := []string{"cron", "create", "--name", "hpk " + p.Name + " " + j.Name, "--monitor-script", filepath.Base(script),
		"--workdir", p.Root, "--deliver", delivery, "--catch-up", "off"}
	for _, s := range j.Skills {
		args = append(args, "--skill", s)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, j.Schedule, prompt)
	return Plan{Job: j.Name, ScriptPath: script, ScriptBody: body, Args: args, Deliver: delivery}
}

var idRE = regexp.MustCompile(`(?i)\b(?:id|job)[:\s]+([a-z0-9_-]{6,})`)

// Install writes the monitor script and creates the Hermes cron job, then records
// its id so usage can be matched. Creating jobs changes Hermes, so the CLI only
// calls this after a person says yes.
func Install(home string, p *project.Project, pl Plan) (string, error) {
	if err := os.MkdirAll(filepath.Dir(pl.ScriptPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(pl.ScriptPath, []byte(pl.ScriptBody), 0o644); err != nil {
		return "", err
	}
	cmd := isolate.Command("hermes", pl.Args...)
	cmd.Env = append(os.Environ(), "HERMES_HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("hermes cron create: %v: %s", err, strings.TrimSpace(string(out)))
	}
	id := ""
	if m := idRE.FindStringSubmatch(string(out)); m != nil {
		id = m[1]
	}
	st, err := Load(p)
	if err != nil {
		return id, err
	}
	st.job(pl.Job).HermesID = id
	if err := Save(p, st); err != nil {
		return id, err
	}
	return id, project.Audit(p.Dir, "cron installed", pl.Job+" "+id)
}

// SetHermesID records a job id by hand (when Hermes' output could not be parsed).
func SetHermesID(p *project.Project, job, id string) error {
	st, err := Load(p)
	if err != nil {
		return err
	}
	st.job(job).HermesID = id
	return Save(p, st)
}

// Uninstall removes one job from Hermes cron (if hpk knows its id), forgets the id
// and deletes its monitor script. Removing changes Hermes, so the CLI asks first.
func Uninstall(home string, p *project.Project, job string) (string, error) {
	st, err := Load(p)
	if err != nil {
		return "", err
	}
	js := st.job(job)
	id := js.HermesID
	if id != "" {
		cmd := isolate.Command("hermes", "cron", "remove", id)
		cmd.Env = append(os.Environ(), "HERMES_HOME="+home)
		if out, err := cmd.CombinedOutput(); err != nil && !strings.Contains(strings.ToLower(string(out)), "not found") {
			return id, fmt.Errorf("hermes cron remove %s: %v: %s", id, err, strings.TrimSpace(string(out)))
		}
	}
	js.HermesID, js.LastOutput, js.LockedAt = "", "", time.Time{}
	_ = os.Remove(filepath.Join(home, "scripts", fmt.Sprintf("hpk-%s-%s.py", p.Name, job)))
	return id, Save(p, st)
}
