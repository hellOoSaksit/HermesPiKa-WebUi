// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package browser

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Step is one recorded action. Labels, URLs and errors are scrubbed before
// they get here; typed text from the person is recorded as a length only.
type Step struct {
	N     int       `json:"n"`
	At    time.Time `json:"at"`
	By    string    `json:"by"` // agent | human
	Tool  string    `json:"tool"`
	Label string    `json:"label,omitempty"`
	URL   string    `json:"url,omitempty"`
	Title string    `json:"title,omitempty"`
	X     float64   `json:"x,omitempty"`
	Y     float64   `json:"y,omitempty"`
	Shot  string    `json:"shot,omitempty"` // file name in the run folder
	Error string    `json:"error,omitempty"`
	shot  []byte
}

// Run is one browsing session, from start to stop.
type Run struct {
	ID      string    `json:"id"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitempty"`
	Steps   int       `json:"steps"`
	First   string    `json:"first,omitempty"` // first page opened
	Cover   string    `json:"cover,omitempty"` // a screenshot to show
	Agent   int       `json:"agent"`
	Human   int       `json:"human"`
	Saved   bool      `json:"saved"` // kept past the 30-day clean-up
}

type recorder struct {
	mu  sync.Mutex
	dir string
	id  string
	run Run
	f   *os.File
}

var runIDRE = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$`)

func runsDir(base string) string { return filepath.Join(base, "runs") }

func newRecorder(base string) (*recorder, error) {
	b := make([]byte, 3)
	rand.Read(b)
	now := time.Now()
	id := now.Format("20060102-150405") + "-" + hex.EncodeToString(b)
	dir := filepath.Join(runsDir(base), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "steps.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	r := &recorder{dir: dir, id: id, run: Run{ID: id, Started: now.UTC()}, f: f}
	r.saveMeta()
	return r, nil
}

func (r *recorder) add(s Step) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return
	}
	r.run.Steps++
	s.N = r.run.Steps
	s.At = time.Now().UTC()
	if len(s.shot) > 0 {
		s.Shot = fmt.Sprintf("%04d.jpg", s.N)
		_ = os.WriteFile(filepath.Join(r.dir, s.Shot), s.shot, 0o600)
		r.run.Cover = s.Shot
	}
	switch s.By {
	case "agent":
		r.run.Agent++
	case "human":
		r.run.Human++
	}
	if r.run.First == "" && s.Tool == "open" && s.URL != "" {
		r.run.First = s.URL
	}
	b, _ := json.Marshal(s)
	r.f.Write(append(b, '\n'))
	r.saveMeta()
}

func (r *recorder) saveMeta() {
	b, _ := json.Marshal(r.run)
	_ = os.WriteFile(filepath.Join(r.dir, "run.json"), b, 0o600)
}

func (r *recorder) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return
	}
	r.f.Close()
	r.f = nil
	r.run.Ended = time.Now().UTC()
	r.saveMeta()
	if r.run.Steps == 0 {
		os.RemoveAll(r.dir)
	}
}

// Runs lists recordings, newest first.
func Runs(home string) []Run {
	base := Dir(home)
	ents, _ := os.ReadDir(runsDir(base))
	out := []Run{}
	for _, e := range ents {
		if !runIDRE.MatchString(e.Name()) {
			continue
		}
		var r Run
		if raw, err := os.ReadFile(filepath.Join(runsDir(base), e.Name(), "run.json")); err == nil && json.Unmarshal(raw, &r) == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out
}

func runDir(home, id string) (string, error) {
	if !runIDRE.MatchString(id) {
		return "", errors.New("no such recording")
	}
	return filepath.Join(Dir(home), "runs", id), nil
}

// RunSteps returns a recording's steps.
func RunSteps(home, id string) ([]Step, error) {
	dir, err := runDir(home, id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(dir, "steps.jsonl"))
	if err != nil {
		return nil, errors.New("no such recording")
	}
	defer f.Close()
	var out []Step
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var s Step
		if json.Unmarshal(sc.Bytes(), &s) == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

var shotRE = regexp.MustCompile(`^[0-9]{4}\.jpg$`)

// Shot returns one screenshot of a recording.
func Shot(home, id, name string) ([]byte, error) {
	dir, err := runDir(home, id)
	if err != nil || !shotRE.MatchString(name) {
		return nil, errors.New("no such screenshot")
	}
	return os.ReadFile(filepath.Join(dir, name))
}

// DeleteRun removes a recording.
func DeleteRun(home, id string) error {
	dir, err := runDir(home, id)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// KeepRun marks a recording to survive the clean-up (or not).
func KeepRun(home, id string, keep bool) error {
	dir, err := runDir(home, id)
	if err != nil {
		return err
	}
	p := filepath.Join(dir, "run.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var r Run
	if err := json.Unmarshal(raw, &r); err != nil {
		return err
	}
	r.Saved = keep
	b, _ := json.Marshal(r)
	return os.WriteFile(p, b, 0o600)
}

func pruneRuns(base string, age time.Duration) {
	ents, _ := os.ReadDir(runsDir(base))
	for _, e := range ents {
		if !runIDRE.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(runsDir(base), e.Name())
		var r Run
		if raw, err := os.ReadFile(filepath.Join(dir, "run.json")); err == nil && json.Unmarshal(raw, &r) == nil && (r.Saved || time.Since(r.Started) < age) {
			continue
		}
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > age {
			os.RemoveAll(dir)
		}
	}
}
