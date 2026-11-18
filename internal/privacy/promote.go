// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package privacy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

// Origin is an anonymous, stable id for a project, so a promoted page can be traced
// back (and removed) without naming the project (§6.2.4).
func Origin(p *project.Project) string {
	sum := sha256.Sum256([]byte("hpk-origin:" + p.Name))
	return "project-" + hex.EncodeToString(sum[:])[:10]
}

var (
	urlRE   = regexp.MustCompile(`https?://[^\s)>\]]+`)
	emailRE = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	// Absolute or home-relative paths on any OS; relative project paths like src/x.go too.
	winPathRE  = regexp.MustCompile(`[A-Za-z]:\\[^\s"'<>|]+`)
	unixPathRE = regexp.MustCompile(`(^|[\s("'])(?:~|/)(?:[\w.-]+/)+[\w.-]*`)
	relPathRE  = regexp.MustCompile(`\b(?:[\w.-]+/)+[\w.-]+\.[A-Za-z0-9]{1,6}\b`)
)

// Redact removes what ties text to a project: its name, people and customers named
// in its glossary and people pages, URLs, e-mails and file paths. It errs on the side
// of removing too much; a person reviews the draft anyway.
func Redact(text string, names []string) string {
	text = urlRE.ReplaceAllString(text, "{url}")
	text = emailRE.ReplaceAllString(text, "{email}")
	text = winPathRE.ReplaceAllString(text, "{path}")
	text = unixPathRE.ReplaceAllString(text, "${1}{path}")
	text = relPathRE.ReplaceAllString(text, "{path}")
	for _, n := range names {
		n = strings.TrimSpace(n)
		if len([]rune(n)) < 3 {
			continue
		}
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(n))
		text = re.ReplaceAllString(text, "{name}")
	}
	return text
}

// sensitiveNames collects the project name and titles of glossary and people pages.
func sensitiveNames(p *project.Project) []string {
	names := []string{p.Name, filepath.Base(p.Root)}
	for _, area := range []string{"glossary", "people"} {
		dir := filepath.Join(p.WikiDir(), area)
		_ = filepath.WalkDir(dir, func(f string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(f, ".md") {
				return nil
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				return nil
			}
			if pg, err := wiki.Parse(raw); err == nil && pg.Meta.Title != "" {
				names = append(names, pg.Meta.Title)
			}
			names = append(names, strings.TrimSuffix(filepath.Base(f), ".md"))
			return nil
		})
	}
	return names
}

// Promote turns one page of a shared project into a redacted draft in the shared
// wiki. It never approves: promotion is a red line, a person decides each one.
// to is "team" or "users/<id>".
func Promote(home string, p *project.Project, page, to string, templates fs.FS) (wiki.Draft, error) {
	if p.Mode() != project.Shared {
		return wiki.Draft{}, fmt.Errorf("%s is %s: nothing from it may go to the shared wiki", p.Name, p.Mode())
	}
	page = path.Clean(filepath.ToSlash(page))
	if !strings.HasSuffix(page, ".md") {
		page += ".md"
	}
	if strings.HasPrefix(page, "../") || strings.HasPrefix(page, "_drafts/") || strings.HasPrefix(page, "raw/") {
		return wiki.Draft{}, errors.New("promote an approved page, not a draft or raw material")
	}
	raw, err := os.ReadFile(filepath.Join(p.WikiDir(), filepath.FromSlash(page)))
	if err != nil {
		return wiki.Draft{}, fmt.Errorf("page %s: %w", page, err)
	}
	pg, err := wiki.Parse(raw)
	if err != nil {
		return wiki.Draft{}, err
	}
	shared, err := EnsureShared(home, to, templates)
	if err != nil {
		return wiki.Draft{}, err
	}
	names := sensitiveNames(p)
	origin := Origin(p)
	area := strings.SplitN(page, "/", 2)[0]
	if area != "lessons" && area != "glossary" {
		area = "lessons" // the shared wiki keeps general lessons; decisions stay with their project
	}
	title := Redact(pg.Meta.Title, names)
	return shared.CreateDraft(wiki.DraftInput{
		Target:     area + "/" + origin + "-" + strings.TrimSuffix(path.Base(page), ".md") + ".md",
		Title:      title,
		Sources:    []string{"promoted from " + origin},
		Summary:    "promoted from " + origin + "; names, paths, URLs and e-mails removed",
		Confidence: pg.Meta.Confidence,
		Author:     "hpk promote",
		Body:       Redact(pg.Body, names),
		Origin:     origin,
		Promoted:   true,
	})
}

func sharedWiki(home, to string) (*wiki.Wiki, error) {
	var dir string
	switch {
	case to == "" || to == "team":
		dir = filepath.Join(project.SharedDir(home), "team")
	case strings.HasPrefix(to, "users/") && regexp.MustCompile(`^users/[a-z0-9._-]{1,64}$`).MatchString(to):
		dir = filepath.Join(project.SharedDir(home), filepath.FromSlash(to))
	default:
		return nil, fmt.Errorf("--to must be team or users/<id>, not %q", to)
	}
	return wiki.Open(dir), nil
}

// EnsureShared initialises a shared wiki the first time something is promoted to it.
func EnsureShared(home, to string, templates fs.FS) (*wiki.Wiki, error) {
	w, err := sharedWiki(home, to)
	if err != nil {
		return nil, err
	}
	if !w.Exists() {
		if _, err := w.Init("shared "+strings.TrimPrefix(to, "users/"), templates); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// UserTarget is the personal shared wiki of a person (users/<slug>), usable as
// Promote's target. Any id becomes a safe folder name: lowercase, and characters
// other than letters, digits, . _ - become "-".
func UserTarget(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(id)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), ".-")
	if len(s) > 64 {
		s = s[:64]
	}
	if s == "" {
		s = "me"
	}
	return "users/" + s
}

// SharedWikiDir is where a promotion target lives (team or users/<slug>).
func SharedWikiDir(home, to string) (string, error) {
	w, err := sharedWiki(home, to)
	if err != nil {
		return "", err
	}
	return w.Dir, nil
}
