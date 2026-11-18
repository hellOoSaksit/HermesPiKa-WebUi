// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/places"
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

// One copy of the location rules, shared with the folder and tidy code (places).
var (
	samePath          = places.SamePath
	within            = places.Within
	forbiddenLocation = places.Forbidden
)

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

// forbiddenRecordings refuses a folder for the browser's recordings. They are
// private to the person: hpk's own folders are off limits as everywhere, and
// when the AI is isolated the folder may not be one the AI can read (the
// Hermes home, a project's folder or wiki, or anything holding them).
func forbiddenRecordings(home, dir string, isolated bool) error {
	if err := forbiddenLocation(home, nil, dir, false); err != nil {
		return err
	}
	if isolated && stillNeeded(home, dir, "") {
		return errors.New("the AI can read that folder; recordings are private, choose another")
	}
	return nil
}
