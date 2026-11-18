// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"hpk.local/hpk/internal/board"
	"hpk.local/hpk/internal/gates"
	"hpk.local/hpk/internal/gitops"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
	"hpk.local/hpk/internal/team"
)

// The Git screen on the project's real repository (internal/gitops).
func (s *Server) gitAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/git", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.loadFor(r, r.URL.Query().Get("project"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		m, err := gitops.Load(ctx, p, s.who(r).ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		enrich(p, m)
		writeJSON(w, m)
	})
	mux.HandleFunc("POST /api/git/action", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
			gitops.Request
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		m, err := gitops.Load(ctx, p, s.who(r).ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// Red lines (push, force-push, delete, hard reset, merge into the base):
		// the person who clicks confirmed it in the Console; in a team their role must allow it.
		line := gitops.RedLine(in.Request, m)
		if line != "" && !s.can(w, r, team.RedLine) {
			return
		}
		res, err := gitops.Do(ctx, p, in.Request, s.who(r).ID)
		detail := strings.TrimSpace(in.Action + " " + strings.Join([]string{in.Branch, in.From, in.Into, in.Onto, in.SHA, in.Name}, " "))
		if line != "" {
			_ = gates.Record(p, gates.Decision{At: time.Now().UTC(), Kind: "human", Lines: []string{line}, Tool: "git", Detail: detail, By: s.who(r).ID, Outcome: map[bool]string{true: "approved", false: "failed"}[err == nil]})
		} else {
			_ = project.Audit(p.Dir, "git "+in.Action, detail+" by "+s.who(r).ID)
		}
		s.hub.send("changed", p.Name)
		if err != nil {
			code := http.StatusBadRequest
			if errors.Is(err, context.DeadlineExceeded) {
				code = http.StatusGatewayTimeout
			}
			writeJSON2(w, code, map[string]any{"error": err.Error(), "commands": res.Commands, "output": res.Output})
			return
		}
		writeJSON(w, res)
	})
}

// enrich adds who works on each branch (hpk sessions) and what they claimed.
func enrich(p *project.Project, m *gitops.Model) {
	sessions, _ := session.List(p)
	var claims []board.Claim
	if db, err := board.Open(p); err == nil {
		claims, _ = db.Claims(false)
		db.Close()
	}
	for _, b := range m.Branches {
		for _, x := range sessions {
			if x.Branch == b.Name {
				b.Owner, b.Session = x.Owner, x.Num
				for _, c := range claims {
					if c.Session == x.ID && len(c.Paths) > 0 {
						b.Claim = strings.Join(c.Paths, ", ")
					}
				}
			}
		}
	}
}
