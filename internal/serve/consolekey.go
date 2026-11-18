// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hpk.local/hpk/internal/isolate"
)

// Console key (solo mode). Without sign-in, anything on this machine could
// call the API: a program the AI runs could switch to Auto mode or approve its
// own red lines. So every change (POST) needs a browser the person opened
// from hpk:
//
//   - `hpk serve` keeps a one-time link, /k/<code>, valid 10 minutes, printed
//     on its console and kept in <private>/console-link (0600, this account
//     only; <private> is <home>/hpk, or a folder the AI cannot read when it
//     is isolated); `hpk open` opens it in the default browser;
//   - opening it gives that browser an HttpOnly, SameSite=Strict cookie for 30
//     days; only its SHA-256 is kept (<private>/console-sessions.json);
//   - the agent's own browser endpoint uses its key, and team mode its sign-in.
//
// With Hermes under a separate OS account (docs/isolation.md) the AI can read
// neither the link file nor the person's browser cookies.

const (
	consoleCookie = "hpk_console"
	linkTTL       = 10 * time.Minute
	consoleTTL    = 30 * 24 * time.Hour
)

type consoleKeys struct {
	mu       sync.Mutex
	home     string
	code     string // current one-time link code
	codeAt   time.Time
	sessions map[string]time.Time // sha256(cookie) -> expiry
	addr     string
}

func newConsoleKeys(home string) *consoleKeys {
	k := &consoleKeys{home: home, sessions: map[string]time.Time{}}
	if raw, err := os.ReadFile(k.sessionsPath()); err == nil {
		_ = json.Unmarshal(raw, &k.sessions)
	}
	return k
}

func (k *consoleKeys) sessionsPath() string {
	return filepath.Join(isolate.PrivateDir(k.home), "console-sessions.json")
}

// LinkFile holds the current one-time link for `hpk open`.
func LinkFile(home string) string { return filepath.Join(isolate.PrivateDir(home), "console-link") }

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func hashHex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// fresh makes a new one-time link and writes it for `hpk open`.
func (k *consoleKeys) fresh() string {
	k.mu.Lock()
	k.code, k.codeAt = randHex(16), time.Now()
	link := "http://" + k.addr + "/k/" + k.code
	k.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(LinkFile(k.home)), 0o700)
	_ = os.WriteFile(LinkFile(k.home), []byte(link+"\n"), 0o600)
	return link
}

// redeem turns a valid one-time code into a browser cookie (and a new link).
func (k *consoleKeys) redeem(code string) (string, error) {
	k.mu.Lock()
	ok := k.code != "" && time.Since(k.codeAt) < linkTTL && subtle.ConstantTimeCompare([]byte(code), []byte(k.code)) == 1
	if ok {
		k.code = ""
	}
	k.mu.Unlock()
	if !ok {
		return "", errors.New("this hpk link has been used or is too old; run `hpk open` for a new one")
	}
	cookie := randHex(32)
	k.mu.Lock()
	now := time.Now()
	for h, exp := range k.sessions {
		if now.After(exp) {
			delete(k.sessions, h)
		}
	}
	k.sessions[hashHex(cookie)] = now.Add(consoleTTL)
	b, _ := json.Marshal(k.sessions)
	k.mu.Unlock()
	_ = os.WriteFile(k.sessionsPath(), b, 0o600)
	k.fresh()
	return cookie, nil
}

func (k *consoleKeys) valid(r *http.Request) bool {
	c, err := r.Cookie(consoleCookie)
	if err != nil || len(c.Value) != 64 {
		return false
	}
	h := hashHex(c.Value)
	k.mu.Lock()
	defer k.mu.Unlock()
	exp, ok := k.sessions[h]
	return ok && time.Now().Before(exp)
}

// forget signs this browser out (Settings > This browser > Sign out).
func (k *consoleKeys) forget(r *http.Request) {
	if c, err := r.Cookie(consoleCookie); err == nil {
		k.mu.Lock()
		delete(k.sessions, hashHex(c.Value))
		b, _ := json.Marshal(k.sessions)
		k.mu.Unlock()
		_ = os.WriteFile(k.sessionsPath(), b, 0o600)
	}
}

// keyExempt: requests that prove themselves another way.
func keyExempt(r *http.Request) bool {
	p := r.URL.Path
	// the agent's browser tools (agent key) and `hpk stop` (stop key) prove themselves
	return p == "/api/browser/agent" || p == "/api/shutdown" || strings.HasPrefix(p, "/k/")
}

// requireConsoleKey wraps the API in solo mode: changes need the cookie.
func (s *Server) requireConsoleKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.keys == nil || s.teamConfig() != nil || r.Method == http.MethodGet || r.Method == http.MethodHead || keyExempt(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !s.keys.valid(r) {
			w.Header().Set("X-HPK-Need-Key", "1")
			writeErr(w, http.StatusUnauthorized, errors.New("open the Console from hpk (run `hpk open`, or click the link `hpk serve` printed); changes need a browser hpk opened"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// consoleKeyRoutes serves the one-time link and the sign-out.
func (s *Server) consoleKeyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /k/{code}", func(w http.ResponseWriter, r *http.Request) {
		if s.keys == nil || s.teamConfig() != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		cookie, err := s.keys.redeem(r.PathValue("code"))
		if err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(err.Error() + "\n"))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: consoleCookie, Value: cookie, Path: "/", MaxAge: int(consoleTTL / time.Second), HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /api/console-key", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"needed": s.keys != nil && s.teamConfig() == nil, "ok": s.keys != nil && s.keys.valid(r)})
	})
	mux.HandleFunc("POST /api/console-key/forget", func(w http.ResponseWriter, r *http.Request) {
		if s.keys != nil {
			s.keys.forget(r)
		}
		http.SetCookie(w, &http.Cookie{Name: consoleCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		writeJSON(w, map[string]any{"ok": true})
	})
}
