// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package folders

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/safepath"
)

// UndoFile is where the last tidy round of project name is kept, under dir.
// The caller picks a dir out of the AI's reach (the person's private folder):
// Undo moves whatever the record names.
func UndoFile(dir, name string) string { return filepath.Join(dir, "tidy", name+".json") }

// Area is where a tidy may act.
type Area struct {
	Root   string   // the project folder, resolved (places.Root)
	Skip   []string // folders inside Root a tidy never enters: wikis, other projects
	Record string   // the undo record (UndoFile)
	Audit  string   // the project's hpk folder, for audit.log
}

// Move is one file going to its rule folder. Paths are project-relative, with
// forward slashes.
type Move struct {
	From  string    `json:"from"`
	To    string    `json:"to"`
	Size  int64     `json:"size"`
	MTime time.Time `json:"mtime"`
}

// Plan is what a tidy would do: the moves, and the files no rule names.
type Plan struct {
	Moves    []Move   `json:"moves"`
	Homeless []string `json:"homeless"`
}

func fold(a string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(a)
	}
	return a
}

// underFolder reports whether rel is anywhere below folder.
func underFolder(rel, folder string) bool {
	return strings.HasPrefix(fold(rel), fold(strings.Trim(folder, "/"))+"/")
}

// skipName: names a tidy never touches: node_modules, and every name the file
// browser and upload refuse (safepath.Name: hidden, keys and credentials,
// device names, control characters …).
func skipName(name string) bool {
	return name == "node_modules" || safepath.Name(name) != nil
}

// planOK: a project-relative path a tidy may move from or to.
func planOK(rel string) bool {
	if safepath.Rel(rel) != nil {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if skipName(part) {
			return false
		}
	}
	return true
}

// skipRels turns skip into paths relative to root, for those inside it ("" is
// root itself: nothing may move).
func skipRels(root string, skip []string) []string {
	var out []string
	for _, s := range skip {
		if s == "" {
			continue
		}
		if r, err := filepath.EvalSymlinks(s); err == nil {
			s = r
		}
		rel, err := filepath.Rel(root, filepath.Clean(s))
		switch {
		case err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)):
			continue
		case rel == ".":
			out = append(out, "")
		default:
			out = append(out, filepath.ToSlash(rel))
		}
	}
	return out
}

// inSkipped reports whether rel is one of the skipped folders or below one.
func inSkipped(rel string, skips []string) bool {
	for _, s := range skips {
		if s == "" || fold(rel) == fold(s) || underFolder(rel, s) {
			return true
		}
	}
	return false
}

func gitCmd(root string, stdin io.Reader, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = stdin
	return cmd
}

func haveGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// gitIgnored returns the paths git ignores. No git, or not a repo: none.
func gitIgnored(root string, paths []string) map[string]bool {
	out := map[string]bool{}
	if len(paths) == 0 || !haveGit() {
		return out
	}
	var in bytes.Buffer
	for _, p := range paths {
		in.WriteString(p)
		in.WriteByte(0)
	}
	cmd := gitCmd(root, &in, "check-ignore", "--stdin", "-z")
	raw, err := cmd.Output()
	if err != nil {
		if ee := (*exec.ExitError)(nil); !errors.As(err, &ee) || ee.ExitCode() != 1 {
			return out // not a repo, or git failed: nothing is ignored
		}
	}
	for _, p := range strings.Split(string(raw), "\x00") {
		if p != "" {
			out[p] = true
		}
	}
	return out
}

// gitTracked reports whether git tracks the file (false when git is missing or
// this is not a repo).
func gitTracked(root, rel string) bool {
	if !haveGit() {
		return false
	}
	return gitCmd(root, nil, "--literal-pathspecs", "ls-files", "--error-unmatch", "--", rel).Run() == nil
}

// PlanTidy lists the files that are not in the folder their extension's rule
// names. Links and junctions are never entered, nor the folders in a.Skip.
func PlanTidy(a Area, r Rules) (Plan, error) {
	plan := Plan{Moves: []Move{}, Homeless: []string{}}
	root := a.Root
	skips := skipRels(root, a.Skip)
	for _, s := range skips {
		if s == "" {
			return plan, nil // the whole folder is someone else's
		}
	}
	type cand struct {
		rel string
		fi  fs.FileInfo
	}
	var files []cand
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		fi, ierr := d.Info()
		isDir := d.IsDir() || (ierr == nil && fi.IsDir())
		if ierr != nil || skipName(d.Name()) || inSkipped(rel, skips) || safepath.IsLink(p, fi) {
			if isDir {
				return filepath.SkipDir // links and junctions are never followed
			}
			return nil
		}
		if isDir || !fi.Mode().IsRegular() {
			return nil
		}
		files = append(files, cand{rel, fi})
		return nil
	})
	if err != nil {
		return plan, err
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.rel
	}
	ignored := gitIgnored(root, names)
	for _, f := range files {
		if ignored[f.rel] {
			continue
		}
		name := f.rel[strings.LastIndex(f.rel, "/")+1:]
		folder, ok := r.For(name)
		if !ok {
			plan.Homeless = append(plan.Homeless, f.rel)
			continue
		}
		if underFolder(f.rel, folder) {
			continue
		}
		to := strings.Trim(folder, "/") + "/" + name
		if inSkipped(to, skips) {
			continue
		}
		plan.Moves = append(plan.Moves, Move{From: f.rel, To: to, Size: f.fi.Size(), MTime: f.fi.ModTime()})
	}
	sort.Slice(plan.Moves, func(i, j int) bool { return plan.Moves[i].From < plan.Moves[j].From })
	sort.Strings(plan.Homeless)
	return plan, nil
}

func numbered(name string, n int) string {
	ext := filepath.Ext(name)
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), n, ext)
}

// place moves src into dir under name without ever replacing a file; a clash
// picks "name (2).ext", … It returns the name used. A tracked file goes by git mv.
func place(root, src, dir, name string, tracked bool) (string, error) {
	for n := 1; n < 10000; n++ {
		cand := name
		if n > 1 {
			cand = numbered(name, n)
		}
		dst := filepath.Join(dir, cand)
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		if tracked {
			// git mv without -f refuses an existing destination
			if gitCmd(root, nil, "--literal-pathspecs", "mv", "--", src, dst).Run() == nil {
				return cand, nil
			}
			if _, err := os.Lstat(dst); err == nil {
				continue
			}
			if _, err := os.Lstat(src); err != nil {
				return "", err
			}
		}
		// a hard link fails if dst appeared meanwhile, so it never replaces
		err := os.Link(src, dst)
		if err == nil {
			if err := os.Remove(src); err != nil {
				os.Remove(dst)
				return "", err
			}
			return cand, nil
		}
		if os.IsExist(err) {
			continue
		}
		// no hard links here (FAT, some network drives): claim the name with
		// O_EXCL, then rename over our own empty claim
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		f.Close()
		if err := os.Rename(src, dst); err != nil {
			os.Remove(dst)
			return "", err
		}
		return cand, nil
	}
	return "", errors.New("too many files with that name")
}

// lastRound is the undo record. Root binds it to the folder it was made in.
type lastRound struct {
	At    time.Time `json:"at"`
	Root  string    `json:"root"`
	Moves []Move    `json:"moves"` // From = where it was, To = where it is now
}

// relocate moves one file by its project-relative paths. Every folder on the
// way is checked with safepath (no link, no junction), the file must still be
// the one that was planned, and neither path may be in a skipped folder. It
// returns the final relative path.
// beforePlace runs between the folder walk and the move; tests use it to swap
// a folder for a junction in that gap.
var beforePlace = func() {}

func relocate(realRoot string, skips []string, from, to string, size int64, mtime time.Time) (string, error) {
	if !planOK(from) || !planOK(to) || inSkipped(from, skips) || inSkipped(to, skips) {
		return "", errors.New("not a plain project path")
	}
	// git runs first: nothing slow between the folder walk and the move
	tracked := gitTracked(realRoot, from)
	src, fi, err := safepath.File(realRoot, from)
	if err != nil {
		return "", err
	}
	if fi.Size() != size || !fi.ModTime().Equal(mtime) {
		return "", errors.New("changed since it was planned")
	}
	slash := strings.LastIndex(to, "/")
	dirRel, name := "", to
	if slash >= 0 {
		dirRel, name = to[:slash], to[slash+1:]
	}
	dir, err := safepath.Dir(realRoot, dirRel, true)
	if err != nil {
		return "", err
	}
	beforePlace()
	got, err := place(realRoot, src, dir, name, tracked)
	if err != nil {
		return "", err
	}
	rel := got
	if dirRel != "" {
		rel = dirRel + "/" + got
	}
	// the folder may have been swapped for a link meanwhile: the file must be
	// reachable at rel by a fresh safe walk, else it goes back where it was
	placed := filepath.Join(dir, got)
	if err := insideAfter(realRoot, rel, placed); err != nil {
		if berr := putBack(placed, src); berr != nil {
			return "", fmt.Errorf("%v; and it could not be put back from %s: %v", err, placed, berr)
		}
		return "", err
	}
	return rel, nil
}

// insideAfter checks that the file just placed at placed is the one a safe
// walk from the root finds at rel.
func insideAfter(realRoot, rel, placed string) error {
	pfi, err := os.Lstat(placed)
	if err != nil {
		return err
	}
	_, wfi, err := safepath.File(realRoot, rel)
	if err != nil || !os.SameFile(pfi, wfi) {
		return errors.New("the folder changed during the move; the file stays where it was")
	}
	return nil
}

// putBack returns a file to src without replacing anything there.
func putBack(placed, src string) error {
	if err := os.Link(placed, src); err == nil {
		return os.Remove(placed)
	} else if os.IsExist(err) {
		return err
	}
	if _, err := os.Lstat(src); err == nil {
		return os.ErrExist
	}
	return os.Rename(placed, src)
}

// ApplyTidy moves the files the person confirmed. A file that changed since the
// plan, or whose folder leads out of the project, is skipped. The round is
// saved in a.Record so it can be undone.
func ApplyTidy(a Area, moves []Move) (done []Move, skipped []string, err error) {
	done, skipped = []Move{}, []string{}
	skips := skipRels(a.Root, a.Skip)
	for _, m := range moves {
		to, e := relocate(a.Root, skips, m.From, m.To, m.Size, m.MTime)
		if e != nil {
			skipped = append(skipped, m.From)
			continue
		}
		done = append(done, Move{From: m.From, To: to, Size: m.Size, MTime: m.MTime})
		_ = project.Audit(a.Audit, "tidy", m.From+" -> "+to)
	}
	if len(done) > 0 {
		raw, e := json.MarshalIndent(lastRound{At: time.Now().UTC(), Root: a.Root, Moves: done}, "", "  ")
		if e == nil {
			if e = os.MkdirAll(filepath.Dir(a.Record), 0o700); e == nil {
				e = os.WriteFile(a.Record, raw, 0o600)
			}
		}
		if e != nil {
			err = fmt.Errorf("the files moved, but the undo record could not be saved: %w", e)
		}
	}
	return done, skipped, err
}

// Undo is what an undo would put back, for the person to confirm.
type Undo struct {
	At    time.Time `json:"at"`
	Moves []Move    `json:"moves"`
}

// ReadUndo returns the last round, when there is one made in a.Root.
func ReadUndo(a Area) (Undo, error) {
	raw, err := os.ReadFile(a.Record)
	if err != nil {
		return Undo{}, errors.New("there is no tidy to undo")
	}
	var last lastRound
	if err := json.Unmarshal(raw, &last); err != nil {
		return Undo{}, errors.New("the undo record is damaged")
	}
	if last.Root == "" || fold(filepath.Clean(last.Root)) != fold(filepath.Clean(a.Root)) {
		return Undo{}, errors.New("the last tidy was made in another folder; it cannot be undone here")
	}
	if last.Moves == nil {
		last.Moves = []Move{}
	}
	return Undo{At: last.At, Moves: last.Moves}, nil
}

// CanUndo reports whether a last round can be undone.
func CanUndo(a Area) bool {
	_, err := ReadUndo(a)
	return err == nil
}

// UndoTidy puts the last round back, if it is the round made at at (the list
// the person saw and confirmed). A file changed since the move stays where it
// is and is reported in skipped.
func UndoTidy(a Area, at time.Time) (done, skipped []string, err error) {
	done, skipped = []string{}, []string{}
	last, err := ReadUndo(a)
	if err != nil {
		return done, skipped, err
	}
	if !last.At.Equal(at) {
		return done, skipped, errors.New("the tidy to undo has changed; look at the list again")
	}
	skips := skipRels(a.Root, a.Skip)
	for i := len(last.Moves) - 1; i >= 0; i-- {
		m := last.Moves[i]
		back, e := relocate(a.Root, skips, m.To, m.From, m.Size, m.MTime)
		if e != nil {
			skipped = append(skipped, m.To)
			continue
		}
		done = append(done, back)
		_ = project.Audit(a.Audit, "tidy-undo", m.To+" -> "+back)
	}
	// done is reported in the order the files were first moved
	for i, j := 0, len(done)-1; i < j; i, j = i+1, j-1 {
		done[i], done[j] = done[j], done[i]
	}
	sort.Strings(skipped)
	if err := os.Remove(a.Record); err != nil {
		return done, skipped, err
	}
	return done, skipped, nil
}
