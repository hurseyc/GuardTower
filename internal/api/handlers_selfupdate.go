package api

import (
	"errors"
	"net/http"

	"github.com/hurseyc/guardtower/internal/selfupdate"
	"github.com/hurseyc/guardtower/internal/version"
)

// handleSelfUpdate reports whether a newer GuardTower release exists and
// how to install it for this install method.
func (s *Server) handleSelfUpdate(w http.ResponseWriter, r *http.Request) {
	if s.SelfUpdate == nil {
		writeJSON(w, 200, selfupdate.Status{Current: version.Version, Method: selfupdate.DetectMethod()})
		return
	}
	writeJSON(w, 200, s.SelfUpdate.Status())
}

// handleSelfUpdateCheck asks GitHub now instead of waiting for the next
// scheduled check.
func (s *Server) handleSelfUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if s.SelfUpdate == nil {
		s.handleSelfUpdate(w, r)
		return
	}
	_ = s.SelfUpdate.Check(r.Context()) // a failure is reported in the status
	writeJSON(w, 200, s.SelfUpdate.Status())
}

// handleSelfUpdateInstall starts an in-app update through the root helper.
func (s *Server) handleSelfUpdateInstall(w http.ResponseWriter, r *http.Request) {
	if s.SelfUpdate == nil {
		writeErr(w, http.StatusConflict, selfupdate.ErrNoSelfUpdate.Error())
		return
	}
	st := s.SelfUpdate.Status()
	if err := s.SelfUpdate.RequestUpdate(); err != nil {
		if errors.Is(err, selfupdate.ErrNoSelfUpdate) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		s.fail(w, r, err)
		return
	}
	s.audit(r, "guardtower.update", "GuardTower", map[string]any{"from": st.Current, "to": st.Latest})
	writeJSON(w, http.StatusAccepted, s.SelfUpdate.Status())
}
