package api

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hurseyc/guardtower/internal/auth"
)

type ctxKey int

const sessionKey ctxKey = 1

func sessionFrom(ctx context.Context) *auth.Session {
	s, _ := ctx.Value(sessionKey).(*auth.Session)
	return s
}

func userFrom(ctx context.Context) *auth.User {
	if s := sessionFrom(ctx); s != nil {
		return s.User
	}
	return nil
}

// securityHeaders sets conservative browser security headers.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// trustedProxies rewrites RemoteAddr from X-Forwarded-For when the direct peer
// is in one of the configured CIDRs.
func trustedProxies(cidrs []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(cidrs) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err == nil {
				if ip := net.ParseIP(host); ip != nil && inAny(ip, cidrs) {
					if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
						parts := strings.Split(xff, ",")
						// Walk from the right, skipping trusted hops.
						for i := len(parts) - 1; i >= 0; i-- {
							c := net.ParseIP(strings.TrimSpace(parts[i]))
							if c == nil {
								break
							}
							if !inAny(c, cidrs) || i == 0 {
								r.RemoteAddr = net.JoinHostPort(c.String(), "0")
								break
							}
						}
					}
					if r.Header.Get("X-Forwarded-Proto") == "https" {
						r.Header.Set("X-GT-Secure", "1")
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func inAny(ip net.IP, cidrs []*net.IPNet) bool {
	for _, c := range cidrs {
		if c.Contains(ip) {
			return true
		}
	}
	return false
}

// withSession loads the session (if any) into the context.
func (s *Server) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := s.auth.SessionFromRequest(r)
		if err != nil {
			s.log.Error("load session", "err", err)
		}
		if sess != nil {
			r = r.WithContext(context.WithValue(r.Context(), sessionKey, sess))
		}
		next.ServeHTTP(w, r)
	})
}

// requireAuth rejects anonymous requests and enforces CSRF on unsafe methods.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := sessionFrom(r.Context())
		if sess == nil {
			writeErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !safeMethod(r.Method) {
			tok := r.Header.Get(auth.CSRFHeader)
			if tok == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(sess.CSRFToken)) != 1 {
				writeErr(w, http.StatusForbidden, "missing or invalid CSRF token")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requireRole enforces a minimum role.
func requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := userFrom(r.Context())
			if u == nil || !u.HasRole(role) {
				writeErr(w, http.StatusForbidden, "insufficient permissions: requires "+role)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireFamily restricts the Family DNS section to designated users.
func requireFamily(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r.Context())
		if u == nil || !u.FamilyAccess {
			writeErr(w, http.StatusForbidden, "Family DNS access has not been granted to this account")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// statusRecorder captures the response code for access logs.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "dur", time.Since(start).String())
		}
	})
}

func recoverer(log interface {
	Error(msg string, args ...any)
}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					log.Error("panic", "path", r.URL.Path, "value", v)
					writeErr(w, http.StatusInternalServerError, "internal error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
