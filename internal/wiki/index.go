// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package wiki

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	indexBegin = "<!-- hpk:index:begin -->"
	indexEnd   = "<!-- hpk:index:end -->"
)

var indexLine = regexp.MustCompile(`^- \[([^\]]*)\]\(([^)]+)\)(?: — (.*))?$`)

// IndexEntry is one line of index.md.
type IndexEntry struct {
	Title, Path, Summary string
}

// Index reads the managed block of index.md.
func (w *Wiki) Index() ([]IndexEntry, error) {
	raw, err := os.ReadFile(w.path("index.md"))
	if err != nil {
		return nil, err
	}
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	a, b := strings.Index(s, indexBegin), strings.Index(s, indexEnd)
	if a < 0 || b < a {
		return nil, errors.New("index.md has no hpk:index block")
	}
	var out []IndexEntry
	for _, line := range strings.Split(s[a+len(indexBegin):b], "\n") {
		if m := indexLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out = append(out, IndexEntry{Title: m[1], Path: m[2], Summary: m[3]})
		}
	}
	return out, nil
}

// setIndexLine adds or replaces the line for a page, keeping lines sorted by path.
func (w *Wiki) setIndexLine(target, title, summary string) error {
	raw, err := os.ReadFile(w.path("index.md"))
	if err != nil {
		return err
	}
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	a, b := strings.Index(s, indexBegin), strings.Index(s, indexEnd)
	if a < 0 || b < a {
		return errors.New("index.md has no hpk:index block")
	}
	entries, _ := w.Index()
	found := false
	for i := range entries {
		if entries[i].Path == target {
			entries[i].Title = title
			if summary != "" {
				entries[i].Summary = summary
			}
			found = true
		}
	}
	if !found {
		entries = append(entries, IndexEntry{Title: title, Path: target, Summary: summary})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	var b2 strings.Builder
	b2.WriteString(indexBegin + "\n")
	for _, e := range entries {
		line := fmt.Sprintf("- [%s](%s)", strings.ReplaceAll(e.Title, "]", ")"), e.Path)
		if e.Summary != "" {
			line += " — " + strings.ReplaceAll(e.Summary, "\n", " ")
		}
		b2.WriteString(line + "\n")
	}
	out := s[:a] + b2.String() + s[b:]
	return os.WriteFile(w.path("index.md"), []byte(out), 0o644)
}

// appendLog adds one line to log.md. The log is never rewritten.
func (w *Wiki) appendLog(action, target, note string) error {
	f, err := os.OpenFile(w.path("log.md"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line := fmt.Sprintf("- %s · %s", w.Now().UTC().Format(time.RFC3339), action)
	if target != "" {
		line += " · " + target
	}
	if note != "" {
		line += " · " + strings.ReplaceAll(note, "\n", " ")
	}
	_, err = f.WriteString(line + "\n")
	return err
}

// Log appends a free note, e.g. from an agent ("ingested 14 notes from git").
func (w *Wiki) Log(note string) error {
	if strings.TrimSpace(note) == "" {
		return errors.New("empty log note")
	}
	if LooksSecret(note) {
		return errors.New("refused: the note looks like it contains a secret")
	}
	if err := w.appendLog("note", "", note); err != nil {
		return err
	}
	return w.commit("wiki: log note")
}

// commit records the wiki state in its own git repository. The wiki may sit inside
// another repo; it still gets its own history so approvals are auditable.
func (w *Wiki) commit(msg string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return nil // git is optional; log.md still records everything
	}
	if _, err := os.Stat(w.path(".git")); errors.Is(err, os.ErrNotExist) {
		if out, err := w.git("init", "-q"); err != nil {
			return fmt.Errorf("git init: %v: %s", err, out)
		}
	}
	if out, err := w.git("add", "-A", "."); err != nil {
		return fmt.Errorf("git add: %v: %s", err, out)
	}
	// Nothing staged → nothing to commit.
	if _, err := w.git("diff", "--cached", "--quiet"); err == nil {
		return nil
	}
	args := []string{"commit", "-q", "-m", msg}
	// Use the person's own git identity when they have one; never invent theirs.
	if out, _ := w.git("config", "user.email"); strings.TrimSpace(out) == "" {
		args = append([]string{"-c", "user.name=hpk", "-c", "user.email=hpk@localhost"}, args...)
	}
	if out, err := w.git(args...); err != nil {
		return fmt.Errorf("git commit: %v: %s", err, out)
	}
	return nil
}

func (w *Wiki) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", w.Dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// WriteRaw stores untouched source material under raw/ (no draft needed). hpk
// writes it for the agent, because the wiki sits inside the Hermes home, which
// Hermes guards. The name stays inside raw/; secrets are refused.
func (w *Wiki) WriteRaw(name string, body []byte) (string, error) {
	clean := path.Clean("/" + strings.ReplaceAll(name, `\`, "/"))[1:]
	if clean == "" || strings.HasPrefix(clean, "..") || !strings.HasSuffix(strings.ToLower(clean), ".md") {
		return "", fmt.Errorf("raw page name %q: give a relative name ending in .md", name)
	}
	if len(body) > 256<<10 {
		return "", errors.New("raw material over 256 KB; keep extracts short")
	}
	if LooksSecret(string(body)) {
		return "", errors.New("refused: the material looks like it contains a secret")
	}
	dst := filepath.Join(w.Dir, "raw", filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	return dst, os.WriteFile(dst, body, 0o644)
}
