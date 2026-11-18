// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package onboard

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"hpk.local/hpk/internal/scan"
)

// Finding is one health issue the person may choose to fix (§8 step 7).
// These are the checks a script can do; contradictions between prose and code
// are left to the model, through the hpk-project skill.
type Finding struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // no-readme | broken-link | missing-file | duplicate-rule | undocumented
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Detail string `json:"detail"`
}

var (
	mdLink   = regexp.MustCompile(`\]\(([^)\s#]+)(?:#[^)]*)?\)`)
	codePath = regexp.MustCompile("`((?:[\\w.-]+/)+[\\w.-]+\\.[A-Za-z0-9]{1,6})`")
)

// Health checks the project's own documents against its files. 0 tokens.
func Health(r scan.Result) []Finding {
	var out []Finding
	add := func(kind, file string, line int, detail string) {
		out = append(out, Finding{Kind: kind, File: file, Line: line, Detail: detail})
	}
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(r.Root, filepath.FromSlash(rel)))
		return err == nil
	}
	// A short path in prose ("i18n/en.json") counts as found when some file ends with it.
	mentioned := func(rel string) bool {
		if exists(rel) {
			return true
		}
		for _, f := range r.All {
			if strings.HasSuffix(f, "/"+rel) {
				return true
			}
		}
		return false
	}

	hasReadme := false
	var allDocs strings.Builder
	controlLines := map[string][]string{} // line → control files that contain it
	for _, s := range r.Sources {
		if s.Kind == "readme" && !strings.Contains(s.Path, "/") {
			hasReadme = true
		}
		raw, err := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(s.Path)))
		if err != nil {
			continue
		}
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		allDocs.WriteString(strings.ToLower(text))
		allDocs.WriteByte('\n')
		dir := path.Dir(s.Path)
		inFence := false
		for i, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			if s.Kind == "control" {
				if t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-*0123456789. ")); len(t) >= 24 {
					key := strings.ToLower(t)
					if !contains(controlLines[key], s.Path) {
						controlLines[key] = append(controlLines[key], s.Path)
					}
				}
			}
			if inFence {
				continue
			}
			for _, m := range mdLink.FindAllStringSubmatch(line, -1) {
				target := m[1]
				if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "/") {
					continue
				}
				rel := path.Clean(path.Join(dir, target))
				if !strings.HasPrefix(rel, "../") && !exists(rel) {
					add("broken-link", s.Path, i+1, "links to "+target+", which does not exist")
				}
			}
			for _, m := range codePath.FindAllStringSubmatch(line, -1) {
				p := m[1]
				if !mentioned(p) && !exists(path.Join(dir, p)) {
					add("missing-file", s.Path, i+1, "mentions "+p+", which is not in the project")
				}
			}
		}
	}
	if !hasReadme {
		add("no-readme", "", 0, "the project has no README at its root")
	}
	var dups []string
	for line, files := range controlLines {
		if len(files) >= 2 {
			dups = append(dups, fmt.Sprintf("%q is repeated in %s", truncate(line, 80), strings.Join(files, ", ")))
		}
	}
	sort.Strings(dups)
	for _, d := range dups {
		add("duplicate-rule", "", 0, d+" — keep it in one place")
	}
	docs := allDocs.String()
	for _, d := range r.TopDirs {
		if strings.HasPrefix(d, ".") || d == "docs" || d == "doc" || d == "notes" {
			continue
		}
		if !strings.Contains(docs, strings.ToLower(d)+"/") && !strings.Contains(docs, "`"+strings.ToLower(d)+"`") {
			add("undocumented", d+"/", 0, "no document mentions "+d+"/")
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	for i := range out {
		out[i].ID = fmt.Sprintf("h-%d", i+1)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
