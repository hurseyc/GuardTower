// Package auth implements local accounts, sessions, roles and CSRF protection.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/store"
)

// Roles, in increasing order of privilege.
const (
	RoleViewer   = "viewer"
	RoleOperator = "operator"
	RoleAdmin    = "admin"
)

var roleRank = map[string]int{RoleViewer: 1, RoleOperator: 2, RoleAdmin: 3}

// ValidRole reports whether r is a known role.
func ValidRole(r string) bool { _, ok := roleRank[r]; return ok }

// SessionCookie is the name of the session cookie.
const SessionCookie = "gt_session"

// CSRFHeader must accompany every state-changing request.
const CSRFHeader = "X-CSRF-Token"

const sessionTTL = 7 * 24 * time.Hour

// ErrInvalidCredentials is returned for any failed login.
var ErrInvalidCredentials = errors.New("invalid username or password")

// User is an account.
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Role         string `json:"role"`
	FamilyAccess bool   `json:"family_access"`
	OIDC         bool   `json:"oidc"`
	CreatedAt    int64  `json:"created_at"`
	LastLoginAt  int64  `json:"last_login_at,omitempty"`
}

// HasRole reports whether u's role is at least min.
func (u *User) HasRole(min string) bool { return roleRank[u.Role] >= roleRank[min] }

// Session is an authenticated browser session.
type Session struct {
	User      *User
	CSRFToken string
	TokenHash string
}

// Service manages users and sessions.
type Service struct {
	st           *store.Store
	SecureCookie bool
	limiter      *loginLimiter
}

// NewService creates the auth service.
func NewService(st *store.Store, secureCookie bool) *Service {
	return &Service{st: st, SecureCookie: secureCookie, limiter: newLoginLimiter()}
}

// NeedsSetup reports whether no users exist yet (first-run wizard).
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	var n int
	err := s.st.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n == 0, err
}

// CreateUser creates a local account.
func (s *Service) CreateUser(ctx context.Context, username, password, role string, family bool) (*User, error) {
	username = strings.TrimSpace(username)
	if !validUsername(username) {
		return nil, errors.New("username must be 1-64 characters: letters, digits, . _ - @")
	}
	if !ValidRole(role) {
		return nil, errors.New("invalid role")
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	res, err := s.st.DB.ExecContext(ctx, `INSERT INTO users (username, password_hash, role, family_access, created_at)
		VALUES (?, ?, ?, ?, ?)`, username, hash, role, boolInt(family), now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, errors.New("username already exists")
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, Username: username, Role: role, FamilyAccess: family, CreatedAt: now}, nil
}

// SetupFirstAdmin creates the initial admin account atomically: it fails if
// any user already exists, so the setup endpoint cannot be replayed.
func (s *Service) SetupFirstAdmin(ctx context.Context, username, password string) (*User, error) {
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	if !validUsername(strings.TrimSpace(username)) {
		return nil, errors.New("username must be 1-64 characters: letters, digits, . _ - @")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	res, err := s.st.DB.ExecContext(ctx, `INSERT INTO users (username, password_hash, role, family_access, created_at)
		SELECT ?, ?, 'admin', 1, ? WHERE NOT EXISTS (SELECT 1 FROM users)`, strings.TrimSpace(username), hash, now)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errors.New("setup has already been completed")
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, Username: strings.TrimSpace(username), Role: RoleAdmin, FamilyAccess: true, CreatedAt: now}, nil
}

// Authenticate verifies a username/password, applying per-IP and per-user
// rate limiting.
func (s *Service) Authenticate(ctx context.Context, username, password, ip string) (*User, error) {
	key := strings.ToLower(strings.TrimSpace(username))
	if !s.limiter.allow(ip) || !s.limiter.allow("u:"+key) {
		return nil, ErrRateLimited
	}
	var u User
	var hash string
	var oidcSub sql.NullString
	err := s.st.DB.QueryRowContext(ctx, `SELECT id, username, password_hash, role, family_access, oidc_subject, created_at
		FROM users WHERE username = ?`, strings.TrimSpace(username)).
		Scan(&u.ID, &u.Username, &hash, &u.Role, &u.FamilyAccess, &oidcSub, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && hash == "") {
		// Burn comparable time so usernames can't be enumerated by timing.
		_, _ = VerifyPassword(dummyHash, password)
		s.limiter.fail(ip)
		s.limiter.fail("u:" + key)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	ok, err := VerifyPassword(hash, password)
	if err != nil || !ok {
		s.limiter.fail(ip)
		s.limiter.fail("u:" + key)
		return nil, ErrInvalidCredentials
	}
	s.limiter.reset("u:" + key)
	u.OIDC = oidcSub.Valid
	return &u, nil
}

// ErrRateLimited is returned when too many failed logins occurred recently.
var ErrRateLimited = errors.New("too many failed attempts; try again in a few minutes")

var dummyHash, _ = HashPassword("guardtower-dummy-password")

// CreateSession issues a new session for u and sets the cookie.
func (s *Service) CreateSession(ctx context.Context, w http.ResponseWriter, r *http.Request, u *User) (*Session, error) {
	token := crypto.RandomToken(32)
	csrf := crypto.RandomToken(24)
	now := time.Now()
	th := crypto.HashToken(token)
	_, err := s.st.DB.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, expires_at, user_agent, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, th, u.ID, csrf, now.Unix(), now.Add(sessionTTL).Unix(),
		truncate(r.UserAgent(), 256), ClientIP(r))
	if err != nil {
		return nil, err
	}
	_, _ = s.st.DB.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, now.Unix(), u.ID)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.SecureCookie || r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		Expires:  now.Add(sessionTTL),
	})
	return &Session{User: u, CSRFToken: csrf, TokenHash: th}, nil
}

// SessionFromRequest resolves the session cookie, or returns nil.
func (s *Service) SessionFromRequest(r *http.Request) (*Session, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil, nil
	}
	th := crypto.HashToken(c.Value)
	var sess Session
	var u User
	var oidcSub sql.NullString
	var exp int64
	err = s.st.DB.QueryRowContext(r.Context(), `SELECT s.csrf_token, s.expires_at, u.id, u.username, u.role, u.family_access, u.oidc_subject, u.created_at, COALESCE(u.last_login_at, 0)
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ?`, th).
		Scan(&sess.CSRFToken, &exp, &u.ID, &u.Username, &u.Role, &u.FamilyAccess, &oidcSub, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if time.Now().Unix() > exp {
		_, _ = s.st.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE token_hash = ?`, th)
		return nil, nil
	}
	u.OIDC = oidcSub.Valid
	sess.User = &u
	sess.TokenHash = th
	return &sess, nil
}

// DestroySession deletes the session and clears the cookie.
func (s *Service) DestroySession(w http.ResponseWriter, r *http.Request, sess *Session) {
	if sess != nil {
		_, _ = s.st.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE token_hash = ?`, sess.TokenHash)
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.SecureCookie || r.TLS != nil, SameSite: http.SameSiteLaxMode})
}

// PurgeExpired removes expired sessions.
func (s *Service) PurgeExpired(ctx context.Context) {
	_, _ = s.st.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, time.Now().Unix())
}

// ListUsers returns all users.
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT id, username, role, family_access, oidc_subject IS NOT NULL, created_at, COALESCE(last_login_at, 0)
		FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.FamilyAccess, &u.OIDC, &u.CreatedAt, &u.LastLoginAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateUser changes a user's role/family flag and optionally password.
// It refuses to demote or remove the last admin.
func (s *Service) UpdateUser(ctx context.Context, id int64, role string, family bool, password string) error {
	if !ValidRole(role) {
		return errors.New("invalid role")
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if role != RoleAdmin {
		if err := ensureOtherAdmin(ctx, tx, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET role = ?, family_access = ? WHERE id = ?`, role, boolInt(family), id); err != nil {
		return err
	}
	if password != "" {
		if err := ValidatePassword(password); err != nil {
			return err
		}
		hash, err := HashPassword(password)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, id); err != nil {
			return err
		}
		// Changing a password signs the user out everywhere.
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ChangeOwnPassword verifies the current password before setting a new one.
func (s *Service) ChangeOwnPassword(ctx context.Context, id int64, current, next string) error {
	var hash string
	if err := s.st.DB.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash); err != nil {
		return err
	}
	if ok, _ := VerifyPassword(hash, current); !ok {
		return ErrInvalidCredentials
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	nh, err := HashPassword(next)
	if err != nil {
		return err
	}
	_, err = s.st.DB.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, nh, id)
	return err
}

// DeleteUser removes a user, refusing to remove the last admin.
func (s *Service) DeleteUser(ctx context.Context, id int64) error {
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ensureOtherAdmin(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureOtherAdmin(ctx context.Context, tx *sql.Tx, id int64) error {
	var role string
	if err := tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ?`, id).Scan(&role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("user not found")
		}
		return err
	}
	if role != RoleAdmin {
		return nil
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND id != ?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return errors.New("cannot remove or demote the last admin")
	}
	return nil
}

// ClientIP returns the remote IP of r (proxy headers are handled by the
// trusted-proxy middleware, which rewrites RemoteAddr).
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func validUsername(u string) bool {
	if len(u) == 0 || len(u) > 64 {
		return false
	}
	for _, c := range u {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-', c == '@':
		default:
			return false
		}
	}
	return true
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// loginLimiter allows 10 failures per key per 10 minutes.
type loginLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

const (
	limiterWindow = 10 * time.Minute
	limiterMax    = 10
)

func newLoginLimiter() *loginLimiter { return &loginLimiter{hits: map[string][]time.Time{}} }

func (l *loginLimiter) prune(key string, now time.Time) []time.Time {
	h := l.hits[key]
	i := 0
	for i < len(h) && now.Sub(h[i]) > limiterWindow {
		i++
	}
	h = h[i:]
	if len(h) == 0 {
		delete(l.hits, key)
	} else {
		l.hits[key] = h
	}
	return h
}

func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, time.Now())) < limiterMax
}

func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.hits[key] = append(l.prune(key, now), now)
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}
