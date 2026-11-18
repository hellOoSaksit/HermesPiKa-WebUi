// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package isolate

import (
	"os"

	"golang.org/x/sys/windows"
)

// StdinIsTerminal reports whether stdin is a console a person types into.
func StdinIsTerminal() bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(os.Stdin.Fd()), &mode) == nil
}
