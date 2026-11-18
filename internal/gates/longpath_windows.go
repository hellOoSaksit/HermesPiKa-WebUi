// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package gates

import (
	"strings"

	"golang.org/x/sys/windows"
)

// longPath expands 8.3 short names (PROGRA~1) when the file exists.
func longPath(p string) string {
	if !strings.Contains(p, "~") {
		return p
	}
	in, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetLongPathName(in, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return p
	}
	return windows.UTF16ToString(buf[:n])
}
