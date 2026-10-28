package api

import (
	"net/http"

	"github.com/hurseyc/guardtower/internal/resilience"
)

func (s *Server) handleResilience(w http.ResponseWriter, r *http.Request) {
	rep, err := s.Resilience.Report(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, rep)
}

// handleResiliencePreview reports the single points of failure a proposed
// placement (group and host) of an instance would create.
func (s *Server) handleResiliencePreview(w http.ResponseWriter, r *http.Request) {
	var body resilience.Placement
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	fs, err := s.Resilience.Preview(r.Context(), body)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"findings": fs})
}
