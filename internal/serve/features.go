// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"errors"
	"net/http"
	"os"
	"strings"
)

// featureOn reports whether a v1-gated feature is switched on. HPK_FEATURES is
// a comma list (for example "team"); everything is off by default.
func featureOn(name string) bool {
	for _, f := range strings.Split(os.Getenv("HPK_FEATURES"), ",") {
		if strings.EqualFold(strings.TrimSpace(f), name) {
			return true
		}
	}
	return false
}

// teamGate hides the team routes in solo mode while the team feature is off.
// A team-mode server (hpk serve --team) is never affected.
func (s *Server) teamGate(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.teamConfig() == nil && !featureOn("team") {
			writeErr(w, http.StatusNotFound, errors.New("team features come in a later version"))
			return
		}
		h(w, r)
	}
}

// features is what the Console reads from the snapshot.
func (s *Server) features() map[string]bool {
	return map[string]bool{"team": s.teamConfig() != nil || featureOn("team")}
}

// teamRoute registers a route behind teamGate.
func (s *Server) teamRoute(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	mux.HandleFunc(pattern, s.teamGate(h))
}
