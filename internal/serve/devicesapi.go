// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

// The team server's side of joining (docs/plans/connected-mode.md §12): owners
// make one-time invite codes; a person's own hpk exchanges a code for a device
// token; owners see and revoke every device, people see and revoke their own.

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"hpk.local/hpk/internal/team"
)

type devStore struct {
	mu    sync.Mutex
	d     *team.Devices
	mod   time.Time // devices.json's time when last read or written here
	fails map[string][]time.Time
}

// withDevices runs fn on the device store, reloading it first if something else
// (hpk team join-code on the command line) changed the file.
func (s *Server) withDevices(fn func(*team.Devices)) {
	s.devs.mu.Lock()
	defer s.devs.mu.Unlock()
	f := team.DevicesFile(s.home)
	var mod time.Time
	if fi, err := os.Stat(f); err == nil {
		mod = fi.ModTime()
	}
	if s.devs.d == nil || !mod.Equal(s.devs.mod) {
		d, err := team.LoadDevices(s.home)
		if err != nil {
			log.Printf("hpk serve: %v (no device can sign in until it is fixed)", err)
			d = &team.Devices{} // empty and unsaved: a broken file is never overwritten
		}
		s.devs.d = d
	}
	fn(s.devs.d)
	if fi, err := os.Stat(f); err == nil {
		s.devs.mod = fi.ModTime()
	}
}

// clientIP is the address of the person: the connection's, or, from a trusted
// proxy, the last address that proxy added to X-Forwarded-For.
func clientIP(r *http.Request, c *team.Config) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if c != nil && len(c.TrustedProxies) > 0 && fromTrustedProxy(r, c.TrustedProxies) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
				return ip
			}
		}
	}
	return net.ParseIP(host)
}

// Wrong codes: at most 10 in 10 minutes from one address.
const joinFailMax, joinFailWindow = 10, 10 * time.Minute

// joinAttempt counts a try before the code is checked, in one critical
// section, so a burst of parallel requests cannot slip past the limit. It
// returns false when the address is over the limit.
func (s *Server) joinAttempt(ip string, now time.Time) bool {
	s.devs.mu.Lock()
	defer s.devs.mu.Unlock()
	if s.devs.fails == nil {
		s.devs.fails = map[string][]time.Time{}
	}
	prune := func(k string) []time.Time {
		keep := s.devs.fails[k][:0]
		for _, t := range s.devs.fails[k] {
			if now.Sub(t) < joinFailWindow {
				keep = append(keep, t)
			}
		}
		if len(keep) == 0 {
			delete(s.devs.fails, k)
			return nil
		}
		s.devs.fails[k] = keep
		return keep
	}
	if len(s.devs.fails) > 1000 { // many addresses (IPv6): drop the stale ones
		for k := range s.devs.fails {
			prune(k)
		}
	}
	keep := prune(ip)
	if len(keep) >= joinFailMax {
		return false
	}
	s.devs.fails[ip] = append(keep, now)
	return true
}

// joinWorked takes back the try a good code used.
func (s *Server) joinWorked(ip string, now time.Time) {
	s.devs.mu.Lock()
	defer s.devs.mu.Unlock()
	l := s.devs.fails[ip]
	for i := len(l) - 1; i >= 0; i-- {
		if l[i].Equal(now) {
			l = append(l[:i], l[i+1:]...)
			break
		}
	}
	if len(l) == 0 {
		delete(s.devs.fails, ip)
	} else {
		s.devs.fails[ip] = l
	}
}

func (s *Server) devicesAPI(mux *http.ServeMux) {
	// A person's own hpk joins with an invite code. No sign-in (the code is the
	// proof), but HTTPS, the IP allowlist (identify) and a limit on wrong codes.
	s.teamRoute(mux, "POST /api/join", func(w http.ResponseWriter, r *http.Request) {
		c := s.teamConfig()
		if c == nil {
			writeErr(w, http.StatusNotFound, errors.New("this hpk is not a team server"))
			return
		}
		ip := clientIP(r, c)
		if !s.viaHTTPS(r) && (ip == nil || !ip.IsLoopback()) {
			writeErr(w, http.StatusForbidden, errors.New("join over HTTPS only"))
			return
		}
		now := time.Now()
		if !s.joinAttempt(ip.String(), now) {
			writeErr(w, http.StatusTooManyRequests, errors.New("too many wrong codes from this address; wait 10 minutes"))
			return
		}
		var in struct {
			Code   string `json:"code"`
			Device string `json:"device"`
		}
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		var dev team.Device
		var token string
		var err error
		s.withDevices(func(d *team.Devices) { dev, token, err = d.Join(c, in.Code, in.Device, now) })
		if err != nil {
			time.Sleep(500 * time.Millisecond) // slow down guessing
			writeErr(w, http.StatusUnauthorized, err)
			return
		}
		s.joinWorked(ip.String(), now)
		m, _ := c.Find(dev.Member)
		s.hub.send("changed", "team")
		writeJSON(w, map[string]any{"token": token, "deviceId": dev.ID, "device": dev.Name, "member": m.ID, "name": m.Label(), "role": m.Role})
	})
	s.teamRoute(mux, "POST /api/team/join-code", func(w http.ResponseWriter, r *http.Request) {
		c := s.teamConfig()
		if c == nil {
			writeErr(w, http.StatusBadRequest, errors.New("team mode is off; start it with hpk team init"))
			return
		}
		var in struct {
			ID string `json:"id"`
		}
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		var code string
		var exp time.Time
		var err error
		s.withDevices(func(d *team.Devices) {
			code, exp, err = d.NewInvite(c, strings.TrimSpace(in.ID), s.who(r).ID, time.Now())
		})
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]any{"code": code, "expires": exp, "member": in.ID})
	})
	list := func(member string) []team.PublicDevice {
		var out []team.PublicDevice
		s.withDevices(func(d *team.Devices) { out = d.For(member) })
		return out
	}
	revoke := func(w http.ResponseWriter, r *http.Request, member string) {
		var in struct {
			ID string `json:"id"`
		}
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		var err error
		s.withDevices(func(d *team.Devices) { err = d.Revoke(in.ID, member) })
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		s.hub.send("changed", "team")
		writeJSON(w, map[string]any{"ok": true})
	}
	s.teamRoute(mux, "GET /api/team/devices", func(w http.ResponseWriter, r *http.Request) {
		if s.teamConfig() == nil {
			writeJSON(w, map[string]any{"devices": []team.PublicDevice{}})
			return
		}
		if !s.can(w, r, team.People) {
			return
		}
		c := s.teamConfig()
		writeJSON(w, map[string]any{"devices": list(""), "allowIPs": c.AllowIPs})
	})
	s.teamRoute(mux, "POST /api/team/devices/revoke", func(w http.ResponseWriter, r *http.Request) {
		revoke(w, r, "")
	})
	mux.HandleFunc("GET /api/me/devices", func(w http.ResponseWriter, r *http.Request) {
		me := s.who(r)
		if !me.Team {
			writeJSON(w, map[string]any{"devices": []team.PublicDevice{}})
			return
		}
		writeJSON(w, map[string]any{"devices": list(me.ID), "current": me.Device})
	})
	mux.HandleFunc("POST /api/me/devices/revoke", func(w http.ResponseWriter, r *http.Request) {
		me := s.who(r)
		if !me.Team {
			writeErr(w, http.StatusBadRequest, errors.New("team mode is off"))
			return
		}
		revoke(w, r, me.ID)
	})
}
