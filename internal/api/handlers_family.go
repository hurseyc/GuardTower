package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurseyc/guardtower/internal/auth"
	"github.com/hurseyc/guardtower/internal/family"
	"github.com/hurseyc/guardtower/internal/notify"
)

// requireFamily admits admins and users granted family access.
func requireFamily(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r.Context())
		if u == nil || (!u.FamilyAccess && !u.HasRole(auth.RoleAdmin)) {
			writeErr(w, http.StatusForbidden, "Family DNS is only visible to admins and users with family access")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// familySettingDefs are the Family DNS settings, editable by family users.
var familySettingDefs = map[string]struct {
	def      string
	validate func(string) error
}{
	family.SettingRetentionDays: {strconv.Itoa(family.DefaultRetentionDays), intRange(7, 3650)},
	family.SettingBedtime:       {family.DefaultBedtime, func(v string) error { _, err := family.ParseClock(v); return err }},
	family.SettingMask:          {"true", oneOf("true", "false")},
	family.SettingAlerts:        {"adult", oneOf("off", "adult", "all")},
	family.SettingQuietHours:    {"", func(v string) error { _, err := family.ParseClock(v); return err }},
	family.SettingDigest:        {"sun 18:00", func(v string) error { _, _, _, err := family.ParseDigest(v); return err }},
}

func intRange(lo, hi int) func(string) error {
	return func(v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n < lo || n > hi {
			return errors.New("must be a whole number between " + strconv.Itoa(lo) + " and " + strconv.Itoa(hi))
		}
		return nil
	}
}

func oneOf(opts ...string) func(string) error {
	return func(v string) error {
		for _, o := range opts {
			if v == o {
				return nil
			}
		}
		return errors.New("must be one of " + strings.Join(opts, ", "))
	}
}

func (s *Server) familyErr(w http.ResponseWriter, r *http.Request, err error) {
	var bi *family.BadInput
	switch {
	case errors.As(err, &bi):
		writeErr(w, http.StatusBadRequest, bi.Msg)
	case errors.Is(err, family.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		s.fail(w, r, err)
	}
}

// familyRange reads ?days= (default 7, max 365) as [from, to).
func familyRange(r *http.Request) (int64, int64) {
	days := int(queryInt(r, "days", 7))
	if days < 1 || days > 365 {
		days = 7
	}
	now := time.Now()
	return now.Add(-time.Duration(days) * 24 * time.Hour).UnixMilli(), now.Add(time.Minute).UnixMilli()
}

func categoriesParam(r *http.Request) []string {
	return splitList(r.URL.Query()["category"])
}

func (s *Server) handleFamilyOverview(w http.ResponseWriter, r *http.Request) {
	from, to := familyRange(r)
	o, err := s.Family.Overview(r.Context(), from, to)
	if err != nil {
		s.familyErr(w, r, err)
		return
	}
	writeJSON(w, 200, o)
}

func (s *Server) handleFamilyDetail(w http.ResponseWriter, r *http.Request) {
	from, to := familyRange(r)
	id := queryInt(r, "person", -1)
	d, err := s.Family.Detail(r.Context(), id, categoriesParam(r), from, to)
	if err != nil {
		s.familyErr(w, r, err)
		return
	}
	writeJSON(w, 200, d)
}

func (s *Server) handleFamilyEvents(w http.ResponseWriter, r *http.Request) {
	from, to := familyRange(r)
	evs, err := s.Family.Events(r.Context(), family.Filter{PersonID: queryInt(r, "person", 0), Device: r.URL.Query().Get("device"),
		Categories: categoriesParam(r), From: from, To: to, Limit: int(queryInt(r, "limit", 200))})
	if err != nil {
		s.familyErr(w, r, err)
		return
	}
	writeJSON(w, 200, evs)
}

func (s *Server) handleFamilyReveal(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	e, err := s.Family.Reveal(r.Context(), id)
	if err != nil {
		s.familyErr(w, r, err)
		return
	}
	// The audit log records that a domain was revealed, not the domain itself.
	s.audit(r, "family.reveal", "family event #"+strconv.FormatInt(id, 10), map[string]any{"person": e.Person, "device": e.Device, "category": e.Category, "domain": family.Mask(e.Domain)})
	writeJSON(w, 200, map[string]string{"domain": e.Domain})
}

func (s *Server) handleFamilyPeople(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Family.People(r.Context())
	if err != nil {
		s.familyErr(w, r, err)
		return
	}
	writeJSON(w, 200, ps)
}

func (s *Server) handleSavePerson(w http.ResponseWriter, r *http.Request) {
	var id int64
	if chi.URLParam(r, "id") != "" {
		var err error
		if id, err = idParam(r); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	var body struct {
		Name  string `json:"name"`
		Emoji string `json:"emoji"`
		Color string `json:"color"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := s.Family.SavePerson(r.Context(), id, body.Name, body.Emoji, body.Color)
	if err != nil {
		s.familyErr(w, r, err)
		return
	}
	s.audit(r, "family.person.save", "person "+p.Name, nil)
	writeJSON(w, 200, p)
}

func (s *Server) handleDeletePerson(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Family.DeletePerson(r.Context(), id); err != nil {
		s.familyErr(w, r, err)
		return
	}
	s.audit(r, "family.person.delete", "person #"+strconv.FormatInt(id, 10), nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleFamilyClients(w http.ResponseWriter, r *http.Request) {
	days := int(queryInt(r, "days", 7))
	if days < 1 || days > 90 {
		days = 7
	}
	cs, err := s.Family.Clients(r.Context(), days)
	if err != nil {
		s.familyErr(w, r, err)
		return
	}
	writeJSON(w, 200, cs)
}

func (s *Server) handleAssignDevice(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Device   string `json:"device"`
		Label    string `json:"label"`
		PersonID int64  `json:"person_id"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Family.AssignDevice(r.Context(), body.Device, body.Label, body.PersonID); err != nil {
		s.familyErr(w, r, err)
		return
	}
	s.audit(r, "family.device.assign", "device "+body.Device, body)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleFamilyLists(w http.ResponseWriter, r *http.Request) {
	ls, err := s.Family.Lists(r.Context())
	if err != nil {
		s.familyErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"lists": ls, "categories": family.Categories, "labels": family.CategoryLabels})
}

func (s *Server) handleSetListCategory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL      string `json:"url"`
		Category string `json:"category"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Family.SetListCategory(r.Context(), body.URL, body.Category); err != nil {
		s.familyErr(w, r, err)
		return
	}
	s.audit(r, "family.list.category", body.URL, body)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleGetFamilySettings(w http.ResponseWriter, r *http.Request) {
	out := map[string]string{}
	for k, d := range familySettingDefs {
		v, err := s.Store.GetSetting(r.Context(), k, d.def)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out[k] = v
	}
	writeJSON(w, 200, out)
}

func (s *Server) handlePutFamilySettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	for k, v := range body {
		d, ok := familySettingDefs[k]
		if !ok {
			writeErr(w, 400, "unknown setting "+k)
			return
		}
		if err := d.validate(strings.TrimSpace(v)); err != nil {
			writeErr(w, 400, k+": "+err.Error())
			return
		}
	}
	for k, v := range body {
		if err := s.Store.SetSetting(r.Context(), k, strings.TrimSpace(v)); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	s.audit(r, "family.settings", "family settings", body)
	s.handleGetFamilySettings(w, r)
}

func (s *Server) handleFamilyProfile(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	g, err := s.Fleet.GetGroup(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// A member to read current filtering from, if it isn't managed yet.
	var member int64
	for _, iid := range g.InstanceIDs {
		if in, err := s.Fleet.GetInstance(r.Context(), iid); err == nil && in.Enabled && in.Status != nil && in.Status.Online {
			member = iid
			break
		}
	}
	var body family.ProfileOptions
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	u := userFrom(r.Context())
	changed, err := family.ApplyProfile(r.Context(), s.Config, id, member, body, u.Username)
	if err != nil {
		s.fail(w, r, badRequest(err))
		return
	}
	s.kickSync()
	s.audit(r, "family.profile", "group #"+strconv.FormatInt(id, 10), body)
	writeJSON(w, 200, map[string]any{"sections": changed})
}

// ---- notification targets (admin) ----

func (s *Server) notifyErr(w http.ResponseWriter, r *http.Request, err error) {
	var bi *notify.BadInput
	switch {
	case errors.As(err, &bi):
		writeErr(w, http.StatusBadRequest, bi.Msg)
	case notify.IsNotFound(err):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		s.fail(w, r, err)
	}
}

func (s *Server) handleListTargets(w http.ResponseWriter, r *http.Request) {
	ts, err := s.Notify.List(r.Context())
	if err != nil {
		s.notifyErr(w, r, err)
		return
	}
	writeJSON(w, 200, ts)
}

func (s *Server) handleSaveTarget(w http.ResponseWriter, r *http.Request) {
	var id int64
	if chi.URLParam(r, "id") != "" {
		var err error
		if id, err = idParam(r); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	var body notify.TargetInput
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	t, err := s.Notify.Save(r.Context(), id, body)
	if err != nil {
		s.notifyErr(w, r, err)
		return
	}
	s.audit(r, "notify.target.save", t.Name, map[string]any{"kind": t.Kind, "url": t.URL, "token_changed": body.Token != nil})
	writeJSON(w, 200, t)
}

func (s *Server) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Notify.Delete(r.Context(), id); err != nil {
		s.notifyErr(w, r, err)
		return
	}
	s.audit(r, "notify.target.delete", "target #"+strconv.FormatInt(id, 10), nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleTestTarget(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Notify.Test(r.Context(), id); err != nil {
		if notify.IsNotFound(err) {
			s.notifyErr(w, r, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
