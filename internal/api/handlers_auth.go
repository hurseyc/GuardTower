package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hurseyc/guardtower/internal/auth"
	"github.com/hurseyc/guardtower/internal/store"
	"github.com/hurseyc/guardtower/internal/version"
)

func (s *Server) audit(r *http.Request, action, target string, detail any) {
	e := store.AuditEntry{Action: action, Target: target, IP: auth.ClientIP(r)}
	if u := userFrom(r.Context()); u != nil {
		e.UserID, e.Username = u.ID, u.Username
	}
	if detail != nil {
		b, _ := json.Marshal(detail)
		e.Detail = string(b)
	}
	if err := s.Store.Audit(r.Context(), e); err != nil {
		s.log.Error("audit", "err", err)
	}
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"name":     "GuardTower",
		"version":  version.Version,
		"commit":   version.Commit,
		"date":     version.Date,
		"features": s.Features,
	})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	need, err := s.auth.NeedsSetup(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"needs_setup": need})
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := decode(r, &c); err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.auth.SetupFirstAdmin(r.Context(), c.Username, c.Password)
	if err != nil {
		s.fail(w, r, badRequest(err))
		return
	}
	sess, err := s.auth.CreateSession(r.Context(), w, r, u)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	r = r.WithContext(withSessionCtx(r.Context(), sess))
	s.audit(r, "setup.complete", u.Username, nil)
	writeJSON(w, 200, map[string]any{"user": u, "csrf_token": sess.CSRFToken})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := decode(r, &c); err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.auth.Authenticate(r.Context(), c.Username, c.Password, auth.ClientIP(r))
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		writeErr(w, http.StatusTooManyRequests, err.Error())
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		s.audit(r, "auth.login_failed", c.Username, nil)
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	case err != nil:
		s.fail(w, r, err)
		return
	}
	sess, err := s.auth.CreateSession(r.Context(), w, r, u)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	r = r.WithContext(withSessionCtx(r.Context(), sess))
	s.audit(r, "auth.login", u.Username, nil)
	writeJSON(w, 200, map[string]any{"user": u, "csrf_token": sess.CSRFToken})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.DestroySession(w, r, sessionFrom(r.Context()))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	writeJSON(w, 200, map[string]any{"user": sess.User, "csrf_token": sess.CSRFToken})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	u := userFrom(r.Context())
	if err := s.auth.ChangeOwnPassword(r.Context(), u.ID, body.Current, body.Next); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeErr(w, http.StatusBadRequest, "current password is incorrect")
			return
		}
		s.fail(w, r, badRequest(err))
		return
	}
	s.audit(r, "user.password_changed", u.Username, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- users (admin) ----

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	us, err := s.auth.ListUsers(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, us)
}

type userInput struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	Role         string `json:"role"`
	FamilyAccess bool   `json:"family_access"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var in userInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.auth.CreateUser(r.Context(), in.Username, in.Password, in.Role, in.FamilyAccess)
	if err != nil {
		s.fail(w, r, badRequest(err))
		return
	}
	s.audit(r, "user.create", u.Username, map[string]any{"role": u.Role, "family_access": u.FamilyAccess})
	writeJSON(w, 201, u)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in userInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.auth.UpdateUser(r.Context(), id, in.Role, in.FamilyAccess, in.Password); err != nil {
		s.fail(w, r, badRequest(err))
		return
	}
	s.audit(r, "user.update", in.Username, map[string]any{"id": id, "role": in.Role, "family_access": in.FamilyAccess, "password_reset": in.Password != ""})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if u := userFrom(r.Context()); u.ID == id {
		writeErr(w, http.StatusBadRequest, "you cannot delete your own account")
		return
	}
	if err := s.auth.DeleteUser(r.Context(), id); err != nil {
		s.fail(w, r, badRequest(err))
		return
	}
	s.audit(r, "user.delete", "", map[string]any{"id": id})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	es, err := s.Store.ListAudit(r.Context(), int(queryInt(r, "limit", 100)), queryInt(r, "before", 0))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, es)
}
