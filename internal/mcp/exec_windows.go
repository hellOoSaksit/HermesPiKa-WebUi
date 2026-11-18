// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package mcp

import (
	"errors"
	"os/exec"
)

// ErrNeedsTerminal: on Windows Hermes reads a token from the keyboard only.
var ErrNeedsTerminal = errors.New("on Windows Hermes reads MCP tokens from the keyboard: add this one in a terminal with hermes mcp add NAME --url URL --auth header")

// CanSendTokens: MCPs without a token can still be added from the Console.
const CanSendTokens = false

func hermesCmd(cmd *exec.Cmd, secret bool) (*exec.Cmd, error) {
	if secret {
		return nil, ErrNeedsTerminal
	}
	return cmd, nil
}
