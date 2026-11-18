// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hpk.local/hpk/internal/browser"
	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/relocate"
	"hpk.local/hpk/internal/team"
	"hpk.local/hpk/internal/vault"
)

// The built-in browser and the personal vault live on a person's own machine
// only: on a team server both are switched off (the server runs no programs and
// keeps no one's passwords).
type personal struct {
	once     sync.Once
	vault    *vault.Vault
	vaultErr error
	browser  *browser.Manager
	agentKey string
	unlockMu sync.Mutex
	unlocks  map[string]time.Time
}

func (s *Server) personalOn() bool { return s.teamConfig() == nil }

func (s *Server) openPersonal() (*vault.Vault, *browser.Manager, error) {
	s.pers.once.Do(func() {
		s.pers.vault, s.pers.vaultErr = vault.Open(s.home)
		s.pers.browser = browser.New(s.home, s.pers.vault)
	})
	return s.pers.vault, s.pers.browser, s.pers.vaultErr
}

// AgentFile is where `hpk mcp-browser` finds this hpk: its address and a key
// that only processes of this OS account can read.
func AgentFile(home string) string { return filepath.Join(home, "hpk", "browser", "agent.json") }

func (s *Server) writeAgentFile(addr string) {
	b := make([]byte, 24)
	rand.Read(b)
	s.pers.agentKey = hex.EncodeToString(b)
	_ = os.MkdirAll(filepath.Dir(AgentFile(s.home)), 0o700)
	raw, _ := json.Marshal(map[string]string{"url": "http://" + addr, "key": s.pers.agentKey})
	_ = os.WriteFile(AgentFile(s.home), raw, 0o600)
}

func (s *Server) browserAPI(mux *http.ServeMux) {
	off := func(w http.ResponseWriter) bool {
		if !s.personalOn() {
			writeErr(w, http.StatusForbidden, errors.New("the built-in browser and the vault run in hpk on a person's own machine, not on a team server"))
			return true
		}
		return false
	}
	mgr := func(w http.ResponseWriter) *browser.Manager {
		if off(w) {
			return nil
		}
		_, m, _ := s.openPersonal()
		return m
	}

	// ---- for the person (Console) ----
	mux.HandleFunc("GET /api/browser", func(w http.ResponseWriter, r *http.Request) {
		if !s.personalOn() {
			writeJSON(w, map[string]any{"enabled": false})
			return
		}
		v, m, err := s.openPersonal()
		exe, exeErr := browser.FindExe()
		self, _ := os.Executable()
		out := map[string]any{"enabled": true, "state": m.State(), "exe": filepath.Base(exe), "runs": len(browser.Runs(s.home)), "recordingsDir": browser.RunsDir(s.home), "hpkPath": self}
		if exeErr != nil {
			out["exeError"] = exeErr.Error()
		}
		if err != nil {
			out["vaultError"] = err.Error()
		} else {
			out["secrets"] = len(v.List())
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("GET /api/browser/frame", func(w http.ResponseWriter, r *http.Request) {
		m := mgr(w)
		if m == nil {
			return
		}
		f := m.Frame()
		if len(f) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(f)
	})
	// Live view: frames (base64 JPEG, at most ~8 a second), steps, clicks, state.
	mux.HandleFunc("GET /api/browser/stream", func(w http.ResponseWriter, r *http.Request) {
		m := mgr(w)
		if m == nil {
			return
		}
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		ch, cancel := m.Subscribe()
		defer cancel()
		send := func(kind string, v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b)
			fl.Flush()
		}
		send("state", m.State())
		if f := m.Frame(); len(f) > 0 {
			fmt.Fprintf(w, "event: frame\ndata: %s\n\n", base64.StdEncoding.EncodeToString(f))
			fl.Flush()
		}
		var lastFrame time.Time
		var pending []byte
		tick := time.NewTicker(125 * time.Millisecond)
		defer tick.Stop()
		keep := time.NewTicker(20 * time.Second)
		defer keep.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case e := <-ch:
				if e.Kind == "frame" {
					pending = e.Frame
					if time.Since(lastFrame) < 125*time.Millisecond {
						continue
					}
					fmt.Fprintf(w, "event: frame\ndata: %s\n\n", base64.StdEncoding.EncodeToString(pending))
					fl.Flush()
					pending, lastFrame = nil, time.Now()
					continue
				}
				send(e.Kind, e.Data)
			case <-tick.C:
				if pending != nil {
					fmt.Fprintf(w, "event: frame\ndata: %s\n\n", base64.StdEncoding.EncodeToString(pending))
					fl.Flush()
					pending, lastFrame = nil, time.Now()
				}
			case <-keep.C:
				fmt.Fprint(w, ": keep-alive\n\n")
				fl.Flush()
			}
		}
	})
	mux.HandleFunc("POST /api/browser/control", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Op     string `json:"op"` // start | stop | driver | act
			Driver string `json:"driver"`
			browser.Action
		}
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		m := mgr(w)
		if m == nil || !s.can(w, r, team.Admin) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		var err error
		var res *browser.Result
		switch in.Op {
		case "start":
			err = m.Start(ctx)
		case "stop":
			m.Stop()
		case "driver":
			m.SetDriver(in.Driver)
		case "act":
			if in.Tool == "fill_secret" {
				err = errors.New("fill secrets from the vault page, or let the AI ask for them by name")
				break
			}
			res, err = m.Do(ctx, "human", in.Action)
		default:
			err = fmt.Errorf("unknown op %q", in.Op)
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "state": m.State(), "result": res})
	})

	// ---- recordings ----
	mux.HandleFunc("GET /api/browser/runs", func(w http.ResponseWriter, r *http.Request) {
		if off(w) {
			return
		}
		writeJSON(w, map[string]any{"runs": browser.Runs(s.home)})
	})
	mux.HandleFunc("GET /api/browser/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if off(w) {
			return
		}
		steps, err := browser.RunSteps(s.home, r.PathValue("id"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, map[string]any{"steps": steps})
	})
	mux.HandleFunc("GET /api/browser/runs/{id}/{shot}", func(w http.ResponseWriter, r *http.Request) {
		if off(w) {
			return
		}
		img, err := browser.Shot(s.home, r.PathValue("id"), r.PathValue("shot"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.Write(img)
	})
	mux.HandleFunc("POST /api/browser/runs", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Op string `json:"op"` // delete | keep | unkeep
			ID string `json:"id"`
		}
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		if off(w) || !s.can(w, r, team.Admin) {
			return
		}
		var err error
		switch in.Op {
		case "delete":
			err = browser.DeleteRun(s.home, in.ID)
		case "keep", "unkeep":
			err = browser.KeepRun(s.home, in.ID, in.Op == "keep")
		default:
			err = fmt.Errorf("unknown op %q", in.Op)
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	// POST /api/browser/recordings-dir {dir}: move the recordings; "" = the default place.
	mux.HandleFunc("POST /api/browser/recordings-dir", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Dir string `json:"dir"`
		}
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		m := mgr(w)
		if m == nil || !s.can(w, r, team.Admin) {
			return
		}
		if in.Dir != "" && !filepath.IsAbs(in.Dir) {
			writeErr(w, http.StatusBadRequest, errors.New("give the folder's full path"))
			return
		}
		if m.State().Running {
			writeErr(w, http.StatusConflict, errors.New("the browser is running; stop it first"))
			return
		}
		src := browser.RunsDir(s.home)
		dst, custom := filepath.Join(browser.Dir(s.home), "runs"), ""
		if in.Dir != "" {
			dst = filepath.Clean(in.Dir)
			if !samePath(dst, filepath.Join(browser.Dir(s.home), "runs")) {
				custom = dst
			}
		}
		if samePath(src, dst) {
			writeJSON(w, map[string]any{"ok": true, "dir": src})
			return
		}
		_, isolated := isolate.Load()
		if custom != "" {
			if err := forbiddenRecordings(s.home, dst, isolated); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
		}
		made := false
		if _, err := os.Stat(src); err != nil {
			// nothing recorded yet: only the setting changes
			if err := browser.SetRunsDir(custom); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			if isolated {
				_ = isolate.MakePrivate(dst)
			}
			writeJSON(w, map[string]any{"ok": true, "dir": dst})
			return
		}
		if _, err := os.Stat(dst); errors.Is(err, os.ErrNotExist) {
			made = true
		}
		if isolated {
			// private before the first file lands in it
			if err := isolate.MakePrivate(dst); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		warning := ""
		err := relocate.MoveWith(src, dst, func() error { return browser.SetRunsDir(custom) })
		var ce *relocate.CleanupError
		if errors.As(err, &ce) {
			warning, err = ce.Error(), nil
		}
		if err != nil {
			if made {
				_ = os.Remove(dst)
			}
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		out := map[string]any{"ok": true, "dir": dst}
		if warning != "" {
			out["warning"] = warning
		}
		writeJSON(w, out)
	})

	// ---- the vault ----
	// Values never leave except "reveal", to the person. Everything that could
	// leak or redirect an existing secret (change, delete, write .env, reveal)
	// needs the vault unlocked with the PIN the person types in the Console: the
	// AI can run programs that call this API, but it does not know the PIN.
	mux.HandleFunc("GET /api/vault", func(w http.ResponseWriter, r *http.Request) {
		if off(w) {
			return
		}
		v, _, err := s.openPersonal()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]any{"entries": v.List(), "keeper": v.Keeper, "managedEnv": len(vault.Managed(s.home)),
			"hasPin": v.HasPIN(), "unlocked": s.vaultUnlocked(r)})
	})
	mux.HandleFunc("POST /api/vault", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Op  string `json:"op"` // put | delete | reveal | writeEnv | setPin | unlock | lock
			PIN string `json:"pin"`
			Old string `json:"oldPin"`
			vault.Entry
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 128<<10)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if off(w) || !s.can(w, r, team.Admin) {
			return
		}
		v, _, err := s.openPersonal()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		needUnlock := in.Op == "delete" || in.Op == "reveal" || in.Op == "writeEnv" || (in.Op == "put" && v.Has(strings.ToLower(strings.TrimSpace(in.Name))))
		if needUnlock && !s.vaultUnlocked(r) {
			writeJSON2(w, http.StatusForbidden, map[string]any{"error": "unlock the vault with your PIN first", "locked": true, "hasPin": v.HasPIN()})
			return
		}
		out := map[string]any{"ok": true}
		switch in.Op {
		case "setPin":
			if err = v.SetPIN(in.Old, in.PIN); err == nil {
				out["unlock"] = s.unlockVault()
			}
		case "unlock":
			if err = v.CheckPIN(in.PIN); err == nil {
				out["unlock"] = s.unlockVault()
			}
		case "lock":
			s.lockVault(r)
		case "put":
			err = v.Put(in.Entry)
		case "delete":
			err = v.Delete(in.Name)
		case "reveal":
			if !isLoopback(r.RemoteAddr) {
				err = errors.New("secrets are shown only on this machine")
				break
			}
			var user, val string
			user, val, err = v.Reveal(in.Name)
			out["user"], out["value"] = user, val
		case "writeEnv":
			var p *project.Project
			p, err = s.loadFor(r, in.Project)
			if err == nil {
				var files []string
				files, err = v.WriteEnv(s.home, p.Name, p.Root)
				out["files"] = files
			}
		default:
			err = fmt.Errorf("unknown op %q", in.Op)
		}
		in.Value, in.User, in.PIN, in.Old = "", "", "", ""
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if in.Op != "reveal" && in.Op != "unlock" {
			log.Printf("vault: %s %s by %s", in.Op, in.Name, s.who(r).ID)
		}
		writeJSON(w, out)
	})

	// ---- for the agent (hpk mcp-browser), with the key from agent.json ----
	mux.HandleFunc("POST /api/browser/agent", func(w http.ResponseWriter, r *http.Request) {
		if off(w) {
			return
		}
		k := r.Header.Get("X-HPK-Agent-Key")
		if s.pers.agentKey == "" || subtle.ConstantTimeCompare([]byte(k), []byte(s.pers.agentKey)) != 1 || !isLoopback(r.RemoteAddr) {
			writeErr(w, http.StatusUnauthorized, errors.New("hpk mcp-browser: wrong or missing key (restart hpk serve, then the chat)"))
			return
		}
		var a browser.Action
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&a); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		_, m, _ := s.openPersonal()
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		res, err := m.Do(ctx, "agent", a)
		if errors.Is(err, browser.ErrHuman) {
			s.hub.send("browser-needs", "driver") // the agent waits for the person to hand the browser back
		}
		if err != nil {
			writeJSON2(w, http.StatusOK, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, res)
	})
}

// ---- vault unlock: a random id in an HttpOnly cookie, valid 10 minutes ----

// The unlock is a random token the Console keeps in memory and sends in the
// X-HPK-Vault header. Not a cookie: a browser sends cookies to every port of
// localhost, so a page the AI serves on another port could ride along.
const vaultHeader = "X-HPK-Vault"

func (s *Server) unlockVault() string {
	b := make([]byte, 24)
	rand.Read(b)
	id := hex.EncodeToString(b)
	s.pers.unlockMu.Lock()
	if s.pers.unlocks == nil {
		s.pers.unlocks = map[string]time.Time{}
	}
	for k, t := range s.pers.unlocks {
		if time.Now().After(t) {
			delete(s.pers.unlocks, k)
		}
	}
	s.pers.unlocks[id] = time.Now().Add(10 * time.Minute)
	s.pers.unlockMu.Unlock()
	return id
}

func (s *Server) vaultUnlocked(r *http.Request) bool {
	tok := r.Header.Get(vaultHeader)
	if tok == "" {
		return false
	}
	s.pers.unlockMu.Lock()
	defer s.pers.unlockMu.Unlock()
	for k, t := range s.pers.unlocks {
		if subtle.ConstantTimeCompare([]byte(k), []byte(tok)) == 1 {
			return time.Now().Before(t)
		}
	}
	return false
}

func (s *Server) lockVault(r *http.Request) {
	tok := r.Header.Get(vaultHeader)
	s.pers.unlockMu.Lock()
	delete(s.pers.unlocks, tok)
	s.pers.unlockMu.Unlock()
}
