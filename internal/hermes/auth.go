// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package hermes

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hpk.local/hpk/internal/isolate"
)

// Managing Hermes' credential pool from the Console. Every change runs the
// `hermes auth` command, so Hermes stays the only place keys are kept. A key
// added here is handed to `hermes auth add` on stdin (never in the command line,
// a log or a file), see addKeyCmd.

var (
	providerRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,63}$`)
	targetRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@ -]{0,63}$`)
)

// ErrNeedsTerminal: Hermes reads the key from the keyboard on this OS.
var ErrNeedsTerminal = errors.New("on Windows Hermes reads keys from the keyboard: add it in a terminal with hermes auth add")

func checkProvider(p string) error {
	if !providerRE.MatchString(p) {
		return fmt.Errorf("%q is not a provider id (examples: anthropic, openrouter, openai)", p)
	}
	return nil
}

func checkTarget(t string) error {
	if !targetRE.MatchString(t) {
		return fmt.Errorf("%q is not a credential id, number or label", t)
	}
	return nil
}

func forget() {
	credMu.Lock()
	credAt = time.Time{}
	credMu.Unlock()
}

func runAuth(ctx context.Context, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := isolate.CommandContext(ctx, "hermes", append([]string{"auth"}, args...)...).CombinedOutput()
	forget()
	if err != nil {
		return fmt.Errorf("hermes auth %s: %s", args[0], lastLine(string(out), err))
	}
	return nil
}

func lastLine(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.Contains(l, "install out of sync") {
			return l
		}
	}
	return err.Error()
}

// AddAPIKey adds an API-key credential for a provider.
func AddAPIKey(ctx context.Context, provider, label, key string, priority int) error {
	if err := checkProvider(provider); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 4096 || strings.ContainsAny(key, "\r\n") {
		return errors.New("paste one API key")
	}
	if label != "" {
		if err := checkTarget(label); err != nil {
			return err
		}
	}
	args := []string{"auth", "add", provider, "--type", "api-key"}
	if label != "" {
		args = append(args, "--label", label)
	}
	if priority >= 0 {
		args = append(args, "--priority", strconv.Itoa(priority))
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd, err := addKeyCmd(ctx, args)
	if err != nil {
		return err
	}
	cmd.Stdin = strings.NewReader(key + "\n")
	out, err := cmd.CombinedOutput()
	forget()
	if err != nil {
		return fmt.Errorf("hermes auth add: %s", lastLine(strings.ReplaceAll(string(out), key, "<key>"), err))
	}
	return nil
}

// RemoveCredential removes one credential (by number, id or label).
func RemoveCredential(ctx context.Context, provider, target string) error {
	if err := checkProvider(provider); err != nil {
		return err
	}
	if err := checkTarget(target); err != nil {
		return err
	}
	return runAuth(ctx, "remove", provider, target)
}

// SetPriority moves a credential (0 = tried first).
func SetPriority(ctx context.Context, provider, target string, priority int) error {
	if err := checkProvider(provider); err != nil {
		return err
	}
	if err := checkTarget(target); err != nil {
		return err
	}
	if priority < 0 || priority > 99 {
		return errors.New("priority is 0 to 99")
	}
	return runAuth(ctx, "priority", provider, target, strconv.Itoa(priority))
}

// ResetCredential clears an "exhausted" mark (one credential, or all of a provider's).
func ResetCredential(ctx context.Context, provider, target string) error {
	if err := checkProvider(provider); err != nil {
		return err
	}
	args := []string{"reset", provider}
	if target != "" {
		if err := checkTarget(target); err != nil {
			return err
		}
		args = append(args, target)
	}
	return runAuth(ctx, args...)
}
