// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"hpk.local/hpk/internal/folders"
	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/launch"
)

type fsEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type fsPlace struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Key   string `json:"key,omitempty"` // projects | home | documents | desktop | root: the Console words it
}

// hiddenDir: places the folder picker never shows or opens: system folders
// and hpk's own private ones.
func (s *Server) hiddenDir(p string) bool {
	for _, g := range []string{folders.TrustedDir(s.home), isolate.PrivateDir(s.home), launch.DataDir()} {
		if g != "" && samePath(p, g) {
			return true
		}
	}
	base := filepath.Base(p)
	if strings.HasPrefix(base, ".") {
		return true
	}
	lower := strings.ToLower(filepath.ToSlash(p))
	if runtime.GOOS == "windows" {
		if vol := strings.ToLower(filepath.VolumeName(p)); vol != "" {
			rest := strings.TrimPrefix(lower, vol)
			if strings.Count(strings.Trim(rest, "/"), "/") == 0 {
				r := strings.Trim(rest, "/")
				if r == "windows" || r == "programdata" || strings.HasPrefix(r, "program files") {
					return true
				}
			}
		}
		return false
	}
	for _, x := range []string{"/proc", "/sys", "/dev"} {
		if lower == x {
			return true
		}
	}
	return false
}

func (s *Server) fsPlaces() []fsPlace {
	var out []fsPlace
	add := func(label, p, key string) {
		if st, err := os.Stat(p); err == nil && st.IsDir() && !s.hiddenDir(p) {
			out = append(out, fsPlace{label, p, key})
		}
	}
	// In Docker the person's projects folder is mounted here (HPK_PROJECTS_DIR,
	// /projects); the container's own home is the Hermes home, not theirs.
	if d := os.Getenv("HPK_PROJECTS_DIR"); d != "" {
		add("Projects", d, "projects")
		add("/", "/", "root")
		return out
	}
	if h, err := os.UserHomeDir(); err == nil {
		add("Home", h, "home")
		add("Documents", filepath.Join(h, "Documents"), "documents")
		add("Desktop", filepath.Join(h, "Desktop"), "desktop")
	}
	if runtime.GOOS == "windows" {
		for c := 'A'; c <= 'Z'; c++ {
			add(string(c)+":", string(c)+":\\", "")
		}
	} else {
		add("/", "/", "root")
	}
	return out
}

// GET /api/fs/dirs?path=: the folders inside path (solo mode only; the Console
// key is needed even for this read, see requireConsoleKey).
func (s *Server) fsAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/fs/dirs", func(w http.ResponseWriter, r *http.Request) {
		if !s.personalOn() {
			http.NotFound(w, r)
			return
		}
		path := r.URL.Query().Get("path")
		out := map[string]any{"path": "", "parent": "", "dirs": []fsEntry{}, "places": s.fsPlaces()}
		if path == "" {
			writeJSON(w, out)
			return
		}
		if !filepath.IsAbs(path) {
			writeErr(w, http.StatusBadRequest, errors.New("give the folder's full path"))
			return
		}
		path = filepath.Clean(path)
		if s.hiddenDir(path) {
			writeErr(w, http.StatusForbidden, errors.New("that folder is not shown"))
			return
		}
		ents, err := os.ReadDir(path)
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("cannot open that folder"))
			return
		}
		dirs := []fsEntry{}
		for _, e := range ents {
			p := filepath.Join(path, e.Name())
			isDir := e.IsDir()
			if !isDir && e.Type()&os.ModeSymlink != 0 {
				if st, err := os.Stat(p); err == nil && st.IsDir() {
					isDir = true
				}
			}
			if isDir && !s.hiddenDir(p) {
				dirs = append(dirs, fsEntry{e.Name(), p})
			}
		}
		sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
		out["path"], out["dirs"] = path, dirs
		if par := filepath.Dir(path); par != path && !s.hiddenDir(par) {
			out["parent"] = par
		}
		writeJSON(w, out)
	})
}
