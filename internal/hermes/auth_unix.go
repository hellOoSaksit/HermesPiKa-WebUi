// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build !windows

package hermes

import (
	"context"
	"os/exec"
	"syscall"

	"hpk.local/hpk/internal/isolate"
)

// addKeyCmd starts `hermes auth add` in a new session, with no controlling
// terminal, so Hermes' secret prompt reads the key from stdin.
func addKeyCmd(ctx context.Context, args []string) (*exec.Cmd, error) {
	cmd := isolate.CommandContext(ctx, "hermes", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd, nil
}

// CanAddKeys reports whether keys can be added from the Console on this OS.
const CanAddKeys = true
