// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package board

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Claim statuses (§20.3).
const (
	Active       = "active"
	WaitingHuman = "waiting-human"
	Done         = "done"
	Stale        = "stale"
)

// Claim is "I am working on this".
type Claim struct {
	ID        string    `json:"id"`
	Task      string    `json:"task"`
	Lane      string    `json:"lane,omitempty"`
	Paths     []string  `json:"paths"`
	Wiki      []string  `json:"wiki,omitempty"`
	Owner     string    `json:"owner"`
	Session   string    `json:"session,omitempty"`
	Started   time.Time `json:"started"`
	Heartbeat time.Time `json:"heartbeat"`
	Status    string    `json:"status"`
}

// Scope is what a new piece of work will touch.
type Scope struct {
	Task  string
	Lane  string
	Paths []string
	Wiki  []string
}

// Overlap levels (§20.4).
const (
	Duplicate = "danger"  // 🔴 same task: stop, a person decides
	Files     = "caution" // 🟠 paths overlap: warn, go on if a person confirms
	Related   = "related" // 🟡 same wiki page or same lane: tell, do not stop
)

// Overlap is one conflict with an existing claim.
type Overlap struct {
	Level string   `json:"level"`
	Claim Claim    `json:"claim"`
	Files []string `json:"files,omitempty"` // overlapping globs or pages
	Why   string   `json:"why"`
}

var nonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// taskKey normalises a task so "Auth: Refresh token" == "auth – refresh-token".
func taskKey(s string) string {
	return strings.Trim(nonWord.ReplaceAllString(strings.ToLower(s), " "), " ")
}

func (d *DB) scan(rows *sql.Rows) ([]Claim, error) {
	defer rows.Close()
	var out []Claim
	for rows.Next() {
		var c Claim
		var paths, wiki, started, hb string
		if err := rows.Scan(&c.ID, &c.Task, &c.Lane, &paths, &wiki, &c.Owner, &c.Session, &started, &hb, &c.Status); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(paths), &c.Paths)
		_ = json.Unmarshal([]byte(wiki), &c.Wiki)
		c.Started, c.Heartbeat = parseTS(started), parseTS(hb)
		out = append(out, c)
	}
	return out, rows.Err()
}

const cols = `id, task, lane, paths, wiki, owner, session, started, heartbeat, status`

// Open claims: everything not done.
func (d *DB) Claims(includeDone bool) ([]Claim, error) {
	q := `SELECT ` + cols + ` FROM claims`
	if !includeDone {
		q += ` WHERE status != 'done'`
	}
	rows, err := d.sql.Query(q + ` ORDER BY started`)
	if err != nil {
		return nil, err
	}
	return d.scan(rows)
}

// Get one claim.
func (d *DB) Get(id string) (Claim, error) {
	rows, err := d.sql.Query(`SELECT `+cols+` FROM claims WHERE id=?`, id)
	if err != nil {
		return Claim{}, err
	}
	list, err := d.scan(rows)
	if err != nil {
		return Claim{}, err
	}
	if len(list) == 0 {
		return Claim{}, fmt.Errorf("no claim %s", id)
	}
	return list[0], nil
}

// Check finds open claims that overlap a scope, worst first. session's own claims are ignored.
func (d *DB) Check(sc Scope, session string) ([]Overlap, error) {
	open, err := d.Claims(false)
	if err != nil {
		return nil, err
	}
	key := taskKey(sc.Task)
	var out []Overlap
	for _, c := range open {
		if session != "" && c.Session == session {
			continue
		}
		switch {
		case key != "" && taskKey(c.Task) == key:
			out = append(out, Overlap{Level: Duplicate, Claim: c, Why: "same task"})
		case len(globOverlap(sc.Paths, c.Paths)) > 0:
			out = append(out, Overlap{Level: Files, Claim: c, Files: globOverlap(sc.Paths, c.Paths), Why: "the same files"})
		case len(intersect(sc.Wiki, c.Wiki)) > 0:
			out = append(out, Overlap{Level: Related, Claim: c, Files: intersect(sc.Wiki, c.Wiki), Why: "the same wiki page"})
		case sc.Lane != "" && sc.Lane == c.Lane:
			out = append(out, Overlap{Level: Related, Claim: c, Why: "the same lane"})
		}
	}
	rank := map[string]int{Duplicate: 0, Files: 1, Related: 2}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Level] < rank[out[j].Level] })
	return out, nil
}

// ErrNeedsPerson means the claim was not made: a duplicate or a file overlap
// that a person must decide (§20.6). The overlaps come back with it.
var ErrNeedsPerson = errors.New("a person must decide")

// ClaimWork checks and then claims. Duplicates always stop; file overlaps stop
// unless confirmed by a person.
func (d *DB) ClaimWork(sc Scope, owner, session string, confirmed bool) (Claim, []Overlap, error) {
	if strings.TrimSpace(sc.Task) == "" {
		return Claim{}, nil, errors.New("a claim needs a task")
	}
	ov, err := d.Check(sc, session)
	if err != nil {
		return Claim{}, nil, err
	}
	for _, o := range ov {
		if o.Level == Duplicate || (o.Level == Files && !confirmed) {
			for _, x := range ov {
				if x.Level != Related {
					d.notify(x.Level, "conflict", x.Claim.Owner, "", x.Claim.ID, x.Files,
						fmt.Sprintf("%s wants to start %q, which overlaps your claim %s (%s)", orDash(owner), sc.Task, x.Claim.ID, x.Why))
				}
			}
			return Claim{}, ov, ErrNeedsPerson
		}
	}
	id, err := d.nextID("claim", "c")
	if err != nil {
		return Claim{}, ov, err
	}
	now := d.Now()
	c := Claim{ID: id, Task: sc.Task, Lane: sc.Lane, Paths: clean(sc.Paths), Wiki: clean(sc.Wiki), Owner: owner, Session: session, Started: now, Heartbeat: now, Status: Active}
	pj, _ := json.Marshal(c.Paths)
	wj, _ := json.Marshal(c.Wiki)
	_, err = d.sql.Exec(`INSERT INTO claims(id,task,task_key,lane,paths,wiki,owner,session,started,heartbeat,status) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Task, taskKey(c.Task), c.Lane, string(pj), string(wj), c.Owner, c.Session, ts(now), ts(now), Active)
	if err == nil {
		for _, o := range ov {
			if o.Level == Files { // confirmed overlap: both owners hear about it
				d.notify(Files, "overlap-confirmed", o.Claim.Owner, c.ID, o.Claim.ID, o.Files, fmt.Sprintf("%s started %q with files that overlap your claim %s", orDash(owner), c.Task, o.Claim.ID))
			}
		}
	}
	return c, ov, err
}

// Heartbeat marks a claim (or every open claim of a session) alive. Paths touched
// outside the scope widen it; the widening is checked again (§20.3).
func (d *DB) Heartbeat(ref string, touched []string) ([]Overlap, error) {
	claims, err := d.forRef(ref)
	if err != nil || len(claims) == 0 {
		return nil, err
	}
	now := d.Now()
	var found []Overlap
	for _, c := range claims {
		var extra []string
		for _, t := range touched {
			t = path.Clean(strings.ReplaceAll(t, `\`, "/"))
			if len(globOverlap([]string{t}, c.Paths)) == 0 {
				extra = append(extra, t)
			}
		}
		if len(extra) > 0 {
			ov, err := d.Check(Scope{Paths: extra}, c.Session)
			if err != nil {
				return nil, err
			}
			for _, o := range ov {
				if o.Level == Files {
					found = append(found, o)
					d.notify(Files, "overlap", o.Claim.Owner, c.ID, o.Claim.ID, o.Files, fmt.Sprintf("claim %s (%s) now also touches %s, which %s holds in %s", c.ID, c.Task, strings.Join(o.Files, ", "), o.Claim.Owner, o.Claim.ID))
				}
			}
			c.Paths = clean(append(c.Paths, extra...))
		}
		pj, _ := json.Marshal(c.Paths)
		status := c.Status
		if status == Stale {
			status = Active
		}
		if _, err := d.sql.Exec(`UPDATE claims SET heartbeat=?, paths=?, status=? WHERE id=?`, ts(now), string(pj), status, c.ID); err != nil {
			return found, err
		}
	}
	return found, nil
}

// SetStatus marks a claim waiting for a person, or back to active.
func (d *DB) SetStatus(ref, status string) error {
	claims, err := d.forRef(ref)
	if err != nil {
		return err
	}
	for _, c := range claims {
		if _, err := d.sql.Exec(`UPDATE claims SET status=? WHERE id=?`, status, c.ID); err != nil {
			return err
		}
	}
	return nil
}

// Release ends a claim; anyone waiting for that work is told.
func (d *DB) Release(ref string) ([]Claim, error) {
	claims, err := d.forRef(ref)
	if err != nil {
		return nil, err
	}
	for _, c := range claims {
		if _, err := d.sql.Exec(`UPDATE claims SET status='done', ended=? WHERE id=?`, ts(d.Now()), c.ID); err != nil {
			return nil, err
		}
		d.notify(Related, "released", "", c.ID, "", c.Paths, fmt.Sprintf("%s released %s (%s)", c.Owner, c.ID, c.Task))
	}
	return claims, nil
}

// MarkStale flags claims with no heartbeat for longer than after; it never releases them (§20.3).
func (d *DB) MarkStale(after time.Duration) ([]Claim, error) {
	open, err := d.Claims(false)
	if err != nil {
		return nil, err
	}
	var stale []Claim
	for _, c := range open {
		if c.Status != Stale && d.Now().Sub(c.Heartbeat) > after {
			if _, err := d.sql.Exec(`UPDATE claims SET status='stale' WHERE id=?`, c.ID); err != nil {
				return stale, err
			}
			c.Status = Stale
			stale = append(stale, c)
			d.notify(Files, "stale", c.Owner, c.ID, "", c.Paths, fmt.Sprintf("your claim %s (%s) has had no heartbeat for %s", c.ID, c.Task, d.Now().Sub(c.Heartbeat).Round(time.Minute)))
		}
	}
	return stale, nil
}

// Who answers "who is on this?" from the board, without disturbing anyone (§20.5).
func (d *DB) Who(q string) ([]Claim, error) {
	open, err := d.Claims(false)
	if err != nil {
		return nil, err
	}
	q = strings.TrimSpace(q)
	key := taskKey(q)
	var out []Claim
	for _, c := range open {
		if (key != "" && strings.Contains(taskKey(c.Task), key)) || len(globOverlap([]string{q}, c.Paths)) > 0 || c.Lane == q || c.ID == q {
			out = append(out, c)
		}
	}
	return out, nil
}

// forRef resolves a claim id, or every open claim of a session.
func (d *DB) forRef(ref string) ([]Claim, error) {
	if strings.HasPrefix(ref, "c-") {
		c, err := d.Get(ref)
		if err != nil {
			return nil, err
		}
		return []Claim{c}, nil
	}
	rows, err := d.sql.Query(`SELECT `+cols+` FROM claims WHERE session=? AND status != 'done'`, ref)
	if err != nil {
		return nil, err
	}
	return d.scan(rows)
}

// ---- path globs ----

// globOverlap returns the patterns of a that could match a file also matched by b.
// Patterns are project-relative: "web/src/auth/**", "api/invoices/totals.ts".
func globOverlap(a, b []string) []string {
	var out []string
	for _, x := range a {
		for _, y := range b {
			if globsMeet(x, y) {
				out = append(out, x)
				break
			}
		}
	}
	return out
}

// globsMeet: two patterns meet when one's fixed prefix lies inside the other's.
func globsMeet(x, y string) bool {
	px, wx := stem(x)
	py, wy := stem(y)
	switch {
	case px == py:
		return true
	case wx && strings.HasPrefix(py+"/", px+"/"):
		return true
	case wy && strings.HasPrefix(px+"/", py+"/"):
		return true
	case !wx && !wy:
		return px == py
	}
	return px == "" || py == ""
}

// stem returns the part before the first wildcard, and whether there was one.
func stem(g string) (string, bool) {
	g = strings.Trim(path.Clean(strings.ReplaceAll(strings.TrimSpace(g), `\`, "/")), "/")
	if i := strings.IndexAny(g, "*?["); i >= 0 {
		return strings.TrimSuffix(g[:i], "/"), true
	}
	return g, false
}

func intersect(a, b []string) []string {
	set := map[string]bool{}
	for _, x := range b {
		set[strings.ToLower(x)] = true
	}
	var out []string
	for _, x := range a {
		if set[strings.ToLower(x)] {
			out = append(out, x)
		}
	}
	return out
}

func clean(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range list {
		x = strings.TrimSpace(strings.ReplaceAll(x, `\`, "/"))
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func orDash(s string) string {
	if s == "" {
		return "someone"
	}
	return s
}
