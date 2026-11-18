// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/launch"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/relocate"
)

// busy reports whether a chat of this project is streaming an answer now.
func (m *ACPManager) busy(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ls := range m.sessions {
		if ls.project == name && ls.streaming {
			return true
		}
	}
	return false
}

// projectBusy: a session linked to the project is working, so its files must stay put.
func (s *Server) projectBusy(name string) bool { return s.acp.busy(name) }

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func within(dir, p string) bool {
	if samePath(dir, p) {
		return true
	}
	d := filepath.Clean(dir) + string(filepath.Separator)
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(p), strings.ToLower(d))
	}
	return strings.HasPrefix(p, d)
}

// forbiddenLocation refuses a folder hpk must not hand to the AI or build on:
// the Hermes home's own files, the trusted folder, the person's private
// folder and hpk's data folder; a folder that holds any of them is refused too.
// A wiki (wiki=true) may not be anywhere in the Hermes home either, except the
// default place (moving back); it may sit inside its own project's folder, not
// inside another project's.
func forbiddenLocation(home string, self *project.Project, dir string, wiki bool) error {
	if wiki && self != nil && samePath(dir, filepath.Join(self.Dir, "wiki")) {
		return nil // moving back to the default place
	}
	guarded := []string{home, filepath.Join(home, "hpk", "trusted"), isolate.PrivateDir(home), launch.DataDir()}
	for i, g := range guarded {
		if g == "" {
			continue
		}
		if within(dir, g) {
			return errors.New("that folder is, or holds, hpk's own files; choose another")
		}
		// a project folder may live inside the Hermes home; its own files may not
		// inside the project's own folder is fine even when that folder is in the home
		inOwnRoot := wiki && i == 0 && self != nil && self.Root != "" && within(self.Root, dir)
		if within(g, dir) && (wiki || i > 0) && !inOwnRoot {
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
			if within(dir, o.Root) {
				return errors.New("a wiki cannot be the project folder itself or hold it")
			}
			continue
		}
		if within(o.Root, dir) || within(dir, o.Root) {
			return errors.New("that folder belongs to another project")
		}
	}
	return nil
}

// stillNeeded reports whether dir must keep the AI's access: it is the Hermes
// home or inside it, or another project's root or wiki is equal to, inside or
// around it. except ("<project>/root" or "<project>/wiki") names a location to
// ignore, one that is being replaced.
func stillNeeded(home, dir, except string) bool {
	if within(home, dir) {
		return true
	}
	all, _ := project.List(home)
	for _, o := range all {
		for kind, loc := range map[string]string{"root": o.Root, "wiki": o.WikiDir()} {
			if loc == "" || except == o.Name+"/"+kind {
				continue
			}
			if within(loc, dir) || within(dir, loc) {
				return true
			}
		}
	}
	return false
}

func isGranted(c *isolate.Config, dir string) bool {
	for _, g := range c.Granted {
		if samePath(g, dir) {
			return true
		}
	}
	return false
}

func grantStatus(err error) int {
	if isolate.IsUnsupported(err) {
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

// POST /api/project/wiki-dir {project, dir}: move the project's wiki to dir.
// POST /api/project/root {project, dir}: point the project at another folder.
// Solo mode only (a team server's folders are not for a browser to choose).
func (s *Server) locationsAPI(mux *http.ServeMux) {
	type req struct {
		Project string `json:"project"`
		Dir     string `json:"dir"`
	}
	prepare := func(w http.ResponseWriter, r *http.Request) (*project.Project, string, bool) {
		if !s.personalOn() {
			writeErr(w, http.StatusForbidden, errors.New("folders are chosen on a person's own machine, not on a team server"))
			return nil, "", false
		}
		var in req
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return nil, "", false
		}
		if !filepath.IsAbs(in.Dir) {
			writeErr(w, http.StatusBadRequest, errors.New("give the folder's full path"))
			return nil, "", false
		}
		if s.projectBusy(p.Name) {
			writeErr(w, http.StatusConflict, errors.New("a chat of this project is working; wait until it is done"))
			return nil, "", false
		}
		return p, filepath.Clean(in.Dir), true
	}

	mux.HandleFunc("POST /api/project/wiki-dir", func(w http.ResponseWriter, r *http.Request) {
		p, dst, ok := prepare(w, r)
		if !ok {
			return
		}
		src := p.WikiDir()
		if samePath(src, dst) {
			writeJSON(w, map[string]any{"ok": true, "dir": src})
			return
		}
		if err := forbiddenLocation(s.home, p, dst, true); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		all, _ := project.List(s.home)
		for _, o := range all {
			if o.Name == p.Name {
				continue
			}
			if within(o.WikiDir(), dst) || within(dst, o.WikiDir()) {
				writeErr(w, http.StatusBadRequest, errors.New("that folder is inside (or holds) another project's wiki"))
				return
			}
		}
		cfg, isolated := isolate.Load()
		grantedBefore := isolated && isGranted(cfg, dst)
		madeDst := false
		if _, err := os.Stat(dst); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			madeDst = true
		}
		undo := func() {
			if madeDst {
				_ = os.Remove(dst)
			}
		}
		if isolated {
			if err := isolate.Grant(cfg, dst); err != nil {
				undo()
				writeErr(w, grantStatus(err), err)
				return
			}
		}
		back := ""
		if !samePath(dst, filepath.Join(p.Dir, "wiki")) {
			back = dst
		}
		warning := ""
		err := relocate.MoveWith(src, dst, func() error { return project.SetWikiLocation(s.home, p.Name, back) })
		var ce *relocate.CleanupError
		if errors.As(err, &ce) {
			warning, err = ce.Error(), nil // the move stands
		}
		if err != nil {
			if isolated && !grantedBefore && !stillNeeded(s.home, dst, "") {
				_ = isolate.Ungrant(cfg, dst)
			}
			undo()
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if isolated && !stillNeeded(s.home, src, "") {
			_ = isolate.Ungrant(cfg, src)
		}
		s.hub.send("sessions", p.Name)
		out := map[string]any{"ok": true, "dir": dst}
		if warning != "" {
			out["warning"] = warning
		}
		writeJSON(w, out)
	})

	mux.HandleFunc("POST /api/project/root", func(w http.ResponseWriter, r *http.Request) {
		p, dst, ok := prepare(w, r)
		if !ok {
			return
		}
		if fi, err := os.Stat(dst); err != nil || !fi.IsDir() {
			writeErr(w, http.StatusBadRequest, errors.New("that folder does not exist"))
			return
		}
		if err := forbiddenLocation(s.home, p, dst, false); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		cfg, isolated := isolate.Load()
		grantedBefore := isolated && isGranted(cfg, dst)
		if isolated {
			if err := isolate.Grant(cfg, dst); err != nil {
				writeErr(w, grantStatus(err), err)
				return
			}
		}
		if err := project.SetField(s.home, p.Name, []string{"root"}, dst, "root: "+dst); err != nil {
			if isolated && !grantedBefore && !stillNeeded(s.home, dst, "") {
				_ = isolate.Ungrant(cfg, dst)
			}
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if isolated && p.Root != "" && !samePath(p.Root, dst) && !stillNeeded(s.home, p.Root, "") {
			_ = isolate.Ungrant(cfg, p.Root)
		}
		s.hub.send("sessions", p.Name)
		writeJSON(w, map[string]any{"ok": true})
	})
}
