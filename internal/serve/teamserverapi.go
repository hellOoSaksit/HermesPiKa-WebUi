// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

// A person's own hpk and its team server (docs/plans/connected-mode.md §12).
// The Console on this machine never sees the device token: it asks this hpk,
// which reads the token from the vault and calls the server, and only on the
// paths listed below.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"hpk.local/hpk/internal/teamlink"
	"hpk.local/hpk/internal/vault"
)

type linkState struct {
	http *http.Client // nil = teamlink.DefaultHTTP(); tests set their own
}

func (s *Server) linkHTTP() *http.Client {
	if s.link.http != nil {
		return s.link.http
	}
	return teamlink.DefaultHTTP()
}

// What the Console on this machine may ask the team server for. Everything
// else is refused here, whatever the person's role there.
var (
	remoteGET = map[string]bool{
		"me": true, "me/devices": true, "team": true, "team/devices": true,
		"snapshot": true, "gates": true, "promotions": true, "wiki/page": true,
	}
	// Only the person's own devices. Making invite codes, adding or removing
	// people, changing roles and deciding approvals mint credentials or answer
	// red lines: a program on this machine (the AI too) can call this local API,
	// so those stay in the team server's own Console.
	remotePOST = map[string]bool{
		"me/devices/revoke": true,
	}
)

func remoteAllowed(method, path string) bool {
	if strings.Contains(path, "..") || strings.Contains(path, "//") {
		return false
	}
	if method == http.MethodGet {
		return remoteGET[path]
	}
	return remotePOST[path]
}

func (s *Server) teamServerAPI(mux *http.ServeMux) {
	// Only on a person's own machine; a team server joins nothing.
	open := func(w http.ResponseWriter) *vault.Vault {
		if !s.personalOn() {
			writeErr(w, http.StatusForbidden, errors.New("joining a team server is for hpk on a person's own machine, not on a team server"))
			return nil
		}
		v, _, err := s.openPersonal()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, errors.New("the vault cannot open, so the device token has nowhere safe to live: "+err.Error()))
			return nil
		}
		return v
	}
	// The device token is sent only to the server it was issued by (kept with it
	// in the vault), never to whatever teamserver.json says now.
	cred := func(v *vault.Vault) teamlink.Credential {
		c, _ := teamlink.DecodeCredential(v.System(teamlink.SecretName))
		return c
	}
	client := func(v *vault.Vault, _ *teamlink.Link) teamlink.Client {
		c := cred(v)
		return teamlink.Client{Base: c.URL, Token: c.Token, HTTP: s.linkHTTP()}
	}

	mux.HandleFunc("GET /api/teamserver", func(w http.ResponseWriter, r *http.Request) {
		if !s.personalOn() {
			writeJSON(w, map[string]any{"available": false})
			return
		}
		l, err := teamlink.Load(s.home)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if l == nil {
			writeJSON(w, map[string]any{"available": true, "connected": false})
			return
		}
		v := open(w)
		if v == nil {
			return
		}
		out := map[string]any{"available": true, "connected": true, "url": l.URL, "member": l.Member, "name": l.Name,
			"role": l.Role, "deviceId": l.DeviceID, "device": l.Device, "joined": l.Joined}
		if c := cred(v); c.Token == "" {
			out["state"] = "noToken"
			writeJSON(w, out)
			return
		} else if err := l.Bound(c); err != nil {
			out["state"], out["error"] = "moved", err.Error()
			writeJSON(w, out)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		var me struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Role string `json:"role"`
		}
		err = client(v, l).JSON(ctx, http.MethodGet, "/api/me", nil, &me)
		var se *teamlink.ServerError
		switch {
		case err == nil && me.ID != "":
			out["state"] = "ok"
			if me.Role != l.Role || me.Name != l.Name {
				l.Role, l.Name = me.Role, me.Name
				_ = l.Save(s.home)
				out["role"], out["name"] = me.Role, me.Name
			}
		case err == nil:
			out["state"] = "revoked" // the server answered, but not as this device
		case errors.As(err, &se) && (se.Code == http.StatusUnauthorized):
			out["state"], out["error"] = "revoked", se.Msg
		default:
			out["state"], out["error"] = "offline", err.Error()
		}
		writeJSON(w, out)
	})

	s.teamRoute(mux, "POST /api/teamserver/join", func(w http.ResponseWriter, r *http.Request) {
		v := open(w)
		if v == nil {
			return
		}
		var in struct {
			URL    string `json:"url"`
			Code   string `json:"code"`
			Device string `json:"device"`
		}
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		if l, _ := teamlink.Load(s.home); l != nil {
			writeErr(w, http.StatusConflict, errors.New("this machine already joined "+l.URL+"; leave it first"))
			return
		}
		if strings.TrimSpace(in.Device) == "" {
			in.Device, _ = os.Hostname()
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		l, token, err := teamlink.Join(ctx, s.linkHTTP(), in.URL, in.Code, in.Device)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := v.PutSystem(teamlink.SecretName, teamlink.Credential{URL: l.URL, Token: token}.Encode()); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if err := l.Save(s.home); err != nil {
			_ = v.DeleteSystem(teamlink.SecretName)
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		s.hub.send("changed", "teamserver")
		writeJSON(w, map[string]any{"ok": true, "url": l.URL, "member": l.Member, "name": l.Name, "role": l.Role, "device": l.Device})
	})

	// Leave: revoke this device on the server (if it can be reached), then forget
	// the token. The vault PIN, when set, is needed: it removes a secret.
	mux.HandleFunc("POST /api/teamserver/leave", func(w http.ResponseWriter, r *http.Request) {
		v := open(w)
		if v == nil {
			return
		}
		var in struct {
			PIN string `json:"pin"`
		}
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		if v.HasPIN() {
			if err := v.CheckPIN(in.PIN); err != nil {
				writeErr(w, http.StatusForbidden, err)
				return
			}
		}
		l, _ := teamlink.Load(s.home)
		revoked := false
		if l != nil && l.Bound(cred(v)) == nil {
			ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
			err := client(v, l).JSON(ctx, http.MethodPost, "/api/me/devices/revoke", map[string]string{"id": l.DeviceID}, nil)
			cancel()
			revoked = err == nil
		}
		if err := v.DeleteSystem(teamlink.SecretName); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if err := teamlink.Remove(s.home); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		s.hub.send("changed", "teamserver")
		writeJSON(w, map[string]any{"ok": true, "revokedOnServer": revoked})
	})

	// The Console's way to the team server: listed paths only, the token added here.
	remote := func(w http.ResponseWriter, r *http.Request) {
		path := r.PathValue("path")
		if !remoteAllowed(r.Method, path) {
			writeErr(w, http.StatusForbidden, errors.New("the Console here does not reach "+path+" on the team server"))
			return
		}
		v := open(w)
		if v == nil {
			return
		}
		l, _ := teamlink.Load(s.home)
		if l == nil {
			writeErr(w, http.StatusConflict, errors.New("this machine has not joined a team server (Settings > Team server)"))
			return
		}
		var body any
		if r.Method == http.MethodPost {
			raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
			if err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			body = rawJSON(raw)
		}
		target := "/api/" + path
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		if err := l.Bound(cred(v)); err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		res, err := client(v, l).Do(ctx, r.Method, target, body)
		if err != nil {
			writeErr(w, http.StatusBadGateway, errors.New("cannot reach the team server: "+err.Error()))
			return
		}
		defer res.Body.Close()
		if ct := res.Header.Get("Content-Type"); strings.HasPrefix(ct, "application/json") {
			w.Header().Set("Content-Type", ct)
		} else {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(res.Body, 8<<20))
	}
	mux.HandleFunc("GET /api/teamserver/remote/{path...}", remote)
	mux.HandleFunc("POST /api/teamserver/remote/{path...}", remote)
}

// rawJSON sends bytes the Console gave as they are (they are JSON already).
type rawJSON []byte

func (b rawJSON) MarshalJSON() ([]byte, error) {
	if len(b) == 0 {
		return []byte("{}"), nil
	}
	return b, nil
}
