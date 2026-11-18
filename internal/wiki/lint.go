// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package wiki

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Finding is one lint result.
type Finding struct {
	Level string `json:"level"` // error | warn
	File  string `json:"file"`
	Line  int    `json:"line,omitempty"`
	Code  string `json:"code"`
	Text  string `json:"text"`
}

// Lint checks the wiki against §7.2 and §13. It reads files only; 0 tokens.
func (w *Wiki) Lint() ([]Finding, error) {
	var out []Finding
	add := func(level, file string, line int, code, text string) {
		out = append(out, Finding{Level: level, File: file, Line: line, Code: code, Text: text})
	}
	pages := map[string]bool{}
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
		if !strings.HasSuffix(rel, ".md") || rel == "log.md" || rel == "index.md" || rel == "SCHEMA.md" {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		pg, err := Parse(raw)
		if err != nil {
			add("error", rel, 1, "front-matter", err.Error())
			return nil
		}
		area := strings.SplitN(rel, "/", 2)[0]
		if _, ok := Areas[area]; !ok || !strings.Contains(rel, "/") {
			add("warn", rel, 0, "location", "page is outside the wiki folders")
		}
		if area == "raw" {
			return nil // raw material keeps whatever shape it came in
		}
		pages[rel] = true
		m := pg.Meta
		for _, f := range [][2]string{{"title", m.Title}, {"type", m.Type}, {"updated", m.Updated}, {"confidence", m.Confidence}} {
			if strings.TrimSpace(f[1]) == "" {
				add("error", rel, 1, "front-matter", "missing "+f[0])
			}
		}
		if len(m.Sources) == 0 {
			add("error", rel, 1, "no-source", "no sources: every claim needs a file:line, commit or session id")
		}
		if t := Tokens(pg.Body); t > PageTokenLimit {
			add("warn", rel, 0, "too-long", fmt.Sprintf("about %d tokens; split pages over %d", t, PageTokenLimit))
		}
		if LooksSecret(pg.Body) {
			add("error", rel, 0, "secret", "looks like it contains a secret; remove it and rotate the credential")
		}
		for _, ln := range RuleLines(pg.Body) {
			add("warn", rel, ln+bodyOffset(raw), "rule", "reads like a rule; rules belong in the repository, not the wiki")
		}
		return nil
	})
	if err != nil {
		return out, err
	}

	// index.md: size, broken links, pages missing from it.
	if raw, err := os.ReadFile(w.path("index.md")); err == nil {
		if t := Tokens(string(raw)); t > IndexTokenLimit {
			add("warn", "index.md", 0, "index-size", fmt.Sprintf("about %d tokens; keep it under %d", t, IndexTokenLimit))
		}
	}
	entries, err := w.Index()
	if err != nil {
		add("error", "index.md", 0, "index", err.Error())
	}
	listed := map[string]bool{}
	for _, e := range entries {
		listed[e.Path] = true
		if !pages[e.Path] {
			add("error", "index.md", 0, "broken-link", "lists "+e.Path+", which does not exist")
		}
	}
	var missing []string
	for p := range pages {
		if !listed[p] {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	for _, p := range missing {
		add("warn", p, 0, "not-indexed", "not listed in index.md")
	}

	// Drafts waiting too long.
	if drafts, err := w.Drafts(); err == nil {
		for _, d := range drafts {
			if c, err := time.Parse(time.RFC3339, d.Meta.Created); err == nil && w.Now().Sub(c) >= StaleAfter {
				add("warn", d.File, 0, "stale-draft", "waiting more than 14 days; `hpk wiki stale` moves it to _drafts/stale/")
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level == "error"
		}
		return out[i].File < out[j].File
	})
	return out, nil
}

// bodyOffset is the number of lines before the body (front-matter and blank lines).
func bodyOffset(raw []byte) int {
	sc := bufio.NewScanner(strings.NewReader(strings.ReplaceAll(string(raw), "\r\n", "\n")))
	n, fences := 0, 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if fences < 2 {
			if line == "---" {
				fences++
			}
			continue
		}
		if strings.TrimSpace(line) != "" {
			return n - 1
		}
	}
	return 0
}
