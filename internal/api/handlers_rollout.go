package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hurseyc/guardtower/internal/rollout"
)

type rolloutRequest struct {
	Target   rollout.Target `json:"target"`
	Sections []string       `json:"sections"`
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	var body rolloutRequest
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := s.Rollout.MakePlan(r.Context(), body.Target, body.Sections)
	if err != nil {
		s.fail(w, r, badRequest(err))
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) rolloutErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, rollout.ErrObserveMode), errors.Is(err, rollout.ErrBusy):
		writeErr(w, http.StatusConflict, err.Error())
	default:
		s.fail(w, r, badRequest(err))
	}
}

func (s *Server) handleStartRollout(w http.ResponseWriter, r *http.Request) {
	var body rolloutRequest
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	u := userFrom(r.Context())
	id, err := s.Rollout.Start(r.Context(), body.Target, body.Sections, u.Username)
	if err != nil {
		s.rolloutErr(w, r, err)
		return
	}
	s.audit(r, "rollout.start", "rollout #"+strconv.FormatInt(id, 10), body)
	writeJSON(w, 202, map[string]int64{"id": id})
}

func (s *Server) handleListRollouts(w http.ResponseWriter, r *http.Request) {
	rs, err := s.Rollout.List(r.Context(), int(queryInt(r, "limit", 50)))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"rollouts": rs, "active_id": s.Rollout.ActiveID(), "observe": s.Rollout.Observing(r.Context())})
}

func (s *Server) handleGetRollout(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ro, err := s.Rollout.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, ro)
}

func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sn, err := s.Rollout.ListSnapshots(r.Context(), id, int(queryInt(r, "limit", 50)))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, sn)
}

func (s *Server) handleTakeSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	sid, err := s.Rollout.TakeSnapshot(r.Context(), id, body.Note)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.audit(r, "snapshot.take", "instance #"+strconv.FormatInt(id, 10), map[string]any{"snapshot_id": sid, "note": body.Note})
	writeJSON(w, 201, map[string]int64{"id": sid})
}

func (s *Server) handleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	if err != nil {
		writeErr(w, 400, "invalid id")
		return
	}
	sn, err := s.Rollout.GetSnapshot(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, sn)
}

func (s *Server) handleRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	if err != nil {
		writeErr(w, 400, "invalid id")
		return
	}
	u := userFrom(r.Context())
	rid, err := s.Rollout.Restore(r.Context(), id, u.Username)
	if err != nil {
		s.rolloutErr(w, r, err)
		return
	}
	s.audit(r, "snapshot.restore", "snapshot #"+strconv.FormatInt(id, 10), map[string]any{"rollout_id": rid})
	writeJSON(w, 202, map[string]int64{"id": rid})
}
