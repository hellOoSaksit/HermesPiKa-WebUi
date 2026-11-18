// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"hpk.local/hpk/internal/folders"
	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/places"
	"hpk.local/hpk/internal/project"
)

// tidyArea is where a tidy of p may act: its folder as checked again now
// (project.yaml is in the AI's reach), never the wikis or other projects
// inside it, with the undo record in the person's private folder.
func (s *Server) tidyArea(p *project.Project) (folders.Area, error) {
	root, err := places.Root(s.home, p)
	if err != nil {
		return folders.Area{}, err
	}
	skip := []string{p.WikiDir()}
	all, _ := project.List(s.home)
	for _, o := range all {
		if o.Name == p.Name {
			continue
		}
		skip = append(skip, o.WikiDir())
		if o.Root != "" && !samePath(o.Root, p.Root) {
			skip = append(skip, o.Root)
		}
	}
	return folders.Area{Root: root, Skip: skip, Record: folders.UndoFile(isolate.PrivateDir(s.home), p.Name), Audit: p.Dir}, nil
}

// tidyProject loads the project of a tidy request and checks its rules are the
// approved ones; ok is false after an error was written.
func (s *Server) tidyProject(w http.ResponseWriter, r *http.Request, name string) (*tidyCtx, bool) {
	if !s.personalOn() {
		http.NotFound(w, r)
		return nil, false
	}
	p, err := s.loadFor(r, name)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return nil, false
	}
	if p.Root == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this project has no folder"))
		return nil, false
	}
	if s.projectBusy(p.Name) {
		writeErr(w, http.StatusConflict, errors.New("a chat of this project is working; try again when it is done"))
		return nil, false
	}
	area, err := s.tidyArea(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return nil, false
	}
	rules, state := folders.Load(s.home, p)
	if state != folders.OK {
		writeJSON2(w, http.StatusConflict, map[string]any{"state": state})
		return nil, false
	}
	return &tidyCtx{area: area, rules: rules}, true
}

type tidyCtx struct {
	area  folders.Area
	rules folders.Rules
}

type ruleOut struct {
	Folder string   `json:"folder"`
	What   string   `json:"what"`
	Exts   []string `json:"exts"`
}

// GET  /api/files/rules?project=      the folder rules and their state {state, rows, bad}; read-only
// GET  /api/files/tidy?project=       what a tidy would move (and canUndo); 409 {state} unless the rules are approved
// POST /api/files/tidy                {project, moves}: move what the person confirmed
// GET  /api/files/tidy/undo?project=  what an undo would put back {at, moves}, for the person to confirm
// POST /api/files/tidy/undo           {project, at}: put that round back
func (s *Server) tidyAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/files/rules", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.loadFor(r, r.URL.Query().Get("project"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		rules, state := folders.Load(s.home, p)
		rows := make([]ruleOut, 0, len(rules.Rows))
		for _, x := range rules.Rows {
			exts := x.Exts
			if exts == nil {
				exts = []string{}
			}
			rows = append(rows, ruleOut{Folder: x.Folder, What: x.What, Exts: exts})
		}
		bad := rules.Bad
		if bad == nil {
			bad = []string{}
		}
		writeJSON(w, map[string]any{"state": state, "rows": rows, "bad": bad})
	})

	mux.HandleFunc("GET /api/files/tidy", func(w http.ResponseWriter, r *http.Request) {
		c, ok := s.tidyProject(w, r, r.URL.Query().Get("project"))
		if !ok {
			return
		}
		plan, err := folders.PlanTidy(c.area, c.rules)
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("cannot read the project folder"))
			return
		}
		writeJSON(w, struct {
			folders.Plan
			CanUndo bool `json:"canUndo"`
		}{plan, folders.CanUndo(c.area)})
	})

	mux.HandleFunc("POST /api/files/tidy", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string
			Moves   []folders.Move
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("send {project, moves} as JSON"))
			return
		}
		c, ok := s.tidyProject(w, r, in.Project)
		if !ok {
			return
		}
		plan, err := folders.PlanTidy(c.area, c.rules)
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("cannot read the project folder"))
			return
		}
		// client paths are never trusted: only a move the current plan also holds
		planned := map[string]string{}
		for _, m := range plan.Moves {
			planned[m.From] = m.To
		}
		for _, m := range in.Moves {
			if to, ok := planned[m.From]; !ok || to != m.To {
				writeErr(w, http.StatusBadRequest, errors.New("that move is not part of the tidy plan: "+m.From))
				return
			}
		}
		done, skipped, err := folders.ApplyTidy(c.area, in.Moves)
		if err != nil {
			writeJSON2(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "done": done, "skipped": skipped})
			return
		}
		writeJSON(w, map[string]any{"done": done, "skipped": skipped})
	})

	mux.HandleFunc("GET /api/files/tidy/undo", func(w http.ResponseWriter, r *http.Request) {
		c, ok := s.tidyProject(w, r, r.URL.Query().Get("project"))
		if !ok {
			return
		}
		u, err := folders.ReadUndo(c.area)
		if err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, u)
	})

	mux.HandleFunc("POST /api/files/tidy/undo", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string
			At      time.Time
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil || in.At.IsZero() {
			writeErr(w, http.StatusBadRequest, errors.New("send {project, at} as JSON, with at from the undo list"))
			return
		}
		c, ok := s.tidyProject(w, r, in.Project)
		if !ok {
			return
		}
		done, skipped, err := folders.UndoTidy(c.area, in.At)
		if err != nil && len(done) == 0 && len(skipped) == 0 {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, map[string]any{"done": done, "skipped": skipped})
	})
}
