// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hpk.local/hpk/internal/board"
	"hpk.local/hpk/internal/gates"
	"hpk.local/hpk/internal/modelswitch"
	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
	"hpk.local/hpk/internal/wiki"
)

// The snapshot uses the same shapes as the Console's demo data (js/data.js), so the
// views stay as they are and live.js only swaps the contents. Times are Unix ms.

type jsProject struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Profile        string   `json:"profile"`
	Owner          string   `json:"owner"`
	Members        []string `json:"members"`
	Root           string   `json:"root"`
	Privacy        string   `json:"privacy"`
	ModelPolicy    string   `json:"modelPolicy"`
	Board          string   `json:"board"`
	SessionsActive int      `json:"sessionsActive"`
	WikiPages      int      `json:"wikiPages"`
	Drafts         int      `json:"drafts"`
	LastIngest     *int64   `json:"lastIngest"`
	Cost30         *float64 `json:"cost30"`
	Tokens30       int      `json:"tokens30"`
	Language       string   `json:"language"`
	Team           bool     `json:"team"`
	ModelRoute     *jsRoute `json:"modelRoute,omitempty"`
	SpentToday     float64  `json:"spentToday"`
	MoneyDaily     float64  `json:"moneyDaily"` // 0 = no cap
	MoneyMonthly   float64  `json:"moneyMonthly"`
}

// jsRoute is the local_only check for the profile Console sessions start with.
type jsRoute struct {
	Profile string `json:"profile"`
	Where   string `json:"where"`
	Local   bool   `json:"local"`
}

type jsSession struct {
	ID        string   `json:"id"`
	Num       string   `json:"num"`
	Parent    *string  `json:"parent"`
	Title     string   `json:"title"`
	Lane      *string  `json:"lane"`
	Owner     string   `json:"owner"`
	Model     string   `json:"model"`
	Shared    []string `json:"sharedWith,omitempty"`
	Status    string   `json:"status"`
	Heartbeat int64    `json:"heartbeat"`
	Tokens    int      `json:"tokens"`
	Cost      *float64 `json:"cost"`
	ViaHpk    bool     `json:"viaHpk"`
	Mode      string   `json:"mode"`
	Worktree  string   `json:"worktree,omitempty"`
	Live      bool     `json:"live"` // hpk serve holds it open over ACP
	// Plan fields used by the plan panel.
	Category string   `json:"category"`
	Effort   string   `json:"effort"`
	Deps     []string `json:"deps"`
	Ev       []string `json:"ev"`
	Repairs  int      `json:"repairs"`
	Role     string   `json:"role"`
	Merged   bool     `json:"merged"`
	Scope    []string `json:"scope"`
}

type jsClaim struct {
	ID        string   `json:"id"`
	Task      string   `json:"task"`
	Lane      string   `json:"lane"`
	Session   string   `json:"session"`
	Paths     []string `json:"paths"`
	Wiki      []string `json:"wiki"`
	Started   int64    `json:"started"`
	Heartbeat int64    `json:"heartbeat"`
	Status    string   `json:"status"`
	Owner     string   `json:"owner"`
}

type jsOverlap struct {
	ID    string   `json:"id"`
	Level string   `json:"level"`
	Kind  string   `json:"kind"`
	A     string   `json:"a"`
	B     string   `json:"b"`
	Files []string `json:"files"`
	Found int64    `json:"found"`
	Note  string   `json:"note"`
}

type jsApproval struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"` // permission | duplicate | question | draft | stale
	RedLine string   `json:"redLine,omitempty"`
	Session string   `json:"session,omitempty"`
	Title   string   `json:"title"`
	Detail  string   `json:"detail"`
	Created int64    `json:"created"`
	Project string   `json:"project"`
	Overlap string   `json:"overlap,omitempty"`
	Drafts  []string `json:"drafts,omitempty"`
	Options []string `json:"options,omitempty"`
}

type jsWikiPage struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Title      string `json:"title"`
	Updated    int64  `json:"updated"`
	Confidence string `json:"confidence"`
}

type jsSection struct {
	Section string       `json:"section"`
	Pages   []jsWikiPage `json:"pages"`
}

type jsDraft struct {
	ID      string      `json:"id"`
	N       int         `json:"n"`
	Path    string      `json:"path"`
	Kind    string      `json:"kind"`
	Area    string      `json:"area"`
	From    string      `json:"from"`
	Summary string      `json:"summary"`
	Sources []string    `json:"sources"`
	Diff    [][2]string `json:"diff"`
}

type jsNotice struct {
	ID     string `json:"id"`
	At     int64  `json:"at"`
	Lamp   string `json:"lamp"`
	Title  string `json:"title"`
	Desc   string `json:"desc"`
	Unread bool   `json:"unread"`
	Href   string `json:"href"`
}

type jsEvent struct {
	Lamp    string `json:"lamp"`
	Who     string `json:"who"`
	Session string `json:"session"`
	Text    string `json:"text"`
	At      int64  `json:"at"`
}

// Snapshot is everything the Console shows for one project.
type Snapshot struct {
	Live          bool            `json:"live"`
	Version       string          `json:"version"`
	Me            jsPerson        `json:"me"`
	Features      map[string]bool `json:"features"`
	ProjectID     string          `json:"projectId"`
	Projects      []jsProject     `json:"projects"`
	Mode          jsMode          `json:"mode"`
	Sessions      []jsSession     `json:"sessions"`
	Claims        []jsClaim       `json:"claims"`
	Overlaps      []jsOverlap     `json:"overlaps"`
	Approvals     []jsApproval    `json:"approvals"`
	WikiTree      []jsSection     `json:"wikiTree"`
	Drafts        []jsDraft       `json:"drafts"`
	Notifications []jsNotice      `json:"notifications"`
	Events        []jsEvent       `json:"events"`
}

type jsPerson struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Initials string `json:"initials"`
	Role     string `json:"role"`
}

type jsMode struct {
	Value     string `json:"value"`
	AutoUntil *int64 `json:"autoUntil"`
	Scope     string `json:"scope"`
}

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func strp(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// BuildSnapshot reads the project's files and databases. 0 tokens.
func (s *Server) BuildSnapshot(r *http.Request, name string) (Snapshot, error) {
	id := s.who(r)
	me := id.ID
	snap := Snapshot{Live: true, Version: s.version, Me: jsPerson{ID: me, Name: orStr(id.Name, me), Initials: initials(orStr(id.Name, me)), Role: id.Role}, Features: s.features()}
	list, err := project.List(s.home)
	list = s.visible(r, list)
	if err != nil {
		return snap, err
	}
	var p *project.Project
	for _, x := range list {
		if x.Name == name || (name == "" && p == nil) {
			p = x
		}
	}
	for _, x := range list {
		snap.Projects = append(snap.Projects, s.projectRow(x, me))
	}
	if p == nil {
		return snap, nil // no projects yet: the Console shows its empty states
	}
	snap.ProjectID = p.Name
	now := time.Now()

	ms := gates.Current(p, now)
	snap.Mode = jsMode{Value: ms.Mode, Scope: "project"}
	if !ms.AutoUntil.IsZero() {
		t := ms.AutoUntil.UnixMilli()
		snap.Mode.AutoUntil = &t
	}

	sessions, _ := session.List(p)
	byID := map[string]*session.Session{}
	for _, x := range sessions {
		byID[x.ID] = x
	}
	for _, x := range sessions {
		snap.Sessions = append(snap.Sessions, s.sessionRow(p, x, ms.Mode, me))
	}

	if db, err := board.Open(p); err == nil {
		claims, _ := db.Claims(false)
		for _, c := range claims {
			snap.Claims = append(snap.Claims, jsClaim{ID: c.ID, Task: c.Task, Lane: c.Lane, Session: c.Session, Paths: nonNil(c.Paths), Wiki: nonNil(c.Wiki),
				Started: c.Started.UnixMilli(), Heartbeat: c.Heartbeat.UnixMilli(), Status: c.Status, Owner: c.Owner})
			if c.Status == board.Stale {
				snap.Approvals = append(snap.Approvals, jsApproval{ID: "stale-" + c.ID, Kind: "stale", Session: c.Session, Project: p.Name, Created: c.Heartbeat.UnixMilli(),
					Title:  fmt.Sprintf("Claim %s (%s) has no heartbeat for %s", c.ID, c.Task, now.Sub(c.Heartbeat).Round(time.Minute)),
					Detail: "Owner: " + c.Owner + ". Ask the session, release the claim, or take it over."})
			}
		}
		// Pairwise overlaps between open claims (0 tokens).
		n := 0
		for i, a := range claims {
			ov, _ := db.Check(board.Scope{Task: a.Task, Lane: a.Lane, Paths: a.Paths, Wiki: a.Wiki}, a.Session)
			for _, o := range ov {
				if o.Claim.ID <= a.ID || !contains(claimIDs(claims[i+1:]), o.Claim.ID) {
					continue
				}
				n++
				id := fmt.Sprintf("o-%d", n)
				kind := map[string]string{board.Duplicate: "duplicate", board.Files: "files", board.Related: "wiki"}[o.Level]
				snap.Overlaps = append(snap.Overlaps, jsOverlap{ID: id, Level: o.Level, Kind: kind, A: a.ID, B: o.Claim.ID, Files: nonNil(o.Files), Found: now.UnixMilli(), Note: o.Why})
				if o.Level == board.Duplicate {
					snap.Approvals = append(snap.Approvals, jsApproval{ID: "dup-" + id, Kind: "duplicate", Session: a.Session, Project: p.Name, Overlap: id, Created: now.UnixMilli(),
						Title: "Duplicate work: " + a.Task, Detail: fmt.Sprintf("%s and %s hold the same task.", a.ID, o.Claim.ID)})
				}
			}
		}
		notices, _ := db.Notices(true)
		for _, nt := range notices {
			lamp := map[string]string{board.Duplicate: "danger", board.Files: "caution", board.Related: "route"}[nt.Level]
			snap.Notifications = append(snap.Notifications, jsNotice{ID: fmt.Sprintf("n-%d", nt.ID), At: nt.At.UnixMilli(), Lamp: lamp, Title: nt.Text, Desc: nt.Kind, Unread: !nt.Read, Href: "#/board"})
		}
		db.Close()
	}

	// Questions from sub-tasks that wait for an answer.
	for _, x := range sessions {
		if x.Parent != "" {
			continue
		}
		items, _ := session.Inbox(p, x.ID)
		for i, it := range items {
			if it.Pending {
				snap.Approvals = append(snap.Approvals, jsApproval{ID: fmt.Sprintf("q-%s-%d", x.ID, i), Kind: "question", Session: it.FromID, Project: p.Name, Created: it.At.UnixMilli(),
					Title: fmt.Sprintf("Sub-task %s asks: %s", it.From, it.Report.Question), Detail: it.Report.Done})
			}
		}
	}

	// Red-line permissions held by sessions hpk serve runs over ACP.
	for _, pm := range s.acp.Pending(p.Name) {
		snap.Approvals = append(snap.Approvals, pm)
	}

	w := wiki.Open(p.WikiDir())
	if w.Exists() {
		snap.WikiTree = wikiTree(w)
		drafts, _ := w.Drafts()
		var ids []string
		for _, d := range drafts {
			jd := jsDraft{ID: fmt.Sprintf("d-%d", d.N), N: d.N, Path: d.Meta.Target, Kind: d.Meta.Kind, Area: strings.SplitN(d.Meta.Target, "/", 2)[0],
				From: d.Meta.Author, Summary: d.Meta.Summary, Sources: nonNil(d.Meta.Sources)}
			for _, l := range strings.Split(d.Body, "\n") {
				jd.Diff = append(jd.Diff, [2]string{"add", l})
			}
			snap.Drafts = append(snap.Drafts, jd)
			ids = append(ids, jd.ID)
		}
		if len(drafts) > 0 {
			snap.Approvals = append(snap.Approvals, jsApproval{ID: "drafts", Kind: "draft", Project: p.Name, Drafts: ids, Created: now.UnixMilli(),
				Title: fmt.Sprintf("%d wiki drafts wait for review", len(drafts)), Detail: draftList(drafts)})
		}
	}

	if ds, err := gates.Decisions(p, now.Add(-48*time.Hour)); err == nil {
		for i := len(ds) - 1; i >= 0 && len(snap.Events) < 14; i-- {
			d := ds[i]
			lamp := map[string]string{"ask": "danger", "auto-allow": "clear", "human": "route", "mode": "route", "no-report": "caution"}[d.Kind]
			if lamp == "" {
				lamp = "idle"
			}
			who := "hpk"
			switch d.By {
			case "auto":
				who = "auto"
			case "person", "":
			default:
				who = d.By
			}
			snap.Events = append(snap.Events, jsEvent{Lamp: lamp, Who: who, Session: d.Session, Text: strings.TrimSpace(d.Kind + " · " + strings.Join(d.Lines, ",") + " " + d.Detail), At: d.At.UnixMilli()})
		}
	}
	sort.SliceStable(snap.Approvals, func(i, j int) bool {
		return approvalRank(snap.Approvals[i].Kind) < approvalRank(snap.Approvals[j].Kind)
	})
	return snap, nil
}

func approvalRank(k string) int {
	return map[string]int{"permission": 0, "duplicate": 1, "question": 2, "stale": 3, "draft": 4}[k]
}

func (s *Server) projectRow(p *project.Project, me string) jsProject {
	row := jsProject{ID: p.Name, Name: p.Name, Kind: "personal", Profile: "", Owner: me, Members: []string{me}, Root: p.Root,
		Privacy: p.Mode(), ModelPolicy: orStr(p.Privacy.ModelPolicy, "any"), Board: "hpk", Language: orStr(p.Language, "en")}
	now := time.Now()
	row.SpentToday = privacy.Spent(s.home, p, time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()))
	caps := privacy.CapsOf(p)
	row.MoneyDaily, row.MoneyMonthly = caps.Daily, caps.Monthly
	if c := privacy.Spent(s.home, p, time.Now().Add(-30*24*time.Hour)); c > 0 {
		row.Cost30 = &c
	}
	if entries, err := os.ReadDir(filepath.Join(p.WikiDir(), "raw", "ingest")); err == nil {
		var last time.Time
		for _, e := range entries {
			if fi, err := e.Info(); err == nil && fi.ModTime().After(last) {
				last = fi.ModTime()
			}
		}
		if !last.IsZero() {
			row.LastIngest = msp(last)
		}
	}
	if p.Privacy.ModelPolicy == privacy.LocalOnly {
		prof, r, err := privacy.CheckActiveProfile(s.home, p)
		row.ModelRoute = &jsRoute{Profile: prof, Where: r.Where(), Local: err == nil}
	}
	if sessions, err := session.List(p); err == nil {
		for _, x := range sessions {
			if seen, _ := privacy.LoadSeen(s.home, x.HermesID); x.HermesID != "" && seen != nil && time.Since(x.Updated) < 30*24*time.Hour {
				row.Tokens30 += int(seen.Tokens())
			}
			if (x.Status == session.Running || x.Status == session.Waiting) && (x.HermesID == "" || s.acp.IsLive(x.ID)) {
				row.SessionsActive++
			}
		}
	}
	w := wiki.Open(p.WikiDir())
	if w.Exists() {
		if idx, err := w.Index(); err == nil {
			row.WikiPages = len(idx)
		}
		if d, err := w.Drafts(); err == nil {
			row.Drafts = len(d)
		}
	}
	return row
}

func (s *Server) sessionRow(p *project.Project, x *session.Session, mode, me string) jsSession {
	row := jsSession{ID: x.ID, Num: x.Num, Parent: strp(x.Parent), Title: x.Title, Lane: strp(x.Lane), Owner: orStr(x.Owner, me), Status: x.Status,
		Heartbeat: ms(x.Updated), ViaHpk: true, Mode: mode, Worktree: x.Worktree, Live: s.acp.IsLive(x.ID),
		Category: "deep", Effort: "medium", Deps: []string{}, Ev: []string{"code", evConf(x.Status)}, Role: "build", Scope: []string{}}
	row.Shared = x.Shared
	if x.Status == session.Failed {
		row.Status = "stopped"
	}
	// Opened from the Console but not held by this serve (it restarted): nobody is
	// listening for its questions here, so do not show it as waiting on you.
	if x.HermesID != "" && !row.Live && (x.Status == session.Running || x.Status == session.Waiting) {
		row.Status = "stale"
	}
	if x.HermesID != "" {
		if seen, _ := privacy.LoadSeen(s.home, x.HermesID); seen != nil {
			row.Tokens = int(seen.Tokens())
			row.Model = seen.Last.Model
			// A switch hpk made shows at once, before the next request reports it.
			if n := len(x.Switches); n > 0 && x.Switches[n-1].At.After(seen.At) {
				_, row.Model = modelswitch.ParseID(x.Model, "")
			}
			if seen.Cost > 0 {
				c := seen.Cost
				row.Cost = &c
			}
		}
	}
	if db, err := board.Open(p); err == nil {
		if claims, err := db.Who(x.ID); err == nil {
			for _, c := range claims {
				if c.Session == x.ID {
					row.Scope = append(row.Scope, c.Paths...)
					if c.Heartbeat.After(x.Updated) {
						row.Heartbeat = c.Heartbeat.UnixMilli()
					}
				}
			}
		}
		db.Close()
	}
	return row
}

func evConf(status string) string {
	switch status {
	case session.Running, session.Waiting:
		return "running"
	case session.Done:
		return "reported"
	case session.Failed:
		return "failed"
	case session.Stopped:
		return "cancelled"
	}
	return "notrun"
}

func wikiTree(w *wiki.Wiki) []jsSection {
	order := []string{"decisions", "architecture", "versions", "lessons", "glossary", "people"}
	var out []jsSection
	for _, area := range order {
		var pages []jsWikiPage
		files, _ := filepath.Glob(filepath.Join(w.Dir, area, "*.md"))
		sort.Strings(files)
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			pg, err := wiki.Parse(raw)
			if err != nil {
				continue
			}
			rel := area + "/" + filepath.Base(f)
			upd, _ := time.Parse("2006-01-02", pg.Meta.Updated)
			pages = append(pages, jsWikiPage{ID: rel, Path: rel, Title: orStr(pg.Meta.Title, rel), Updated: ms(upd), Confidence: orStr(pg.Meta.Confidence, "medium")})
		}
		if len(pages) > 0 {
			out = append(out, jsSection{Section: area, Pages: pages})
		}
	}
	return out
}

func draftList(ds []wiki.Draft) string {
	var parts []string
	for _, d := range ds {
		parts = append(parts, fmt.Sprintf("%s (%s)", d.Meta.Target, d.Meta.Kind))
	}
	return strings.Join(parts, ", ")
}

func initials(name string) string {
	f := strings.Fields(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(name))
	out := ""
	for _, x := range f {
		out += strings.ToUpper(string([]rune(x)[0]))
		if len(out) == 2 {
			break
		}
	}
	if out == "" {
		out = "?"
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func claimIDs(cs []board.Claim) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}
