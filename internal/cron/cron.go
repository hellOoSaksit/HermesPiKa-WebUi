// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package cron runs hpk's scheduled jobs through Hermes cron (docs/DESIGN.md §12)
// without wasting tokens: each job has a 0-token "prep" step that Hermes runs as a
// --monitor-script before the model. Hermes skips the model whenever the script's
// output is unchanged (verified in Hermes v0.23.1), so a quiet night costs nothing.
// hpk reads Hermes' cron usage log to enforce budgets Hermes does not have.
package cron

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
)

// Job is one scheduled job.
type Job struct {
	Name     string
	Schedule string // cron expression, local time
	Budget   int    // tokens per run
	Skills   []string
}

// Jobs (§12). Budgets come from project.yaml when set.
func Jobs(p *project.Project) []Job {
	b := budgets(p)
	return []Job{
		{Name: "nightly", Schedule: "0 2 * * *", Budget: b.Nightly, Skills: []string{"hpk-wiki", "hpk-project"}},
		{Name: "weekly", Schedule: "0 8 * * 1", Budget: b.Weekly, Skills: []string{"hpk-wiki"}},
		{Name: "digest", Schedule: "0 18 * * *", Budget: b.Digest, Skills: nil},
	}
}

// JobByName finds a job.
func JobByName(p *project.Project, name string) (Job, error) {
	for _, j := range Jobs(p) {
		if j.Name == name {
			return j, nil
		}
	}
	return Job{}, fmt.Errorf("unknown job %q (nightly, weekly, digest)", name)
}

type budgetCfg struct {
	Nightly, Weekly, Digest, Daily int
}

func budgets(p *project.Project) budgetCfg {
	b := budgetCfg{Nightly: 60_000, Weekly: 120_000, Digest: 10_000, Daily: 150_000}
	raw, err := os.ReadFile(filepath.Join(p.Dir, "project.yaml"))
	if err != nil {
		return b
	}
	var y struct {
		Budget struct {
			Nightly int `yaml:"nightly_max_tokens"`
			Weekly  int `yaml:"weekly_max_tokens"`
			Digest  int `yaml:"digest_max_tokens"`
			Daily   int `yaml:"cron_daily_max_tokens"`
		} `yaml:"budget"`
	}
	if yaml.Unmarshal(raw, &y) == nil {
		if y.Budget.Nightly > 0 {
			b.Nightly = y.Budget.Nightly
		}
		if y.Budget.Weekly > 0 {
			b.Weekly = y.Budget.Weekly
		}
		if y.Budget.Digest > 0 {
			b.Digest = y.Budget.Digest
		}
		if y.Budget.Daily > 0 {
			b.Daily = y.Budget.Daily
		}
	}
	return b
}

// ---- state: <project>/cron.json ----

// JobState is what hpk remembers per job.
type JobState struct {
	HermesID    string    `json:"hermes_id,omitempty"` // job id in Hermes cron
	LastOutput  string    `json:"last_output,omitempty"`
	Watermark   time.Time `json:"watermark,omitempty"` // work before this is done
	LastCommit  string    `json:"last_commit,omitempty"`
	Paused      bool      `json:"paused,omitempty"` // over budget: a person must allow it again
	PauseReason string    `json:"pause_reason,omitempty"`
	LockedAt    time.Time `json:"locked_at,omitempty"`
}

// State is every job of a project, plus how far the Hermes usage log was read.
type State struct {
	Jobs      map[string]*JobState `json:"jobs"`
	UsageRead int64                `json:"usage_read"` // byte offset in Hermes cron/usage_audit.jsonl
}

func statePath(p *project.Project) string { return filepath.Join(p.Dir, "cron.json") }

// Load reads cron.json.
func Load(p *project.Project) (*State, error) {
	st := &State{Jobs: map[string]*JobState{}}
	raw, err := os.ReadFile(statePath(p))
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, st); err != nil {
		return nil, err
	}
	if st.Jobs == nil {
		st.Jobs = map[string]*JobState{}
	}
	return st, nil
}

// Save writes cron.json atomically.
func Save(p *project.Project, st *State) error {
	raw, _ := json.MarshalIndent(st, "", "  ")
	tmp := statePath(p) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, statePath(p))
}

func (st *State) job(name string) *JobState {
	if st.Jobs[name] == nil {
		st.Jobs[name] = &JobState{}
	}
	return st.Jobs[name]
}

// ---- usage: Hermes' own per-fire log, read only ----

// Usage is one fire of one of this project's jobs.
type Usage struct {
	At       time.Time `json:"at"`
	Job      string    `json:"job"`
	Tokens   int       `json:"tokens"`
	Model    string    `json:"model,omitempty"`
	Duration int       `json:"duration_ms"`
	Error    string    `json:"error,omitempty"`
}

// Account reads new lines of <home>/cron/usage_audit.jsonl, keeps the ones for
// this project's jobs in <project>/usage.jsonl and returns them. Over-budget runs
// pause their job until a person allows it again (Hermes has no per-job cap).
func Account(home string, p *project.Project) ([]Usage, error) {
	st, err := Load(p)
	if err != nil {
		return nil, err
	}
	byID := map[string]string{}
	for name, js := range st.Jobs {
		if js.HermesID != "" {
			byID[js.HermesID] = name
		}
	}
	f, err := os.Open(filepath.Join(home, "cron", "usage_audit.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() < st.UsageRead {
		st.UsageRead = 0 // the log was rotated
	}
	if _, err := f.Seek(st.UsageRead, 0); err != nil {
		return nil, err
	}
	var out []Usage
	r := bufio.NewReader(f)
	read := st.UsageRead
	b := budgets(p)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) == 0 || err != nil && !strings.HasSuffix(string(line), "\n") {
			break // a half-written last line is read next time
		}
		read += int64(len(line))
		var rec struct {
			TS          string `json:"ts"`
			JobID       string `json:"job_id"`
			TotalTokens *int   `json:"total_tokens"`
			Model       string `json:"model"`
			DurationMS  int    `json:"duration_ms"`
			Error       string `json:"error"`
		}
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		name, ok := byID[rec.JobID]
		if !ok {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, rec.TS)
		u := Usage{At: at, Job: name, Model: rec.Model, Duration: rec.DurationMS, Error: rec.Error}
		if rec.TotalTokens != nil {
			u.Tokens = *rec.TotalTokens
		}
		// Hermes' total counts every cached re-read in full. When hpk's hooks saw the
		// run, use the weighted count instead (cache reads at a tenth), as the live check does.
		if w, ok := weightedRun(home, p, name, at); ok {
			u.Tokens = w
		}
		out = append(out, u)
		// The run is over once Hermes logs it. A run stopped mid-way (budget, error)
		// never reaches `hpk cron done`, so its lock is released here.
		if js := st.job(name); !js.LockedAt.IsZero() && !at.Before(js.LockedAt) {
			js.LockedAt = time.Time{}
		}
		if j, err := JobByName(p, name); err == nil && u.Tokens > j.Budget {
			js := st.job(name)
			js.Paused, js.PauseReason = true, fmt.Sprintf("run at %s used %d tokens, over the %d budget", at.Local().Format("2006-01-02 15:04"), u.Tokens, j.Budget)
		}
	}
	st.UsageRead = read
	for _, u := range out {
		raw, _ := json.Marshal(u)
		appendLine(filepath.Join(p.Dir, "usage.jsonl"), raw)
	}
	if over, why := privacy.OverMoney(home, p, time.Now()); over {
		for _, j := range Jobs(p) {
			if js := st.job(j.Name); !js.Paused {
				js.Paused, js.PauseReason = true, why
			}
		}
	}
	if day := TokensSince(p, time.Now().Add(-24*time.Hour)); day > b.Daily {
		for _, j := range Jobs(p) {
			js := st.job(j.Name)
			if !js.Paused {
				js.Paused, js.PauseReason = true, fmt.Sprintf("cron used %d tokens in 24 h, over the daily %d", day, b.Daily)
			}
		}
	}
	return out, Save(p, st)
}

// TokensSince sums recorded usage.
func TokensSince(p *project.Project, since time.Time) int {
	n := 0
	for _, u := range History(p) {
		if !u.At.Before(since) {
			n += u.Tokens
		}
	}
	return n
}

// History returns every recorded run, oldest first.
func History(p *project.Project) []Usage {
	raw, err := os.ReadFile(filepath.Join(p.Dir, "usage.jsonl"))
	if err != nil {
		return nil
	}
	var out []Usage
	for _, l := range strings.Split(string(raw), "\n") {
		var u Usage
		if json.Unmarshal([]byte(l), &u) == nil && u.Job != "" {
			out = append(out, u)
		}
	}
	return out
}

// Allow un-pauses a job (a person decided the budget may be spent).
func Allow(p *project.Project, job string) error {
	st, err := Load(p)
	if err != nil {
		return err
	}
	js := st.job(job)
	js.Paused, js.PauseReason = false, ""
	if err := Save(p, st); err != nil {
		return err
	}
	return project.Audit(p.Dir, "cron allowed", job)
}

// Prompt renders a job's prompt.
func Prompt(p *project.Project, j Job, prompts fs.FS, language string) (string, error) {
	raw, err := fs.ReadFile(prompts, j.Name+".md")
	if err != nil {
		return "", err
	}
	promote := ""
	if p.Mode() == project.Shared {
		promote = "4a. Lessons that would help any project (not just this one): list them so a person can run `hpk wiki promote`. Never promote yourself.\n"
	}
	if language == "" {
		language = "English"
	}
	r := strings.NewReplacer("{{PROJECT}}", p.Name, "{{WIKI}}", p.WikiDir(), "{{BUDGET}}", fmt.Sprint(j.Budget),
		"{{DATE}}", "<today>", "{{PROMOTE}}", promote, "{{LANGUAGE}}", language)
	// The tag lets hpk's hooks link the run to its project and job: Hermes runs cron
	// inside the gateway, whose working directory is not the project's.
	return r.Replace(string(raw)) + "\n\n" + JobTag(p.Name, j.Name) + "\n", nil
}

// JobTag marks a cron prompt as an hpk job; see hook session_link.
func JobTag(projectName, job string) string { return "[hpk-job " + projectName + "/" + job + "]" }

var jobTagRE = regexp.MustCompile(`\[hpk-job ([a-z0-9][a-z0-9._-]*)/(nightly|weekly|digest)\]`)

// ParseJobTag finds the tag in a prompt.
func ParseJobTag(s string) (projectName, job string, ok bool) {
	m := jobTagRE.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

func appendLine(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// weightedRun finds the session hpk linked to this job's run (finished near at)
// and returns its weighted token count.
func weightedRun(home string, p *project.Project, job string, at time.Time) (int, bool) {
	for _, id := range project.JobSessions(home, p.Name, job) {
		seen, err := privacy.LoadSeen(home, id)
		if err != nil || seen == nil {
			continue
		}
		if d := seen.At.Sub(at); d > -15*time.Minute && d < 15*time.Minute {
			return int(seen.Weighted()), true
		}
	}
	return 0, false
}
