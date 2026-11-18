// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"errors"
	"fmt"
	"net/http"

	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/session"
	"hpk.local/hpk/internal/team"
)

// chatAccess guards every /api/sessions/{id}/… handler. The chat must exist in
// the project the request names (a chat id from another project, even a live
// one, is "not found" — the request's project is what the person was allowed
// to see), and in a team only the chat's owner, the people it is shared with,
// or a Console owner may read or use it. It writes the error and returns false.
func (s *Server) chatAccess(w http.ResponseWriter, r *http.Request, p *project.Project, id string) (*session.Session, bool) {
	sess, err := session.Get(p, id)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no chat %s in %s", id, p.Name))
		return nil, false
	}
	if live := s.acp.ProjectOf(id); live != "" && live != p.Name {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no chat %s in %s", id, p.Name))
		return nil, false
	}
	if me := s.who(r); me.Team && me.Role != team.Owner && sess.Owner != "" && sess.Owner != me.ID && !sess.SharedWith(me.ID) {
		writeErr(w, http.StatusForbidden, errors.New("this chat belongs to "+sess.Owner+"; ask them to share it"))
		return nil, false
	}
	return sess, true
}
