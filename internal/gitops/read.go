// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package gitops is the HPK Console's Git screen on a real repository: it reads
// the history, branches, remotes and stashes with git, and runs the commands the
// screen offers (fetch, pull, push, branch, merge, rebase, interactive rebase,
// cherry-pick, revert, reset, stash, undo, conflicts). It never rewrites history
// without an undo point, never touches a dirty working folder, and does work on
// a branch inside the folder where that branch is checked out, or in a
// throw-away worktree when it is checked out nowhere, so the person's own
// checkout is left alone.
package gitops

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hpk.local/hpk/internal/project"
)

// Commit is one commit, newest first in Model.Commits.
type Commit struct {
	ID      string     `json:"id"`
	Full    string     `json:"full"`
	Msg     string     `json:"msg"`
	Author  string     `json:"author"`
	At      int64      `json:"at"` // unix ms
	Parents []string   `json:"parents"`
	Files   [][3]any   `json:"files"` // [path, added, deleted]
	files   []fileStat // parsed numstat
}

type fileStat struct {
	path     string
	add, del int
}

// Branch is a local branch.
type Branch struct {
	Name     string `json:"name"`
	Tip      string `json:"tip"`
	Remote   string `json:"remote,omitempty"`   // short id of its upstream, when it has one
	Upstream string `json:"upstream,omitempty"` // e.g. origin/main
	Base     bool   `json:"base,omitempty"`
	Where    string `json:"where,omitempty"` // folder where it is checked out
	Owner    string `json:"owner,omitempty"`
	Session  string `json:"session,omitempty"`
	Claim    string `json:"claim,omitempty"`
	PR       *PR    `json:"pr,omitempty"`
}

// PR is what gh reports for an open pull request.
type PR struct {
	N     int    `json:"n"`
	Draft bool   `json:"draft"`
	CI    string `json:"ci"` // pass | fail | running | none
	URL   string `json:"url,omitempty"`
}

// Stash is one stash entry.
type Stash struct {
	ID  string `json:"id"`
	Msg string `json:"msg"`
	At  int64  `json:"at"`
}

// InProgress is a rebase, merge, cherry-pick or revert that stopped on a conflict.
type InProgress struct {
	Kind  string     `json:"kind"`
	Dir   string     `json:"dir"`
	Files []Conflict `json:"files"`
}

// Conflict is one conflicted file with both sides.
type Conflict struct {
	Path   string `json:"path"`
	Ours   string `json:"ours"`   // what the target already has (for rebase: the branch it lands on)
	Theirs string `json:"theirs"` // what is being brought in
}

// Model is everything the Git screen draws.
type Model struct {
	Live       bool        `json:"live"`
	Root       string      `json:"root"`
	Head       string      `json:"head"`
	Me         string      `json:"me"`
	Commits    []*Commit   `json:"commits"`
	Branches   []*Branch   `json:"branches"`
	Stashes    []Stash     `json:"stashes"`
	Undo       []UndoPoint `json:"undo"`
	Dirty      []string    `json:"dirty,omitempty"` // changed files in the main checkout
	InProgress *InProgress `json:"inProgress,omitempty"`
	Remote     string      `json:"remote,omitempty"` // URL of origin, if any
	Error      string      `json:"error,omitempty"`
}

// MaxCommits is how much history the screen draws.
const MaxCommits = 300

func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true", "GIT_MERGE_AUTOEDIT=no", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	s := strings.TrimRight(string(out), "\r\n")
	if err != nil {
		return s, fmt.Errorf("git %s: %s", strings.Join(args, " "), lastLines(s, err))
	}
	return s, nil
}

func lastLines(s string, err error) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return err.Error()
	}
	l := strings.Split(s, "\n")
	if len(l) > 6 {
		l = l[len(l)-6:]
	}
	return strings.Join(l, " / ")
}

const sep = "\x1f"

// Load reads the project's repository. me is the person looking (for "mine").
func Load(ctx context.Context, p *project.Project, me string) (*Model, error) {
	root := p.Root
	if _, err := git(ctx, root, "rev-parse", "--git-dir"); err != nil {
		return &Model{Root: root, Error: "not a git repository"}, nil
	}
	m := &Model{Live: true, Root: root, Me: me}
	m.Head, _ = git(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	m.Remote, _ = git(ctx, root, "remote", "get-url", "origin")

	// history with file stats, newest first, children before parents. Branches,
	// remotes, tags and HEAD only: undo points (refs/hpk/undo) and stashes would
	// keep rewritten commits on the picture.
	which := []string{"--branches", "--remotes", "--tags"}
	if _, err := git(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		which = append(which, "HEAD")
	}
	args := append([]string{"log"}, which...)
	args = append(args, "--topo-order", "-n", strconv.Itoa(MaxCommits), "--numstat",
		"--format=@@"+strings.Join([]string{"%H", "%P", "%an", "%at", "%s"}, sep))
	out, err := git(ctx, root, args...)
	if err != nil {
		return nil, err
	}
	var cur *Commit
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "@@") {
			f := strings.SplitN(line[2:], sep, 5)
			if len(f) < 5 {
				continue
			}
			at, _ := strconv.ParseInt(f[3], 10, 64)
			cur = &Commit{ID: f[0][:7], Full: f[0], Author: f[2], At: at * 1000, Msg: f[4], Files: [][3]any{}}
			for _, par := range strings.Fields(f[1]) {
				cur.Parents = append(cur.Parents, par[:7])
			}
			m.Commits = append(m.Commits, cur)
			continue
		}
		if cur == nil || strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 {
			continue
		}
		a, _ := strconv.Atoi(f[0]) // "-" for binary files: 0
		d, _ := strconv.Atoi(f[1])
		cur.files = append(cur.files, fileStat{f[2], a, d})
		cur.Files = append(cur.Files, [3]any{f[2], a, d})
	}
	// parents outside the window would leave dangling lines: drop them
	have := map[string]bool{}
	for _, c := range m.Commits {
		have[c.ID] = true
	}
	for _, c := range m.Commits {
		var ps []string
		for _, par := range c.Parents {
			if have[par] {
				ps = append(ps, par)
			}
		}
		c.Parents = ps
	}

	// branches, their upstreams, and where each is checked out
	refs, _ := git(ctx, root, "for-each-ref", "refs/heads", "--format=%(refname:short)"+sep+"%(objectname:short=7)"+sep+"%(upstream:short)")
	where := worktrees(ctx, root)
	base := baseBranch(ctx, root, p)
	for _, l := range nonEmpty(refs) {
		f := strings.SplitN(l, sep, 3)
		if len(f) < 3 {
			continue
		}
		b := &Branch{Name: f[0], Tip: f[1], Upstream: f[2], Base: f[0] == base, Where: where[f[0]]}
		if b.Upstream != "" {
			if sha, err := git(ctx, root, "rev-parse", "--short=7", b.Upstream); err == nil {
				b.Remote = sha
			}
		}
		m.Branches = append(m.Branches, b)
	}

	// stashes
	st, _ := git(ctx, root, "stash", "list", "--format=%gd"+sep+"%ct"+sep+"%s")
	for _, l := range nonEmpty(st) {
		f := strings.SplitN(l, sep, 3)
		if len(f) == 3 {
			at, _ := strconv.ParseInt(f[1], 10, 64)
			m.Stashes = append(m.Stashes, Stash{ID: f[0], At: at * 1000, Msg: f[2]})
		}
	}

	// uncommitted changes in the main checkout
	if s, err := git(ctx, root, "status", "--porcelain"); err == nil {
		for _, l := range nonEmpty(s) {
			if len(l) > 3 {
				m.Dirty = append(m.Dirty, l[3:])
			}
		}
	}
	if prs := openPRs(ctx, root, m.Remote); prs != nil {
		for _, b := range m.Branches {
			b.PR = prs[b.Name]
		}
	}
	m.InProgress = inProgress(ctx, root, where)
	m.Undo = loadUndo(p)
	return m, nil
}

func nonEmpty(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimRight(l, "\r"); strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// worktreeDirs lists every checkout of the repository, detached ones included
// (a rebase in progress detaches its worktree).
func worktreeDirs(ctx context.Context, root string) []string {
	out, _ := git(ctx, root, "worktree", "list", "--porcelain")
	var dirs []string
	for _, l := range nonEmpty(out) {
		if strings.HasPrefix(l, "worktree ") {
			dirs = append(dirs, filepath.FromSlash(strings.TrimPrefix(l, "worktree ")))
		}
	}
	return dirs
}

// worktrees maps branch name → folder where it is checked out.
func worktrees(ctx context.Context, root string) map[string]string {
	out, _ := git(ctx, root, "worktree", "list", "--porcelain")
	m := map[string]string{}
	dir := ""
	for _, l := range nonEmpty(out) {
		switch {
		case strings.HasPrefix(l, "worktree "):
			dir = filepath.FromSlash(strings.TrimPrefix(l, "worktree "))
		case strings.HasPrefix(l, "branch refs/heads/"):
			m[strings.TrimPrefix(l, "branch refs/heads/")] = dir
		}
	}
	return m
}

// baseBranch: project.yaml git.base, else origin's default, else main or master.
func baseBranch(ctx context.Context, root string, p *project.Project) string {
	var b string
	_ = project.Field(p, []string{"git", "base"}, &b)
	if b != "" {
		return b
	}
	if s, err := git(ctx, root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(s, "origin/")
	}
	for _, c := range []string{"main", "master", "trunk"} {
		if _, err := git(ctx, root, "rev-parse", "--verify", "--quiet", "refs/heads/"+c); err == nil {
			return c
		}
	}
	return ""
}

// inProgress finds an operation that stopped on a conflict in any checkout.
func inProgress(ctx context.Context, root string, where map[string]string) *InProgress {
	dirs := []string{root}
	for _, d := range worktreeDirs(ctx, root) {
		if !sameDir(d, root) {
			dirs = append(dirs, d)
		}
	}
	_ = where
	for _, d := range dirs {
		gd, err := git(ctx, d, "rev-parse", "--git-dir")
		if err != nil {
			continue
		}
		if !filepath.IsAbs(gd) {
			gd = filepath.Join(d, gd)
		}
		kind := ""
		switch {
		case exists(filepath.Join(gd, "rebase-merge")), exists(filepath.Join(gd, "rebase-apply")):
			kind = "rebase"
		case exists(filepath.Join(gd, "MERGE_HEAD")):
			kind = "merge"
		case exists(filepath.Join(gd, "CHERRY_PICK_HEAD")):
			kind = "cherry-pick"
		case exists(filepath.Join(gd, "REVERT_HEAD")):
			kind = "revert"
		}
		if kind == "" {
			continue
		}
		ip := &InProgress{Kind: kind, Dir: d}
		names, _ := git(ctx, d, "diff", "--name-only", "--diff-filter=U")
		for _, f := range nonEmpty(names) {
			ours, _ := git(ctx, d, "show", ":2:"+f)
			theirs, _ := git(ctx, d, "show", ":3:"+f)
			ip.Files = append(ip.Files, Conflict{Path: f, Ours: clip(ours), Theirs: clip(theirs)})
		}
		return ip
	}
	return nil
}

func clip(s string) string {
	if len(s) > 20000 {
		return s[:20000] + "\n…"
	}
	return s
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

var errDirty = errors.New("this checkout has uncommitted changes: commit or stash them first")

func sameDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
