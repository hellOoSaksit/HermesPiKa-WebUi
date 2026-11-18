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
		moved := project.WikiLocations(s.home)[p.Name] != ""
		cfg, isolated := isolate.Load()
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
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		back := ""
		if !samePath(dst, filepath.Join(p.Dir, "wiki")) {
			back = dst
		}
		err := relocate.MoveWith(src, dst, func() error { return project.SetWikiLocation(s.home, p.Name, back) })
		if err != nil {
			if isolated {
				_ = isolate.Ungrant(cfg, dst)
			}
			undo()
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if isolated && moved {
			_ = isolate.Ungrant(cfg, src)
		}
		s.hub.send("sessions", p.Name)
		writeJSON(w, map[string]any{"ok": true, "dir": dst})
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
		cfg, isolated := isolate.Load()
		if isolated {
			if err := isolate.Grant(cfg, dst); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		if err := project.SetField(s.home, p.Name, []string{"root"}, dst, "root: "+dst); err != nil {
			if isolated && !samePath(p.Root, dst) {
				_ = isolate.Ungrant(cfg, dst)
			}
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if isolated && p.Root != "" && !samePath(p.Root, dst) {
			_ = isolate.Ungrant(cfg, p.Root)
		}
		s.hub.send("sessions", p.Name)
		writeJSON(w, map[string]any{"ok": true})
	})
}
