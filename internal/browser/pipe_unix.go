// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build !windows

package browser

import "os/exec"

// attachPipes hands the browser its pipe ends as fd 3 (commands in) and fd 4
// (answers out), where --remote-debugging-pipe looks for them.
func attachPipes(cmd *exec.Cmd, d *devtoolsPipes) ([]string, error) {
	cmd.ExtraFiles = append(cmd.ExtraFiles, d.childIn, d.childOut)
	return []string{"--remote-debugging-pipe"}, nil
}
