// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/hermes"
	"hpk.local/hpk/internal/install"
	"hpk.local/hpk/internal/launch"
	hsetup "hpk.local/hpk/internal/setup"
	"hpk.local/hpk/internal/version"
)

// The Console is an installable web app; some systems have no MIME entry for its manifest.
func init() { _ = mime.AddExtensionType(".webmanifest", "application/manifest+json") }

// life is the daemon's own lifecycle: the stop key from the run file, the
// cancel that ends Run, and the one Hermes install job.
type life struct {
	stopKey string
	stop    context.CancelFunc
	job     hsetup.Job
}

// lifecycleAPI: first-run setup (Hermes, hpk into Hermes), start at login and
// `hpk stop`. All of it is for a person's own machine, never a team server.
func (s *Server) lifecycleAPI(mux *http.ServeMux) {
	own := func(w http.ResponseWriter, r *http.Request) bool {
		if !s.personalOn() || !isLoopback(r.RemoteAddr) {
			writeErr(w, http.StatusForbidden, errors.New("setup runs in hpk on a person's own machine (this is a team server or another machine)"))
			return false
		}
		return true
	}

	// `hpk stop`: only with the key from the run file, which only this person can read.
	mux.HandleFunc("POST /api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		k := r.Header.Get("X-HPK-Stop-Key")
		if s.life.stop == nil || s.life.stopKey == "" || !isLoopback(r.RemoteAddr) || subtle.ConstantTimeCompare([]byte(k), []byte(s.life.stopKey)) != 1 {
			writeErr(w, http.StatusForbidden, errors.New("hpk stop: wrong or missing stop key"))
			return
		}
		log.Printf("hpk stop: shutting down")
		writeJSON(w, map[string]bool{"ok": true})
		go func() { time.Sleep(100 * time.Millisecond); s.life.stop() }()
	})

	mux.HandleFunc("GET /api/setup", func(w http.ResponseWriter, r *http.Request) {
		if !own(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		info := hermes.Detect(ctx)
		pl, err := install.PlanInstall(install.Resolve(s.home, ""))
		in := map[string]any{"changes": pl.Changes, "diff": pl.Diff, "config": pl.Paths.Config, "bin": pl.Paths.Bin, "skills": pl.Paths.SkillsDir}
		if err != nil {
			in["error"] = err.Error()
		}
		data := launch.DataDir()
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]any{
			"hermes": map[string]any{"found": info.CLI != "", "version": info.Version, "cli": info.CLI, "acp": info.ACP, "error": info.Error, "managed": info.CLI != "" && sameDir(info.CLI, hsetup.BinDir(data))},
			"hpk":    map[string]any{"version": version.Version, "installed": err == nil && len(pl.Changes) == 0 && fileExists(pl.Paths.Bin), "plan": in},
			"offer":  hsetup.MakeOffer(data),
			"job":    s.life.job.Snapshot(),
			"home":   s.home,
		})
	})
	mux.HandleFunc("POST /api/setup/hermes", func(w http.ResponseWriter, r *http.Request) {
		if !own(w, r) || !consented(w, r) {
			return
		}
		if err := s.life.job.Start(launch.DataDir(), func() { s.hub.send("changed", "setup") }); err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	// Same steps as `hpk install --yes`, after the person saw the change in the Console.
	mux.HandleFunc("POST /api/setup/install", func(w http.ResponseWriter, r *http.Request) {
		if !own(w, r) || !consented(w, r) {
			return
		}
		pl, err := install.PlanInstall(install.Resolve(s.home, ""))
		if err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		self, _ := os.Executable()
		m, err := install.Apply(pl, self, assets.Skills(), version.Version, time.Now())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		s.hub.send("changed", "setup")
		writeJSON(w, m)
	})

	mux.HandleFunc("GET /api/autostart", func(w http.ResponseWriter, r *http.Request) {
		if !own(w, r) {
			return
		}
		a, err := launch.ThisAutostart()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		file, _ := a.File()
		writeJSON(w, map[string]any{"supported": a.Supported(), "enabled": a.Supported() && a.Enabled(), "os": a.GOOS, "file": file, "exe": a.Exe})
	})
	mux.HandleFunc("POST /api/autostart", func(w http.ResponseWriter, r *http.Request) {
		if !own(w, r) {
			return
		}
		var in struct {
			On bool `json:"on"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		a, err := launch.ThisAutostart()
		if err == nil {
			if in.On {
				err = a.Enable()
			} else {
				err = a.Disable()
			}
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		log.Printf("start at login: %v", in.On)
		writeJSON(w, map[string]any{"enabled": a.Enabled()})
	})
}

// consented reads {"consent": true}: a person agreed to what the Console showed.
func consented(w http.ResponseWriter, r *http.Request) bool {
	var in struct {
		Consent bool `json:"consent"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in)
	if !in.Consent {
		writeErr(w, http.StatusBadRequest, errors.New("a person must agree first (consent: true)"))
		return false
	}
	return true
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func sameDir(file, dir string) bool {
	a, err1 := os.Stat(dir)
	b, err2 := os.Stat(filepath.Dir(file))
	return err1 == nil && err2 == nil && os.SameFile(a, b)
}
