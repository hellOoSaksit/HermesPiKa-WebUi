// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package notify sends short notices about a project to a messaging platform
// through Hermes (`hermes send`), so hpk never holds a Telegram, Slack or LINE
// token. The Console always gets every notice; this is the extra channel.
//
// Only shared projects use it: private and isolated projects keep their content
// on this machine (docs/privacy.md), so their notices stay in the Console.
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/project"
)

// Kinds of notices.
const (
	Approval  = "approval"  // the agent waits for a person
	Report    = "report"    // a chat finished a turn and reported
	Budget    = "budget"    // a cap stopped work
	Duplicate = "duplicate" // two people or agents on the same work: never batched
	Test      = "test"
)

// Batching: at most one notice per project and kind every 5 minutes, except
// duplicates, which always go out (same rule as the Console bell).
const batchWindow = 5 * time.Minute

// Target is where a project's notices go: a `hermes send --to` target such as
// "telegram" or "slack:#builds". Empty means Console only.
func Target(p *project.Project) string {
	var to string
	_ = project.Field(p, []string{"notify", "to"}, &to)
	return strings.TrimSpace(to)
}

// ErrLocal is returned for private and isolated projects.
var ErrLocal = errors.New("private and isolated projects keep notices on this machine (Console only)")

// CheckTarget validates a target before it is saved.
func CheckTarget(p *project.Project, to string) error {
	if to == "" {
		return nil
	}
	if p.Mode() != project.Shared {
		return ErrLocal
	}
	if len(to) > 200 || strings.ContainsAny(to, "\r\n\"'`$;&|<>") {
		return fmt.Errorf("%q is not a Hermes send target (examples: telegram, slack:#builds)", to)
	}
	return nil
}

// Sender runs the actual send; tests replace it.
var Sender = func(ctx context.Context, to, text string) error {
	cmd := isolate.CommandContext(ctx, "hermes", "send", "--to", to, "--quiet", "--file", "-")
	cmd.Stdin = strings.NewReader(text)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("hermes send: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

var mu sync.Mutex

type state struct {
	Last map[string]time.Time `json:"last"`
}

// Send delivers one notice for project p, unless the project is local-only, has
// no target, or the same kind went out within the batch window. It returns
// whether it sent. Errors are also written to the project's audit log.
func Send(p *project.Project, kind, text string, now time.Time) (bool, error) {
	to := Target(p)
	if to == "" {
		return false, nil
	}
	if p.Mode() != project.Shared {
		return false, ErrLocal
	}
	mu.Lock()
	f := filepath.Join(p.Dir, "notify.json")
	var st state
	if raw, err := os.ReadFile(f); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	if st.Last == nil {
		st.Last = map[string]time.Time{}
	}
	if kind != Duplicate && kind != Test && now.Sub(st.Last[kind]) < batchWindow {
		mu.Unlock()
		return false, nil
	}
	st.Last[kind] = now
	raw, _ := json.Marshal(st)
	_ = os.WriteFile(f, raw, 0o644)
	mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	msg := fmt.Sprintf("[hpk · %s] %s", p.Name, text)
	if err := Sender(ctx, to, msg); err != nil {
		_ = project.Audit(p.Dir, "notify failed", kind+" to "+to+": "+err.Error())
		return false, err
	}
	_ = project.Audit(p.Dir, "notify sent", kind+" to "+to)
	return true, nil
}

// Targets lists what `hermes send --list --json` offers, for the Console's picker.
func Targets(ctx context.Context) ([]string, error) {
	out, err := isolate.CommandContext(ctx, "hermes", "send", "--list", "--json").Output()
	if err != nil {
		return nil, err
	}
	var r struct {
		Platforms map[string][]map[string]any `json:"platforms"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	var list []string
	for plat, chans := range r.Platforms {
		list = append(list, plat)
		for _, c := range chans {
			for _, k := range []string{"target", "id", "chat_id"} {
				if v, ok := c[k]; ok {
					list = append(list, fmt.Sprintf("%s:%v", plat, v))
					break
				}
			}
		}
	}
	return list, nil
}
