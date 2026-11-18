// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package hermes

import (
	"context"
	"os/exec"
)

// addKeyCmd: on Windows Hermes reads the key from the console keyboard, not
// stdin, and hpk will not put a key in a command line. Add keys in a terminal.
func addKeyCmd(ctx context.Context, args []string) (*exec.Cmd, error) {
	return nil, ErrNeedsTerminal
}

// CanAddKeys reports whether keys can be added from the Console on this OS.
const CanAddKeys = false
