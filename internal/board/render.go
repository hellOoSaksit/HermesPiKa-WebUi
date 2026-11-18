// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package board

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	markBegin = "<!-- hpk:claims -->"
	markEnd   = "<!-- /hpk:claims -->"
)

// Table renders the open claims as Markdown, grouped by lane.
func (d *DB) Table() (string, error) {
	open, err := d.Claims(false)
	if err != nil {
		return "", err
	}
	sort.SliceStable(open, func(i, j int) bool { return open[i].Lane < open[j].Lane })
	var b strings.Builder
	fmt.Fprintf(&b, "_Rendered by hpk from board.db at %s. Edit claims with `hpk board`, not here._\n\n", d.Now().UTC().Format("2006-01-02 15:04 UTC"))
	if len(open) == 0 {
		b.WriteString("Nobody holds a claim right now.\n")
		return b.String(), nil
	}
	b.WriteString("| Claim | Lane | Task | Who | Paths | Last heartbeat | Status |\n|---|---|---|---|---|---|---|\n")
	for _, c := range open {
		ago := d.Now().Sub(c.Heartbeat).Round(time.Minute)
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s ago | %s |\n", c.ID, dash(c.Lane), esc(c.Task), esc(c.Owner), esc(strings.Join(c.Paths, " ")), ago, c.Status)
	}
	return b.String(), nil
}

// Render writes the human view (§20.2): kind "hpk" owns the whole BOARD.md;
// kind "markdown" only replaces the block between the hpk:claims markers.
func (d *DB) Render(root, file, kind string) (string, error) {
	if file == "" {
		file = "BOARD.md"
	}
	target := filepath.Join(root, filepath.FromSlash(file))
	table, err := d.Table()
	if err != nil {
		return "", err
	}
	block := markBegin + "\n" + table + markEnd
	switch kind {
	case "", "hpk":
		body := "# Board\n\nWho is working on what. This file is generated; the data lives in hpk's board.db.\n\n" + block + "\n"
		return target, os.WriteFile(target, []byte(body), 0o644)
	case "markdown":
		raw, err := os.ReadFile(target)
		if err != nil {
			return "", err
		}
		s := string(raw)
		a, b := strings.Index(s, markBegin), strings.Index(s, markEnd)
		if a < 0 || b < a {
			return "", errors.New(file + " has no <!-- hpk:claims --> … <!-- /hpk:claims --> block; add one where the claims should go. hpk never edits the rest")
		}
		out := s[:a] + block + s[b+len(markEnd):]
		return target, os.WriteFile(target, []byte(out), 0o644)
	default:
		return "", fmt.Errorf("board kind %q is not rendered by hpk", kind)
	}
}

func esc(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ") }

func dash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}
