// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package hermes talks to Hermes Agent only through its official interfaces.
// M0: find the CLI and read its version. Never patches Hermes or its config.
package hermes

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"hpk.local/hpk/internal/isolate"
)

// Info describes the Hermes install hpk found.
type Info struct {
	CLI     string `json:"cli,omitempty"`
	ACP     string `json:"acp,omitempty"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Detect looks for `hermes` and `hermes-acp` on PATH and asks the CLI for its version.
func Detect(ctx context.Context) Info {
	var info Info
	cli, err := exec.LookPath("hermes")
	if err != nil {
		info.Error = "hermes not found on PATH"
		return info
	}
	info.CLI = cli
	if acp, err := exec.LookPath("hermes-acp"); err == nil {
		info.ACP = acp
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := isolate.CommandContext(ctx, cli, "--version").Output()
	if err != nil {
		info.Error = "hermes --version failed: " + err.Error()
		return info
	}
	info.Version = firstLine(string(out))
	return info
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
