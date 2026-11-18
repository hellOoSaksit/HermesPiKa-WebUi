// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Hermes may run a session somewhere other than the project folder: hermes-acp
// can hand sessions to a running gateway, whose working directory and
// environment are its own. So hpk records which project each Hermes session it
// opened belongs to, and hooks look the session up before trusting the cwd.

type sessionLink struct {
	HermesID string `json:"hermes_id"`
	Project  string `json:"project"`
	Job      string `json:"job,omitempty"`   // an hpk cron job, when the session is one of its runs
	Owner    string `json:"owner,omitempty"` // the person who started it (team budgets)
}

func linkFile(home, hermesID string) string {
	sum := sha256.Sum256([]byte(hermesID))
	return filepath.Join(home, "hpk", "sessions", hex.EncodeToString(sum[:8])+".json")
}

// HomeOf is the Hermes home a loaded project lives in.
func HomeOf(p *Project) string { return filepath.Dir(filepath.Dir(filepath.Dir(p.Dir))) }

// LinkSession records that a Hermes session works on project p.
func LinkSession(p *Project, hermesID string) error { return LinkJob(p, hermesID, "") }

// LinkJob records that a Hermes session is a run of one of p's cron jobs.
func LinkJob(p *Project, hermesID, job string) error {
	return link(p, sessionLink{HermesID: hermesID, Job: job})
}

// LinkOwned records that a Hermes session works on p and was started by owner.
func LinkOwned(p *Project, hermesID, owner string) error {
	return link(p, sessionLink{HermesID: hermesID, Owner: owner})
}

func link(p *Project, l sessionLink) error {
	hermesID := l.HermesID
	l.Project = p.Name
	if hermesID == "" {
		return nil
	}
	f := linkFile(HomeOf(p), hermesID)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	raw, _ := json.Marshal(l)
	return os.WriteFile(f, raw, 0o644)
}

// OwnerOf returns who started a linked session, or "".
func OwnerOf(home, hermesID string) string {
	raw, err := os.ReadFile(linkFile(home, hermesID))
	if err != nil {
		return ""
	}
	var l sessionLink
	if json.Unmarshal(raw, &l) != nil || l.HermesID != hermesID {
		return ""
	}
	return l.Owner
}

// OwnedSessions lists the Hermes sessions a person started, in every project.
func OwnedSessions(home, owner string) []string {
	entries, _ := os.ReadDir(filepath.Join(home, "hpk", "sessions"))
	var out []string
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(home, "hpk", "sessions", e.Name()))
		if err != nil {
			continue
		}
		var l sessionLink
		if json.Unmarshal(raw, &l) == nil && l.Owner != "" && strings.EqualFold(l.Owner, owner) {
			out = append(out, l.HermesID)
		}
	}
	return out
}

// BySession returns the project a Hermes session was linked to, or "".
func BySession(home, hermesID string) string { name, _ := LinkOf(home, hermesID); return name }

// LinkOf returns the project and (for cron runs) the job of a linked session.
func LinkOf(home, hermesID string) (string, string) {
	if hermesID == "" {
		return "", ""
	}
	raw, err := os.ReadFile(linkFile(home, hermesID))
	if err != nil {
		return "", ""
	}
	var l sessionLink
	if json.Unmarshal(raw, &l) != nil || l.HermesID != hermesID {
		return "", ""
	}
	return l.Project, l.Job
}

// JobSessions lists the Hermes sessions linked to one cron job of a project.
func JobSessions(home, projectName, job string) []string { return linked(home, projectName, job, true) }

// Sessions lists every Hermes session linked to a project (chats and cron runs).
func Sessions(home, projectName string) []string { return linked(home, projectName, "", false) }

func linked(home, projectName, job string, byJob bool) []string {
	entries, _ := os.ReadDir(filepath.Join(home, "hpk", "sessions"))
	var out []string
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(home, "hpk", "sessions", e.Name()))
		if err != nil {
			continue
		}
		var l sessionLink
		if json.Unmarshal(raw, &l) == nil && l.Project == projectName && (!byJob || l.Job == job) {
			out = append(out, l.HermesID)
		}
	}
	return out
}
