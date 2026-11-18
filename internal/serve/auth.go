// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"strings"
	"sync"
	"time"

	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/team"
)

// identity is who is behind a request.
type identity struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Role     string   `json:"role"`
	Projects []string `json:"projects,omitempty"` // empty = all
	Team     bool     `json:"team"`               // false = solo mode
	Device   string   `json:"device,omitempty"`   // signed in with a device token (id)
}

func (i identity) sees(projectName string) bool {
	return team.Person{Projects: i.Projects}.Sees(projectName)
}

type ctxKey struct{}

// who returns the person behind a request (the solo owner outside team mode).
func (s *Server) who(r *http.Request) identity {
	if r != nil {
		if id, ok := r.Context().Value(ctxKey{}).(identity); ok {
			return id
		}
	}
	return s.soloOwner()
}

func (s *Server) soloOwner() identity {
	return identity{ID: osUser(), Name: osUser(), Role: team.Owner}
}

func osUser() string {
	if v := os.Getenv("HPK_USER"); v != "" {
		return v
	}
	if u, err := user.Current(); err == nil {
		n := u.Username
		if i := strings.LastIndexAny(n, `\/`); i >= 0 {
			n = n[i+1:]
		}
		return n
	}
	return "me"
}

// ---- team config and logins ----

type logins struct {
	mu   sync.Mutex
	byID map[string]login
}

type login struct {
	member  string
	token   string // the member's token hash when they signed in: a rotated or re-issued token ends this sign-in
	expires time.Time
}

const loginTTL = 12 * time.Hour
const cookieName = "hpk_session"

func (s *Server) teamConfig() *team.Config {
	c, err := team.Load(s.home)
	if err != nil || c == nil || !c.Enabled {
		return nil
	}
	return c
}

// TeamEnabled reports whether serve runs in team mode.
func (s *Server) TeamEnabled() bool { return s.teamConfig() != nil }

func (l *logins) start(member, tokenHash string) string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for k, v := range l.byID {
		if now.After(v.expires) {
			delete(l.byID, k)
		}
	}
	l.byID[id] = login{member: member, token: tokenHash, expires: now.Add(loginTTL)}
	return id
}

func (l *logins) member(id string) (login, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.byID[id]
	if !ok || time.Now().After(v.expires) {
		delete(l.byID, id)
		return login{}, false
	}
	return v, true
}

func (l *logins) end(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byID, id)
}

// public paths need no sign-in: the Console's static files, health, sign-in itself.
func public(r *http.Request) bool {
	p := r.URL.Path
	if !strings.HasPrefix(p, "/api/") {
		return r.Method == http.MethodGet || r.Method == http.MethodHead
	}
	return p == "/api/health" || p == "/api/login" || p == "/api/me" || p == "/api/logout" || p == "/api/join"
}

// authenticate finds the person behind a request in team mode.
func (s *Server) authenticate(c *team.Config, r *http.Request) (*team.Person, string, error) {
	// A device token (a person's own hpk, joined with an invite code) works in
	// either sign-in mode: an owner made the invite on purpose.
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer "+team.DevicePrefix) {
		var m *team.Person
		var dev team.Device
		ok := false
		s.withDevices(func(d *team.Devices) {
			m, dev, ok = d.ByToken(c, strings.TrimPrefix(a, "Bearer "), clientIP(r, c).String(), time.Now())
		})
		if !ok {
			return nil, "", errors.New("this device is not joined (revoked, or its person left the team)")
		}
		return m, dev.ID, nil
	}
	m, err := s.authenticatePerson(c, r)
	return m, "", err
}

func (s *Server) authenticatePerson(c *team.Config, r *http.Request) (*team.Person, error) {
	switch c.Auth {
	case "header":
		if !fromTrustedProxy(r, c.TrustedProxies) {
			return nil, errors.New("not from a trusted proxy")
		}
		id := strings.TrimSpace(r.Header.Get(c.Header))
		if id == "" {
			return nil, errors.New("the proxy sent no user")
		}
		if m, ok := c.Find(id); ok {
			return m, nil
		}
		return nil, errors.New(id + " is not in the team")
	default:
		if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
			if m, ok := c.ByToken(strings.TrimPrefix(a, "Bearer ")); ok {
				return m, nil
			}
			return nil, errors.New("unknown token")
		}
		ck, err := r.Cookie(cookieName)
		if err != nil {
			return nil, errors.New("not signed in")
		}
		lg, ok := s.logins.member(ck.Value)
		if !ok {
			return nil, errors.New("sign-in expired")
		}
		m, ok := c.Find(lg.member)
		if !ok {
			s.logins.end(ck.Value)
			return nil, errors.New("no longer in the team")
		}
		// the token they signed in with was rotated, or they were removed and added again
		if m.TokenHash == "" || subtle.ConstantTimeCompare([]byte(m.TokenHash), []byte(lg.token)) != 1 {
			s.logins.end(ck.Value)
			return nil, errors.New("your token changed; sign in again")
		}
		return m, nil
	}
}

func fromTrustedProxy(r *http.Request, proxies []string) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	for _, p := range proxies {
		if _, n, err := net.ParseCIDR(p); err == nil && ip != nil && n.Contains(ip) {
			return true
		}
		if pi := net.ParseIP(p); pi != nil && ip != nil && pi.Equal(ip) {
			return true
		}
	}
	return false
}

// action is the least permission a request needs (checked again, finer, in some handlers).
func action(r *http.Request) string {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return team.View
	}
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/api/sessions"), p == "/api/notices/read", strings.HasPrefix(p, "/api/approvals/"):
		return team.Work // red-line permits need more; the handler checks
	case p == "/api/wiki/drafts":
		return team.ApproveDraft // core areas need more; the handler checks
	case strings.HasPrefix(p, "/api/git/"):
		return team.Work // red lines (push, delete, …) need more; the handler checks
	case p == "/api/wiki/promote", p == "/api/promotions":
		return team.Work // a draft only; deciding a team promotion needs an owner (handler)
	case p == "/api/me/devices/revoke":
		return team.View // a person's own device; the handler checks whose
	case p == "/api/mode", p == "/api/gates/redlines":
		return team.RedLine
	case strings.HasPrefix(p, "/api/team"):
		return team.People
	}
	return team.Admin // everything else changes settings; unknown paths fail closed
}

// identify wraps the API: in team mode every request (but public ones) needs a
// signed-in member whose role allows the action.
func (s *Server) identify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := s.teamConfig()
		if c == nil {
			if !loopbackHost(r.Host) {
				writeErr(w, http.StatusMisdirectedRequest, errors.New("this HPK Console answers only to localhost / 127.0.0.1 (team mode serves other names)"))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, s.soloOwner())))
			return
		}
		if !c.IPAllowed(clientIP(r, c)) {
			writeErr(w, http.StatusForbidden, errors.New("this address may not reach the team server (allow_ips in team.yaml)"))
			return
		}
		m, dev, err := s.authenticate(c, r)
		if err != nil {
			if public(r) {
				next.ServeHTTP(w, r)
				return
			}
			writeErr(w, http.StatusUnauthorized, errors.New("sign in to HPK Console: "+err.Error()))
			return
		}
		id := identity{ID: m.ID, Name: m.Label(), Role: m.Role, Projects: m.Projects, Team: true, Device: dev}
		if !public(r) && !team.Can(id.Role, action(r)) {
			writeErr(w, http.StatusForbidden, errors.New("your role ("+id.Role+") cannot do this"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

// can is the finer check handlers make (red-line permits, core wiki areas).
func (s *Server) can(w http.ResponseWriter, r *http.Request, act string) bool {
	id := s.who(r)
	if team.Can(id.Role, act) {
		return true
	}
	writeErr(w, http.StatusForbidden, errors.New("your role ("+id.Role+") cannot do this"))
	return false
}

// visible filters projects to those the person may see.
func (s *Server) visible(r *http.Request, list []*project.Project) []*project.Project {
	id := s.who(r)
	out := list[:0:0]
	for _, p := range list {
		if id.sees(p.Name) {
			out = append(out, p)
		}
	}
	return out
}

// auth endpoints: sign in with a token, sign out, who am I.
func (s *Server) authAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		c := s.teamConfig()
		if c == nil {
			writeJSON(w, s.soloOwner())
			return
		}
		if id, ok := r.Context().Value(ctxKey{}).(identity); ok {
			writeJSON(w, id)
			return
		}
		writeJSON(w, map[string]any{"team": true, "signedIn": false, "auth": c.Auth})
	})
	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		c := s.teamConfig()
		if c == nil || c.Auth != "token" {
			writeErr(w, http.StatusBadRequest, errors.New("sign-in with a token is only for team mode with auth: token"))
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		m, ok := c.ByToken(strings.TrimSpace(in.Token))
		if !ok {
			time.Sleep(500 * time.Millisecond) // slow down guessing
			writeErr(w, http.StatusUnauthorized, errors.New("that token is not valid"))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.logins.start(m.ID, m.TokenHash), Path: "/", HttpOnly: true,
			SameSite: http.SameSiteStrictMode, Secure: s.viaHTTPS(r), MaxAge: int(loginTTL.Seconds())})
		writeJSON(w, identity{ID: m.ID, Name: m.Label(), Role: m.Role, Projects: m.Projects, Team: true})
	})
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		if ck, err := r.Cookie(cookieName); err == nil {
			s.logins.end(ck.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		writeJSON(w, map[string]any{"ok": true})
	})
}

// decodeOnly reads a JSON body (≤64 KB) that names no project.
func (s *Server) decodeOnly(w http.ResponseWriter, r *http.Request, v any) (struct{}, bool) {
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad JSON: %w", err))
		return struct{}{}, false
	}
	return struct{}{}, true
}

// teamAPI lets owners manage people from the Console. Tokens are returned once.
func (s *Server) teamAPI(mux *http.ServeMux) {
	type row struct {
		ID       string    `json:"id"`
		Name     string    `json:"name"`
		Role     string    `json:"role"`
		Projects []string  `json:"projects"`
		Added    time.Time `json:"added"`
		HasToken bool      `json:"hasToken"`
		Daily    float64   `json:"budgetDaily"`
		Monthly  float64   `json:"budgetMonthly"`
		Today    float64   `json:"spentToday"`
		Month    float64   `json:"spentMonth"`
	}
	mux.HandleFunc("GET /api/team", func(w http.ResponseWriter, r *http.Request) {
		c := s.teamConfig()
		if c == nil {
			writeJSON(w, map[string]any{"enabled": false, "members": []row{}})
			return
		}
		out := []row{}
		now := time.Now()
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		for _, m := range c.Members {
			out = append(out, row{ID: m.ID, Name: m.Label(), Role: m.Role, Projects: m.Projects, Added: m.Added, HasToken: m.TokenHash != "",
				Daily: m.BudgetDaily, Monthly: m.BudgetMonthly, Today: privacy.SpentBy(s.home, m.ID, day), Month: privacy.SpentBy(s.home, m.ID, month)})
		}
		writeJSON(w, map[string]any{"enabled": true, "auth": c.Auth, "members": out})
	})
	change := func(w http.ResponseWriter, r *http.Request, fn func(c *team.Config, in teamIn) (string, error)) {
		c := s.teamConfig()
		if c == nil {
			writeErr(w, http.StatusBadRequest, errors.New("team mode is off; start it with hpk team init"))
			return
		}
		var in teamIn
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		token, err := fn(c, in)
		if err == nil {
			err = c.Save(s.home)
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		s.hub.send("changed", "team")
		writeJSON(w, map[string]any{"ok": true, "token": token})
	}
	mux.HandleFunc("POST /api/team/invite", func(w http.ResponseWriter, r *http.Request) {
		change(w, r, func(c *team.Config, in teamIn) (string, error) {
			return c.Add(strings.TrimSpace(in.ID), strings.TrimSpace(in.Name), in.Role, in.Projects, time.Now())
		})
	})
	mux.HandleFunc("POST /api/team/role", func(w http.ResponseWriter, r *http.Request) {
		change(w, r, func(c *team.Config, in teamIn) (string, error) { return "", c.SetRole(in.ID, in.Role) })
	})
	mux.HandleFunc("POST /api/team/remove", func(w http.ResponseWriter, r *http.Request) {
		change(w, r, func(c *team.Config, in teamIn) (string, error) {
			if strings.EqualFold(in.ID, s.who(r).ID) {
				return "", errors.New("you cannot remove yourself; ask another owner")
			}
			if err := c.Remove(in.ID); err != nil {
				return "", err
			}
			var derr error
			s.withDevices(func(d *team.Devices) { derr = d.RevokeMember(in.ID) })
			return "", derr
		})
	})
	mux.HandleFunc("POST /api/team/budget", func(w http.ResponseWriter, r *http.Request) {
		change(w, r, func(c *team.Config, in teamIn) (string, error) { return "", c.SetBudget(in.ID, in.Daily, in.Monthly) })
	})
	mux.HandleFunc("POST /api/team/rotate", func(w http.ResponseWriter, r *http.Request) {
		change(w, r, func(c *team.Config, in teamIn) (string, error) { return c.Rotate(in.ID) })
	})
}

type teamIn struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Role     string   `json:"role"`
	Projects []string `json:"projects"`
	Daily    float64  `json:"daily"`
	Monthly  float64  `json:"monthly"`
}

// personBudgetOK refuses to start model work for a team member at their own cap.
func (s *Server) personBudgetOK(w http.ResponseWriter, r *http.Request) bool {
	me := s.who(r)
	if !me.Team {
		return true
	}
	c := s.teamConfig()
	if c == nil {
		return true
	}
	m, ok := c.Find(me.ID)
	if !ok {
		return true
	}
	if over, why := privacy.OverPerson(s.home, m.ID, m.BudgetDaily, m.BudgetMonthly, time.Now()); over {
		writeErr(w, http.StatusConflict, errors.New(why+". An owner can raise it on the Team page"))
		return false
	}
	return true
}

// viaHTTPS reports whether the person reached hpk over HTTPS: directly, or through
// a reverse proxy that terminated TLS and said so (X-Forwarded-Proto). The cookie
// is then marked Secure.
// viaHTTPS: the request reached us over TLS, here or at a proxy we trust
// (X-Forwarded-Proto is believed only from trusted_proxies in team.yaml).
func (s *Server) viaHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return false
	}
	c := s.teamConfig()
	return c != nil && fromTrustedProxy(r, c.TrustedProxies)
}

// loopbackHost: a Host header naming this machine (with or without a port).
// Without team mode the Console answers only to these names, so a web page
// that rebinds its own domain to 127.0.0.1 (DNS rebinding) is refused.
func loopbackHost(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	h = strings.TrimSuffix(strings.Trim(h, "[]"), ".")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// hostOnly is the host part of a Host header, with or without a port.
func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.Trim(hostport, "[]")
}
