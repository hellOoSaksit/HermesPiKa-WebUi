// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package gates

import (
	"path"
	"regexp"
	"strings"
)

var driveRE = regexp.MustCompile(`^[A-Za-z]:`)

// normPath turns a path into the form the path rules expect, seeing through
// forms Windows treats as the same file: trailing dots and spaces
// (AGENTS.md.), alternate data streams (.env::$DATA) and 8.3 short names
// (AGENTS~1.MD, expanded when the file exists).
func normPath(p string) string {
	p = longPath(p)
	p = strings.ReplaceAll(p, `\`, "/")
	drive := ""
	if driveRE.MatchString(p) {
		drive, p = p[:2], p[2:]
	}
	if i := strings.IndexByte(p, ':'); i >= 0 { // a stream name
		p = p[:i]
	}
	parts := strings.Split(p, "/")
	for i, s := range parts {
		if s == "." || s == ".." {
			continue
		}
		parts[i] = strings.TrimRight(s, ". ")
	}
	return path.Clean(drive + strings.Join(parts, "/"))
}
