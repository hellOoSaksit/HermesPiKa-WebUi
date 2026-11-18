// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package wiki

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DraftInput is what an agent (or a person) proposes.
type DraftInput struct {
	Target     string   // e.g. lessons/csv-encoding.md
	Title      string   // defaults to the existing page's title on an edit
	Type       string   // defaults to the area name
	Sources    []string // required: file:line, commit, or session id
	Summary    string   // one line: what this adds or changes
	Confidence string   // low | medium | high (default medium)
	Author     string
	Body       string
	Origin     string // set by promotion only
	Promoted   bool   // set by promotion only
}

// Draft is a draft on disk.
type Draft struct {
	N    int    `json:"n"`
	File string `json:"file"` // relative to the wiki
	Meta Meta   `json:"meta"`
	Body string `json:"-"`
}

var draftName = regexp.MustCompile(`^(\d+)-.*\.md$`)

// CreateDraft checks the proposal and writes _drafts/<n>-<slug>.md.
func (w *Wiki) CreateDraft(in DraftInput) (Draft, error) {
	if !w.Exists() {
		return Draft{}, errors.New("no wiki here; run `hpk wiki init` first")
	}
	target, area, err := cleanTarget(in.Target)
	if err != nil {
		return Draft{}, err
	}
	if area.Free {
		return Draft{}, errors.New("raw/ is written directly, not through drafts")
	}
	var sources []string
	for _, s := range in.Sources {
		if s = strings.TrimSpace(s); s != "" {
			sources = append(sources, s)
		}
	}
	if len(sources) == 0 {
		return Draft{}, errors.New("a draft needs at least one source (file:line, commit or session id) — no source, no page")
	}
	body := strings.TrimSpace(strings.TrimPrefix(in.Body, "\ufeff"))
	if body == "" {
		return Draft{}, errors.New("the draft is empty")
	}
	if LooksSecret(body) || LooksSecret(in.Summary) || LooksSecret(in.Title) {
		return Draft{}, errors.New("refused: the draft looks like it contains a secret (key, password or token). Secrets never go to the wiki")
	}
	if t := Tokens(body); t > PageTokenLimit {
		return Draft{}, fmt.Errorf("the page is about %d tokens; the limit is %d. Split it into smaller pages", t, PageTokenLimit)
	}
	kind := "new"
	title := strings.TrimSpace(in.Title)
	if raw, err := os.ReadFile(w.path(target)); err == nil {
		kind = "edit"
		if old, err := Parse(raw); err == nil && title == "" {
			title = old.Meta.Title
		}
	}
	if title == "" {
		return Draft{}, errors.New("a new page needs a title")
	}
	typ := in.Type
	if typ == "" {
		typ = strings.TrimSuffix(strings.SplitN(target, "/", 2)[0], "s")
	}
	conf := in.Confidence
	if conf == "" {
		conf = "medium"
	}
	if conf != "low" && conf != "medium" && conf != "high" {
		return Draft{}, fmt.Errorf("confidence must be low, medium or high, not %q", conf)
	}
	now := w.Now()
	n, err := w.nextDraftNumber()
	if err != nil {
		return Draft{}, err
	}
	d := Draft{
		N:    n,
		File: fmt.Sprintf("_drafts/%d-%s.md", n, slug(title)),
		Meta: Meta{Title: title, Type: typ, Sources: sources, Updated: now.Format("2006-01-02"), Confidence: conf,
			Target: target, Kind: kind, Summary: strings.TrimSpace(in.Summary), Author: in.Author, Created: now.UTC().Format(time.RFC3339),
			Origin: in.Origin, Promoted: in.Promoted},
		Body: body,
	}
	if err := os.WriteFile(w.path(d.File), Format(Page{Meta: d.Meta, Body: body}), 0o644); err != nil {
		return Draft{}, err
	}
	if err := w.appendLog("draft", target, fmt.Sprintf("draft %d by %s: %s", n, orDash(in.Author), orDash(d.Meta.Summary))); err != nil {
		return d, err
	}
	return d, w.commit(fmt.Sprintf("wiki: draft %d %s", n, target))
}

// nextDraftNumber never reuses a number, even after approve or discard.
func (w *Wiki) nextDraftNumber() (int, error) {
	max := 0
	for _, dir := range []string{"_drafts", "_drafts/stale", "_drafts/discarded"} {
		entries, err := os.ReadDir(w.path(dir))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
		for _, e := range entries {
			if m := draftName.FindStringSubmatch(e.Name()); m != nil {
				if n, _ := strconv.Atoi(m[1]); n > max {
					max = n
				}
			}
		}
	}
	// Numbers of approved drafts are only in the log; read them so they are not reused.
	if raw, err := os.ReadFile(w.path("log.md")); err == nil {
		for _, m := range regexp.MustCompile(`\bdraft (\d+)\b`).FindAllStringSubmatch(string(raw), -1) {
			if n, _ := strconv.Atoi(m[1]); n > max {
				max = n
			}
		}
	}
	return max + 1, nil
}

// Drafts lists pending drafts, oldest number first.
func (w *Wiki) Drafts() ([]Draft, error) {
	entries, err := os.ReadDir(w.path("_drafts"))
	if err != nil {
		return nil, err
	}
	var out []Draft
	for _, e := range entries {
		m := draftName.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			continue
		}
		rel := "_drafts/" + e.Name()
		raw, err := os.ReadFile(w.path(rel))
		if err != nil {
			return nil, err
		}
		p, err := Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		n, _ := strconv.Atoi(m[1])
		out = append(out, Draft{N: n, File: rel, Meta: p.Meta, Body: p.Body})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N < out[j].N })
	return out, nil
}

// Decision on a batch of drafts.
type Result struct {
	N      int    `json:"n"`
	Target string `json:"target,omitempty"`
	Done   string `json:"done,omitempty"`  // approved | discarded
	Error  string `json:"error,omitempty"` // why this one was not done
}

// Approve moves drafts to their pages, updates index.md and log.md, and commits once.
// by is who approved: a person's name, or "auto" for Auto mode, which may only
// approve areas §7.3 allows.
func (w *Wiki) Approve(nums []int, by string) ([]Result, error) {
	drafts, err := w.Drafts()
	if err != nil {
		return nil, err
	}
	byN := map[int]Draft{}
	for _, d := range drafts {
		byN[d.N] = d
	}
	var res []Result
	changed := false
	for _, n := range nums {
		d, ok := byN[n]
		if !ok {
			res = append(res, Result{N: n, Error: "no pending draft with this number"})
			continue
		}
		target, area, err := cleanTarget(d.Meta.Target)
		if err != nil {
			res = append(res, Result{N: n, Error: err.Error()})
			continue
		}
		if by == "auto" && d.Meta.Promoted {
			res = append(res, Result{N: n, Target: target, Error: "promotion to the shared wiki is a red line; a person must approve"})
			continue
		}
		if by == "auto" && !area.AutoOK {
			res = append(res, Result{N: n, Target: target, Error: "Auto cannot approve " + strings.SplitN(target, "/", 2)[0] + "/; a person must"})
			continue
		}
		page := Page{Meta: Meta{Title: d.Meta.Title, Type: d.Meta.Type, Sources: d.Meta.Sources, Updated: w.Now().Format("2006-01-02"), Confidence: d.Meta.Confidence, Origin: d.Meta.Origin}, Body: d.Body}
		if err := os.MkdirAll(filepath.Dir(w.path(target)), 0o755); err != nil {
			return res, err
		}
		if err := os.WriteFile(w.path(target), Format(page), 0o644); err != nil {
			return res, err
		}
		if err := os.Remove(w.path(d.File)); err != nil {
			return res, err
		}
		if err := w.setIndexLine(target, page.Meta.Title, d.Meta.Summary); err != nil {
			return res, err
		}
		if err := w.appendLog("approve", target, fmt.Sprintf("draft %d (%s) approved by %s", n, d.Meta.Kind, orDash(by))); err != nil {
			return res, err
		}
		res = append(res, Result{N: n, Target: target, Done: "approved"})
		changed = true
	}
	if changed {
		return res, w.commit("wiki: approve " + numList(res, "approved") + " by " + orDash(by))
	}
	return res, nil
}

// Discard moves drafts to _drafts/discarded/ (nothing is deleted) and logs why.
func (w *Wiki) Discard(nums []int, by, why string) ([]Result, error) {
	drafts, err := w.Drafts()
	if err != nil {
		return nil, err
	}
	byN := map[int]Draft{}
	for _, d := range drafts {
		byN[d.N] = d
	}
	var res []Result
	for _, n := range nums {
		d, ok := byN[n]
		if !ok {
			res = append(res, Result{N: n, Error: "no pending draft with this number"})
			continue
		}
		dst := "_drafts/discarded/" + filepath.Base(d.File)
		if err := os.Rename(w.path(d.File), w.path(dst)); err != nil {
			return res, err
		}
		if err := w.appendLog("discard", d.Meta.Target, fmt.Sprintf("draft %d discarded by %s: %s", n, orDash(by), orDash(why))); err != nil {
			return res, err
		}
		res = append(res, Result{N: n, Target: d.Meta.Target, Done: "discarded"})
	}
	if len(res) > 0 {
		return res, w.commit("wiki: discard " + numList(res, "discarded"))
	}
	return res, nil
}

// MoveStale moves drafts older than 14 days to _drafts/stale/ (§7.4.5).
func (w *Wiki) MoveStale() ([]int, error) {
	drafts, err := w.Drafts()
	if err != nil {
		return nil, err
	}
	var moved []int
	for _, d := range drafts {
		created, err := time.Parse(time.RFC3339, d.Meta.Created)
		if err != nil || w.Now().Sub(created) < StaleAfter {
			continue
		}
		if err := os.Rename(w.path(d.File), w.path("_drafts/stale/"+filepath.Base(d.File))); err != nil {
			return moved, err
		}
		if err := w.appendLog("stale", d.Meta.Target, fmt.Sprintf("draft %d moved to stale after 14 days", d.N)); err != nil {
			return moved, err
		}
		moved = append(moved, d.N)
	}
	if len(moved) > 0 {
		return moved, w.commit(fmt.Sprintf("wiki: %d stale drafts", len(moved)))
	}
	return moved, nil
}

// ParseNums reads "1,3" or "2-4,7" into numbers.
func ParseNums(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if a, b, ok := strings.Cut(part, "-"); ok {
			x, err1 := strconv.Atoi(strings.TrimSpace(a))
			y, err2 := strconv.Atoi(strings.TrimSpace(b))
			if err1 != nil || err2 != nil || y < x || y-x > 500 {
				return nil, fmt.Errorf("bad range %q", part)
			}
			for i := x; i <= y; i++ {
				out = append(out, i)
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("bad number %q", part)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, errors.New("no draft numbers given")
	}
	return out, nil
}

func numList(res []Result, done string) string {
	var ns []string
	for _, r := range res {
		if r.Done == done {
			ns = append(ns, strconv.Itoa(r.N))
		}
	}
	return strings.Join(ns, ",")
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// EditDraftBody replaces a draft's body and keeps its front matter, so a person can
// fix a redacted promotion before approving it. Secrets are still refused.
func (w *Wiki) EditDraftBody(n int, body, by string) error {
	ds, err := w.Drafts()
	if err != nil {
		return err
	}
	for _, d := range ds {
		if d.N != n {
			continue
		}
		if LooksSecret(body) {
			return errors.New("refused: the text looks like it contains a secret")
		}
		raw, err := os.ReadFile(w.path(d.File))
		if err != nil {
			return err
		}
		s := strings.ReplaceAll(string(raw), "\r\n", "\n")
		end := strings.Index(s[3:], "\n---\n")
		if !strings.HasPrefix(s, "---") || end < 0 {
			return fmt.Errorf("draft %d has no front matter", n)
		}
		head := s[:3+end+5]
		if err := os.WriteFile(w.path(d.File), []byte(head+"\n"+strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))+"\n"), 0o644); err != nil {
			return err
		}
		return w.Log(fmt.Sprintf("draft %d edited by %s", n, by))
	}
	return fmt.Errorf("no draft %d", n)
}

// CoreTarget reports whether a draft target is in an area only a person with more
// say may approve (decisions/, architecture/, people/ …): the areas Auto cannot.
func CoreTarget(target string) bool {
	_, area, err := cleanTarget(target)
	return err != nil || !area.AutoOK
}
