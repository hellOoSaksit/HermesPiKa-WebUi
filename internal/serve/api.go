// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"hpk.local/hpk/internal/board"
	"hpk.local/hpk/internal/folders"
	"hpk.local/hpk/internal/gates"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
	"hpk.local/hpk/internal/team"
	"hpk.local/hpk/internal/wiki"
)

func (s *Server) api(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/events", s.events)
	s.pages(mux)
	s.modelAPI(mux)

	mux.HandleFunc("GET /api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		snap, err := s.BuildSnapshot(r, r.URL.Query().Get("project"))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, snap)
	})

	mux.HandleFunc("GET /api/sessions/{id}/chat", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.loadFor(r, r.URL.Query().Get("project"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		id := r.PathValue("id")
		// In a team, a chat belongs to whoever started it; others see it on the board
		// but read it only when it is shared with them, or as owners of the whole Console (§19.3).
		if _, ok := s.chatAccess(w, r, p, id); !ok {
			return
		}
		msgs, live, streaming := s.acp.Transcript(id)
		if msgs == nil {
			msgs = []Msg{}
		}
		// A chat this serve does not hold still shows where its model changed.
		if sess, err := session.Get(p, id); err == nil && !live {
			for _, sw := range sess.Switches {
				msgs = append(msgs, Msg{T: "switch", At: sw.At.UnixMilli(), From: sw.From, To: sw.To, Way: sw.Way, Fill: sw.Fill})
			}
		}
		// Main chats also show what their sub-tasks sent back.
		if sess, err := session.Get(p, id); err == nil && sess.Parent == "" {
			items, _ := session.Inbox(p, id)
			for _, it := range items {
				m := Msg{T: "summary", At: it.At.UnixMilli(), Text: it.Report.Done}
				if it.Pending {
					m.T = "clarify"
					m.Text = it.Report.Question
				}
				m.ID = it.FromID
				msgs = append(msgs, m)
			}
		}
		writeJSON(w, map[string]any{"messages": msgs, "live": live, "streaming": streaming})
	})

	mux.HandleFunc("GET /api/wiki/page", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.loadFor(r, r.URL.Query().Get("project"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		rel := path.Clean(strings.TrimPrefix(r.URL.Query().Get("path"), "/"))
		if strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, "_drafts") || !strings.HasSuffix(rel, ".md") {
			writeErr(w, http.StatusBadRequest, errors.New("bad page path"))
			return
		}
		raw, err := os.ReadFile(filepath.Join(p.WikiDir(), filepath.FromSlash(rel)))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		pg, err := wiki.Parse(raw)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeJSON(w, map[string]any{"id": rel, "path": rel, "title": pg.Meta.Title, "sources": nonNil(pg.Meta.Sources),
			"confidence": pg.Meta.Confidence, "updated": pg.Meta.Updated, "markdown": pg.Body})
	})

	// ---- writes (all need the X-HPK header, see sameOrigin) ----

	mux.HandleFunc("POST /api/mode", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
			Value   string `json:"value"`
			Minutes int    `json:"minutes"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		st, err := gates.SetMode(p, in.Value, time.Duration(in.Minutes)*time.Minute, "", s.who(r).ID, time.Now())
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		s.hub.send("changed", p.Name)
		writeJSON(w, st)
	})

	mux.HandleFunc("POST /api/approvals/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project  string `json:"project"`
			Decision string `json:"decision"` // approve | session | deny
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		id := r.PathValue("id")
		var err error
		switch {
		case strings.HasPrefix(id, "perm-"):
			// What the agent asks to run may cross a red line: maintainers and owners decide.
			if !s.can(w, r, team.RedLine) {
				return
			}
			err = s.acp.Decide(id, p.Name, in.Decision)
			if err == nil {
				_ = gates.Record(p, gates.Decision{At: time.Now().UTC(), Kind: "human", Detail: id, By: s.who(r).ID, Outcome: in.Decision})
			}
		case strings.HasPrefix(id, "stale-"):
			// Approve = release the stale claim; deny = keep waiting.
			if in.Decision != "deny" {
				err = withBoard(p, func(db *board.DB) error { _, e := db.Release(strings.TrimPrefix(id, "stale-")); return e })
			}
		case strings.HasPrefix(id, "dup-"), strings.HasPrefix(id, "q-"), id == "drafts":
			err = fmt.Errorf("%s is decided where it lives (board, chat or wiki drafts)", id)
		default:
			err = fmt.Errorf("unknown approval %s", id)
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		s.hub.send("changed", p.Name)
		writeJSON(w, map[string]string{"ok": id})
	})

	mux.HandleFunc("POST /api/wiki/drafts", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project  string `json:"project"`
			Nums     []int  `json:"nums"`
			Decision string `json:"decision"` // approve | discard
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		wk := wiki.Open(p.WikiDir())
		var res []wiki.Result
		var err error
		if in.Decision == "approve" {
			// decisions/, architecture/, people/ … need a maintainer or owner (§19.2).
			if ds, e := wk.Drafts(); e == nil {
				for _, d := range ds {
					for _, n := range in.Nums {
						if d.N == n && wiki.CoreTarget(d.Meta.Target) && !s.can(w, r, team.ApproveCore) {
							return
						}
					}
				}
			}
			res, err = wk.Approve(in.Nums, s.who(r).ID)
			if err == nil {
				if ferr := folders.AfterApprove(s.home, p, res); ferr != nil {
					fmt.Fprintln(os.Stderr, "hpk serve: folder rules:", ferr)
				}
			}
		} else {
			res, err = wk.Discard(in.Nums, s.who(r).ID, "from HPK Console")
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		s.hub.send("changed", p.Name)
		writeJSON(w, res)
	})

	mux.HandleFunc("POST /api/notices/read", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		if err := withBoard(p, func(db *board.DB) error { return db.MarkRead() }); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project      string            `json:"project"`
			Name         string            `json:"name"`
			Parent       string            `json:"parent"`
			Vars         map[string]string `json:"vars"`
			FirstMessage string            `json:"firstMessage"`
			Start        bool              `json:"start"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		sess, err := session.New(p, session.NewOptions{Parent: in.Parent, Name: in.Name, Vars: in.Vars, Task: in.FirstMessage,
			Worktree: in.Parent != "", Owner: s.who(r).ID})
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if in.Start && !s.personBudgetOK(w, r) {
			return
		}
		if in.Start {
			// Opening the session in Hermes runs a model: the person pressed Start.
			if err := s.acp.Start(p, sess.ID, "", in.FirstMessage, s.who(r).ID); err != nil {
				writeErr(w, http.StatusBadGateway, fmt.Errorf("session %s recorded, but Hermes did not start: %w", sess.Num, err))
				return
			}
		}
		s.hub.send("sessions", p.Name)
		writeJSON(w, sess)
	})

	mux.HandleFunc("POST /api/sessions/{id}/prompt", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
			Text    string `json:"text"`
			How     string `json:"how"`
			// project-relative paths of files uploaded with /api/files/upload
			Attachments []string `json:"attachments"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		if _, ok := s.chatAccess(w, r, p, r.PathValue("id")); !ok {
			return
		}
		for _, a := range in.Attachments {
			if allowedParts(a) == nil && !filepath.IsAbs(a) {
				in.Text += "\n\nAttached file (in the project folder): " + a
			}
		}
		if strings.TrimSpace(in.Text) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("empty message"))
			return
		}
		if !s.personBudgetOK(w, r) {
			return
		}
		if err := s.acp.Prompt(p, r.PathValue("id"), in.Text, orStr(in.How, "send"), s.who(r).ID); err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/sessions/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		if _, ok := s.chatAccess(w, r, p, r.PathValue("id")); !ok {
			return
		}
		if err := s.acp.Cancel(r.PathValue("id")); err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/sessions/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
			Text    string `json:"text"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		if _, ok := s.chatAccess(w, r, p, r.PathValue("id")); !ok {
			return
		}
		if !s.personBudgetOK(w, r) {
			return
		}
		if err := s.acp.Start(p, r.PathValue("id"), in.Text, "", s.who(r).ID); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	// Share a chat with other members (team mode): its owner, or an owner of the Console.
	mux.HandleFunc("POST /api/sessions/{id}/share", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string   `json:"project"`
			With    []string `json:"with"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		sess, ok := s.chatAccess(w, r, p, r.PathValue("id"))
		if !ok {
			return
		}
		me := s.who(r)
		if sess.Owner != "" && !strings.EqualFold(sess.Owner, me.ID) && me.Role != team.Owner {
			writeErr(w, http.StatusForbidden, errors.New("only "+sess.Owner+" can share this chat"))
			return
		}
		if c := s.teamConfig(); c != nil {
			for _, id := range in.With {
				if _, ok := c.Find(id); !ok {
					writeErr(w, http.StatusBadRequest, errors.New(id+" is not in the team"))
					return
				}
			}
		}
		if _, err := session.Share(p, sess.ID, in.With); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		_ = project.Audit(p.Dir, "chat shared", sess.Num+" with "+strings.Join(in.With, ", ")+" by "+me.ID)
		s.hub.send("sessions", p.Name)
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/sessions/{id}/rename", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
			Title   string `json:"title"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		if _, ok := s.chatAccess(w, r, p, r.PathValue("id")); !ok {
			return
		}
		sess, err := session.Rename(p, r.PathValue("id"), in.Title, nil)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		s.hub.send("sessions", p.Name)
		writeJSON(w, sess)
	})
}

// load finds a project by name, or the first one.
// loadFor loads a project the person behind r may see (the first one when name is empty).
func (s *Server) loadFor(r *http.Request, name string) (*project.Project, error) {
	if name != "" {
		if !s.who(r).sees(name) {
			return nil, fmt.Errorf("no project %s", name)
		}
		return project.Load(s.home, name)
	}
	list, err := project.List(s.home)
	list = s.visible(r, list)
	if err != nil || len(list) == 0 {
		return nil, errors.New("no projects yet: hpk project add <folder>")
	}
	return list[0], nil
}

// decode reads a JSON body (≤1 MB) and loads its project.
func (s *Server) decode(w http.ResponseWriter, r *http.Request, v any, proj func() string) (*project.Project, bool) {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad JSON: %w", err))
		return nil, false
	}
	p, err := s.loadFor(r, proj())
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return nil, false
	}
	return p, true
}

func withBoard(p *project.Project, fn func(*board.DB) error) error {
	db, err := board.Open(p)
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(db)
}
