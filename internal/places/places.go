// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package places decides which folders hpk, running as the person, may act on
// for a project. project.yaml lives in the Hermes home, which the AI can write,
// so its root is checked again at every use, not only when the person set it.
package places

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/launch"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/safepath"
)

// SamePath compares two paths, ignoring case on Windows.
func SamePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Within reports whether p is dir or below it.
func Within(dir, p string) bool {
	if SamePath(dir, p) {
		return true
	}
	d := filepath.Clean(dir) + string(filepath.Separator)
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(p), strings.ToLower(d))
	}
	return strings.HasPrefix(p, d)
}

// Forbidden refuses a folder hpk must not hand to the AI or build on: the
// Hermes home's own files, the trusted folder, the person's private folder and
// hpk's data folder; a folder that holds any of them is refused too. A wiki
// (wiki=true) may not be anywhere in the Hermes home either, except the default
// place (moving back); it may sit inside its own project's folder, not inside
// another project's.
func Forbidden(home string, self *project.Project, dir string, wiki bool) error {
	if wiki && self != nil && SamePath(dir, filepath.Join(self.Dir, "wiki")) {
		return nil // moving back to the default place
	}
	guarded := []string{home, filepath.Join(home, "hpk", "trusted"), isolate.PrivateDir(home), launch.DataDir()}
	for i, g := range guarded {
		if g == "" {
			continue
		}
		if Within(dir, g) {
			return errors.New("that folder is, or holds, hpk's own files; choose another")
		}
		// a project folder may live inside the Hermes home; its own files may not
		// inside the project's own folder is fine even when that folder is in the home
		inOwnRoot := wiki && i == 0 && self != nil && self.Root != "" && Within(self.Root, dir)
		if Within(g, dir) && (wiki || i > 0) && !inOwnRoot {
			return errors.New("that folder is inside hpk's own files; choose another")
		}
	}
	if !wiki {
		return nil
	}
	all, _ := project.List(home)
	for _, o := range all {
		if o.Root == "" {
			continue
		}
		if self != nil && o.Name == self.Name {
			if Within(dir, o.Root) {
				return errors.New("a wiki cannot be the project folder itself or hold it")
			}
			continue
		}
		if Within(o.Root, dir) || Within(dir, o.Root) {
			return errors.New("that folder belongs to another project")
		}
	}
	return nil
}

// Root returns the project's folder, resolved, when hpk may act on it as the
// person: a full path, not one of hpk's own folders (Forbidden), and, while the
// AI is isolated, a folder the AI was given anyway (the Hermes home or a folder
// shared with it, not a read-only one) with no link or junction from that
// folder down to the root. Both checks run on the path as written and again on
// the path links resolve to. So a root the
// AI wrote into project.yaml can never make hpk write, move or list a place the
// AI could not reach by itself.
func Root(home string, p *project.Project) (string, error) {
	if p == nil || p.Root == "" {
		return "", errors.New("this project has no folder")
	}
	root := filepath.Clean(p.Root)
	if !filepath.IsAbs(root) {
		return "", errors.New("the project folder in project.yaml is not a full path; set it again in the Console")
	}
	c, isolated := isolate.For(home)
	// check the path as written and, after resolving links, where it really is
	check := func(dir string) error {
		if err := Forbidden(home, p, dir, false); err != nil {
			return fmt.Errorf("the project folder in project.yaml: %w", err)
		}
		if !isolated {
			return nil
		}
		base := sharedBase(c, dir)
		if base == "" {
			return errors.New("the project folder in project.yaml is not one you shared with the AI; set it again in the Console (Project, Folder)")
		}
		// no link or junction from the shared folder down to the root: the AI
		// could make one lead anywhere (EvalSymlinks leaves junctions alone)
		rel, err := filepath.Rel(base, dir)
		if err != nil {
			return err
		}
		cur := base
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if part == "." || part == "" {
				continue
			}
			cur = filepath.Join(cur, part)
			if fi, err := os.Lstat(cur); err == nil && safepath.IsLink(cur, fi) {
				return errors.New("the project folder is, or is inside, a link or junction; point the project at the real folder")
			}
		}
		return nil
	}
	if err := check(root); err != nil {
		return "", err
	}
	real, err := safepath.Root(root)
	if err != nil {
		return "", err
	}
	if err := check(real); err != nil {
		return "", err
	}
	return real, nil
}

// sharedBase returns the folder through which the isolated AI can already
// write dir (the Hermes home, a granted folder, a read-write mount), or "".
// Each is tried as written and resolved (macOS /var is /private/var).
func sharedBase(c *isolate.Config, dir string) string {
	for _, ro := range c.ReadOnly {
		if Within(ro, dir) || Within(resolved(ro), dir) {
			return ""
		}
	}
	bases := []string{c.Home}
	bases = append(bases, c.Granted...)
	for _, m := range c.Mounts {
		if !m.ReadOnly {
			bases = append(bases, m.Host)
		}
	}
	for _, b := range bases {
		for _, cand := range []string{b, resolved(b)} {
			if cand != "" && Within(cand, dir) {
				return cand
			}
		}
	}
	return ""
}

func resolved(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
