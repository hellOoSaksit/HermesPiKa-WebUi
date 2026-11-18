// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package folders reads the project's folder rules page (decisions/folders.md):
// which kind of file goes in which folder. The page is the person's decision, so
// the rules are used only while the page's hash equals the hash saved when the
// person approved it. hpk only creates missing folders; it never moves a file.
package folders

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"hpk.local/hpk/internal/safepath"
)

// Page is the wiki page that holds the rules.
const Page = "decisions/folders.md"

// Rule is one table row.
type Rule struct {
	Folder string
	What   string
	Exts   []string // lower-case, leading dot
}

// Rules is a parsed page.
type Rules struct {
	Rows     []Rule
	Answered bool     // the page exists, even with no rows ("no split")
	Bad      []string // one message per rejected line
}

var (
	headerCells = []string{"folder", "what goes here", "extensions"}
	alignCell   = regexp.MustCompile(`^:?-+:?$`)
	extSplit    = regexp.MustCompile(`[\s,]+`)
)

func cells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isHeader(c []string) bool {
	if len(c) < 3 {
		return false
	}
	for i, h := range headerCells {
		if strings.ToLower(c[i]) != h {
			return false
		}
	}
	return true
}

func isAlign(c []string) bool {
	for _, x := range c {
		if !alignCell.MatchString(x) {
			return false
		}
	}
	return len(c) > 0
}

// Folder normalizes a rule's folder cell: forward slashes, no trailing slash.
func Folder(cell string) string {
	return strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(cell), `\`, "/"), "/")
}

// SafeFolder reports why a folder cannot be used under the project, or "". It
// takes the same name rules as upload and the file browser (safepath), so a
// rule never names a folder hpk would refuse to write to.
func SafeFolder(f string) string {
	f = Folder(f)
	switch {
	case f == "":
		return "folder is empty"
	case strings.HasPrefix(f, "/") || filepath.IsAbs(f) || filepath.VolumeName(f) != "" || strings.Contains(f, ":"):
		return "folder must be inside the project, not an absolute path"
	}
	for _, seg := range strings.Split(f, "/") {
		if seg == ".." {
			return "folder must stay inside the project (no ..)"
		}
	}
	if err := safepath.Rel(f); err != nil {
		return "folder name not allowed: " + err.Error()
	}
	return ""
}

// Parse reads the table after the header line. The page is a person's
// decision, so a bad row is reported in Bad and skipped, never guessed at.
func Parse(body string) Rules {
	r := Rules{Answered: true}
	seen := map[string]bool{}
	started := false
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		if !started {
			if strings.Contains(line, "|") && isHeader(cells(line)) {
				started = true
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(line, "|") {
			break // the table ended
		}
		c := cells(line)
		if isAlign(c) {
			continue
		}
		if len(c) < 3 {
			r.Bad = append(r.Bad, fmt.Sprintf("row %q needs 3 columns: Folder | What goes here | Extensions", strings.TrimSpace(line)))
			continue
		}
		folder := Folder(c[0])
		if why := SafeFolder(folder); why != "" {
			r.Bad = append(r.Bad, fmt.Sprintf("row %q: %s", folder, why))
			continue
		}
		var exts []string
		bad := ""
		for _, e := range extSplit.Split(c[2], -1) {
			if e == "" {
				continue
			}
			e = strings.ToLower(e)
			if !strings.HasPrefix(e, ".") {
				e = "." + e
			}
			if e == "." {
				continue
			}
			if seen[e] {
				bad = fmt.Sprintf("row %q: extension %s is already used by an earlier row", folder, e)
				break
			}
			exts = append(exts, e)
		}
		if bad == "" && len(exts) == 0 {
			bad = fmt.Sprintf("row %q: no extensions", folder)
		}
		if bad != "" {
			r.Bad = append(r.Bad, bad)
			continue
		}
		for _, e := range exts {
			seen[e] = true
		}
		r.Rows = append(r.Rows, Rule{Folder: folder, What: c[1], Exts: exts})
	}
	return r
}

// For returns the folder for a file name, by its last extension.
func (r Rules) For(name string) (string, bool) {
	e := strings.ToLower(filepath.Ext(name))
	if e == "" || e == "." {
		return "", false
	}
	for _, row := range r.Rows {
		for _, x := range row.Exts {
			if x == e {
				return row.Folder, true
			}
		}
	}
	return "", false
}
