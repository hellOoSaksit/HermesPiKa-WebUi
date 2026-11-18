// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package onboard is the add-project flow (docs/DESIGN.md §8). hpk does the
// zero-token steps itself (consent, scan, estimate, project.yaml, git history,
// queue, health); the reading and writing that needs a model is done by Hermes
// through the hpk-project skill, one queued source at a time, under a token budget.
package onboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/scan"
	"hpk.local/hpk/internal/wiki"
)

// Depths the person can choose at consent (§8 step 1).
const (
	Full  = "full"  // every knowledge source except agent-control files
	Light = "light" // README and docs only
	Skip  = "skip"  // set the project up, read nothing
)

// Options for Add.
type Options struct {
	Root     string
	Name     string
	Depth    string
	Privacy  string
	Language string
	Exclude  []string // globs the person cut at the estimate step
	Budget   int      // ingest token cap; 0 = default
	Price    float64  // USD per 1M input tokens for the estimate (illustrative)
}

// Defaults.
const (
	DefaultBudget = 400_000
	DefaultPrice  = 0.8
	outputRatio   = 0.15 // facts written back, as a share of tokens read
)

// Item is one source in the ingest queue.
type Item struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Tokens int    `json:"tokens"`
	Hash   string `json:"hash"`
	Status string `json:"status"` // pending | done | skipped
	Raw    string `json:"raw"`    // where the extracted facts go, relative to the wiki
}

// State is <project>/state.json: where the flow stands and what was read (§5.1, §8 step 10).
type State struct {
	Version    int       `json:"version"`
	Step       string    `json:"step"` // ingest | organise | health | done
	Depth      string    `json:"depth"`
	Budget     int       `json:"budget"`
	UsedTokens int       `json:"used_tokens"`
	Price      float64   `json:"price_per_mtok"`
	LastCommit string    `json:"last_commit,omitempty"`
	Created    time.Time `json:"created"`
	Queue      []Item    `json:"queue"`
}

// Estimate is what the person sees before anything is read with a model.
type Estimate struct {
	Sources  []Item   `json:"sources"`
	Tokens   int      `json:"tokens"`
	CostUSD  float64  `json:"cost_usd"`
	Budget   int      `json:"budget"`
	OverBy   int      `json:"over_by,omitempty"`
	Approx   bool     `json:"approx"` // true before consent: sizes only, nothing read
	Skipped  int      `json:"secrets_skipped"`
	Excluded []string `json:"excluded,omitempty"`
}

// Pick chooses the sources for a depth, minus the person's exclusions.
// Agent-control files (AGENTS.md, CLAUDE.md …) are rules, not knowledge: never ingested.
func Pick(r scan.Result, depth string, exclude []string) (picked []scan.Source, excluded []string) {
	if depth == Skip {
		return nil, nil
	}
	for _, s := range r.Sources {
		if s.Kind == "control" || (depth == Light && !s.Light) {
			continue
		}
		if matchAny(s.Path, exclude) {
			excluded = append(excluded, s.Path)
			continue
		}
		picked = append(picked, s)
	}
	return picked, excluded
}

// Plan builds the estimate. With read=false it uses file sizes only (before consent);
// with read=true it reads the picked files and counts tokens properly.
func Plan(r scan.Result, depth string, exclude []string, budget int, price float64, read bool) (Estimate, error) {
	if budget <= 0 {
		budget = DefaultBudget
	}
	if price <= 0 {
		price = DefaultPrice
	}
	picked, excluded := Pick(r, depth, exclude)
	e := Estimate{Budget: budget, Approx: !read, Skipped: len(r.Skipped), Excluded: excluded}
	for _, s := range picked {
		it := Item{Path: s.Path, Kind: s.Kind, Tokens: s.Tokens, Status: "pending", Raw: rawName(s.Path)}
		if read {
			raw, err := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(s.Path)))
			if err != nil {
				return e, err
			}
			it.Tokens = wiki.Tokens(string(raw))
			sum := sha256.Sum256(raw)
			it.Hash = hex.EncodeToString(sum[:8])
		}
		e.Sources = append(e.Sources, it)
		e.Tokens += it.Tokens
	}
	e.CostUSD = float64(e.Tokens) * (1 + outputRatio) / 1e6 * price
	if e.Tokens > budget {
		e.OverBy = e.Tokens - budget
	}
	return e, nil
}

// Add runs steps 2–4 and 9 after the person consented: project.yaml, wiki,
// git history into raw/, and the ingest queue. It refuses a plan over budget.
func Add(home string, o Options, r scan.Result) (*project.Project, Estimate, error) {
	if o.Depth != Full && o.Depth != Light && o.Depth != Skip {
		return nil, Estimate{}, fmt.Errorf("depth must be full, light or skip, not %q", o.Depth)
	}
	if o.Name == "" {
		o.Name = nameFrom(r.Root)
	}
	if o.Privacy == "" {
		o.Privacy = project.Shared
	}
	if o.Language == "" {
		o.Language = "en"
	}
	est, err := Plan(r, o.Depth, o.Exclude, o.Budget, o.Price, true)
	if err != nil {
		return nil, est, err
	}
	if est.OverBy > 0 {
		return nil, est, fmt.Errorf("the picked sources are about %d tokens, %d over the budget of %d. Cut some with --exclude, choose --depth light, or raise --budget", est.Tokens, est.OverBy, est.Budget)
	}

	var f project.File
	f.Name, f.Root, f.Language = o.Name, r.Root, o.Language
	f.Privacy.Mode, f.Privacy.ModelPolicy = o.Privacy, "any"
	if r.BoardFile != "" {
		f.Board.Kind, f.Board.File = "markdown", r.BoardFile
	} else {
		f.Board.Kind = "hpk"
	}
	f.Board.StaleAfterMinutes = 30
	f.Worktrees.Dir = "worktrees/"
	f.Gates.DefaultMode = "ask"
	f.Checks.Test = r.TestCmd
	f.Ingest.Depth = o.Depth
	f.Ingest.Include = includeFor(o.Depth)
	f.Ingest.Exclude = append([]string{"**/.env*", "**/secrets/**", "node_modules/**"}, o.Exclude...)
	f.Budget.IngestMaxTokens = est.Budget
	f.Budget.NightlyMaxTokens = 60_000
	f.Delivery = "console"

	p, err := project.Write(home, f)
	if err != nil {
		return nil, est, err
	}
	w := wiki.Open(p.WikiDir())
	if _, err := w.Init(p.Name, wikiTemplates); err != nil {
		return p, est, err
	}
	if r.Git.IsRepo && o.Depth != Skip {
		if err := writeGitHistory(r.Root, p.WikiDir()); err != nil {
			return p, est, err
		}
	}
	st := State{Version: 1, Step: "ingest", Depth: o.Depth, Budget: est.Budget, Price: priceOr(o.Price),
		LastCommit: r.Git.Head, Created: time.Now().UTC(), Queue: est.Sources}
	if o.Depth == Skip || len(st.Queue) == 0 {
		st.Step = "health"
	}
	if err := SaveState(p, st); err != nil {
		return p, est, err
	}
	return p, est, w.Log(fmt.Sprintf("project added (%s): %d sources queued, about %d tokens", o.Depth, len(st.Queue), est.Tokens))
}

// wikiTemplates is set by the caller (the binary embeds them) to keep this package free of assets.
var wikiTemplates fs.FS

// SetTemplates wires in the embedded wiki templates.
func SetTemplates(t fs.FS) { wikiTemplates = t }

// writeGitHistory puts the last 300 commit subjects in raw/ (0 tokens). No author names.
func writeGitHistory(root, wikiDir string) error {
	out, err := exec.Command("git", "-C", root, "log", "-300", "--date=short", "--format=- %h %ad %s").Output()
	if err != nil {
		return nil // an empty or odd repo is not fatal
	}
	body := "---\ntitle: Git history (last 300 commits)\ntype: raw\nsources: [git log]\nupdated: " + time.Now().Format("2006-01-02") + "\nconfidence: high\n---\n\n" + string(out)
	return os.WriteFile(filepath.Join(wikiDir, "raw", "git-history.md"), []byte(body), 0o644)
}

func includeFor(depth string) []string {
	if depth == Light {
		return []string{"README*", "docs/**"}
	}
	return []string{"README*", "docs/**", "notes/**", "**/adr/**", "CHANGELOG*", "CONTRIBUTING*", "*.md"}
}

// rawName is where facts from a source are written: raw/ingest/<path-with-dashes>.md
func rawName(src string) string {
	s := strings.NewReplacer("/", "--", " ", "-").Replace(strings.TrimSuffix(src, path.Ext(src)))
	return "raw/ingest/" + s + ".md"
}

var nameRE = regexp.MustCompile(`[^a-z0-9._-]+`)

func nameFrom(root string) string {
	n := strings.Trim(nameRE.ReplaceAllString(strings.ToLower(filepath.Base(root)), "-"), "-.")
	if n == "" {
		n = "project"
	}
	return n
}

func priceOr(p float64) float64 {
	if p <= 0 {
		return DefaultPrice
	}
	return p
}

// matchAny supports simple globs: *, ** and plain prefixes like "docs/old/".
func matchAny(p string, globs []string) bool {
	for _, g := range globs {
		g = strings.TrimSpace(filepath.ToSlash(g))
		if g == "" {
			continue
		}
		if strings.HasSuffix(g, "/") && strings.HasPrefix(p, g) {
			return true
		}
		if re, err := globRE(g); err == nil && re.MatchString(p) {
			return true
		}
	}
	return false
}

func globRE(g string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(g); i++ {
		switch c := g[i]; c {
		case '*':
			if i+1 < len(g) && g[i+1] == '*' {
				b.WriteString(".*")
				i++
				if i+1 < len(g) && g[i+1] == '/' {
					i++
					b.WriteString("/?")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// ---- state.json and the ingest queue ----

func statePath(p *project.Project) string { return filepath.Join(p.Dir, "state.json") }

// LoadState reads state.json.
func LoadState(p *project.Project) (State, error) {
	raw, err := os.ReadFile(statePath(p))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, fmt.Errorf("%s has no add-project state; run `hpk project add` first", p.Name)
		}
		return State{}, err
	}
	var st State
	return st, json.Unmarshal(raw, &st)
}

// SaveState writes state.json atomically.
func SaveState(p *project.Project, st State) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := statePath(p) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, statePath(p))
}

// ErrBudget means the next source would cross the ingest budget: stop and ask (§13 #11).
var ErrBudget = errors.New("the ingest budget is used up; a person must raise budget.ingest_max_tokens or skip sources")

// Next returns the next pending source, or nil when the queue is empty.
func Next(p *project.Project) (*Item, State, error) {
	st, err := LoadState(p)
	if err != nil {
		return nil, st, err
	}
	for i := range st.Queue {
		it := &st.Queue[i]
		if it.Status != "pending" {
			continue
		}
		if st.UsedTokens+it.Tokens > st.Budget {
			return it, st, ErrBudget
		}
		return it, st, nil
	}
	return nil, st, nil
}

// Done marks a source as read; tokens are what it really cost, or the estimate.
func Done(p *project.Project, src string, tokens int, skipped bool) (State, error) {
	st, err := LoadState(p)
	if err != nil {
		return st, err
	}
	for i := range st.Queue {
		if st.Queue[i].Path != src {
			continue
		}
		if st.Queue[i].Status != "pending" {
			return st, fmt.Errorf("%s is already %s", src, st.Queue[i].Status)
		}
		if skipped {
			st.Queue[i].Status = "skipped"
		} else {
			if tokens <= 0 {
				tokens = st.Queue[i].Tokens
			}
			st.Queue[i].Status = "done"
			st.UsedTokens += tokens
		}
		if pendingCount(st) == 0 && st.Step == "ingest" {
			st.Step = "organise"
		}
		return st, SaveState(p, st)
	}
	return st, fmt.Errorf("%s is not in the ingest queue", src)
}

// Advance moves the flow to its next named step (organise → health → done).
func Advance(p *project.Project, to string) (State, error) {
	st, err := LoadState(p)
	if err != nil {
		return st, err
	}
	order := map[string]int{"ingest": 0, "organise": 1, "health": 2, "done": 3}
	if _, ok := order[to]; !ok {
		return st, fmt.Errorf("unknown step %q", to)
	}
	if to == "organise" && pendingCount(st) > 0 {
		return st, fmt.Errorf("%d sources are still pending", pendingCount(st))
	}
	st.Step = to
	return st, SaveState(p, st)
}

func pendingCount(st State) int {
	n := 0
	for _, it := range st.Queue {
		if it.Status == "pending" {
			n++
		}
	}
	return n
}

// Changed lists queued sources whose content differs from what was ingested (for cron).
func Changed(p *project.Project) ([]string, error) {
	st, err := LoadState(p)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, it := range st.Queue {
		raw, err := os.ReadFile(filepath.Join(p.Root, filepath.FromSlash(it.Path)))
		if err != nil {
			out = append(out, it.Path+" (gone)")
			continue
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:8]) != it.Hash {
			out = append(out, it.Path)
		}
	}
	sort.Strings(out)
	return out, nil
}

// MaxNotes caps what an agent may hand hpk for one raw page.
const MaxNotes = 64 << 10

// WriteRaw writes the facts an agent found in one queued source to that source's
// raw page. hpk writes it, not the agent: the wiki sits inside the Hermes home,
// which Hermes guards, and the same checks apply to every note (size, secrets,
// front matter naming its source).
func WriteRaw(p *project.Project, src string, notes []byte) (string, error) {
	st, err := LoadState(p)
	if err != nil {
		return "", err
	}
	var it *Item
	for i := range st.Queue {
		if st.Queue[i].Path == src {
			it = &st.Queue[i]
		}
	}
	if it == nil {
		return "", fmt.Errorf("%s is not in the ingest queue", src)
	}
	if len(notes) > MaxNotes {
		return "", fmt.Errorf("notes for %s are over %d KB; keep raw notes short", src, MaxNotes>>10)
	}
	body := strings.TrimSpace(strings.ReplaceAll(string(notes), "\r\n", "\n"))
	if body == "" {
		return "", fmt.Errorf("no notes given for %s", src)
	}
	if wiki.LooksSecret(body) {
		return "", fmt.Errorf("the notes for %s look like they contain a secret; nothing was written", src)
	}
	if !strings.HasPrefix(body, "---") {
		body = fmt.Sprintf("---\ntitle: %s\ntype: raw\nsources: [%s]\nupdated: %s\n---\n\n%s", src, src, time.Now().Format("2006-01-02"), body)
	}
	dst := filepath.Join(p.WikiDir(), filepath.FromSlash(it.Raw))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	return dst, os.WriteFile(dst, []byte(body+"\n"), 0o644)
}
