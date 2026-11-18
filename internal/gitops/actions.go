// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package gitops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"hpk.local/hpk/internal/project"
)

// Step is one line of an interactive rebase, oldest first.
type Step struct {
	SHA    string `json:"sha"`
	Action string `json:"action"` // pick | reword | squash | fixup | drop
	Msg    string `json:"msg"`
}

// Request is one command from the Git screen.
type Request struct {
	Action string `json:"action"`
	Branch string `json:"branch,omitempty"`
	Onto   string `json:"onto,omitempty"`
	From   string `json:"from,omitempty"`
	Into   string `json:"into,omitempty"`
	SHA    string `json:"sha,omitempty"`
	Mode   string `json:"mode,omitempty"` // reset: soft | mixed | hard
	Name   string `json:"name,omitempty"`
	At     string `json:"at,omitempty"`
	Switch bool   `json:"switch,omitempty"`
	Stash  string `json:"stash,omitempty"`
	Undo   string `json:"undo,omitempty"`
	Steps  []Step `json:"steps,omitempty"`
	File   string `json:"file,omitempty"`
	Side   string `json:"side,omitempty"` // resolve: ours | theirs
	Remote bool   `json:"remote,omitempty"`
}

// Result says what ran.
type Result struct {
	Commands []string `json:"commands"`
	Output   string   `json:"output,omitempty"`
	Conflict bool     `json:"conflict,omitempty"`
	Note     string   `json:"note,omitempty"`
}

// RedLine names the red line an action crosses ("" for none), so the server can
// check the person's role and record the decision.
func RedLine(r Request, m *Model) string {
	switch r.Action {
	case "push", "pr":
		return "push"
	case "delete":
		return "delete"
	case "reset":
		if r.Mode == "hard" {
			return "delete"
		}
	case "merge":
		for _, b := range m.Branches {
			if b.Name == r.Into && b.Base {
				return "push" // into the base branch
			}
		}
	case "rebase", "interactive":
		name := r.Branch
		for _, b := range m.Branches {
			if b.Name == name && b.Upstream != "" {
				return "push" // rewrites pushed history (force-with-lease)
			}
		}
	}
	return ""
}

var (
	mu        sync.Mutex // one git operation per hpk serve at a time
	shaRE     = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	stashRE   = regexp.MustCompile(`^stash@\{\d{1,4}\}$`)
	errNoRepo = errors.New("not a git repository")
)

type runner struct {
	ctx  context.Context
	res  *Result
	root string
}

func (r *runner) git(dir string, args ...string) (string, error) {
	r.res.Commands = append(r.res.Commands, "git "+strings.Join(args, " "))
	out, err := git(r.ctx, dir, args...)
	if out != "" {
		r.res.Output = strings.TrimSpace(r.res.Output + "\n" + out)
	}
	return out, err
}

func (r *runner) checkRef(name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%q is not a branch name", name)
	}
	if _, err := git(r.ctx, r.root, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("git does not allow the branch name %q", name)
	}
	return nil
}

func (r *runner) exists(branch string) bool {
	_, err := git(r.ctx, r.root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// remoteExists: origin has the branch (a PR base may exist only there).
func (r *runner) remoteExists(branch string) bool {
	_, err := git(r.ctx, r.root, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return err == nil
}

func (r *runner) commit(sha string) error {
	if !shaRE.MatchString(sha) {
		return fmt.Errorf("%q is not a commit id", sha)
	}
	if _, err := git(r.ctx, r.root, "cat-file", "-e", sha+"^{commit}"); err != nil {
		return fmt.Errorf("no commit %s", sha)
	}
	return nil
}

func clean(ctx context.Context, dir string) error {
	s, err := git(ctx, dir, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "" {
		return errDirty
	}
	return nil
}

// onBranch runs fn in the folder where branch is checked out (it must be clean),
// or in a throw-away worktree. A worktree is kept when fn stops on a conflict.
func (r *runner) onBranch(p *project.Project, branch string, fn func(dir string) error) error {
	if dir := worktrees(r.ctx, r.root)[branch]; dir != "" {
		if err := clean(r.ctx, dir); err != nil {
			return fmt.Errorf("%s (in %s)", err, dir)
		}
		return fn(dir)
	}
	tmp, err := tempDir(p)
	if err != nil {
		return err
	}
	if _, err := r.git(r.root, "worktree", "add", "--quiet", tmp, branch); err != nil {
		return err
	}
	err = fn(tmp)
	if !r.res.Conflict {
		_, _ = git(r.ctx, r.root, "worktree", "remove", "--force", tmp)
	}
	return err
}

func tempDir(p *project.Project) (string, error) {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	d := filepath.Join(p.Dir, "git-work", hex.EncodeToString(b))
	return d, os.MkdirAll(filepath.Dir(d), 0o755)
}

// stopped marks a conflict: the operation waits in its folder for the person.
func (r *runner) stopped(err error, dir string) error {
	if err == nil {
		return nil
	}
	if ip := inProgress(r.ctx, dir, nil); ip != nil && len(ip.Files) > 0 {
		r.res.Conflict = true
		r.res.Note = "stopped on a conflict in " + ip.Files[0].Path
		return nil
	}
	return err
}

func (r *runner) upstream(branch string) string {
	s, err := git(r.ctx, r.root, "rev-parse", "--abbrev-ref", branch+"@{upstream}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// Do runs one request on the project's repository. who goes in the undo log.
func Do(ctx context.Context, p *project.Project, req Request, who string) (*Result, error) {
	mu.Lock()
	defer mu.Unlock()
	res := &Result{}
	r := &runner{ctx: ctx, res: res, root: p.Root}
	if _, err := git(ctx, p.Root, "rev-parse", "--git-dir"); err != nil {
		return res, errNoRepo
	}
	head, _ := git(ctx, p.Root, "symbolic-ref", "--quiet", "--short", "HEAD")
	// A name that starts with "-" would be read by git (or gh) as an option
	// (--output=, --exec=, --receive-pack=…): no branch name may.
	for _, n := range []string{req.Branch, req.Onto, req.From, req.Into, req.Name} {
		if strings.HasPrefix(strings.TrimSpace(n), "-") {
			return res, fmt.Errorf("%q is not a branch name", n)
		}
	}
	err := func() error {
		switch req.Action {
		case "fetch":
			_, err := r.git(r.root, "fetch", "--prune", "--all")
			return err

		case "pull":
			if !r.exists(req.Branch) {
				return fmt.Errorf("no branch %s", req.Branch)
			}
			return r.onBranch(p, req.Branch, func(dir string) error { _, err := r.git(dir, "pull", "--ff-only"); return err })

		case "push":
			if !r.exists(req.Branch) {
				return fmt.Errorf("no branch %s", req.Branch)
			}
			if r.upstream(req.Branch) == "" {
				_, err := r.git(r.root, "push", "-u", "origin", req.Branch)
				return err
			}
			_, err := r.git(r.root, "push", "origin", req.Branch)
			return err

		case "branch":
			if err := r.checkRef(req.Name); err != nil {
				return err
			}
			if r.exists(req.Name) {
				return fmt.Errorf("a branch %s already exists", req.Name)
			}
			at := req.At
			if at == "" {
				at = "HEAD"
			} else if err := r.commit(at); err != nil {
				return err
			}
			if req.Switch {
				if err := clean(ctx, r.root); err != nil {
					return err
				}
				_, err := r.git(r.root, "switch", "-c", req.Name, at)
				return err
			}
			_, err := r.git(r.root, "branch", req.Name, at)
			return err

		case "checkout":
			if !r.exists(req.Branch) {
				return fmt.Errorf("no branch %s", req.Branch)
			}
			if err := clean(ctx, r.root); err != nil {
				return err
			}
			_, err := r.git(r.root, "switch", req.Branch)
			return err

		case "merge":
			if !r.exists(req.From) || !r.exists(req.Into) {
				return errors.New("both branches must exist")
			}
			saveUndo(ctx, p, "merge "+req.From+" into "+req.Into, who)
			return r.onBranch(p, req.Into, func(dir string) error {
				_, err := r.git(dir, "merge", "--no-ff", "--no-edit", req.From)
				return r.stopped(err, dir)
			})

		case "rebase":
			if !r.exists(req.Branch) || !r.exists(req.Onto) {
				return errors.New("both branches must exist")
			}
			saveUndo(ctx, p, "rebase "+req.Branch+" onto "+req.Onto, who)
			up := r.upstream(req.Branch)
			err := r.onBranch(p, req.Branch, func(dir string) error {
				_, err := r.git(dir, "rebase", req.Onto)
				return r.stopped(err, dir)
			})
			if err == nil && !res.Conflict && up != "" {
				_, err = r.git(r.root, "push", "--force-with-lease", "origin", req.Branch)
			}
			return err

		case "interactive":
			return r.interactive(p, req, who)

		case "cherry":
			if err := r.commit(req.SHA); err != nil {
				return err
			}
			if err := clean(ctx, r.root); err != nil {
				return err
			}
			_, err := r.git(r.root, "cherry-pick", req.SHA)
			return r.stopped(err, r.root)

		case "revert":
			if err := r.commit(req.SHA); err != nil {
				return err
			}
			if err := clean(ctx, r.root); err != nil {
				return err
			}
			_, err := r.git(r.root, "revert", "--no-edit", req.SHA)
			return r.stopped(err, r.root)

		case "reset":
			if err := r.commit(req.SHA); err != nil {
				return err
			}
			if req.Mode != "soft" && req.Mode != "mixed" && req.Mode != "hard" {
				return errors.New("reset mode is soft, mixed or hard")
			}
			saveUndo(ctx, p, "reset "+head+" to "+req.SHA, who)
			_, err := r.git(r.root, "reset", "--"+req.Mode, req.SHA)
			return err

		case "delete":
			if !r.exists(req.Branch) {
				return fmt.Errorf("no branch %s", req.Branch)
			}
			if req.Branch == head {
				return errors.New("switch to another branch before deleting this one")
			}
			up := r.upstream(req.Branch)
			saveUndo(ctx, p, "delete "+req.Branch, who)
			if _, err := r.git(r.root, "branch", "-D", req.Branch); err != nil {
				return err
			}
			if req.Remote && up != "" {
				_, err := r.git(r.root, "push", "origin", "--delete", strings.TrimPrefix(up, "origin/"))
				return err
			}
			return nil

		case "stash":
			_, err := r.git(r.root, "stash", "push", "--include-untracked", "-m", "hpk: "+time.Now().Format("2006-01-02 15:04"))
			return err

		case "pop":
			if !stashRE.MatchString(req.Stash) {
				return errors.New("not a stash id")
			}
			_, err := r.git(r.root, "stash", "pop", req.Stash)
			return r.stopped(err, r.root)

		case "undo":
			return r.restore(p, req.Undo)

		case "resolve", "continue", "abort":
			return r.finish(p, req)

		case "pr":
			return r.openPR(p, req)
		}
		return fmt.Errorf("unknown action %q", req.Action)
	}()
	if err != nil {
		return res, err
	}
	return res, nil
}

// interactive replays the branch's commits onto base in a throw-away worktree with
// cherry-pick, applying keep / reword / squash / fixup / drop, then moves the
// branch. Nothing changes if a step conflicts.
func (r *runner) interactive(p *project.Project, req Request, who string) error {
	if !r.exists(req.Branch) || !r.exists(req.Onto) {
		return errors.New("both branches must exist")
	}
	if len(req.Steps) == 0 {
		return errors.New("no steps")
	}
	for i, s := range req.Steps {
		if err := r.commit(s.SHA); err != nil {
			return err
		}
		switch s.Action {
		case "pick", "reword", "squash", "fixup", "drop":
		default:
			return fmt.Errorf("unknown step %q", s.Action)
		}
		if i == 0 && (s.Action == "squash" || s.Action == "fixup") {
			return errors.New("the first commit cannot be squashed into one above it")
		}
		if (s.Action == "reword" || s.Action == "squash") && strings.TrimSpace(s.Msg) == "" {
			return errors.New("a reworded commit needs a message")
		}
	}
	base, err := git(r.ctx, r.root, "merge-base", req.Branch, req.Onto)
	if err != nil {
		return err
	}
	tmp, err := tempDir(p)
	if err != nil {
		return err
	}
	if _, err := r.git(r.root, "worktree", "add", "--quiet", "--detach", tmp, strings.TrimSpace(base)); err != nil {
		return err
	}
	defer git(r.ctx, r.root, "worktree", "remove", "--force", tmp)
	for _, s := range req.Steps {
		var err error
		switch s.Action {
		case "drop":
			continue
		case "pick":
			_, err = r.git(tmp, "cherry-pick", "--allow-empty", s.SHA)
		case "reword":
			if _, err = r.git(tmp, "cherry-pick", "--allow-empty", s.SHA); err == nil {
				_, err = r.git(tmp, "commit", "--amend", "--allow-empty", "-m", s.Msg)
			}
		case "fixup":
			if _, err = r.git(tmp, "cherry-pick", "--no-commit", s.SHA); err == nil {
				_, err = r.git(tmp, "commit", "--amend", "--allow-empty", "--no-edit")
			}
		case "squash":
			prev, _ := git(r.ctx, tmp, "log", "-1", "--format=%B")
			if _, err = r.git(tmp, "cherry-pick", "--no-commit", s.SHA); err == nil {
				_, err = r.git(tmp, "commit", "--amend", "--allow-empty", "-m", strings.TrimSpace(prev)+"\n\n"+s.Msg)
			}
		}
		if err != nil {
			_, _ = git(r.ctx, tmp, "cherry-pick", "--abort")
			return fmt.Errorf("step %s %s does not apply cleanly; nothing was changed", s.Action, s.SHA)
		}
	}
	tip, err := git(r.ctx, tmp, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	tip = strings.TrimSpace(tip)
	saveUndo(r.ctx, p, "interactive rebase of "+req.Branch, who)
	up := r.upstream(req.Branch)
	if dir := worktrees(r.ctx, r.root)[req.Branch]; dir != "" {
		if err := clean(r.ctx, dir); err != nil {
			return err
		}
		if _, err := r.git(dir, "reset", "--keep", tip); err != nil {
			return err
		}
	} else if _, err := r.git(r.root, "branch", "-f", req.Branch, tip); err != nil {
		return err
	}
	if up != "" {
		_, err = r.git(r.root, "push", "--force-with-lease", "origin", req.Branch)
		return err
	}
	return nil
}

// finish resolves a conflicted file, or continues / aborts the stopped operation.
func (r *runner) finish(p *project.Project, req Request) error {
	ip := inProgress(r.ctx, r.root, worktrees(r.ctx, r.root))
	if ip == nil {
		return errors.New("nothing is waiting on a conflict")
	}
	dir := ip.Dir
	switch req.Action {
	case "resolve":
		ok := false
		for _, f := range ip.Files {
			ok = ok || f.Path == req.File
		}
		if !ok {
			return fmt.Errorf("%s is not conflicted", req.File)
		}
		if req.Side != "ours" && req.Side != "theirs" {
			return errors.New("pick ours or theirs")
		}
		if _, err := r.git(dir, "checkout", "--"+req.Side, "--", req.File); err != nil {
			return err
		}
		_, err := r.git(dir, "add", "--", req.File)
		return err
	case "continue":
		if left, _ := git(r.ctx, dir, "diff", "--name-only", "--diff-filter=U"); strings.TrimSpace(left) != "" {
			return errors.New("resolve every conflicted file first")
		}
		_, err := r.git(dir, ip.Kind, "--continue")
		if err := r.stopped(err, dir); err != nil {
			return err
		}
	case "abort":
		if _, err := r.git(dir, ip.Kind, "--abort"); err != nil {
			return err
		}
	}
	// a throw-away worktree goes once the operation is over
	if !r.res.Conflict && strings.HasPrefix(dir, filepath.Join(p.Dir, "git-work")) {
		_, _ = git(r.ctx, r.root, "worktree", "remove", "--force", dir)
	}
	return nil
}

// openPR opens a draft pull request with gh (pushing the branch first if needed).
func (r *runner) openPR(p *project.Project, req Request) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("gh (GitHub CLI) is not installed; for GitLab use glab mr create --draft")
	}
	if !r.exists(req.Branch) {
		return fmt.Errorf("no branch %s", req.Branch)
	}
	into := req.Into
	if into == "" {
		into = baseBranch(r.ctx, r.root, p)
	}
	if !r.exists(into) && !r.remoteExists(into) {
		return fmt.Errorf("no branch %s", into)
	}
	if r.upstream(req.Branch) == "" {
		if _, err := r.git(r.root, "push", "-u", "origin", req.Branch); err != nil {
			return err
		}
	}
	title, _ := git(r.ctx, r.root, "log", "-1", "--format=%s", "refs/heads/"+req.Branch, "--")
	r.res.Commands = append(r.res.Commands, "gh pr create --draft --base "+into+" --head "+req.Branch)
	ctx, cancel := context.WithTimeout(r.ctx, time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "pr", "create", "--draft", "--base", into, "--head", req.Branch, "--title", strings.TrimSpace(title), "--body", "Opened from HPK Console.")
	cmd.Dir = r.root
	out, err := cmd.CombinedOutput()
	r.res.Output = strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("gh pr create: %s", lastLines(string(out), err))
	}
	invalidatePRs()
	return nil
}

// ---- undo points ----

// UndoPoint is where every branch pointed before a history-changing command.
type UndoPoint struct {
	ID   string            `json:"id"`
	What string            `json:"what"`
	By   string            `json:"by,omitempty"`
	At   int64             `json:"at"`
	Head string            `json:"head"`
	Tips map[string]string `json:"tips"`
}

const keepUndo = 30

func undoFile(p *project.Project) string { return filepath.Join(p.Dir, "git-undo.json") }

func loadUndo(p *project.Project) []UndoPoint {
	var list []UndoPoint
	if raw, err := os.ReadFile(undoFile(p)); err == nil {
		_ = json.Unmarshal(raw, &list)
	}
	return list
}

// saveUndo records every branch tip, and keeps the commits alive with
// refs/hpk/undo/<n>/<i> so git never prunes what Undo needs.
func saveUndo(ctx context.Context, p *project.Project, what, who string) {
	out, err := git(ctx, p.Root, "for-each-ref", "refs/heads", "--format=%(refname:short)"+sep+"%(objectname)")
	if err != nil {
		return
	}
	list := loadUndo(p)
	n := 1
	if len(list) > 0 {
		fmt.Sscanf(strings.TrimPrefix(list[0].ID, "refs/hpk/undo/"), "%d", &n)
		n++
	}
	u := UndoPoint{ID: fmt.Sprintf("refs/hpk/undo/%d", n), What: what, By: who, At: time.Now().UnixMilli(), Tips: map[string]string{}}
	u.Head, _ = git(ctx, p.Root, "symbolic-ref", "--quiet", "--short", "HEAD")
	i := 0
	for _, l := range nonEmpty(out) {
		f := strings.SplitN(l, sep, 2)
		if len(f) == 2 {
			u.Tips[f[0]] = f[1]
			_, _ = git(ctx, p.Root, "update-ref", fmt.Sprintf("%s/%d", u.ID, i), f[1])
			i++
		}
	}
	list = append([]UndoPoint{u}, list...)
	for len(list) > keepUndo {
		old := list[len(list)-1]
		refs, _ := git(ctx, p.Root, "for-each-ref", "--format=%(refname)", old.ID+"/")
		for _, ref := range nonEmpty(refs) {
			_, _ = git(ctx, p.Root, "update-ref", "-d", ref)
		}
		list = list[:len(list)-1]
	}
	raw, _ := json.MarshalIndent(list, "", "  ")
	_ = os.WriteFile(undoFile(p), raw, 0o644)
}

// restore puts every branch back where an undo point saw it.
func (r *runner) restore(p *project.Project, id string) error {
	list := loadUndo(p)
	var u *UndoPoint
	for i := range list {
		if list[i].ID == id {
			u = &list[i]
		}
	}
	if u == nil {
		return fmt.Errorf("no undo point %s", id)
	}
	where := worktrees(r.ctx, r.root)
	for name, sha := range u.Tips {
		cur, err := git(r.ctx, r.root, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
		if err == nil && strings.TrimSpace(cur) == sha {
			continue
		}
		switch dir := where[name]; {
		case err != nil: // deleted since: bring it back
			if _, err := r.git(r.root, "branch", name, sha); err != nil {
				return err
			}
		case dir != "":
			if err := clean(r.ctx, dir); err != nil {
				return fmt.Errorf("%s (in %s)", err, dir)
			}
			if _, err := r.git(dir, "reset", "--keep", sha); err != nil {
				return err
			}
		default:
			if _, err := r.git(r.root, "update-ref", "refs/heads/"+name, sha); err != nil {
				return err
			}
		}
	}
	r.res.Note = "branches are back where they were at " + time.UnixMilli(u.At).Format("15:04")
	return nil
}
