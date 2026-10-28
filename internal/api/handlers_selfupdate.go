package api

import (
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
