// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build !windows

package mcp

import (
	"errors"
	"os/exec"
	"syscall"
)

// ErrNeedsTerminal is never returned on this OS.
var ErrNeedsTerminal = errors.New("add this MCP's token in a terminal with: hermes mcp add")

// CanSendTokens: Hermes reads a token from stdin when it has no terminal.
const CanSendTokens = true

// hermesCmd starts hermes in a new session with no controlling terminal, so
// Hermes' secret prompt reads the token from stdin.
func hermesCmd(cmd *exec.Cmd, secret bool) (*exec.Cmd, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd, nil
}
