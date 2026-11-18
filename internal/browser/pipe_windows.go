// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package browser

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// attachPipes: on Windows the browser takes its pipe handles by number
// (--remote-debugging-io-pipes=<in>,<out>); only this child inherits them.
func attachPipes(cmd *exec.Cmd, d *devtoolsPipes) ([]string, error) {
	in, out := windows.Handle(d.childIn.Fd()), windows.Handle(d.childOut.Fd())
	for _, h := range []windows.Handle{in, out} {
		if err := windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			return nil, err
		}
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.AdditionalInheritedHandles = append(cmd.SysProcAttr.AdditionalInheritedHandles, syscall.Handle(in), syscall.Handle(out))
	return []string{"--remote-debugging-pipe", fmt.Sprintf("--remote-debugging-io-pipes=%d,%d", uintptr(in), uintptr(out))}, nil
}
