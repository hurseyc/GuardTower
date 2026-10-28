package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hurseyc/guardtower/internal/proxmox"
)

func (s *Server) proxmoxErr(w http.ResponseWriter, r *http.Request, err error) {
	var bi *proxmox.BadInput
	switch {
	case errors.As(err, &bi):
		writeErr(w, http.StatusBadRequest, bi.Msg)
	case errors.Is(err, proxmox.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		s.fail(w, r, err)
	}
}

func (s *Server) handleListProxmox(w http.ResponseWriter, r *http.Request) {
	cs, err := s.Proxmox.List(r.Context())
	if err != nil {
		s.proxmoxErr(w, r, err)
		return
	}
	gs, err := s.Proxmox.Guests(r.Context())
	if err != nil {
		s.proxmoxErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"clusters": cs, "guests": gs})
}

func (s *Server) handleSaveProxmox(w http.ResponseWriter, r *http.Request) {
	var id int64
	if chi.URLParam(r, "id") != "" {
		var err error
		if id, err = idParam(r); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	var body proxmox.ClusterInput
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.Proxmox.Save(r.Context(), id, body)
	if err != nil {
		s.proxmoxErr(w, r, err)
		return
	}
	s.audit(r, "proxmox.save", c.Name, map[string]any{"url": c.URL, "token_id": c.TokenID, "secret_changed": body.Secret != nil})
	// Place instances straight away so the user sees the result.
	res, serr := s.Proxmox.Sync(r.Context(), c.ID)
	out := map[string]any{"cluster": c, "sync": res}
	if serr != nil {
		out["sync_error"] = serr.Error()
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleDeleteProxmox(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Proxmox.Delete(r.Context(), id); err != nil {
		s.proxmoxErr(w, r, err)
		return
	}
	s.audit(r, "proxmox.delete", "proxmox #"+strconv.FormatInt(id, 10), nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleTestProxmox checks a connection; ?id= reuses that connection's
// stored secret when none is sent.
func (s *Server) handleTestProxmox(w http.ResponseWriter, r *http.Request) {
	var body proxmox.ClusterInput
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	v, n, err := s.Proxmox.Test(r.Context(), id, body)
	if err != nil {
		var bi *proxmox.BadInput
		if errors.As(err, &bi) || errors.Is(err, proxmox.ErrNotFound) {
			s.proxmoxErr(w, r, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "version": v, "guests": n})
}

func (s *Server) handleSyncProxmox(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.Proxmox.Sync(r.Context(), id)
	if err != nil {
		if errors.Is(err, proxmox.ErrNotFound) {
			s.proxmoxErr(w, r, err)
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, 200, res)
}
