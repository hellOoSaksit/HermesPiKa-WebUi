// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

// The Console pages beyond the snapshot: gates, privacy, scheduled jobs, AI
// accounts, structure, doctor and adding a project. Every value comes from a
// real file or from Hermes; nothing is invented. Writes go through the same
// packages as the CLI, so the CLI and the Console can never disagree.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"hpk.local/hpk/assets"
	"hpk.local/hpk/internal/cron"
	"hpk.local/hpk/internal/doctor"
	"hpk.local/hpk/internal/gates"
	"hpk.local/hpk/internal/hermes"
	"hpk.local/hpk/internal/isolate"
	"hpk.local/hpk/internal/notify"
	"hpk.local/hpk/internal/onboard"
	"hpk.local/hpk/internal/privacy"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/scan"
	"hpk.local/hpk/internal/team"
	"hpk.local/hpk/internal/wiki"
)

// Built-in red lines, in the order the Console lists them.
var builtinLines = []string{gates.Push, gates.Delete, gates.Secrets, gates.Control, gates.Model, gates.WikiCore, gates.Promote, gates.Egress}

func msp(t time.Time) *int64 {
	if t.IsZero() {
		return nil
	}
	v := t.UnixMilli()
	return &v
}

func (s *Server) pages(mux *http.ServeMux) {
	onboard.SetTemplates(assets.WikiTemplates())

	project1 := func(w http.ResponseWriter, r *http.Request) (*project.Project, bool) {
		p, err := s.loadFor(r, r.URL.Query().Get("project"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return nil, false
		}
		return p, true
	}

	// ---- gates: red lines, audit, Auto digest ----

	mux.HandleFunc("GET /api/gates", func(w http.ResponseWriter, r *http.Request) {
		p, ok := project1(w, r)
		if !ok {
			return
		}
		type line struct {
			ID     string `json:"id"`
			Key    string `json:"key"`
			Text   string `json:"text,omitempty"`
			Locked bool   `json:"locked"`
		}
		var lines []line
		for _, k := range builtinLines {
			lines = append(lines, line{ID: "r-" + k, Key: k, Locked: true})
		}
		var extra []string
		_ = project.Field(p, []string{"gates", "red_lines_extra"}, &extra)
		for i, x := range extra {
			lines = append(lines, line{ID: "x-" + strconv.Itoa(i), Key: "custom", Text: x})
		}
		now := time.Now()
		decisions, _ := gates.Decisions(p, now.Add(-30*24*time.Hour))
		audit := auditRows(p, decisions, s.who(r).ID)
		dig, _ := gates.Digest(p, now.Add(-24*time.Hour))
		type digestRow struct {
			ID         string `json:"id"`
			N          int    `json:"n"`
			At         *int64 `json:"at"`
			Session    string `json:"session"`
			What       string `json:"what"`
			Why        string `json:"why"`
			Revertable bool   `json:"revertable"`
		}
		digest := []digestRow{}
		for i, d := range dig {
			what := d.Tool
			if d.Detail != "" {
				what += ": " + d.Detail
			}
			digest = append(digest, digestRow{ID: "ad-" + strconv.Itoa(i+1), N: i + 1, At: msp(d.At), Session: d.Session, What: what, Why: "Auto mode"})
		}
		writeJSON(w, map[string]any{"redLines": lines, "audit": audit, "digest": digest})
	})

	mux.HandleFunc("POST /api/gates/redlines", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
			Add     string `json:"add"`
			Remove  string `json:"remove"` // x-<index>
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		var extra []string
		_ = project.Field(p, []string{"gates", "red_lines_extra"}, &extra)
		switch {
		case strings.TrimSpace(in.Add) != "":
			extra = append(extra, strings.TrimSpace(in.Add))
		case strings.HasPrefix(in.Remove, "x-"):
			i, err := strconv.Atoi(strings.TrimPrefix(in.Remove, "x-"))
			if err != nil || i < 0 || i >= len(extra) {
				writeErr(w, http.StatusBadRequest, errors.New("no such project red line"))
				return
			}
			extra = append(extra[:i], extra[i+1:]...)
		default:
			writeErr(w, http.StatusBadRequest, errors.New("add or remove is needed; built-in red lines cannot be removed"))
			return
		}
		var v any = extra
		if len(extra) == 0 {
			v = []string{}
		}
		if err := project.SetField(s.home, p.Name, []string{"gates", "red_lines_extra"}, v, "red lines changed by "+s.who(r).ID); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		s.hub.send("changed", p.Name)
		writeJSON(w, map[string]any{"ok": true})
	})

	// ---- privacy and promotions ----

	mux.HandleFunc("POST /api/privacy", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project     string `json:"project"`
			Mode        string `json:"mode"`
			Confirm     bool   `json:"confirm"` // a person agreed to open the project up
			ModelPolicy string `json:"modelPolicy"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		if in.Mode != "" {
			if _, err := project.SetPrivacy(s.home, p.Name, in.Mode, in.Confirm); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
		}
		if in.ModelPolicy == "any" || in.ModelPolicy == "local_only" {
			if err := project.SetField(s.home, p.Name, []string{"privacy", "model_policy"}, in.ModelPolicy, "model policy changed by "+s.who(r).ID); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		s.hub.send("changed", p.Name)
		writeJSON(w, map[string]any{"ok": true})
	})

	// Promotions: drafts this project sent to the team shared wiki, and to the
	// person's own (users/<id>). Owners decide team ones; each person decides their own.
	wikiFor := func(r *http.Request, which string) (string, *wiki.Wiki, error) {
		to := "team"
		if which == "mine" {
			to = privacy.UserTarget(s.who(r).ID)
		}
		dir, err := privacy.SharedWikiDir(s.home, to)
		if err != nil {
			return to, nil, err
		}
		return to, wiki.Open(dir), nil
	}
	mux.HandleFunc("GET /api/promotions", func(w http.ResponseWriter, r *http.Request) {
		p, ok := project1(w, r)
		if !ok {
			return
		}
		type promo struct {
			ID    string     `json:"id"`
			N     int        `json:"n"`
			Wiki  string     `json:"wiki"` // team | mine
			Page  string     `json:"page"`
			Title string     `json:"title"`
			Diff  [][]string `json:"diff"`
		}
		out := []promo{}
		origin := privacy.Origin(p)
		for _, which := range []string{"team", "mine"} {
			_, sw, err := wikiFor(r, which)
			if err != nil || !sw.Exists() {
				continue
			}
			drafts, _ := sw.Drafts()
			for _, d := range drafts {
				if d.Meta.Origin != origin {
					continue
				}
				var diff [][]string
				for _, l := range strings.Split(strings.TrimRight(d.Body, "\n"), "\n") {
					diff = append(diff, []string{"add", l})
				}
				out = append(out, promo{ID: which + "-" + strconv.Itoa(d.N), N: d.N, Wiki: which, Page: d.Meta.Target, Title: d.Meta.Title, Diff: diff})
			}
		}
		writeJSON(w, out)
	})

	mux.HandleFunc("POST /api/promotions", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project  string `json:"project"`
			N        int    `json:"n"`
			Wiki     string `json:"wiki"`     // team (default) | mine
			Decision string `json:"decision"` // approve | discard | edit
			Body     string `json:"body"`     // edit: the new text
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		// The team shared wiki is everyone's: an owner decides. A personal one is its person's.
		if in.Wiki != "mine" && !s.can(w, r, team.Admin) {
			return
		}
		to, sw, err := wikiFor(r, in.Wiki)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		// Only drafts that came from this project can be decided from its page.
		found := false
		if ds, e := sw.Drafts(); e == nil {
			for _, d := range ds {
				found = found || d.N == in.N && d.Meta.Origin == privacy.Origin(p)
			}
		}
		if !found {
			writeErr(w, http.StatusNotFound, fmt.Errorf("no promotion %d from %s", in.N, p.Name))
			return
		}
		switch in.Decision {
		case "approve":
			_, err = sw.Approve([]int{in.N}, s.who(r).ID)
		case "edit":
			err = sw.EditDraftBody(in.N, in.Body, s.who(r).ID)
		default:
			_, err = sw.Discard([]int{in.N}, s.who(r).ID, "kept local")
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		_ = project.Audit(p.Dir, "promotion "+in.Decision, fmt.Sprintf("%s draft %d by %s", to, in.N, s.who(r).ID))
		s.hub.send("changed", p.Name)
		writeJSON(w, map[string]any{"ok": true})
	})

	// Send an approved page to a shared wiki as a redacted draft (a person approves it there).
	mux.HandleFunc("POST /api/wiki/promote", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
			Page    string `json:"page"`
			To      string `json:"to"` // team | mine
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		to := "team"
		if in.To == "mine" {
			to = privacy.UserTarget(s.who(r).ID)
		}
		d, err := privacy.Promote(s.home, p, in.Page, to, assets.WikiTemplates())
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		_ = project.Audit(p.Dir, "page promoted", in.Page+" to "+to+" by "+s.who(r).ID)
		s.hub.send("changed", p.Name)
		writeJSON(w, map[string]any{"n": d.N, "to": to})
	})

	// ---- scheduled jobs ----

	mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		p, ok := project1(w, r)
		if !ok {
			return
		}
		_, _ = cron.Account(s.home, p) // pick up new runs from Hermes' usage log
		st, err := cron.Load(p)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		hist := cron.History(p)
		type job struct {
			ID         string `json:"id"`
			Key        string `json:"key"`
			Schedule   string `json:"schedule"`
			Installed  bool   `json:"installed"`
			Last       *int64 `json:"last"`
			LastStatus string `json:"lastStatus"`
			Model      string `json:"model"`
			Tokens     int    `json:"tokens"`
			Budget     int    `json:"budget"`
			Paused     bool   `json:"paused"`
			Note       string `json:"note,omitempty"`
		}
		type run struct {
			Job    string `json:"job"`
			At     *int64 `json:"at"`
			Status string `json:"status"`
			Tokens int    `json:"tokens"`
			Out    string `json:"out"`
		}
		jobs := []job{}
		for _, j := range cron.Jobs(p) {
			js := st.Jobs[j.Name]
			if js == nil {
				js = &cron.JobState{}
			}
			row := job{ID: j.Name, Key: j.Name, Schedule: j.Schedule, Installed: js.HermesID != "", Budget: j.Budget, Paused: js.Paused, Note: js.PauseReason, LastStatus: "none"}
			for i := len(hist) - 1; i >= 0; i-- {
				if hist[i].Job == j.Name {
					row.Last, row.Tokens, row.Model = msp(hist[i].At), hist[i].Tokens, hist[i].Model
					row.LastStatus = "ok"
					if hist[i].Error != "" {
						row.LastStatus = "fail"
					}
					break
				}
			}
			jobs = append(jobs, row)
		}
		runs := []run{}
		for i := len(hist) - 1; i >= 0 && len(runs) < 30; i-- {
			u := hist[i]
			status := "ok"
			if u.Error != "" {
				status = "fail"
			}
			runs = append(runs, run{Job: u.Job, At: msp(u.At), Status: status, Tokens: u.Tokens, Out: u.Error})
		}
		writeJSON(w, map[string]any{"jobs": jobs, "runs": runs, "installCmd": "hpk cron install --project " + p.Name})
	})

	// Run now: Hermes runs the job on its next tick. The monitor script still decides
	// whether there is work, and the job budget still applies (hook model_usage).
	mux.HandleFunc("POST /api/jobs/{name}/run", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		name := r.PathValue("name")
		st, err := cron.Load(p)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		js := st.Jobs[name]
		switch {
		case js == nil || js.HermesID == "":
			writeErr(w, http.StatusBadRequest, fmt.Errorf("%s is not scheduled in Hermes yet", name))
			return
		case js.Paused:
			writeErr(w, http.StatusConflict, fmt.Errorf("%s is paused: %s", name, js.PauseReason))
			return
		}
		if over, why := privacy.OverMoney(s.home, p, time.Now()); over {
			writeErr(w, http.StatusConflict, errors.New(why))
			return
		}
		id := js.HermesID
		_ = project.Audit(p.Dir, "job run requested", name+" by "+s.who(r).ID)
		go func() {
			cmd := isolate.Command("hermes", "cron", "run", id)
			if out, err := cmd.CombinedOutput(); err != nil {
				_ = project.Audit(p.Dir, "job run failed", name+": "+strings.TrimSpace(string(out)))
			}
			s.hub.send("changed", p.Name)
		}()
		writeJSON(w, map[string]any{"started": true})
	})
	mux.HandleFunc("POST /api/jobs/{name}/pause", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
			Paused  bool   `json:"paused"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		name := r.PathValue("name")
		if _, err := cron.JobByName(p, name); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		var err error
		if in.Paused {
			var st *cron.State
			if st, err = cron.Load(p); err == nil {
				if st.Jobs[name] == nil {
					st.Jobs[name] = &cron.JobState{}
				}
				st.Jobs[name].Paused, st.Jobs[name].PauseReason = true, "paused by "+s.who(r).ID
				err = cron.Save(p, st)
			}
		} else {
			err = cron.Allow(p, name)
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		_ = project.Audit(p.Dir, "job "+name, fmt.Sprintf("paused=%v by %s", in.Paused, s.who(r).ID))
		s.hub.send("changed", p.Name)
		writeJSON(w, map[string]any{"ok": true})
	})

	// ---- Hermes: accounts, profiles, doctor ----

	mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		list, err := hermes.Credentials(r.Context())
		out := map[string]any{"credentials": list}
		if err != nil {
			out["error"] = "hermes auth list: " + err.Error()
		}
		if list == nil {
			out["credentials"] = []hermes.Credential{}
		}
		out["canAddKeys"] = hermes.CanAddKeys
		out["canManage"] = s.who(r).Role == team.Owner
		writeJSON(w, out)
	})

	// Manage Hermes' credential pool (owners). hpk runs `hermes auth`; a new key
	// goes to Hermes on stdin and is never stored, logged or echoed by hpk.
	mux.HandleFunc("POST /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Action   string `json:"action"` // add | remove | priority | reset
			Provider string `json:"provider"`
			Target   string `json:"target"` // credential id, number or label
			Label    string `json:"label"`
			Key      string `json:"key"`
			Priority *int   `json:"priority"`
		}
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		if !s.can(w, r, team.Admin) {
			return
		}
		ctx := r.Context()
		var err error
		switch in.Action {
		case "add":
			// A key may cross the network only encrypted: HTTPS (here or at the proxy),
			// or a browser on this machine.
			if !s.viaHTTPS(r) && !isLoopback(r.Host) && !isLoopback(net.JoinHostPort(hostOnly(r.Host), "0")) {
				writeErr(w, http.StatusForbidden, errors.New("send API keys over HTTPS only; this Console is on plain HTTP"))
				return
			}
			p := -1
			if in.Priority != nil {
				p = *in.Priority
			}
			err = hermes.AddAPIKey(ctx, in.Provider, in.Label, in.Key, p)
		case "remove":
			err = hermes.RemoveCredential(ctx, in.Provider, in.Target)
		case "priority":
			if in.Priority == nil {
				err = errors.New("give a priority")
			} else {
				err = hermes.SetPriority(ctx, in.Provider, in.Target, *in.Priority)
			}
		case "reset":
			err = hermes.ResetCredential(ctx, in.Provider, in.Target)
		default:
			err = fmt.Errorf("unknown action %q", in.Action)
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		log.Printf("accounts: %s %s %s by %s", in.Action, in.Provider, orStr(in.Label, in.Target), s.who(r).ID)
		s.hub.send("changed", "accounts")
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /api/structure", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"profiles": hermes.Profiles(s.home)})
	})

	mux.HandleFunc("GET /api/doctor", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		writeJSON(w, doctor.Run(ctx, s.home, ""))
	})

	// ---- project settings the Console can change (project.yaml) ----

	mux.HandleFunc("GET /api/notify/targets", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		list, err := notify.Targets(ctx)
		if err != nil {
			writeJSON(w, map[string]any{"targets": []string{}, "error": "hermes send --list failed: " + err.Error()})
			return
		}
		writeJSON(w, map[string]any{"targets": list})
	})
	mux.HandleFunc("POST /api/notify/test", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		if notify.Target(p) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("set where to send first"))
			return
		}
		if _, err := notify.Send(p, notify.Test, "Test from HPK Console. Notices for this project arrive here.", time.Now()); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/project/settings", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Project string `json:"project"`
			Key     string `json:"key"`
			Value   any    `json:"value"` // nil resets to the default
		}
		p, ok := s.decode(w, r, &in, func() string { return in.Project })
		if !ok {
			return
		}
		path, check := settingPath(in.Key)
		if in.Key == "notifyTo" {
			to, _ := in.Value.(string)
			if err := notify.CheckTarget(p, strings.TrimSpace(to)); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			if strings.TrimSpace(to) == "" {
				in.Value = nil
			}
		}
		if path == nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("%s cannot be changed from the Console; edit project.yaml", in.Key))
			return
		}
		if in.Value != nil {
			if err := check(in.Value); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
		}
		if err := project.SetField(s.home, p.Name, path, in.Value, "setting changed by "+s.who(r).ID); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		s.hub.send("changed", p.Name)
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /api/project/settings", func(w http.ResponseWriter, r *http.Request) {
		p, ok := project1(w, r)
		if !ok {
			return
		}
		out := map[string]any{}
		for _, k := range []string{"mode", "board", "language", "ingestMax", "nightlyMax", "dailyMax", "moneyDaily", "moneyMonthly", "notifyTo", "test"} {
			path, _ := settingPath(k)
			var v any
			_ = project.Field(p, path, &v)
			if v != nil {
				out[k] = v
			}
		}
		now := time.Now()
		out["root"], out["wikiDir"] = p.Root, p.WikiDir()
		out["spentToday"] = privacy.Spent(s.home, p, time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()))
		out["spentMonth"] = privacy.Spent(s.home, p, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()))
		writeJSON(w, out)
	})

	// ---- add a project (docs/DESIGN.md §8): scan is 0 tokens, add needs consent ----

	mux.HandleFunc("POST /api/projects/scan", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path  string `json:"path"`
			Depth string `json:"depth"`
		}
		if !decodeOnly(w, r, &in) {
			return
		}
		res, err := scanDir(in.Path)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, scanView(res, in.Depth))
	})

	mux.HandleFunc("POST /api/projects", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path        string   `json:"path"`
			Name        string   `json:"name"`
			Depth       string   `json:"depth"`
			Privacy     string   `json:"privacy"`
			ModelPolicy string   `json:"modelPolicy"`
			Exclude     []string `json:"exclude"`
			Language    string   `json:"language"`
			Consent     bool     `json:"consent"`
		}
		if !decodeOnly(w, r, &in) {
			return
		}
		if !in.Consent {
			writeErr(w, http.StatusBadRequest, errors.New("adding a project needs a person's consent"))
			return
		}
		res, err := scanDir(in.Path)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		p, est, err := onboard.Add(s.home, onboard.Options{Root: res.Root, Name: in.Name, Depth: in.Depth, Privacy: in.Privacy,
			Language: in.Language, Exclude: in.Exclude, Price: onboard.DefaultPrice}, res)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if in.ModelPolicy == "local_only" {
			_ = project.SetField(s.home, p.Name, []string{"privacy", "model_policy"}, "local_only", "project added")
		}
		s.hub.send("changed", p.Name)
		writeJSON(w, map[string]any{"name": p.Name, "estimate": est, "next": "hpk project ingest " + p.Name + " --next"})
	})
}

func decodeOnly(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad JSON: %w", err))
		return false
	}
	return true
}

// settingPath maps a Console setting to its project.yaml key and a check.
func settingPath(key string) ([]string, func(any) error) {
	oneOf := func(vals ...string) func(any) error {
		return func(v any) error {
			s, _ := v.(string)
			for _, x := range vals {
				if s == x {
					return nil
				}
			}
			return fmt.Errorf("%v: use one of %s", v, strings.Join(vals, ", "))
		}
	}
	positive := func(v any) error {
		f, ok := v.(float64)
		if !ok || f < 0 || f != float64(int(f)) {
			return fmt.Errorf("%v: use a whole number", v)
		}
		return nil
	}
	usd := func(v any) error {
		if f, ok := v.(float64); !ok || f < 0 || f > 100000 {
			return fmt.Errorf("%v: use an amount in USD (0 = no cap)", v)
		}
		return nil
	}
	switch key {
	case "notifyTo":
		return []string{"notify", "to"}, func(v any) error { return nil } // checked with the project above
	case "moneyDaily":
		return []string{"budget", "money_daily_usd"}, usd
	case "moneyMonthly":
		return []string{"budget", "money_monthly_usd"}, usd
	case "mode":
		return []string{"gates", "default_mode"}, oneOf("manual", "ask", "auto")
	case "board":
		return []string{"board", "kind"}, oneOf("hpk", "markdown")
	case "language":
		return []string{"language"}, oneOf("en", "th")
	case "ingestMax":
		return []string{"budget", "ingest_max_tokens"}, positive
	case "nightlyMax":
		return []string{"budget", "nightly_max_tokens"}, positive
	case "dailyMax":
		return []string{"budget", "cron_daily_max_tokens"}, positive
	case "test":
		return []string{"checks", "test"}, func(v any) error {
			if _, ok := v.(string); !ok {
				return errors.New("test must be a command")
			}
			return nil
		}
	}
	return nil, nil
}

func scanDir(p string) (scan.Result, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return scan.Result{}, errors.New("give the project's folder")
	}
	if strings.HasPrefix(p, "~") {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[1:])
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return scan.Result{}, err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return scan.Result{}, fmt.Errorf("%s is not a folder on this machine", p)
	}
	return scan.Run(abs)
}

// scanView shapes a scan for the wizard: facts, sources with the depth's pick,
// what was skipped (secrets are never opened) and the 0-token health checks.
func scanView(res scan.Result, depth string) map[string]any {
	if depth == "" {
		depth = onboard.Full
	}
	picked, _ := onboard.Pick(res, depth, nil)
	in := map[string]bool{}
	for _, s := range picked {
		in[s.Path] = true
	}
	type src struct {
		Path   string `json:"path"`
		Kind   string `json:"kind"`
		Tokens int    `json:"tokens"`
		Pick   bool   `json:"pick"`
		Light  bool   `json:"light"`
	}
	sources := []src{}
	for _, s := range res.Sources {
		if s.Kind == "control" {
			continue // rules, not knowledge: never ingested
		}
		sources = append(sources, src{Path: s.Path, Kind: s.Kind, Tokens: s.Tokens, Pick: in[s.Path], Light: s.Light})
	}
	total := 0
	for _, n := range res.Languages {
		total += n
	}
	type lang struct {
		Name  string
		Files int
	}
	var ls []lang
	for k, n := range res.Languages {
		ls = append(ls, lang{k, n})
	}
	sort.Slice(ls, func(i, j int) bool {
		return ls[i].Files > ls[j].Files || (ls[i].Files == ls[j].Files && ls[i].Name < ls[j].Name)
	})
	langs := [][]any{}
	other := 0
	for i, l := range ls {
		pct := 0
		if total > 0 {
			pct = l.Files * 100 / total
		}
		if i < 5 && pct > 0 {
			langs = append(langs, []any{l.Name, pct})
		} else {
			other += pct
		}
	}
	if other > 0 {
		langs = append(langs, []any{"Other", other})
	}
	skipped := []string{}
	for _, s := range res.Skipped {
		skipped = append(skipped, s.Path)
	}
	var last *int64
	if t, err := time.Parse(time.RFC3339, res.Git.LastCommit); err == nil {
		last = msp(t)
	}
	name := strings.ToLower(filepath.Base(res.Root))
	return map[string]any{
		"path": res.Root, "name": name, "files": res.Files, "languages": langs,
		"commits": res.Git.Commits, "contributors": res.Git.Contributors, "lastCommit": last, "isRepo": res.Git.IsRepo,
		"sources": sources, "skipped": skipped, "health": onboard.Health(res),
		"testCmd": res.TestCmd, "boardFile": res.BoardFile,
		"budget": onboard.DefaultBudget, "pricePerMTok": onboard.DefaultPrice,
	}
}

// auditRows merges gate decisions and project audit.log into the Console's rows, newest first.
func auditRows(p *project.Project, ds []gates.Decision, me string) []map[string]any {
	type row struct {
		at  time.Time
		m   map[string]any
		seq int // later lines of the same second are newer
	}
	var rows []row
	for _, d := range ds {
		actor, action, result := "agent", "write", "ok"
		target := d.Tool
		if d.Detail != "" {
			target = strings.TrimSpace(d.Tool + " " + d.Detail)
		}
		switch d.Kind {
		case "ask":
			action, result = "request", "asked human"
			if len(d.Lines) > 0 {
				target += " (" + strings.Join(d.Lines, ", ") + ")"
			}
		case "auto-allow":
			actor, action = "auto", "approve"
		case "human":
			actor = orDefault(d.By, me)
			action = "approve"
			if d.Outcome == "deny" || d.Outcome == "denied" {
				action, result = "deny", "denied"
			}
		case "mode":
			actor = orDefault(d.By, me)
			target = "mode → " + d.Mode
		}
		rows = append(rows, row{at: d.At, seq: len(rows), m: map[string]any{"at": msp(d.At), "actor": actor, "action": action, "target": target, "session": d.Session, "result": result}})
	}
	if f, err := os.Open(filepath.Join(p.Dir, "audit.log")); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			parts := strings.SplitN(sc.Text(), "\t", 3)
			if len(parts) < 2 || strings.HasPrefix(parts[1], "[") {
				continue // "[who] kind" lines mirror decisions.jsonl, already listed above
			}
			at, err := time.Parse(time.RFC3339, parts[0])
			if err != nil || time.Since(at) > 30*24*time.Hour {
				continue
			}
			target := parts[1]
			if len(parts) == 3 && parts[2] != "" {
				target += ": " + parts[2]
			}
			rows = append(rows, row{at: at, seq: len(rows), m: map[string]any{"at": msp(at), "actor": me, "action": "write", "target": target, "result": "ok"}})
		}
		f.Close()
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].at.Equal(rows[j].at) {
			return rows[i].at.After(rows[j].at)
		}
		return rows[i].seq > rows[j].seq
	})
	out := []map[string]any{}
	for i, r := range rows {
		if i >= 300 {
			break
		}
		out = append(out, r.m)
	}
	return out
}

func orDefault(s, d string) string {
	if s == "" || s == "hpk" {
		return d
	}
	return s
}
