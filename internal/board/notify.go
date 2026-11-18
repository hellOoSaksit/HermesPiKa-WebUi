// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package board

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Notice is one alert for a person (§20.7).
type Notice struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	Level  string    `json:"level"`
	Kind   string    `json:"kind"`
	Owner  string    `json:"owner,omitempty"`
	ClaimA string    `json:"claim_a,omitempty"`
	ClaimB string    `json:"claim_b,omitempty"`
	Files  []string  `json:"files,omitempty"`
	Text   string    `json:"text"`
	Read   bool      `json:"read"`
}

// Batching (§20.7): at most one notice per person and kind every 5 minutes,
// except duplicates (🔴), which always go out.
const batchWindow = 5 * time.Minute

func (d *DB) notify(level, kind, owner, a, b string, files []string, text string) {
	now := d.Now()
	if level != Duplicate {
		var n int
		_ = d.sql.QueryRow(`SELECT COUNT(*) FROM notices WHERE owner=? AND kind=? AND at > ?`, owner, kind, ts(now.Add(-batchWindow))).Scan(&n)
		if n > 0 {
			return
		}
	}
	fj, _ := json.Marshal(files)
	_, _ = d.sql.Exec(`INSERT INTO notices(at,level,kind,owner,claim_a,claim_b,files,text) VALUES(?,?,?,?,?,?,?,?)`, ts(now), level, kind, owner, a, b, string(fj), text)
}

// Notices returns unread notices (or all), newest first.
func (d *DB) Notices(all bool) ([]Notice, error) {
	q := `SELECT id,at,level,kind,owner,claim_a,claim_b,files,text,read FROM notices`
	if !all {
		q += ` WHERE read=0`
	}
	rows, err := d.sql.Query(q + ` ORDER BY id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notice
	for rows.Next() {
		var n Notice
		var at, files string
		var read int
		if err := rows.Scan(&n.ID, &at, &n.Level, &n.Kind, &n.Owner, &n.ClaimA, &n.ClaimB, &files, &n.Text, &read); err != nil {
			return nil, err
		}
		n.At, n.Read = parseTS(at), read == 1
		_ = json.Unmarshal([]byte(files), &n.Files)
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkRead clears notices.
func (d *DB) MarkRead() error {
	_, err := d.sql.Exec(`UPDATE notices SET read=1 WHERE read=0`)
	return err
}

// ---- asking another session (§20.5) ----

// AskItem is a status question to another session.
type AskItem struct {
	ID       int64             `json:"id"`
	At       time.Time         `json:"at"`
	From     string            `json:"from"`
	To       string            `json:"to"`
	Question string            `json:"question"`
	Status   string            `json:"status"` // queued | answered | timeout
	Answer   map[string]string `json:"answer,omitempty"`
}

// AskTimeout: no answer in 10 minutes means busy; the owner is told.
const AskTimeout = 10 * time.Minute

// Ask queues a status question. It is delivered to the target session with
// "/queue" (never interrupting it); the owner is told someone asked.
func (d *DB) Ask(from, to, question, toOwner string) (AskItem, error) {
	now := d.Now()
	res, err := d.sql.Exec(`INSERT INTO asks(at,from_sess,to_sess,question,status) VALUES(?,?,?,?, 'queued')`, ts(now), from, to, question)
	if err != nil {
		return AskItem{}, err
	}
	id, _ := res.LastInsertId()
	d.notify(Related, "asked", toOwner, "", "", nil, fmt.Sprintf("%s asked session %s: %s", orDash(from), to, question))
	return AskItem{ID: id, At: now, From: from, To: to, Question: question, Status: "queued"}, nil
}

// QueuedText is what hpk sends into the target session: a fixed format so the
// answer is short and machine-readable.
func QueuedText(a AskItem) string {
	return fmt.Sprintf("/queue [hpk ask %d from %s] %s\nAnswer briefly with: hpk board answer %d --status … --progress … --eta … --files …", a.ID, a.From, a.Question, a.ID)
}

// Answer records a reply and refreshes the target's claims.
func (d *DB) Answer(id int64, fields map[string]string) (AskItem, error) {
	aj, _ := json.Marshal(fields)
	if _, err := d.sql.Exec(`UPDATE asks SET status='answered', answer=?, answered_at=? WHERE id=? AND status!='answered'`, string(aj), ts(d.Now()), id); err != nil {
		return AskItem{}, err
	}
	a, err := d.getAsk(id)
	if err == nil {
		files := strings.FieldsFunc(fields["files"], func(r rune) bool { return r == ',' || r == ' ' })
		_, _ = d.Heartbeat(a.To, files)
	}
	return a, err
}

// ExpireAsks marks unanswered questions older than AskTimeout and tells the owner.
func (d *DB) ExpireAsks(ownerOf func(session string) string) ([]AskItem, error) {
	rows, err := d.sql.Query(`SELECT id FROM asks WHERE status='queued' AND at < ?`, ts(d.Now().Add(-AskTimeout)))
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	var out []AskItem
	for _, id := range ids {
		d.sql.Exec(`UPDATE asks SET status='timeout' WHERE id=?`, id)
		a, err := d.getAsk(id)
		if err != nil {
			continue
		}
		owner := ""
		if ownerOf != nil {
			owner = ownerOf(a.To)
		}
		d.notify(Files, "ask-timeout", owner, "", "", nil, fmt.Sprintf("session %s did not answer %q within 10 minutes", a.To, a.Question))
		out = append(out, a)
	}
	return out, nil
}

// Asks lists questions, newest first.
func (d *DB) Asks() ([]AskItem, error) {
	rows, err := d.sql.Query(`SELECT id FROM asks ORDER BY id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	var out []AskItem
	for _, id := range ids {
		if a, err := d.getAsk(id); err == nil {
			out = append(out, a)
		}
	}
	return out, nil
}

func (d *DB) getAsk(id int64) (AskItem, error) {
	var a AskItem
	var at, answer string
	err := d.sql.QueryRow(`SELECT id,at,from_sess,to_sess,question,status,answer FROM asks WHERE id=?`, id).Scan(&a.ID, &at, &a.From, &a.To, &a.Question, &a.Status, &answer)
	a.At = parseTS(at)
	_ = json.Unmarshal([]byte(answer), &a.Answer)
	return a, err
}

// ---- worktree diff check (§20.4: every commit and nightly) ----

// WorktreeClash is a file changed in two worktrees at once.
type WorktreeClash struct {
	File      string   `json:"file"`
	Worktrees []string `json:"worktrees"`
}

// DiffWorktrees compares what each worktree under <root>/worktrees changed
// against the main branch, committed or not. 0 tokens: git only.
func (d *DB) DiffWorktrees(root, base string) ([]WorktreeClash, error) {
	dirs, _ := filepath.Glob(filepath.Join(root, "worktrees", "*"))
	byFile := map[string][]string{}
	for _, dir := range dirs {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}
		changed := map[string]bool{}
		for _, args := range [][]string{{"diff", "--name-only", base + "...HEAD"}, {"diff", "--name-only"}, {"diff", "--name-only", "--cached"}} {
			out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
			if err != nil {
				continue
			}
			for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if f != "" {
					changed[f] = true
				}
			}
		}
		for f := range changed {
			byFile[f] = append(byFile[f], filepath.Base(dir))
		}
	}
	var out []WorktreeClash
	for f, wts := range byFile {
		if len(wts) > 1 {
			sort.Strings(wts)
			out = append(out, WorktreeClash{File: f, Worktrees: wts})
			d.notify(Files, "worktree-overlap", "", "", "", []string{f}, fmt.Sprintf("%s is changed in %s", f, strings.Join(wts, " and ")))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, nil
}
