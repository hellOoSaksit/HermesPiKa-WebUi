// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"hpk.local/hpk/internal/selfinstall"
)

// runSelfInstall is `hpk self-install` and `hpk self-uninstall`.
func runSelfInstall(uninstall bool, stdout, stderr io.Writer) int {
	if uninstall {
		if err := selfinstall.Uninstall(stdout); err != nil {
			fmt.Fprintln(stderr, "hpk self-uninstall:", err)
			return 1
		}
		return 0
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, "hpk self-install:", err)
		return 1
	}
	res, err := selfinstall.Install(exe, stdout)
	if err != nil {
		fmt.Fprintln(stderr, "hpk self-install:", err)
		return 1
	}
	fmt.Fprintf(stdout, "hpk is installed at %s\n", res.Target)
	return 0
}

// autoSelfInstall runs when `hpk` starts with no arguments from somewhere
// other than its installed place (a double-clicked download): it installs
// itself, then starts the installed copy. handled is false when nothing was
// done and the normal start should go on.
func autoSelfInstall(stdout, stderr io.Writer) (code int, handled bool) {
	if os.Getenv("HPK_NO_SELFINSTALL") == "1" {
		return 0, false
	}
	exe, err := os.Executable()
	if err != nil || selfinstall.IsInstalled(exe) {
		return 0, false
	}
	res, err := selfinstall.Install(exe, stdout)
	if err != nil {
		// Never block a first start: say what went wrong and run from here.
		fmt.Fprintln(stderr, "hpk: could not install itself:", err)
		return 0, false
	}
	fmt.Fprintf(stdout, "hpk is installed for you at %s. Starting HPK Console…\n", res.Target)
	cmd := exec.Command(res.Target)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), true
		}
		fmt.Fprintln(stderr, "hpk:", err)
		return 1, true
	}
	return 0, true
}
