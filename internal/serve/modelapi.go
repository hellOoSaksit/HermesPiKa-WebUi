// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"hpk.local/hpk/internal/acp"
	"hpk.local/hpk/internal/modelswitch"
	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
)

// Model switching in a chat (docs/model-switch.md): the picker's plan, the switch
// itself, and "ask once" on another model.

// modelView is what the picker shows for one chat.
type modelView struct {
	Live     bool                 `json:"live"`
	Busy     bool                 `json:"busy"`
	Current  string               `json:"current"`
	Policy   string               `json:"policy"`
	State    modelswitch.State    `json:"state"`
	Options  []modelswitch.Option `json:"options"`
	Switches []session.Switch     `json:"switches"`
}

// liveState is a copy of what the manager knows about a live session's model.
type liveState struct {
	hermesID  string
	model     string
	models    acp.Models
	streaming bool
	history   int
	tail      int
}

func (m *ACPManager) modelState(projectName, hpkID string) (liveState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ls := m.sessions[hpkID]
	if ls == nil || ls.project != projectName {
		return liveState{}, false
	}
	st := liveState{hermesID: ls.hermesID, model: ls.model, models: m.models[ls.project], streaming: ls.streaming}
	// Hermes' own estimate (system prompt + history + tools) when it sent one; else count the log.
	n := 0
	for i := len(ls.log) - 1; i >= 0; i-- {
		x := ls.log[i]
		t := modelswitch.Tokens(x.Text + x.Out + x.Command)
		st.history += t
		if x.T == "msg" || x.T == "tool" {
			if n < modelswitch.TailMessages {
				st.tail += t
			}
			n++
		}
	}
	if ls.ctxUsed > st.history {
		st.history = ls.ctxUsed
	}
	return st, true
}

// SwitchModel moves a live chat to another model. A compacting switch runs
// Hermes' /compress on the old model first (its cache is warm there). The chat
// is busy until both finish; messages sent meanwhile wait in Hermes' queue.
func (m *ACPManager) SwitchModel(p *project.Project, hpkID string, o modelswitch.Option, carry, by string) error {
	m.mu.Lock()
	ls := m.sessions[hpkID]
	c := m.clients[p.Name]
	// The manager finds chats by id alone: never act on one that belongs to another project.
	if ls == nil || c == nil || ls.project != p.Name {
		m.mu.Unlock()
		return errors.New("this chat is not open in hpk serve; open it to switch models")
	}
	if ls.streaming {
		m.mu.Unlock()
		return errors.New("the agent is answering; switch when the turn ends")
	}
	ls.streaming = true
	from := ls.model
	m.mu.Unlock()
	m.notify("chat", hpkID)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		var err error
		if o.Way == modelswitch.Compact {
			_, err = c.Prompt(ctx, ls.hermesID, "/compress")
		}
		if err == nil {
			err = c.SetModel(ctx, ls.hermesID, o.Model.ID)
		}
		now := time.Now()
		m.mu.Lock()
		ls.streaming = false
		ls.endStream()
		if err != nil {
			ls.log = append(ls.log, &Msg{T: "note", Kind: "modelError", At: now.UnixMilli(), Text: "The model did not change: " + err.Error()})
		} else {
			ls.model, ls.carry, ls.ctxUsed, ls.ctxSize = o.Model.ID, carry, 0, 0
			ls.log = append(ls.log, &Msg{T: "switch", At: now.UnixMilli(), From: from, To: o.Model.ID, Way: o.Way, Fill: o.Fill})
		}
		m.mu.Unlock()
		if err == nil {
			_, _ = session.RecordSwitch(p, hpkID, session.Switch{At: now.UTC(), From: from, To: o.Model.ID, Way: o.Way, Fill: o.Fill, By: by})
			what := fmt.Sprintf("%s → %s (%s, history %.0f%% of the new window) by %s", from, o.Model.ID, o.Way, o.Fill*100, by)
			if o.CrossProvider {
				what += "; red line model approved"
			}
			_ = project.Audit(p.Dir, "model switched", what)
		}
		m.notify("chat", hpkID)
		m.notify("sessions", p.Name)
	}()
	return nil
}

// modelOf describes a model Hermes names provider:model, with what models.dev knows.
func (s *Server) modelOf(p *project.Project, id, name string, cur privacy.Route) modelswitch.Model {
	prov, mod := modelswitch.ParseID(id, cur.Provider)
	x := modelswitch.Model{ID: id, Provider: prov, Name: orStr(name, mod)}
	if w, ok := privacy.WindowOf(s.home, prov, mod); ok {
		x.Window, x.Known = w, true
	}
	if pr, ok := privacy.PriceOf(s.home, prov, mod); ok {
		x.Input, x.Write, x.Priced = pr.Input, pr.CacheWrite, true
	}
	// Hermes keeps the endpoint when the provider stays (keep_endpoint).
	r := privacy.Route{Provider: prov}
	if strings.EqualFold(prov, cur.Provider) {
		r.BaseURL = cur.BaseURL
	}
	x.Local = privacy.IsLocal(r, p.Privacy.LocalHosts)
	return x
}

// readRecall reads recalled wiki pages the Console names; bad or missing paths are skipped.
func readRecall(p *project.Project, paths []string) []modelswitch.Page {
	var out []modelswitch.Page
	seen := map[string]bool{}
	for _, rel := range paths {
		rel = path.Clean(strings.TrimPrefix(strings.TrimSpace(rel), "/"))
		if seen[rel] || rel == "." || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, "_drafts") || !strings.HasSuffix(rel, ".md") {
			continue
		}
		seen[rel] = true
		raw, err := os.ReadFile(filepath.Join(p.WikiDir(), filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		out = append(out, modelswitch.Page{Path: rel, Body: string(raw)})
		if len(out) >= 12 {
			break
		}
	}
	return out
}

// plan builds the picker for a chat. It works on chats this serve holds open.
func (s *Server) plan(p *project.Project, sess *session.Session, recall []modelswitch.Page) (modelView, liveState, error) {
	v := modelView{Options: []modelswitch.Option{}, Switches: sess.Switches}
	if v.Switches == nil {
		v.Switches = []session.Switch{}
	}
	if p.Privacy.ModelPolicy == privacy.LocalOnly {
		v.Policy = privacy.LocalOnly
	}
	st, ok := s.acp.modelState(p.Name, sess.ID)
	if !ok {
		return v, st, nil
	}
	v.Live, v.Busy, v.Current = true, st.streaming, st.model
	var cur privacy.Route
	if seen, _ := privacy.LoadSeen(s.home, st.hermesID); seen != nil {
		cur = seen.Last
	}
	if v.Current == "" && cur.Model != "" {
		v.Current = cur.Provider + ":" + cur.Model
	}
	if cp, _ := modelswitch.ParseID(v.Current, ""); cp != "" {
		cur.Provider = cp
	}
	brief := ""
	if raw, err := os.ReadFile(sess.Brief); err == nil {
		brief = string(raw)
	}
	v.State = modelswitch.State{History: st.history, Tail: st.tail, Brief: modelswitch.Tokens(brief)}
	for _, pg := range recall {
		v.State.Recall += modelswitch.Tokens(pg.Body)
	}
	if v.State.Recall > modelswitch.RecallBudget {
		v.State.Recall = modelswitch.RecallBudget
	}
	curM := s.modelOf(p, v.Current, "", cur)
	list := st.models.Available
	if len(list) == 0 && v.Current != "" {
		list = []acp.ModelInfo{{ModelID: v.Current}}
	}
	pol := modelswitch.Policy{LocalOnly: v.Policy == privacy.LocalOnly}
	for _, mi := range list {
		v.Options = append(v.Options, modelswitch.Plan(curM, v.State, s.modelOf(p, mi.ModelID, mi.Name, cur), pol))
	}
	return v, st, nil
}

func (s *Server) modelAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{id}/models", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.loadFor(r, r.URL.Query().Get("project"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		sess, ok := s.chatAccess(w, r, p, r.PathValue("id"))
		if !ok {
			return
		}
		var paths []string
		if q := r.URL.Query().Get("recall"); q != "" {
			paths = strings.Split(q, ",")
		}
		v, _, err := s.plan(p, sess, readRecall(p, paths))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, v)
	})

	// Switch the chat's model. Another provider needs confirm (the model red line);
	// a local_only project never leaves this machine.
	mux.HandleFunc("POST /api/sessions/{id}/model", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string   `json:"project"`
			Model   string   `json:"model"`
			Confirm bool     `json:"confirm"`
			Recall  []string `json:"recall"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		sess, ok := s.chatAccess(w, r, p, r.PathValue("id"))
		if !ok {
			return
		}
		recall := readRecall(p, in.Recall)
		v, _, _ := s.plan(p, sess, recall)
		if !v.Live {
			writeErr(w, http.StatusConflict, errors.New("this chat is not open in hpk serve; open it to switch models"))
			return
		}
		var o *modelswitch.Option
		for i := range v.Options {
			if v.Options[i].Model.ID == in.Model {
				o = &v.Options[i]
			}
		}
		if o == nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("hermes does not offer %q for this chat", in.Model))
			return
		}
		if err := modelswitch.Allowed(*o, in.Confirm); err != nil {
			code := http.StatusConflict
			if o.Locked == "local_only" {
				code = http.StatusForbidden
			}
			writeErr(w, code, err)
			return
		}
		if o.Way == modelswitch.Compact && !s.personBudgetOK(w, r) {
			return
		}
		brief := ""
		if raw, err := os.ReadFile(sess.Brief); err == nil {
			brief = string(raw)
		}
		carry, _ := modelswitch.CarryText(v.Current, o.Model.ID, o.Way, brief, recall)
		if err := s.acp.SwitchModel(p, sess.ID, *o, carry, s.who(r).ID); err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "way": o.Way, "carry": o.Carry, "firstCost": o.FirstCost})
	})

	// Ask another model once: a sub-task on that model gets the question and the
	// main chat's brief, answers, and reports back to the main chat's inbox. The
	// main chat keeps its model and its cache.
	mux.HandleFunc("POST /api/sessions/{id}/ask", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
			Model   string `json:"model"`
			Text    string `json:"text"`
			Confirm bool   `json:"confirm"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		if strings.TrimSpace(in.Text) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("ask what? the question is empty"))
			return
		}
		parent, ok := s.chatAccess(w, r, p, r.PathValue("id"))
		if !ok {
			return
		}
		v, _, _ := s.plan(p, parent, nil)
		var o *modelswitch.Option
		for i := range v.Options {
			if v.Options[i].Model.ID == in.Model {
				o = &v.Options[i]
			}
		}
		if o == nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("hermes does not offer %q for this chat (open the chat first)", in.Model))
			return
		}
		if o.Locked == "local_only" || (o.CrossProvider && !in.Confirm) {
			o.Current = false
			if err := modelswitch.Allowed(*o, in.Confirm); err != nil {
				writeErr(w, http.StatusConflict, err)
				return
			}
		}
		if !s.personBudgetOK(w, r) {
			return
		}
		_, short := modelswitch.ParseID(in.Model, "")
		task := strings.TrimSpace(in.Text) + "\n\nThis is a one-question consult from the main chat on " + in.Model +
			". Answer it, change no files, then send your report (Done: your answer in short) and stop."
		sub, err := session.New(p, session.NewOptions{Parent: parent.ID, Name: "ask " + short, Task: task, Owner: s.who(r).ID})
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		_, _ = session.SetModel(p, sub.ID, in.Model, true)
		if err := s.acp.startOn(p, sub.ID, "", in.Text, s.who(r).ID, in.Model); err != nil {
			writeErr(w, http.StatusBadGateway, fmt.Errorf("sub-task %s recorded, but Hermes did not start it: %w", sub.Num, err))
			return
		}
		if o.CrossProvider {
			_ = project.Audit(p.Dir, "model red line approved", "ask once on "+in.Model+" from "+parent.Num+" by "+s.who(r).ID)
		}
		if x, err := session.Get(p, sub.ID); err == nil {
			sub = x
		}
		s.hub.send("sessions", p.Name)
		writeJSON(w, sub)
	})
}
