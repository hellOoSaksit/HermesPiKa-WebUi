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
)

// LastFile is the record of the last tidy round, beside project.yaml.
const LastFile = "tidy-last.json"

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

// skipName: names a tidy never touches. Hidden entries, node_modules, and
// names the file browser refuses (keys, credentials, Windows device names).
func skipName(name string) bool {
	if name == "node_modules" || strings.HasPrefix(name, ".") {
		return true
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return true
	}
	l := strings.ToLower(name)
	for _, ext := range []string{".pem", ".key", ".p12", ".pfx", ".kdbx"} {
		if strings.HasSuffix(l, ext) {
			return true
		}
	}
	if strings.HasPrefix(l, "id_rsa") || strings.HasPrefix(l, "id_ed25519") || strings.HasPrefix(l, "credentials") {
		return true
	}
	base := strings.ToLower(strings.SplitN(name, ".", 2)[0])
	switch base {
	case "con", "prn", "aux", "nul", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		return true
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

// PlanTidy lists the files that are not in the folder their extension's rule names.
func PlanTidy(root string, r Rules) (Plan, error) {
	plan := Plan{Moves: []Move{}, Homeless: []string{}}
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
		if skipName(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || d.IsDir() {
			return nil // links are never followed
		}
		fi, err := d.Info()
		if err != nil || !fi.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		files = append(files, cand{filepath.ToSlash(rel), fi})
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
		plan.Moves = append(plan.Moves, Move{From: f.rel, To: strings.Trim(folder, "/") + "/" + name, Size: f.fi.Size(), MTime: f.fi.ModTime()})
	}
	sort.Slice(plan.Moves, func(i, j int) bool { return plan.Moves[i].From < plan.Moves[j].From })
	sort.Strings(plan.Homeless)
	return plan, nil
}

// relOK refuses a path that is not a plain project-relative path.
func relOK(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.ContainsAny(rel, "\\:\x00") || filepath.IsAbs(rel) {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func inside(root, p string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// destDir resolves a project-relative folder for writing, creating it when
// missing. It must stay inside the project after following links.
func destDir(realRoot, rel string) (string, error) {
	dest := filepath.Join(realRoot, filepath.FromSlash(rel))
	probe := dest
	for {
		if _, err := os.Lstat(probe); err == nil {
			break
		}
		next := filepath.Dir(probe)
		if next == probe {
			return "", errors.New("outside the project")
		}
		probe = next
	}
	real, err := filepath.EvalSymlinks(probe)
	if err != nil || !inside(realRoot, real) {
		return "", errors.New("outside the project")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	real, err = filepath.EvalSymlinks(dest)
	if err != nil || !inside(realRoot, real) {
		return "", errors.New("outside the project")
	}
	return real, nil
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

type lastRound struct {
	At    time.Time `json:"at"`
	Moves []Move    `json:"moves"` // From = where it was, To = where it is now
}

// relocate moves one file by its project-relative paths, checking links and that
// the file is still the one that was planned. It returns the final relative path.
func relocate(root, realRoot, from, to string, size int64, mtime time.Time) (string, error) {
	if !relOK(from) || !relOK(to) {
		return "", errors.New("not a plain project path")
	}
	src := filepath.Join(realRoot, filepath.FromSlash(from))
	fi, err := os.Lstat(src)
	if err != nil || !fi.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	if fi.Size() != size || !fi.ModTime().Equal(mtime) {
		return "", errors.New("changed since it was planned")
	}
	if real, err := filepath.EvalSymlinks(filepath.Dir(src)); err != nil || !inside(realRoot, real) {
		return "", errors.New("outside the project")
	}
	slash := strings.LastIndex(to, "/")
	dirRel, name := "", to
	if slash >= 0 {
		dirRel, name = to[:slash], to[slash+1:]
	}
	dir := realRoot
	if dirRel != "" {
		if dir, err = destDir(realRoot, dirRel); err != nil {
			return "", err
		}
	}
	got, err := place(realRoot, src, dir, name, gitTracked(realRoot, from))
	if err != nil {
		return "", err
	}
	if dirRel == "" {
		return got, nil
	}
	return dirRel + "/" + got, nil
}

// ApplyTidy moves the files the person confirmed. A file that changed since the
// plan, or whose folder leads out of the project, is skipped. The round is
// saved in projectDir/tidy-last.json so it can be undone.
func ApplyTidy(root, projectDir string, moves []Move) (done []Move, skipped []string, err error) {
	done, skipped = []Move{}, []string{}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return done, skipped, err
	}
	for _, m := range moves {
		to, e := relocate(root, realRoot, m.From, m.To, m.Size, m.MTime)
		if e != nil {
			skipped = append(skipped, m.From)
			continue
		}
		done = append(done, Move{From: m.From, To: to, Size: m.Size, MTime: m.MTime})
		_ = project.Audit(projectDir, "tidy", m.From+" -> "+to)
	}
	if len(done) > 0 {
		raw, e := json.MarshalIndent(lastRound{At: time.Now().UTC(), Moves: done}, "", "  ")
		if e == nil {
			e = os.WriteFile(filepath.Join(projectDir, LastFile), raw, 0o644)
		}
		if e != nil {
			err = fmt.Errorf("the files moved, but the undo record could not be saved: %w", e)
		}
	}
	return done, skipped, err
}

// CanUndo reports whether a last round is saved.
func CanUndo(projectDir string) bool {
	_, err := os.Stat(filepath.Join(projectDir, LastFile))
	return err == nil
}

// UndoTidy puts the last round back. A file changed since the move stays where
// it is and is reported in skipped.
func UndoTidy(root, projectDir string) (done, skipped []string, err error) {
	done, skipped = []string{}, []string{}
	file := filepath.Join(projectDir, LastFile)
	raw, err := os.ReadFile(file)
	if err != nil {
		return done, skipped, errors.New("there is no tidy to undo")
	}
	var last lastRound
	if err := json.Unmarshal(raw, &last); err != nil {
		return done, skipped, errors.New("the undo record is damaged")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return done, skipped, err
	}
	for i := len(last.Moves) - 1; i >= 0; i-- {
		m := last.Moves[i]
		back, e := relocate(root, realRoot, m.To, m.From, m.Size, m.MTime)
		if e != nil {
			skipped = append(skipped, m.To)
			continue
		}
		done = append(done, back)
		_ = project.Audit(projectDir, "tidy-undo", m.To+" -> "+back)
	}
	// done is reported in the order the files were first moved
	for i, j := 0, len(done)-1; i < j; i, j = i+1, j-1 {
		done[i], done[j] = done[j], done[i]
	}
	sort.Strings(skipped)
	if err := os.Remove(file); err != nil {
		return done, skipped, err
	}
	return done, skipped, nil
}
