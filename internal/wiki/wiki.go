// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package wiki manages a project wiki (docs/DESIGN.md §7): the folder layout,
// drafts and their approval, index.md, the append-only log.md, lint and git.
// Everything here is plain file and git work: 0 tokens.
package wiki

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Areas are the folders a page can live in, and who may approve each (§7.3).
var Areas = map[string]Area{
	"raw":          {Free: true},
	"lessons":      {AutoOK: true},
	"versions":     {AutoOK: true},
	"glossary":     {AutoOK: true},
	"decisions":    {},
	"architecture": {},
	"people":       {},
}

// Area says how pages in a folder are written.
type Area struct {
	Free   bool // written directly, no draft (raw/)
	AutoOK bool // Auto mode may approve drafts here; otherwise a person must
}

// Limits from §7.2 and §13.
const (
	PageTokenLimit  = 1500
	IndexTokenLimit = 2000
	StaleAfter      = 14 * 24 * time.Hour
)

// Wiki is one project wiki on disk.
type Wiki struct {
	Dir string
	Now func() time.Time // for tests
}

// Open returns a handle; it does not check that the wiki exists.
func Open(dir string) *Wiki { return &Wiki{Dir: dir, Now: time.Now} }

func (w *Wiki) path(rel string) string { return filepath.Join(w.Dir, filepath.FromSlash(rel)) }

// Exists reports whether the wiki has been initialised.
func (w *Wiki) Exists() bool {
	_, err := os.Stat(w.path("SCHEMA.md"))
	return err == nil
}

// Init lays out a new wiki from the templates and makes the first commit.
// Existing files are never overwritten, so running it twice is safe.
func (w *Wiki) Init(project string, templates fs.FS) (created []string, err error) {
	if project == "" {
		project = filepath.Base(filepath.Dir(w.Dir))
	}
	for _, d := range []string{"raw", "decisions", "architecture", "versions", "lessons", "glossary", "people", "_drafts", "_drafts/stale", "_drafts/discarded"} {
		p := w.path(d)
		if err := os.MkdirAll(p, 0o755); err != nil {
			return created, err
		}
		keep := filepath.Join(p, ".gitkeep")
		if _, err := os.Stat(keep); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(keep, nil, 0o644); err != nil {
				return created, err
			}
		}
	}
	date := w.Now().Format("2006-01-02")
	for _, name := range []string{"SCHEMA.md", "index.md", "log.md"} {
		dst := w.path(name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		raw, err := fs.ReadFile(templates, name)
		if err != nil {
			return created, err
		}
		s := strings.NewReplacer("{{PROJECT}}", project, "{{DATE}}", date).Replace(string(raw))
		if err := os.WriteFile(dst, []byte(s), 0o644); err != nil {
			return created, err
		}
		created = append(created, name)
	}
	if len(created) == 0 {
		return created, nil // already set up
	}
	if err := w.appendLog("init", "", "wiki created for "+project); err != nil {
		return created, err
	}
	return created, w.commit("wiki: init " + project)
}

// cleanTarget validates a page path such as "lessons/csv-encoding.md".
func cleanTarget(t string) (string, Area, error) {
	t = strings.TrimSpace(filepath.ToSlash(t))
	t = path.Clean(strings.TrimPrefix(t, "./"))
	if t == "SCHEMA.md" {
		return "", Area{}, errors.New("SCHEMA.md is changed by a person only, never through a draft")
	}
	if strings.HasPrefix(t, "../") || strings.HasPrefix(t, "/") || strings.Contains(t, ":") {
		return "", Area{}, fmt.Errorf("target %q is outside the wiki", t)
	}
	if !strings.HasSuffix(t, ".md") {
		t += ".md"
	}
	area := strings.SplitN(t, "/", 2)[0]
	a, ok := Areas[area]
	if !ok || !strings.Contains(t, "/") {
		return "", Area{}, fmt.Errorf("target %q must be inside one of: decisions, architecture, versions, lessons, glossary, people, raw", t)
	}
	return t, a, nil
}

// slug turns a title into a file-name-safe slug (keeps Thai letters).
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r > 127 && r != ' ':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if r := []rune(out); len(r) > 48 {
		out = strings.Trim(string(r[:48]), "-")
	}
	if out == "" {
		out = "page"
	}
	return out
}
