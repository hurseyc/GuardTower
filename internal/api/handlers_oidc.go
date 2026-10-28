package api

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/hurseyc/guardtower/internal/auth"
)

// handleOIDCLogin sends the browser to the sign-in provider.
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		http.NotFound(w, r)
		return
	}
	to, err := s.OIDC.Start(w, r)
	if err != nil {
		s.log.Warn("oidc: start", "err", err)
		ssoError(w, r, "Single sign-on is unavailable: "+err.Error())
		return
	}
	http.Redirect(w, r, to, http.StatusFound) //nolint:gosec // to is the provider's discovered authorization endpoint
}

// handleOIDCCallback finishes sign-in and starts a session.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		http.NotFound(w, r)
		return
	}
	id, err := s.OIDC.Finish(w, r)
	if err != nil {
		var shown *auth.ErrOIDC
		if errors.As(err, &shown) {
			ssoError(w, r, shown.Msg)
			return
		}
		s.log.Warn("oidc: callback", "err", err)
		ssoError(w, r, "Single sign-on failed. Ask an admin to check GuardTower's log.")
		return
	}
	role, family, ok := s.OIDC.Role(id.Groups)
	if !ok {
		s.audit(r, "auth.login_failed", id.Username, map[string]any{"method": "oidc", "reason": "no matching group"})
		ssoError(w, r, "Your account isn't in a group that may use GuardTower.")
		return
	}
	u, err := s.auth.LoginOIDC(r.Context(), id, role, family)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sess, err := s.auth.CreateSession(r.Context(), w, r, u)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	r = r.WithContext(withSessionCtx(r.Context(), sess))
	s.audit(r, "auth.login", u.Username, map[string]any{"method": "oidc", "role": role})
	http.Redirect(w, r, "/", http.StatusFound)
}

func ssoError(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/?sso_error="+url.QueryEscape(msg), http.StatusFound)
}
