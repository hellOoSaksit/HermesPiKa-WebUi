// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package isolate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Finding is one line for hpk doctor and hpk isolate status.
type Finding struct {
	Level  string // ok | warn | fail | info
	Name   string
	Detail string
	Fix    string
}

// InContainer reports whether hpk runs inside a container (the docker/hermes
// image, a team server): there the person's vault and browser are off anyway.
func InContainer() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

// Findings checks the isolation of the AI for this Hermes home. Read-only and
// quick: it does not start anything as the AI (hpk isolate check does).
func Findings(home string) []Finding {
	c, ok := Load()
	if !ok {
		if InContainer() {
			return []Finding{{"info", "ai isolation", "hpk runs in a container; the vault and built-in browser are off here", ""}}
		}
		return []Finding{{"warn", "ai isolation", "Hermes (the AI) runs in your OS account: it could read the vault key, the browser profile and the Console link",
			"hpk isolate setup (docs/isolation.md)"}}
	}
	if !samePath(c.Home, home) {
		return []Finding{{"warn", "ai isolation", "set up for the Hermes home " + c.Home + ", not " + home, "hpk isolate teardown, then hpk isolate setup"}}
	}
	var fs []Finding
	switch c.Mode {
	case ModeUser:
		exists, pw := UserStatus(c)
		switch {
		case !exists:
			fs = append(fs, Finding{"fail", "ai isolation", "the account " + c.User + " is gone; hpk cannot start Hermes", "hpk isolate setup"})
		case !pw:
			fs = append(fs, Finding{"fail", "ai isolation", "the password of " + c.User + " is missing from " + PasswordFile(c), "hpk isolate setup"})
		default:
			fs = append(fs, Finding{"ok", "ai isolation", "Hermes runs as the account " + c.User + "; your vault, browser and Console key are in " + c.Private, ""})
		}
		if _, err := os.Stat(c.Bin); err != nil {
			fs = append(fs, Finding{"fail", "ai hpk", c.Bin + " is missing: Hermes' hooks and the relay need it", "hpk install"})
		}
	case ModeDocker:
		out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", c.Container).Output()
		if err != nil || strings.TrimSpace(string(out)) != "true" {
			fs = append(fs, Finding{"fail", "ai isolation", "the container " + c.Container + " is not running; hpk cannot start Hermes", "docker compose -f " + c.Compose + " up -d"})
		} else {
			fs = append(fs, Finding{"ok", "ai isolation", "Hermes runs in the container " + c.Container + "; your vault, browser and Console key are in " + c.Private, ""})
		}
	}
	if left := Leftovers(home); len(left) > 0 {
		fs = append(fs, Finding{"warn", "private files", "still in the Hermes home, where the AI can read them: " + strings.Join(left, ", "), "stop hpk, then hpk isolate setup moves them"})
	}
	return fs
}

// Check starts hpk's probe as the AI (through the relay) and judges it.
func Check(c *Config) ([]string, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self, "ai-relay", "--probe")
	cmd.Dir = c.Home
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-time.After(2 * time.Minute): // the first logon makes the account's profile
		_ = cmd.Process.Kill()
		return nil, errors.New("the AI's side did not answer in 2 minutes")
	}
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(errb.String()+" "+out.String()))
	}
	if c.Mode == ModeDocker {
		return nil, nil // hermes --version ran in the container; the private folder is not mounted there
	}
	var r ProbeResult
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &r); err != nil {
		return nil, fmt.Errorf("unexpected answer from the AI's side: %q", strings.TrimSpace(out.String()))
	}
	return Judge(c, r), nil
}
