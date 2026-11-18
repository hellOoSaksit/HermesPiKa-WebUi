// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package wiki

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Hit is one search result.
type Hit struct {
	File    string `json:"file"`
	Title   string `json:"title"`
	Line    int    `json:"line,omitempty"`
	Snippet string `json:"snippet"`
	Score   int    `json:"score"`
}

// Search follows §7.5: index.md first, then the pages themselves. No embeddings.
// Every word must appear (case-insensitive); titles and index lines rank higher.
func (w *Wiki) Search(query string, limit int) ([]Hit, error) {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil, nil
	}
	has := func(s string) bool {
		s = strings.ToLower(s)
		for _, w := range words {
			if !strings.Contains(s, w) {
				return false
			}
		}
		return true
	}
	seen := map[string]*Hit{}
	if entries, err := w.Index(); err == nil {
		for _, e := range entries {
			if has(e.Title + " " + e.Summary + " " + e.Path) {
				seen[e.Path] = &Hit{File: e.Path, Title: e.Title, Snippet: e.Summary, Score: 10}
			}
		}
	}
	err := filepath.WalkDir(w.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(w.Dir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".git" || rel == "_drafts" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".md") || rel == "index.md" || rel == "log.md" {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		pg, err := Parse(raw)
		if err != nil {
			return nil
		}
		if !has(pg.Meta.Title + "\n" + pg.Body) {
			return nil
		}
		h := seen[rel]
		if h == nil {
			h = &Hit{File: rel, Title: pg.Meta.Title}
			seen[rel] = h
		}
		if has(pg.Meta.Title) {
			h.Score += 5
		}
		for i, line := range strings.Split(pg.Body, "\n") {
			l := strings.ToLower(line)
			for _, w := range words {
				if strings.Contains(l, w) {
					h.Score++
					if h.Line == 0 {
						h.Line = i + 1
						h.Snippet = strings.TrimSpace(line)
					}
				}
			}
		}
		return nil
	})
	var out []Hit
	for _, h := range seen {
		if r := []rune(h.Snippet); len(r) > 160 {
			h.Snippet = string(r[:157]) + "…"
		}
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].File < out[j].File
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, err
}
