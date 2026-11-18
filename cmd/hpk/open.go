// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package main

import (
	"fmt"
	"io"
	"time"

	"hpk.local/hpk/internal/launch"
	"hpk.local/hpk/internal/project"
)

// `hpk open` opens the Console with the one-time link `hpk serve` keeps in
// <home>/hpk/console-link, so this browser may make changes (the Console key).
func openConsole(stdout, stderr io.Writer) int {
	link, ok := launch.ConsoleLink(project.Home(), time.Time{})
	if !ok {
		fmt.Fprintln(stderr, "hpk open: no link yet; start the Console first with `hpk` or `hpk serve`")
		return 1
	}
	openConsoleLink(link, time.Time{}, 0, stderr)
	fmt.Fprintln(stdout, "Opened the HPK Console in your browser.")
	return 0
}
