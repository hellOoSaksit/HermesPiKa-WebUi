// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package gitops

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Open pull requests come from gh, when it is installed and signed in; hpk holds
// no GitHub token. Cached for a minute; any failure just means "no PR info".
var (
	prMu    sync.Mutex
	prAt    time.Time
	prRoot  string
	prCache map[string]*PR
)

func invalidatePRs() {
	prMu.Lock()
	prAt = time.Time{}
	prMu.Unlock()
}

func openPRs(ctx context.Context, root, remote string) map[string]*PR {
	if !strings.Contains(remote, "github") {
		return nil
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil
	}
	prMu.Lock()
	defer prMu.Unlock()
	if prRoot == root && time.Since(prAt) < time.Minute {
		return prCache
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "pr", "list", "--state", "open", "--limit", "50", "--json", "number,headRefName,isDraft,url,statusCheckRollup")
	cmd.Dir = root
	out, err := cmd.Output()
	prRoot, prAt, prCache = root, time.Now(), map[string]*PR{}
	if err != nil {
		return prCache
	}
	var list []struct {
		Number  int    `json:"number"`
		Head    string `json:"headRefName"`
		IsDraft bool   `json:"isDraft"`
		URL     string `json:"url"`
		Checks  []struct {
			Status, Conclusion, State string
		} `json:"statusCheckRollup"`
	}
	if json.Unmarshal(out, &list) != nil {
		return prCache
	}
	for _, x := range list {
		ci := "none"
		if len(x.Checks) > 0 {
			ci = "pass"
		}
		for _, c := range x.Checks {
			s := strings.ToUpper(c.Conclusion + " " + c.State + " " + c.Status)
			switch {
			case strings.Contains(s, "FAIL"), strings.Contains(s, "ERROR"), strings.Contains(s, "TIMED_OUT"), strings.Contains(s, "CANCELLED"):
				ci = "fail"
			case ci != "fail" && (strings.Contains(s, "IN_PROGRESS") || strings.Contains(s, "QUEUED") || strings.Contains(s, "PENDING")):
				ci = "running"
			}
		}
		prCache[x.Head] = &PR{N: x.Number, Draft: x.IsDraft, CI: ci, URL: x.URL}
	}
	return prCache
}
