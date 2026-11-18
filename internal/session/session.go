// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package session keeps hpk's own record of main chats and their sub-tasks
// (docs/DESIGN.md §9–10): lineage numbers, titles from the project template,
// worktrees, briefs and the fixed-format report a sub-task sends back.
// Hermes' ACP cannot record a parent (docs/m0-findings.md), so hpk does, in board.db.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"hpk.local/hpk/internal/board"
	"hpk.local/hpk/internal/project"
)

// Statuses (the Console shows the same set).
const (
	Queued  = "queued"
	Running = "running"
	Waiting = "waiting" // held for a person
	Done    = "done"
	Failed  = "failed"
	Stopped = "stopped"
)

// Session is one main chat or sub-task.
type Session struct {
	ID       string    `json:"id"`
	Num      string    `json:"num"` // lineage: 1, 1.2, 1.2.1
	Parent   string    `json:"parent,omitempty"`
	Title    string    `json:"title"`
	Name     string    `json:"name"`
	Lane     string    `json:"lane,omitempty"`
	Status   string    `json:"status"`
	HermesID string    `json:"hermes_id,omitempty"` // the ACP / Hermes session id
	Worktree string    `json:"worktree,omitempty"`
	Branch   string    `json:"branch,omitempty"`
	Cwd      string    `json:"cwd"`
	Brief    string    `json:"brief,omitempty"`
	Owner    string    `json:"owner,omitempty"`
	Shared   []string  `json:"shared_with,omitempty"` // team members who may read this chat
	Reported bool      `json:"reported,omitempty"`
	Model    string    `json:"model,omitempty"`    // the model hpk last set (provider:model)
	Switches []Switch  `json:"switches,omitempty"` // model changes in this chat, oldest first
	AskOnce  bool      `json:"ask_once,omitempty"` // a one-question sub-task on another model
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

type file struct {
	Sessions []*Session `json:"sessions"`
}

var mu sync.Mutex // in-process; across processes SQLite (WAL + busy timeout) serialises writers

// legacyPath is the M5 store, imported into board.db once.
func legacyPath(p *project.Project) string { return filepath.Join(p.Dir, "sessions.json") }

func load(p *project.Project) (*file, error) {
	db, err := board.Open(p)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := migrate(p, db); err != nil {
		return nil, err
	}
	rows, err := db.SQL().Query(`SELECT data FROM sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	f := &file{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var s Session
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, err
		}
		f.Sessions = append(f.Sessions, &s)
	}
	return f, rows.Err()
}

func save(p *project.Project, f *file) error {
	db, err := board.Open(p)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.SQL().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, s := range f.Sessions {
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO sessions(id,data) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, s.ID, string(raw)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// migrate imports sessions.json from M5 the first time board.db is used.
func migrate(p *project.Project, db *board.DB) error {
	raw, err := os.ReadFile(legacyPath(p))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("sessions.json: %w", err)
	}
	for _, s := range f.Sessions {
		data, _ := json.Marshal(s)
		if _, err := db.SQL().Exec(`INSERT INTO sessions(id,data) VALUES(?,?) ON CONFLICT(id) DO NOTHING`, s.ID, string(data)); err != nil {
			return err
		}
	}
	return os.Rename(legacyPath(p), legacyPath(p)+".migrated")
}

// List returns all sessions in lineage order (1, 1.1, 1.1.1, 1.2, 2 …).
func List(p *project.Project) ([]*Session, error) {
	f, err := load(p)
	if err != nil {
		return nil, err
	}
	sort.Slice(f.Sessions, func(i, j int) bool { return lineageLess(f.Sessions[i].Num, f.Sessions[j].Num) })
	return f.Sessions, nil
}

// Get finds a session by hpk id, lineage number or Hermes id.
func Get(p *project.Project, ref string) (*Session, error) {
	list, err := List(p)
	if err != nil {
		return nil, err
	}
	for _, s := range list {
		if s.ID == ref || s.Num == ref || (s.HermesID != "" && s.HermesID == ref) {
			return s, nil
		}
	}
	return nil, fmt.Errorf("no session %q in %s", ref, p.Name)
}

// Settings from project.yaml that sessions use (§5.3).
type Settings struct {
	TitleTemplate string   `yaml:"title_template"`
	AskOnStart    []string `yaml:"ask_on_start"`
	MaxChildren   int      `yaml:"max_children"`
}

func settings(p *project.Project) Settings {
	s := Settings{TitleTemplate: "{name}", MaxChildren: 4}
	raw, err := os.ReadFile(filepath.Join(p.Dir, "project.yaml"))
	if err != nil {
		return s
	}
	var y struct {
		Session Settings `yaml:"session"`
	}
	if yaml.Unmarshal(raw, &y) == nil {
		if y.Session.TitleTemplate != "" {
			s.TitleTemplate = y.Session.TitleTemplate
		}
		s.AskOnStart = y.Session.AskOnStart
		if y.Session.MaxChildren > 0 {
			s.MaxChildren = y.Session.MaxChildren
		}
	}
	return s
}

// NewOptions for a main chat or sub-task.
type NewOptions struct {
	Parent   string // hpk id or lineage number; empty for a main chat
	Name     string // task name, e.g. "auth › api-tests"
	Lane     string
	Vars     map[string]string // template values, e.g. version=1.4.0
	Worktree bool              // sub-task gets its own git worktree (default for sub-tasks)
	Task     string            // what to do (goes into the brief)
	Read     []string          // files or wiki pages to read first
	Owner    string
	Now      time.Time
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

// Title renders the project's title template. A sub-task is named after its main
// chat plus its own name (§10): "Version 1.4.0 release › auth › api-tests".
func Title(p *project.Project, parent *Session, name string, vars map[string]string) (string, error) {
	if parent != nil {
		return parent.Title + " › " + name, nil
	}
	st := settings(p)
	v := map[string]string{"name": name}
	for k, x := range vars {
		v[k] = x
	}
	var missing []string
	out := placeholder.ReplaceAllStringFunc(st.TitleTemplate, func(m string) string {
		k := m[1 : len(m)-1]
		if x, ok := v[k]; ok && x != "" {
			return x
		}
		missing = append(missing, k)
		return m
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("the title needs %s; ask the person (session.ask_on_start) and pass --var %s=…", strings.Join(missing, ", "), missing[0])
	}
	return strings.Join(strings.Fields(out), " "), nil
}

// New records a session, makes its worktree and writes its brief. Starting it in
// Hermes (ACP) is a separate step so this stays free of side effects on Hermes.
func New(p *project.Project, o NewOptions) (*Session, error) {
	mu.Lock()
	defer mu.Unlock()
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	o.Name = strings.TrimSpace(o.Name)
	if o.Name == "" {
		return nil, errors.New("a session needs a name")
	}
	f, err := load(p)
	if err != nil {
		return nil, err
	}
	var parent *Session
	if o.Parent != "" {
		for _, s := range f.Sessions {
			if s.ID == o.Parent || s.Num == o.Parent {
				parent = s
			}
		}
		if parent == nil {
			return nil, fmt.Errorf("no parent session %q", o.Parent)
		}
		live := 0
		for _, s := range f.Sessions {
			if s.Parent == parent.ID && (s.Status == Queued || s.Status == Running || s.Status == Waiting) {
				live++
			}
		}
		if max := settings(p).MaxChildren; live >= max {
			return nil, fmt.Errorf("%s already has %d sub-tasks running (limit %d, session.max_children); finish or stop one first", parent.Num, live, max)
		}
	}
	title, err := Title(p, parent, o.Name, o.Vars)
	if err != nil {
		return nil, err
	}
	s := &Session{ID: newID(), Parent: "", Title: title, Name: o.Name, Lane: o.Lane, Status: Queued, Cwd: p.Root, Owner: o.Owner,
		Created: o.Now.UTC(), Updated: o.Now.UTC()}
	if parent != nil {
		s.Parent = parent.ID
		if s.Lane == "" {
			s.Lane = parent.Lane
		}
	}
	s.Num = nextNum(f.Sessions, parent)
	if parent != nil && o.Worktree {
		wt, branch, err := addWorktree(p, s)
		if err != nil {
			return nil, err
		}
		s.Worktree, s.Branch, s.Cwd = wt, branch, wt
	}
	brief, err := writeBrief(p, s, parent, o)
	if err != nil {
		return nil, err
	}
	s.Brief = brief
	f.Sessions = append(f.Sessions, s)
	if err := save(p, f); err != nil {
		return nil, err
	}
	return s, project.Audit(p.Dir, "session new", s.Num+" "+s.Title)
}

func nextNum(all []*Session, parent *Session) string {
	n := 0
	for _, s := range all {
		if (parent == nil && s.Parent == "") || (parent != nil && s.Parent == parent.ID) {
			n++
		}
	}
	if parent == nil {
		return fmt.Sprint(n + 1)
	}
	return fmt.Sprintf("%s.%d", parent.Num, n+1)
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// addWorktree creates <root>/worktrees/<lineage>-<slug> on branch hpk/<lineage>-<slug>.
func addWorktree(p *project.Project, s *Session) (string, string, error) {
	if out, err := exec.Command("git", "-C", p.Root, "rev-parse", "--is-inside-work-tree").Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
		return "", "", fmt.Errorf("%s is not a git repository; a sub-task needs a worktree (or use --no-worktree)", p.Root)
	}
	slug := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(s.Name), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	name := strings.ReplaceAll(s.Num, ".", "-") + "-" + slug
	dir := filepath.Join(p.Root, "worktrees", name)
	branch := "hpk/" + name
	if out, err := exec.Command("git", "-C", p.Root, "worktree", "add", "-q", "-b", branch, dir).CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return dir, branch, nil
}

// Rename changes the title. sync, when given, also renames it in Hermes
// (`hermes sessions rename`, docs/m0-findings.md §4).
func Rename(p *project.Project, ref, title string, sync func(hermesID, title string) error) (*Session, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, errors.New("empty title")
	}
	s, err := update(p, ref, func(s *Session) error { s.Title = title; return nil })
	if err != nil {
		return nil, err
	}
	if sync != nil && s.HermesID != "" {
		if err := sync(s.HermesID, title); err != nil {
			return s, fmt.Errorf("renamed in hpk, but Hermes said: %w", err)
		}
	}
	return s, nil
}

// AutoTitle is what to do with a title Hermes suggests: keep a title that already
// fits the project template (§10, "a name that matches the template is set").
func AutoTitle(p *project.Project, s *Session, suggested string) (string, bool) {
	if s.Title != "" && s.Title != s.Name {
		return s.Title, false
	}
	return suggested, suggested != "" && suggested != s.Title
}

// SetStatus records a status change.
func SetStatus(p *project.Project, ref, status string) (*Session, error) {
	switch status {
	case Queued, Running, Waiting, Done, Failed, Stopped:
	default:
		return nil, fmt.Errorf("unknown status %q", status)
	}
	return update(p, ref, func(s *Session) error { s.Status = status; return nil })
}

// Attach stores the Hermes session id once ACP has opened the session.
func Attach(p *project.Project, ref, hermesID string) (*Session, error) {
	s, err := update(p, ref, func(s *Session) error { s.HermesID, s.Status = hermesID, Running; return nil })
	if err != nil {
		return nil, err
	}
	return s, project.LinkOwned(p, hermesID, s.Owner)
}

func update(p *project.Project, ref string, fn func(*Session) error) (*Session, error) {
	mu.Lock()
	defer mu.Unlock()
	f, err := load(p)
	if err != nil {
		return nil, err
	}
	for _, s := range f.Sessions {
		if s.ID == ref || s.Num == ref || (s.HermesID != "" && s.HermesID == ref) {
			if err := fn(s); err != nil {
				return nil, err
			}
			s.Updated = time.Now().UTC()
			return s, save(p, f)
		}
	}
	return nil, fmt.Errorf("no session %q in %s", ref, p.Name)
}

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "s-" + hex.EncodeToString(b)
}

// lineageLess orders "1.10" after "1.9".
func lineageLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			var x, y int
			fmt.Sscan(as[i], &x)
			fmt.Sscan(bs[i], &y)
			return x < y
		}
	}
	return len(as) < len(bs)
}

// Share sets who besides the owner may read a chat (team mode). Empty = only the owner.
func Share(p *project.Project, ref string, with []string) (*Session, error) {
	var clean []string
	seen := map[string]bool{}
	for _, w := range with {
		w = strings.TrimSpace(w)
		if w != "" && !seen[strings.ToLower(w)] {
			seen[strings.ToLower(w)] = true
			clean = append(clean, w)
		}
	}
	return update(p, ref, func(s *Session) error { s.Shared = clean; return nil })
}

// SharedWith reports whether a person may read the chat.
func (s *Session) SharedWith(id string) bool {
	if strings.EqualFold(s.Owner, id) {
		return true
	}
	for _, w := range s.Shared {
		if strings.EqualFold(w, id) {
			return true
		}
	}
	return false
}
