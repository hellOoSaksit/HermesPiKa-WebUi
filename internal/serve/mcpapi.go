// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"

	"hpk.local/hpk/internal/hermes"
	"hpk.local/hpk/internal/mcp"
	"hpk.local/hpk/internal/team"
)

// MCP servers of the active Hermes profile. Reading is open to members;
// adding, removing and testing change what tools the agent gets, so they are
// for owners (action() maps POST /api/mcp to team.Admin).
// On a team server only web MCPs (URLs) are allowed: see package mcp.
func (s *Server) mcpAPI(mux *http.ServeMux) {
	dir := func() string { return mcp.Dir(s.home, hermes.ActiveProfile(s.home)) }
	place := func() bool { return mcp.ServerMode(s.teamConfig() != nil) }

	mux.HandleFunc("GET /api/mcp", func(w http.ResponseWriter, r *http.Request) {
		server := place()
		list, err := mcp.List(dir(), server)
		if me := s.who(r); me.Team && me.Role != team.Owner {
			// keys often ride in a URL query or an argument: owners only
			for i := range list {
				list[i].URL = mcp.SafeURL(list[i].URL)
				list[i].Args = nil
			}
		}
		out := map[string]any{
			"servers":       list,
			"place":         map[bool]string{true: "server", false: "local"}[server],
			"os":            runtime.GOOS,
			"canSendTokens": mcp.CanSendTokens,
			"canManage":     s.who(r).Role == team.Owner,
			"profile":       hermes.ActiveProfile(s.home),
		}
		if err != nil {
			out["error"] = err.Error()
		}
		writeJSON(w, out)
	})

	mux.HandleFunc("POST /api/mcp", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Op string `json:"op"` // add | remove | test
			mcp.Spec
		}
		if _, ok := s.decodeOnly(w, r, &in); !ok {
			return
		}
		if !s.can(w, r, team.Admin) {
			return
		}
		ctx := r.Context()
		var (
			res *mcp.Result
			err error
		)
		switch in.Op {
		case "add":
			if in.Token != "" && !s.viaHTTPS(r) && !isLoopback(r.Host) && !isLoopback(net.JoinHostPort(hostOnly(r.Host), "0")) {
				writeErr(w, http.StatusForbidden, errors.New("send tokens over HTTPS only; this Console is on plain HTTP"))
				return
			}
			res, err = mcp.Add(ctx, dir(), in.Spec, place())
		case "remove":
			err = mcp.Remove(ctx, dir(), in.Name)
		case "test":
			// A server here may not run what it refuses to add.
			list, _ := mcp.List(dir(), place())
			var cur *mcp.Server
			for i := range list {
				if list[i].Name == in.Name {
					cur = &list[i]
				}
			}
			switch {
			case cur == nil:
				err = fmt.Errorf("no MCP named %q", in.Name)
			case cur.Blocked != "":
				err = errors.New(cur.Blocked)
			case !cur.Enabled:
				// Saved off when it did not answer: adding it again probes it and switches it on.
				res, err = mcp.Enable(ctx, dir(), in.Name, place())
			default:
				res, err = mcp.Test(ctx, dir(), in.Name)
			}
		default:
			err = fmt.Errorf("unknown op %q", in.Op)
		}
		in.Token = ""
		if err != nil {
			code := http.StatusBadRequest
			if errors.Is(err, mcp.ErrNeedsTerminal) {
				code = http.StatusConflict
			}
			writeJSON2(w, code, map[string]any{"error": err.Error(), "result": res})
			return
		}
		what := mcp.SafeURL(in.URL)
		if what == "" {
			what = in.Command
		}
		log.Printf("mcp: %s %s %s by %s", in.Op, in.Name, what, s.who(r).ID)
		s.hub.send("changed", "mcp")
		writeJSON(w, map[string]any{"ok": true, "result": res})
	})
}
