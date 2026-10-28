package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/hurseyc/guardtower/internal/rollout"
	"github.com/hurseyc/guardtower/internal/upgrade"
)

func (s *Server) upgradeErr(w http.ResponseWriter, r *http.Request, err error) {
	var bi *upgrade.BadInput
	if errors.As(err, &bi) {
		writeErr(w, http.StatusBadRequest, bi.Msg)
		return
	}
	s.rolloutErr(w, r, err)
}

func (s *Server) handleUpgradeOverview(w http.ResponseWriter, r *http.Request) {
	o, err := s.Upgrade.Overview(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, o)
}

func (s *Server) handleUpgradeCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	err := s.Upgrade.CheckAll(ctx, true)
	o, oerr := s.Upgrade.Overview(r.Context())
	if oerr != nil {
		s.fail(w, r, oerr)
		return
	}
	if err != nil {
		o.CheckError = err.Error()
	}
	writeJSON(w, 200, o)
}

type upgradeRequest struct {
	Target  rollout.Target `json:"target"`
	Version string         `json:"version"`
}

func (s *Server) handleUpgradePlan(w http.ResponseWriter, r *http.Request) {
	var body upgradeRequest
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := s.Upgrade.MakePlan(r.Context(), body.Target, body.Version)
	if err != nil {
		s.upgradeErr(w, r, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) handleUpgradeStart(w http.ResponseWriter, r *http.Request) {
	var body upgradeRequest
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	u := userFrom(r.Context())
	id, err := s.Upgrade.Start(r.Context(), body.Target, body.Version, u.Username)
	if err != nil {
		s.upgradeErr(w, r, err)
		return
	}
	s.audit(r, "upgrade.start", "rollout #"+strconv.FormatInt(id, 10), body)
	writeJSON(w, 202, map[string]int64{"id": id})
}

func (s *Server) handleGetUpgradeConfig(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.Upgrade.GetConfig(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) handlePutUpgradeConfig(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var body upgrade.ConfigInput
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.Upgrade.SaveConfig(r.Context(), id, body)
	if err != nil {
		s.upgradeErr(w, r, err)
		return
	}
	if in, err := s.Fleet.GetInstance(r.Context(), id); err == nil {
		s.Upgrade.CheckInstance(r.Context(), in, false)
	}
	s.audit(r, "upgrade.config", "instance #"+strconv.FormatInt(id, 10), map[string]any{"driver": c.Driver, "pinned_version": c.PinnedVersion,
		"docker_endpoint": c.Docker.Endpoint, "docker_container": c.Docker.Container, "ssh_host": c.SSH.Host, "ssh_key_changed": body.SSHKey != nil})
	writeJSON(w, 200, c)
}

func (s *Server) handleTestUpgradeConfig(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var body struct {
		Kind   string              `json:"kind"`
		Config upgrade.ConfigInput `json:"config"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	res, err := s.Upgrade.Test(ctx, id, body.Kind, body.Config)
	if err != nil {
		s.upgradeErr(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handlePinHostKey(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var body struct {
		HostKey string `json:"host_key"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Upgrade.PinHostKey(r.Context(), id, body.HostKey); err != nil {
		s.upgradeErr(w, r, err)
		return
	}
	if in, err := s.Fleet.GetInstance(r.Context(), id); err == nil {
		s.Upgrade.CheckInstance(r.Context(), in, false)
	}
	s.audit(r, "upgrade.pin_host_key", "instance #"+strconv.FormatInt(id, 10), nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handlePutUpgradePolicy(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var body upgrade.Policy
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Upgrade.SetPolicy(r.Context(), id, body); err != nil {
		s.upgradeErr(w, r, err)
		return
	}
	s.audit(r, "upgrade.policy", "group #"+strconv.FormatInt(id, 10), body)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
