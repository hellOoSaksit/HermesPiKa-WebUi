// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hpk.local/hpk/internal/hermes"
	"hpk.local/hpk/internal/mcp"
	"hpk.local/hpk/internal/procs"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/team"
)

// Background jobs: what is running for the agent right now, and a way to stop it.
//   - "process": a command Hermes started in the background (terminal tool),
//     read from Hermes' own processes.json; only processes on this machine can be stopped;
//   - "mcp": a local MCP program (Blender bridge, Roblox Studio MCP, …) found
//     among this machine's processes by its configured command.
//
// The Hermes gateway itself is never listed: stopping it is `hermes gateway`'s job.
type bgJob struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // process | mcp
	Title     string `json:"title"`
	Cmd       string `json:"cmd"`
	Cwd       string `json:"cwd,omitempty"`
	Project   string `json:"project,omitempty"`
	PID       int    `json:"pid,omitempty"`
	Start     string `json:"start,omitempty"`
	StartedAt int64  `json:"startedAt,omitempty"` // ms
	Session   string `json:"session,omitempty"`
	MCP       string `json:"mcp,omitempty"`
	Running   bool   `json:"running"`
	CanKill   bool   `json:"canKill"`
	Where     string `json:"where,omitempty"` // sandbox: runs in Hermes' sandbox, not here
	Children  int    `json:"children,omitempty"`
}

func (s *Server) bgJobs(ctx context.Context) ([]bgJob, error) {
	dir := mcp.Dir(s.home, hermes.ActiveProfile(s.home))
	list, perr := procs.List(ctx)
	byPID := map[int]procs.Proc{}
	for _, p := range list {
		byPID[p.PID] = p
	}
	projects, _ := project.List(s.home)
	projectOf := func(cwd string) string {
		best := ""
		for _, p := range projects {
			if p.Root != "" && (cwd == p.Root || strings.HasPrefix(cwd, p.Root+string(filepath.Separator))) && len(p.Root) > len(best) {
				best = p.Name
			}
		}
		return best
	}
	jobs := []bgJob{}

	// Hermes' background processes
	var entries []struct {
		ID        string  `json:"session_id"`
		Command   string  `json:"command"`
		PID       int     `json:"pid"`
		Scope     string  `json:"pid_scope"`
		Cwd       string  `json:"cwd"`
		StartedAt float64 `json:"started_at"`
		Key       string  `json:"session_key"`
		Task      string  `json:"task_id"`
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "processes.json")); err == nil {
		_ = json.Unmarshal(raw, &entries)
	}
	for _, e := range entries {
		j := bgJob{ID: "proc:" + e.ID, Kind: "process", Title: firstWords(e.Command, 6), Cmd: e.Command, Cwd: e.Cwd, PID: e.PID,
			StartedAt: int64(e.StartedAt * 1000), Session: orStr(e.Key, e.Task), Project: projectOf(e.Cwd)}
		if e.Scope != "" && e.Scope != "host" {
			j.Where = e.Scope
		} else if p, ok := byPID[e.PID]; ok && e.PID > 0 && sameProcess(p, e.StartedAt) && procs.UnderHermes(list, e.PID) {
			// the id alone is not enough: an ended job's id may now be another
			// program's. It must have started when Hermes says and be Hermes' child.
			j.Running, j.Start, j.CanKill = true, p.Start, true
			j.ID = jobID(j.ID, p)
			j.Children = len(procs.Descendants(list, e.PID))
		}
		jobs = append(jobs, j)
	}

	// local MCP programs
	servers, _ := mcp.List(dir, false)
	self := os.Getpid()
	for _, sv := range servers {
		if sv.Kind != "local" {
			continue
		}
		for _, p := range list {
			if p.PID == self || !mcpMatches(sv, p.Cmd) || !procs.UnderHermes(list, p.PID) {
				continue
			}
			// a child of another match is part of that one (npx → node, uvx → python)
			if parent, ok := byPID[p.PPID]; ok && mcpMatches(sv, parent.Cmd) {
				continue
			}
			jobs = append(jobs, bgJob{ID: jobID(fmt.Sprintf("mcp:%s:%d", sv.Name, p.PID), p), Kind: "mcp", Title: sv.Name, Cmd: p.Cmd, PID: p.PID, Start: p.Start,
				MCP: sv.Name, Running: true, CanKill: true, Children: len(procs.Descendants(list, p.PID))})
		}
	}
	if perr != nil {
		return jobs, fmt.Errorf("could not list processes: %w", perr)
	}
	return jobs, nil
}

// mcpMatches: the command line runs this MCP (not a `hermes mcp …` probe).
func mcpMatches(sv mcp.Server, cmd string) bool {
	if strings.Contains(cmd, "hermes mcp ") || strings.Contains(cmd, "hpk") && strings.Contains(cmd, " serve") {
		return false
	}
	// the program's own name and every configured argument, never the name alone
	// (a bare "node" or "python" would match every such process of the person)
	if len(sv.Args) == 0 {
		return false
	}
	base := strings.TrimSuffix(filepath.Base(strings.ReplaceAll(sv.Command, "\\", "/")), ".exe")
	want := append([]string{}, sv.Args...)
	if base != "npx" && base != "uvx" && base != "pnpx" && base != "bunx" {
		want = append(want, base) // launchers replace themselves with the package's program
	}
	for _, w := range want {
		if !strings.Contains(cmd, w) {
			return false
		}
	}
	return true
}

// sameProcess: the OS says it started within 2 s of when Hermes recorded it.
func sameProcess(p procs.Proc, startedAt float64) bool {
	if p.At.IsZero() || startedAt <= 0 {
		return false
	}
	d := p.At.Sub(time.Unix(0, int64(startedAt*float64(time.Second))))
	return d < 2*time.Second && d > -2*time.Second
}

// jobID carries the start time, so a kill names exactly the process the list showed.
func jobID(base string, p procs.Proc) string {
	h := fnv.New32a()
	h.Write([]byte(p.Start))
	return fmt.Sprintf("%s@%08x", base, h.Sum32())
}

func firstWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) > n {
		return strings.Join(f[:n], " ") + " …"
	}
	return strings.Join(f, " ")
}

func (s *Server) bgAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/bg", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		jobs, err := s.bgJobs(ctx)
		jobs = redactJobs(s.who(r), jobs)
		out := map[string]any{"jobs": jobs, "canKill": s.who(r).Role == team.Owner}
		if err != nil {
			out["error"] = err.Error()
		}
		writeJSON(w, out)
	})

	mux.HandleFunc("POST /api/bg/kill", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID string `json:"id"`
		}
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		if !s.can(w, r, team.Admin) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		jobs, _ := s.bgJobs(ctx)
		var hit *bgJob
		for i := range jobs {
			if jobs[i].ID == in.ID {
				hit = &jobs[i]
			}
		}
		if hit == nil || !hit.Running {
			writeErr(w, http.StatusNotFound, procs.ErrGone)
			return
		}
		if !hit.CanKill {
			writeErr(w, http.StatusBadRequest, errors.New("this one runs in Hermes' sandbox; stop it from its chat"))
			return
		}
		if err := procs.Kill(ctx, hit.PID, hit.Start); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		log.Printf("bg: stopped %s (pid %d) by %s", hit.ID, hit.PID, s.who(r).ID)
		s.hub.send("changed", "bg")
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /api/mcp/usage", func(w http.ResponseWriter, r *http.Request) {
		dir := mcp.Dir(s.home, hermes.ActiveProfile(s.home))
		dirs := []string{filepath.Join(dir, "skills")}
		if dir != s.home {
			dirs = append(dirs, filepath.Join(s.home, "skills"))
		}
		writeJSON(w, map[string]any{"usage": redactUsage(s.who(r), mcp.Usages(s.home, dirs, time.Now()))})
	})
}

// In a team, only owners see command lines, folders and other people's
// projects; others see the jobs of projects they may see, without the details.
func redactJobs(me identity, jobs []bgJob) []bgJob {
	if !me.Team || me.Role == team.Owner {
		return jobs
	}
	out := []bgJob{}
	for _, j := range jobs {
		if j.Project == "" || !me.sees(j.Project) {
			continue
		}
		j.Cmd, j.Cwd, j.Session = "", "", ""
		if j.Kind == "process" {
			j.Title = "background command"
		}
		out = append(out, j)
	}
	return out
}

func redactUsage(me identity, all map[string]*mcp.Usage) map[string]*mcp.Usage {
	if !me.Team || me.Role == team.Owner {
		return all
	}
	for _, u := range all {
		keep := u.Projects[:0]
		for _, pc := range u.Projects {
			if me.sees(pc.Name) {
				pc.Root, pc.Repo = "", ""
				keep = append(keep, pc)
			}
		}
		u.Projects = keep
		u.Sessions = []mcp.Count{}
		u.Skills = []mcp.Skill{}
	}
	return all
}
