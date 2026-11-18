// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package serve runs the hpk daemon: the HPK Console, its JSON API, live events
// (SSE) and the hermes-acp processes for sessions started from the Console.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hpk.local/hpk/internal/launch"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/version"
)

// The Console bundles its font; some systems have no MIME entry for it.
// MIME types are registered when the server is built, not in init(): on
// Windows the first mime call reads the registry (tens of ms), and every
// `hpk hook` run would pay for it.
var typesOnce sync.Once

func registerTypes() {
	typesOnce.Do(func() {
		_ = mime.AddExtensionType(".woff2", "font/woff2")
		_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
		registerFileTypes()
	})
}

// Options configure the daemon.
type Options struct {
	Addr     string // listen address; loopback by default
	Console  fs.FS  // the embedded HPK Console
	Home     string // Hermes home; empty = project.Home()
	Launcher Launcher
	// Team mode on a network: TLS, or plain HTTP only when a proxy adds TLS in front.
	TLSCert, TLSKey string
	InsecureHTTP    bool
	// Ready is called once the daemon listens (`hpk` opens the browser then).
	Ready func(base string)
}

// Server holds the daemon's state.
type Server struct {
	home    string
	version string
	acp     *ACPManager
	hub     *hub
	logins  *logins
	pers    personal     // built-in browser + vault (own machine only)
	keys    *consoleKeys // solo mode: changes need a browser hpk opened (consolekey.go)
	life    life         // run file, hpk stop, first-run setup (setupapi.go)
	devs    devStore     // team server: invite codes and joined devices
	link    linkState    // own machine: the team server it joined
}

// NewServer builds a server without listening (tests use it with httptest).
func NewServer(o Options) *Server {
	h := o.Home
	if h == "" {
		h = project.Home()
	}
	s := &Server{home: h, version: version.Version, hub: newHub(), logins: &logins{byID: map[string]login{}}}
	s.acp = NewACPManager(h, o.Launcher, s.hub.send, osUser)
	return s
}

// Handler builds the HTTP routes.
func Handler(o Options) http.Handler { return NewServer(o).Routes(o.Console) }

// Routes wires the Console and the API.
func (s *Server) Routes(console fs.FS) http.Handler {
	registerTypes()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "ok", "version": version.Version})
	})
	s.api(mux)
	s.authAPI(mux)
	s.teamAPI(mux)
	s.gitAPI(mux)
	s.mcpAPI(mux)
	s.bgAPI(mux)
	s.filesAPI(mux)
	s.browserAPI(mux)
	s.consoleKeyRoutes(mux)
	s.lifecycleAPI(mux)
	s.devicesAPI(mux)
	s.teamServerAPI(mux)
	if console != nil {
		mux.Handle("GET /", noCacheHTML(http.FileServerFS(console)))
	}
	return secureHeaders(sameOrigin(s.identify(s.requireConsoleKey(mux))))
}

// Run serves until ctx is cancelled.
func Run(ctx context.Context, o Options) error {
	if o.Addr == "" {
		o.Addr = "127.0.0.1:7878"
	}
	s := NewServer(o)
	tls := o.TLSCert != "" || o.TLSKey != ""
	if tls && (o.TLSCert == "" || o.TLSKey == "") {
		return errors.New("hpk serve: give both --tls-cert and --tls-key")
	}
	if !isLoopback(o.Addr) {
		// Beyond this machine only with sign-in (team mode) and TLS, or behind a
		// proxy that adds TLS (--insecure-http says so out loud).
		if !s.TeamEnabled() {
			return errors.New("hpk serve: listening beyond loopback needs team mode (hpk team init; docs/team.md)")
		}
		if !tls && !o.InsecureHTTP {
			return errors.New("hpk serve: on a network, give --tls-cert and --tls-key (or --insecure-http when a proxy in front adds TLS)")
		}
	}
	ln, err := net.Listen("tcp", o.Addr)
	if err != nil {
		return err
	}
	defer s.acp.Close()
	// The run file lets `hpk` find this daemon and `hpk stop` end it (launch package).
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.life.stop, s.life.stopKey = cancel, launch.NewKey()
	if done, err := launch.WriteRun(launch.Run{PID: os.Getpid(), Addr: ln.Addr().String(), URL: launch.ConsoleURL(ln.Addr().String()), Key: s.life.stopKey, Started: time.Now().Unix(), Version: version.Version}); err == nil {
		defer done()
	}
	if s.personalOn() {
		s.writeAgentFile(ln.Addr().String())
		s.keys = newConsoleKeys(s.home)
		s.keys.addr = ln.Addr().String()
		link := s.keys.fresh()
		go func() { // a new link now and then, so one left on screen goes stale
			t := time.NewTicker(linkTTL)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					s.keys.fresh()
				}
			}
		}()
		defer os.Remove(LinkFile(s.home))
		log.Printf("Open the Console with this one-time link (or run: hpk open):\n  %s", link)
		defer func() {
			if s.pers.browser != nil {
				s.pers.browser.Stop()
			}
		}()
	}
	go s.watch(ctx)
	srv := &http.Server{Handler: s.Routes(o.Console), ReadHeaderTimeout: 5 * time.Second}
	scheme := "http"
	if tls {
		scheme = "https"
	}
	mode := "solo"
	if s.TeamEnabled() {
		mode = "team"
	}
	log.Printf("HPK Console on %s://%s (%s mode)", scheme, ln.Addr(), mode)
	if o.Ready != nil {
		go o.Ready(scheme + "://" + ln.Addr().String())
	}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	serveFn := func() error { return srv.Serve(ln) }
	if tls {
		serveFn = func() error { return srv.ServeTLS(ln, o.TLSCert, o.TLSKey) }
	}
	if err := serveFn(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ---- live events ----

// eventsTick is the keep-alive, and how often an open stream re-checks who it is for.
var eventsTick = 25 * time.Second

type hub struct {
	mu      sync.Mutex
	clients map[chan string]bool
}

func newHub() *hub { return &hub{clients: map[chan string]bool{}} }

func (h *hub) send(event, data string) {
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, strings.ReplaceAll(data, "\n", " "))
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c <- msg:
		default: // a slow client misses an event; it refetches on the next one
		}
	}
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	c := make(chan string, 32)
	s.hub.mu.Lock()
	s.hub.clients[c] = true
	s.hub.mu.Unlock()
	defer func() { s.hub.mu.Lock(); delete(s.hub.clients, c); s.hub.mu.Unlock() }()
	fmt.Fprint(w, "event: hello\ndata: {}\n\n")
	fl.Flush()
	tick := time.NewTicker(eventsTick)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-c:
			fmt.Fprint(w, m)
			fl.Flush()
		case <-tick.C:
			// still allowed? a revoked device, a removed member, a rotated token or
			// an expired sign-in ends the stream (it was checked only when it opened)
			if tc := s.teamConfig(); tc != nil {
				if _, _, err := s.authenticate(tc, r); err != nil {
					return
				}
			}
			fmt.Fprint(w, ": keep-alive\n\n")
			fl.Flush()
		}
	}
}

// watch notices changes made outside serve (CLI, hooks, cron) by file times.
func (s *Server) watch(ctx context.Context) {
	last := map[string]string{}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		list, err := project.List(s.home)
		if err != nil {
			continue
		}
		for _, p := range list {
			sig := signature(p)
			if prev, ok := last[p.Name]; ok && prev != sig {
				s.hub.send("changed", p.Name)
			}
			last[p.Name] = sig
		}
	}
}

func signature(p *project.Project) string {
	var b strings.Builder
	for _, f := range []string{"board.db", "board.db-wal", "decisions.jsonl", "audit.log", "gates.json", "project.yaml", "wiki/index.md", "wiki/log.md"} {
		if fi, err := os.Stat(filepath.Join(p.Dir, filepath.FromSlash(f))); err == nil {
			fmt.Fprintf(&b, "%s:%d:%d;", f, fi.Size(), fi.ModTime().UnixNano())
		}
	}
	for _, d := range []string{"wiki/_drafts", "inbox"} {
		if es, err := os.ReadDir(filepath.Join(p.Dir, filepath.FromSlash(d))); err == nil {
			for _, e := range es {
				if fi, err := e.Info(); err == nil {
					fmt.Fprintf(&b, "%s/%s:%d:%d;", d, e.Name(), fi.Size(), fi.ModTime().UnixNano())
				}
			}
		}
	}
	return b.String()
}

// ---- helpers and protections ----

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// sameOrigin blocks cross-site writes: every POST needs the X-HPK header (which a
// plain form or image cannot send) and, when the browser says, a loopback origin.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-HPK") != "1" {
				writeErr(w, http.StatusForbidden, errors.New("missing X-HPK header"))
				return
			}
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				// Same origin as the page (team mode on a server), or this machine.
				if err != nil || !strings.EqualFold(u.Host, r.Host) && !strings.EqualFold(u.Host, r.Header.Get("X-Forwarded-Host")) && !isLoopback(u.Host) && !isLoopback(net.JoinHostPort(u.Hostname(), "0")) {
					writeErr(w, http.StatusForbidden, errors.New("cross-origin request refused"))
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// The Console has no build step and no hashed file names, so HTML must not be cached.
func noCacheHTML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/vendor/"):
			// bundled libraries (three.js) change only with a new hpk build; revalidated by Last-Modified
			w.Header().Set("Cache-Control", "no-cache")
		case r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") || strings.HasSuffix(r.URL.Path, ".js") || strings.HasSuffix(r.URL.Path, ".json"):
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// The Console loads nothing from other hosts, so the policy can be strict.
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; connect-src 'self' blob: data:; frame-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeJSON2(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
