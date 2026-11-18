// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/wiki"
)

// writeBrief makes the standard brief (§9): the task, what to read first, the red
// lines, and the report format the sub-task must send back.
func writeBrief(p *project.Project, s, parent *Session, o NewOptions) (string, error) {
	dir := filepath.Join(p.Dir, "briefs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s\n\n", s.Num, s.Title)
	if parent != nil {
		fmt.Fprintf(&b, "You are sub-task %s of %s (%s). Work only in your worktree: %s (branch %s).\n\n", s.Num, parent.Num, parent.Title, s.Cwd, s.Branch)
	} else {
		fmt.Fprintf(&b, "You are main chat %s of project %s. Split large work into sub-tasks with hpk session new --parent %s.\n\n", s.Num, p.Name, s.Num)
	}
	b.WriteString("## Task\n\n")
	if t := strings.TrimSpace(o.Task); t != "" {
		b.WriteString(t + "\n\n")
	} else {
		b.WriteString(s.Name + "\n\n")
	}
	b.WriteString("## Read first\n\n- The wiki index: `$WIKI_PATH/index.md` (skill hpk-wiki).\n")
	for _, r := range o.Read {
		fmt.Fprintf(&b, "- %s\n", r)
	}
	b.WriteString("\n## Red lines — a person decides, in every mode\n\n")
	b.WriteString("Push, tag or merge · delete anything · read .env or other secrets · change agent instruction files, skills, project.yaml or Hermes config · change model · write wiki decisions/architecture/people directly · write to the shared wiki · send data off this machine.\n")
	b.WriteString("Run `hpk gates lines` for the full list, including this project's own.\n\n")
	b.WriteString("## When you finish a turn of work\n\nSend a report of at most 15 lines with these labels, then stop:\n\n")
	b.WriteString("```sh\nhpk session report " + s.Num + " --project " + p.Name + " <<'EOF'\nDone: …\nDecided: …\nLessons: …\nOpen: …\nQuestion: … (or \"none\")\nEOF\n```\n\n")
	b.WriteString("If you are blocked, put the question in `Question:`. It goes to your main chat first, then to a person.\n")
	path := filepath.Join(dir, s.ID+".md")
	return path, os.WriteFile(path, []byte(b.String()), 0o644)
}

// Report is a sub-task's fixed-format summary (§9).
type Report struct {
	Done, Decided, Lessons, Open, Question string
}

var labels = map[string]string{
	"done": "Done", "decided": "Decided", "lessons": "Lessons", "lesson": "Lessons", "open": "Open", "question": "Question",
	"ทำแล้ว": "Done", "ตัดสินใจ": "Decided", "บทเรียน": "Lessons", "ค้าง": "Open", "คำถาม": "Question",
}

var labelRE = regexp.MustCompile(`^\s*[-*]?\s*([^\s:：]+)\s*[:：]\s*(.*)$`)

// ParseReport checks the format: at most 15 lines, labelled, Done required.
func ParseReport(text string) (Report, error) {
	text = strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	var r Report
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return r, errors.New("the report is empty")
	}
	if len(lines) > 15 {
		return r, fmt.Errorf("the report has %d lines; keep it to 15 or fewer", len(lines))
	}
	if wiki.LooksSecret(text) {
		return r, errors.New("refused: the report looks like it contains a secret")
	}
	field := map[string]*string{"Done": &r.Done, "Decided": &r.Decided, "Lessons": &r.Lessons, "Open": &r.Open, "Question": &r.Question}
	var cur *string
	for _, l := range lines {
		if m := labelRE.FindStringSubmatch(l); m != nil {
			if key, ok := labels[strings.ToLower(m[1])]; ok {
				cur = field[key]
				*cur = strings.TrimSpace(m[2])
				continue
			}
		}
		if cur == nil {
			return r, fmt.Errorf("line %q has no label; use Done:, Decided:, Lessons:, Open:, Question:", strings.TrimSpace(l))
		}
		*cur = strings.TrimSpace(*cur + " " + strings.TrimSpace(l))
	}
	if r.Done == "" {
		return r, errors.New("the report needs a Done: line")
	}
	if q := strings.ToLower(strings.TrimSpace(r.Question)); q == "none" || q == "-" || q == "ไม่มี" {
		r.Question = ""
	}
	return r, nil
}

// InboxItem is what lands in the main chat (the Console shows it as a card).
type InboxItem struct {
	At      time.Time `json:"at"`
	From    string    `json:"from"` // sub-task lineage number
	FromID  string    `json:"from_id"`
	Title   string    `json:"title"`
	Report  Report    `json:"report"`
	Kind    string    `json:"kind"` // summary | question
	Pending bool      `json:"pending,omitempty"`
}

// File records a report: the main chat's inbox, wiki raw/sessions/, the wiki log,
// and the sub-task's status. A question stays pending until someone answers.
func File(p *project.Project, ref, text string, final bool) (*Session, Report, error) {
	r, err := ParseReport(text)
	if err != nil {
		return nil, r, err
	}
	s, err := Get(p, ref)
	if err != nil {
		return nil, r, err
	}
	now := time.Now().UTC()
	target := s.Parent
	if target == "" {
		target = s.ID // a main chat's report stays with itself
	}
	kind := "summary"
	if r.Question != "" {
		kind = "question"
	}
	item := InboxItem{At: now, From: s.Num, FromID: s.ID, Title: s.Title, Report: r, Kind: kind, Pending: r.Question != ""}
	raw, _ := json.Marshal(item)
	if err := appendLine(filepath.Join(p.Dir, "inbox", target+".jsonl"), raw); err != nil {
		return nil, r, err
	}
	w := wiki.Open(p.WikiDir())
	if w.Exists() {
		body := fmt.Sprintf("---\ntitle: %q\ntype: raw\nsources: [session %s]\nupdated: %s\nconfidence: medium\n---\n\nDone: %s\nDecided: %s\nLessons: %s\nOpen: %s\nQuestion: %s\n",
			s.Num+" "+s.Title, s.ID, now.Format("2006-01-02"), r.Done, r.Decided, r.Lessons, r.Open, r.Question)
		dir := filepath.Join(p.WikiDir(), "raw", "sessions")
		_ = os.MkdirAll(dir, 0o755)
		// Never overwrite: two reports in the same second get -2, -3 …
		base := fmt.Sprintf("%s-%s", strings.ReplaceAll(s.Num, ".", "-"), now.Format("20060102-150405"))
		name := base + ".md"
		for i := 2; ; i++ {
			if _, err := os.Stat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
				break
			}
			name = fmt.Sprintf("%s-%d.md", base, i)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return nil, r, err
		}
		_ = w.Log(fmt.Sprintf("report from %s (%s): %s", s.Num, kind, r.Done))
	}
	status := s.Status
	switch {
	case r.Question != "":
		status = Waiting
	case final:
		status = Done
	}
	s, err = update(p, s.ID, func(x *Session) error { x.Reported, x.Status = true, status; return nil })
	return s, r, err
}

// Inbox returns the items for a main chat, oldest first.
func Inbox(p *project.Project, ref string) ([]InboxItem, error) {
	s, err := Get(p, ref)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(p.Dir, "inbox", s.ID+".jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []InboxItem
	for _, l := range strings.Split(string(raw), "\n") {
		var it InboxItem
		if json.Unmarshal([]byte(l), &it) == nil {
			out = append(out, it)
		}
	}
	return out, nil
}

func appendLine(path string, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}
