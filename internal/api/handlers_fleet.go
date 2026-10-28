package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/hurseyc/guardtower/internal/auth"
	"github.com/hurseyc/guardtower/internal/config"
	"github.com/hurseyc/guardtower/internal/fleet"
)

func withSessionCtx(ctx context.Context, sess *auth.Session) context.Context {
	return context.WithValue(ctx, sessionKey, sess)
}

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	is, err := s.Fleet.ListInstances(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, is)
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	in, err := s.Fleet.GetInstance(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, in)
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var in fleet.InstanceInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	inst, err := s.Fleet.CreateInstance(r.Context(), in)
	if err != nil {
		s.fail(w, r, badRequest(err))
		return
	}
	s.audit(r, "instance.create", inst.Name, map[string]any{"id": inst.ID, "url": inst.URL})
	s.Poller.Kick()
	writeJSON(w, 201, inst)
}

func (s *Server) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in fleet.InstanceInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	inst, err := s.Fleet.UpdateInstance(r.Context(), id, in)
	if err != nil {
		if errors.Is(err, fleet.ErrNotFound) {
			s.fail(w, r, err)
			return
		}
		s.fail(w, r, badRequest(err))
		return
	}
	s.audit(r, "instance.update", inst.Name, map[string]any{"id": id, "password_changed": in.Password != nil})
	s.Poller.Kick()
	writeJSON(w, 200, inst)
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	inst, err := s.Fleet.GetInstance(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Fleet.DeleteInstance(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Config.DeleteScope(r.Context(), config.ScopeRef{Scope: config.ScopeInstance, ID: id}); err != nil {
		s.log.Error("delete instance layers", "err", err)
	}
	s.audit(r, "instance.delete", inst.Name, map[string]any{"id": id})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleTestInstance checks connectivity and credentials before saving.
func (s *Server) handleTestInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID int64 `json:"id"`
		fleet.InstanceInput
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.Fleet.ClientFromInput(r.Context(), body.ID, body.InstanceInput)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	start := time.Now()
	st, err := c.Status(ctx)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	res := map[string]any{"ok": true, "version": st.Version, "latency_ms": time.Since(start).Milliseconds(),
		"dns_addresses": st.DNSAddresses, "protection_enabled": st.ProtectionEnabled}
	if v, err := c.Version(ctx, false); err == nil {
		res["can_autoupdate"] = v.CanAutoUpdate
		res["new_version"] = v.NewVersion
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleRefreshInstance(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	in, err := s.Fleet.GetInstance(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	st := s.Poller.Probe(r.Context(), in)
	if err := s.Fleet.SaveStatus(r.Context(), id, st); err != nil {
		s.fail(w, r, err)
		return
	}
	in.Status = &st
	writeJSON(w, 200, in)
}

func (s *Server) handleSetProtection(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var body struct {
		Enabled         bool  `json:"enabled"`
		DurationMinutes int64 `json:"duration_minutes"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	in, err := s.Fleet.GetInstance(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.Fleet.Client(in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := c.SetProtection(r.Context(), body.Enabled, time.Duration(body.DurationMinutes)*time.Minute); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.audit(r, "instance.protection", in.Name, body)
	s.Poller.Kick()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- groups ----

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	gs, err := s.Fleet.ListGroups(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, gs)
}

func (s *Server) handleSaveGroup(w http.ResponseWriter, r *http.Request) {
	var id int64
	if r.Method == http.MethodPut {
		var err error
		if id, err = idParam(r); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Color       string `json:"color"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	g, err := s.Fleet.SaveGroup(r.Context(), id, body.Name, body.Description, body.Color)
	if err != nil {
		if errors.Is(err, fleet.ErrNotFound) {
			s.fail(w, r, err)
			return
		}
		s.fail(w, r, badRequest(err))
		return
	}
	s.audit(r, "group.save", g.Name, map[string]any{"id": g.ID})
	writeJSON(w, 200, g)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Fleet.DeleteGroup(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Config.DeleteScope(r.Context(), config.ScopeRef{Scope: config.ScopeGroup, ID: id}); err != nil {
		s.log.Error("delete group layers", "err", err)
	}
	s.kickSync()
	s.audit(r, "group.delete", "", map[string]any{"id": id})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleSetGroupMembers replaces a group's membership. Instances listed here
// move into the group; former members not listed become ungrouped.
func (s *Server) handleSetGroupMembers(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var body struct {
		InstanceIDs []int64 `json:"instance_ids"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	g, err := s.Fleet.GetGroup(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	want := map[int64]bool{}
	for _, iid := range body.InstanceIDs {
		want[iid] = true
		if err := s.Fleet.SetInstanceGroup(r.Context(), iid, id); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	for _, iid := range g.InstanceIDs {
		if !want[iid] {
			if err := s.Fleet.SetInstanceGroup(r.Context(), iid, 0); err != nil {
				s.fail(w, r, err)
				return
			}
		}
	}
	s.audit(r, "group.members", g.Name, body)
	s.kickSync()
	g, _ = s.Fleet.GetGroup(r.Context(), id)
	writeJSON(w, 200, g)
}

// ---- hosts & sites ----

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	hs, err := s.Fleet.ListHosts(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, hs)
}

func (s *Server) handleSaveHost(w http.ResponseWriter, r *http.Request) {
	var id int64
	if r.Method == http.MethodPut {
		var err error
		if id, err = idParam(r); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	var h fleet.Host
	if err := decode(r, &h); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.Fleet.SaveHost(r.Context(), id, h)
	if err != nil {
		if errors.Is(err, fleet.ErrNotFound) {
			s.fail(w, r, err)
			return
		}
		s.fail(w, r, badRequest(err))
		return
	}
	s.audit(r, "host.save", out.Name, map[string]any{"id": out.ID})
	writeJSON(w, 200, out)
}

func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Fleet.DeleteHost(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, "host.delete", "", map[string]any{"id": id})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleListSites(w http.ResponseWriter, r *http.Request) {
	ss, err := s.Fleet.ListSites(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, ss)
}

func (s *Server) handleSaveSite(w http.ResponseWriter, r *http.Request) {
	var id int64
	if r.Method == http.MethodPut {
		var err error
		if id, err = idParam(r); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.Fleet.SaveSite(r.Context(), id, body.Name)
	if err != nil {
		s.fail(w, r, badRequest(err))
		return
	}
	s.audit(r, "site.save", out.Name, nil)
	writeJSON(w, 200, out)
}

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Fleet.DeleteSite(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, "site.delete", "", map[string]any{"id": id})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleOverview returns everything the dashboard needs in one call.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	is, err := s.Fleet.ListInstances(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	gs, err := s.Fleet.ListGroups(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	hs, err := s.Fleet.ListHosts(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ss, err := s.Fleet.ListSites(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	online := 0
	for _, i := range is {
		if i.Status != nil && i.Status.Online {
			online++
		}
	}
	writeJSON(w, 200, map[string]any{
		"instances": is, "groups": gs, "hosts": hs, "sites": ss,
		"summary": map[string]int{"instances": len(is), "online": online, "groups": len(gs), "hosts": len(hs)},
	})
}

// handleEvents streams events to the browser via Server-Sent Events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	ch, cancel := s.Bus.Subscribe(256)
	defer cancel()
	keep := time.NewTicker(25 * time.Second)
	defer keep.Stop()
	u := userFrom(r.Context())
	_, _ = w.Write([]byte(": connected\n\n"))
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keep.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			fl.Flush()
		case e, ok := <-ch:
			if !ok {
				return
			}
			// Family events are only delivered to users with family access.
			if len(e.Type) > 7 && e.Type[:7] == "family." && !u.FamilyAccess {
				continue
			}
			b, err := json.Marshal(e.Data)
			if err != nil {
				continue
			}
			_, _ = w.Write([]byte("event: " + e.Type + "\ndata: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			fl.Flush()
		}
	}
}
