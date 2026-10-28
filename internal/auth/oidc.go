package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/hurseyc/guardtower/internal/crypto"
)

// OIDCConfig configures single sign-on with an OpenID Connect provider
// (Authentik, Authelia, Keycloak, Entra ID, Google…).
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// RedirectURL is https://<guardtower>/api/v1/auth/oidc/callback. When
	// empty it is derived from each request's host.
	RedirectURL string
	Scopes      []string
	// GroupsClaim names the ID-token claim holding the user's groups.
	GroupsClaim string
	// Users in these groups get the role. With none set, everyone the
	// provider lets in gets DefaultRole.
	AdminGroups, OperatorGroups, ViewerGroups []string
	// FamilyGroups get Family DNS access (admins always have it).
	FamilyGroups []string
	DefaultRole  string
	// Label is the sign-in button's text, e.g. "Sign in with Authentik".
	Label string
}

// Enabled reports whether OIDC is configured.
func (c OIDCConfig) Enabled() bool { return c.Issuer != "" && c.ClientID != "" }

// OIDC runs the authorization-code flow (with PKCE) against the provider.
type OIDC struct {
	Cfg OIDCConfig
	// Secure marks the flow cookie Secure even on plain-HTTP requests.
	Secure bool

	mu       sync.Mutex
	provider *oidc.Provider
}

// NewOIDC returns nil when cfg is not enabled.
func NewOIDC(cfg OIDCConfig) *OIDC {
	if !cfg.Enabled() {
		return nil
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "profile", "email", "groups"}
	} else if !slices.Contains(cfg.Scopes, oidc.ScopeOpenID) {
		cfg.Scopes = append([]string{oidc.ScopeOpenID}, cfg.Scopes...)
	}
	if cfg.GroupsClaim == "" {
		cfg.GroupsClaim = "groups"
	}
	if !ValidRole(cfg.DefaultRole) {
		cfg.DefaultRole = RoleViewer
	}
	if cfg.Label == "" {
		cfg.Label = "Sign in with SSO"
	}
	return &OIDC{Cfg: cfg}
}

// ErrOIDC is a sign-in failure the user should see.
type ErrOIDC struct{ Msg string }

func (e *ErrOIDC) Error() string { return e.Msg }

const oidcCookie = "gt_oidc"

// discover fetches the provider's configuration once it is reachable, so a
// provider that is down at startup doesn't stop GuardTower from starting.
func (o *OIDC) discover(ctx context.Context) (*oidc.Provider, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.provider != nil {
		return o.provider, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(ctx, o.Cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("contacting the sign-in provider %s: %w", o.Cfg.Issuer, err)
	}
	o.provider = p
	return p, nil
}

func (o *OIDC) oauth(p *oidc.Provider, r *http.Request) *oauth2.Config {
	redirect := o.Cfg.RedirectURL
	if redirect == "" {
		scheme := "http"
		if r.TLS != nil || o.Secure {
			scheme = "https"
		}
		redirect = scheme + "://" + r.Host + "/api/v1/auth/oidc/callback"
	}
	return &oauth2.Config{ClientID: o.Cfg.ClientID, ClientSecret: o.Cfg.ClientSecret, Endpoint: p.Endpoint(),
		RedirectURL: redirect, Scopes: o.Cfg.Scopes}
}

// Start sets the flow cookie (state, nonce, PKCE verifier) and returns the
// provider URL to send the browser to.
func (o *OIDC) Start(w http.ResponseWriter, r *http.Request) (string, error) {
	p, err := o.discover(r.Context())
	if err != nil {
		return "", err
	}
	state, nonce, verifier := crypto.RandomToken(24), crypto.RandomToken(24), oauth2.GenerateVerifier()
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: state + "." + nonce + "." + verifier, Path: "/api/v1/auth/oidc",
		HttpOnly: true, Secure: o.Secure || r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	return o.oauth(p, r).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

// Identity is who the provider says signed in.
type Identity struct {
	Subject  string // issuer-scoped, stored as users.oidc_subject
	Username string
	Groups   []string
}

// Finish completes the flow from the callback request.
func (o *OIDC) Finish(w http.ResponseWriter, r *http.Request) (*Identity, error) {
	c, err := r.Cookie(oidcCookie)
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: "", Path: "/api/v1/auth/oidc", MaxAge: -1, HttpOnly: true,
		Secure: o.Secure || r.TLS != nil, SameSite: http.SameSiteLaxMode})
	if err != nil {
		return nil, &ErrOIDC{"the sign-in took too long or cookies are blocked; please try again"}
	}
	parts := strings.Split(c.Value, ".")
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		msg := q.Get("error_description")
		if msg == "" {
			msg = e
		}
		return nil, &ErrOIDC{"the sign-in provider refused: " + msg}
	}
	if len(parts) != 3 || q.Get("state") == "" || q.Get("state") != parts[0] {
		return nil, &ErrOIDC{"the sign-in response doesn't match this browser; please try again"}
	}
	p, err := o.discover(r.Context())
	if err != nil {
		return nil, err
	}
	tok, err := o.oauth(p, r).Exchange(r.Context(), q.Get("code"), oauth2.VerifierOption(parts[2]))
	if err != nil {
		return nil, fmt.Errorf("exchanging the sign-in code: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("the sign-in provider returned no ID token")
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: o.Cfg.ClientID}).Verify(r.Context(), raw)
	if err != nil {
		return nil, fmt.Errorf("verifying the ID token: %w", err)
	}
	if idt.Nonce != parts[1] {
		return nil, &ErrOIDC{"the sign-in response was replayed; please try again"}
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return nil, err
	}
	if _, has := claims[o.Cfg.GroupsClaim]; !has {
		// Some providers only put groups in the userinfo response.
		if ui, err := p.UserInfo(r.Context(), oauth2.StaticTokenSource(tok)); err == nil {
			var extra map[string]any
			if ui.Claims(&extra) == nil {
				for k, v := range extra {
					if _, dup := claims[k]; !dup {
						claims[k] = v
					}
				}
			}
		}
	}
	id := &Identity{Subject: o.Cfg.Issuer + "|" + idt.Subject, Groups: stringsClaim(claims[o.Cfg.GroupsClaim])}
	for _, k := range []string{"preferred_username", "email", "name"} {
		if v, _ := claims[k].(string); v != "" {
			id.Username = v
			break
		}
	}
	if id.Username == "" {
		id.Username = idt.Subject
	}
	return id, nil
}

func stringsClaim(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// Role maps groups to a role and Family DNS access. ok is false when the
// user is in none of the configured groups.
func (o *OIDC) Role(groups []string) (role string, family, ok bool) {
	in := func(list []string) bool {
		for _, g := range list {
			if slices.Contains(groups, g) {
				return true
			}
		}
		return false
	}
	c := o.Cfg
	switch {
	case in(c.AdminGroups):
		role = RoleAdmin
	case in(c.OperatorGroups):
		role = RoleOperator
	case in(c.ViewerGroups):
		role = RoleViewer
	case len(c.AdminGroups)+len(c.OperatorGroups)+len(c.ViewerGroups) == 0:
		role = c.DefaultRole
	default:
		return "", false, false
	}
	return role, role == RoleAdmin || in(c.FamilyGroups), true
}

var unsafeUsername = regexp.MustCompile(`[^A-Za-z0-9._@-]+`)

// LoginOIDC finds or creates the account for id and updates its role from
// the provider's groups. Single sign-on accounts have no password, and an
// existing local account with the same name is never taken over.
func (s *Service) LoginOIDC(ctx context.Context, id *Identity, role string, family bool) (*User, error) {
	var u User
	err := s.st.DB.QueryRowContext(ctx, `SELECT id, username, created_at FROM users WHERE oidc_subject = ?`, id.Subject).
		Scan(&u.ID, &u.Username, &u.CreatedAt)
	switch {
	case err == nil:
		if _, err := s.st.DB.ExecContext(ctx, `UPDATE users SET role = ?, family_access = ? WHERE id = ?`, role, boolInt(family), u.ID); err != nil {
			return nil, err
		}
	case errors.Is(err, sql.ErrNoRows):
		name := unsafeUsername.ReplaceAllString(id.Username, "-")
		if len(name) > 48 {
			name = name[:48]
		}
		if name == "" {
			name = "sso-user"
		}
		var taken int
		_ = s.st.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = ?`, name).Scan(&taken)
		if taken > 0 {
			h := sha256.Sum256([]byte(id.Subject))
			name += "-" + hex.EncodeToString(h[:3])
		}
		now := time.Now().Unix()
		res, err := s.st.DB.ExecContext(ctx, `INSERT INTO users (username, password_hash, role, family_access, oidc_subject, created_at)
			VALUES (?, '', ?, ?, ?, ?)`, name, role, boolInt(family), id.Subject, now)
		if err != nil {
			return nil, err
		}
		u.ID, _ = res.LastInsertId()
		u.Username, u.CreatedAt = name, now
	default:
		return nil, err
	}
	u.Role, u.FamilyAccess, u.OIDC = role, family, true
	return &u, nil
}
