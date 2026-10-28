// Package api serves the REST API, the SSE event stream and the embedded web UI.
package api

import (
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/hurseyc/guardtower/internal/auth"
	"github.com/hurseyc/guardtower/internal/events"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/store"
)

// Deps are the services the API depends on.
type Deps struct {
	Log      *slog.Logger
	Store    *store.Store
	Auth     *auth.Service
	Fleet    *fleet.Service
	Poller   *fleet.Poller
	Bus      *events.Bus
	WebFS    fs.FS // built SPA (index.html at root); nil serves a placeholder
	DocsFS   fs.FS // markdown docs
	Proxies  []*net.IPNet
	Features map[string]bool
}

// Server is the HTTP handler.
type Server struct {
	Deps
	log    *slog.Logger
	auth   *auth.Service
	router chi.Router
	routes []routeInfo
}

type routeInfo struct{ Method, Pattern string }

// New builds the server and its routes.
func New(d Deps) *Server {
	s := &Server{Deps: d, log: d.Log, auth: d.Auth}
	r := chi.NewRouter()
	r.Use(recoverer(d.Log), trustedProxies(d.Proxies), securityHeaders, s.accessLog)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/readyz", s.handleReady)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.withSession)
		r.Get("/meta", s.handleMeta)
		r.Get("/setup", s.handleSetupStatus)
		r.Post("/setup", s.handleSetup)
		r.Post("/auth/login", s.handleLogin)
		r.Get("/docs", s.handleDocsIndex)
		r.Get("/docs/*", s.handleDocPage)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/me", s.handleMe)
			r.Post("/me/password", s.handleChangePassword)
			r.Get("/events", s.handleEvents)

			// Read-only fleet inventory: any signed-in user.
			r.Get("/instances", s.handleListInstances)
			r.Get("/instances/{id}", s.handleGetInstance)
			r.Get("/groups", s.handleListGroups)
			r.Get("/hosts", s.handleListHosts)
			r.Get("/sites", s.handleListSites)
			r.Get("/overview", s.handleOverview)

			r.Group(func(r chi.Router) {
				r.Use(requireRole(auth.RoleOperator))
				r.Post("/instances/{id}/protection", s.handleSetProtection)
				r.Post("/instances/{id}/refresh", s.handleRefreshInstance)
			})

			r.Group(func(r chi.Router) {
				r.Use(requireRole(auth.RoleAdmin))
				r.Post("/instances", s.handleCreateInstance)
				r.Post("/instances/test", s.handleTestInstance)
				r.Put("/instances/{id}", s.handleUpdateInstance)
				r.Delete("/instances/{id}", s.handleDeleteInstance)
				r.Post("/groups", s.handleSaveGroup)
				r.Put("/groups/{id}", s.handleSaveGroup)
				r.Delete("/groups/{id}", s.handleDeleteGroup)
				r.Put("/groups/{id}/members", s.handleSetGroupMembers)
				r.Post("/hosts", s.handleSaveHost)
				r.Put("/hosts/{id}", s.handleSaveHost)
				r.Delete("/hosts/{id}", s.handleDeleteHost)
				r.Post("/sites", s.handleSaveSite)
				r.Put("/sites/{id}", s.handleSaveSite)
				r.Delete("/sites/{id}", s.handleDeleteSite)
				r.Get("/users", s.handleListUsers)
				r.Post("/users", s.handleCreateUser)
				r.Put("/users/{id}", s.handleUpdateUser)
				r.Delete("/users/{id}", s.handleDeleteUser)
				r.Get("/audit", s.handleListAudit)
			})
		})
		r.NotFound(func(w http.ResponseWriter, r *http.Request) { writeErr(w, 404, "no such API endpoint") })
	})

	r.Handle("/*", s.spaHandler())
	s.router = r
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }

// Router exposes the router for registering additional modules.
func (s *Server) Router() chi.Router { return s.router }

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}

// spaHandler serves static assets and falls back to index.html for client routes.
func (s *Server) spaHandler() http.Handler {
	if s.WebFS == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><title>GuardTower</title><p>The web UI was not built into this binary. Run <code>make web</code> and rebuild.</p>`))
		})
	}
	files := http.FileServer(http.FS(s.WebFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := s.WebFS.Open(p); err == nil {
			st, _ := f.Stat()
			f.Close()
			if st != nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		// Client-side route: serve index.html.
		data, err := fs.ReadFile(s.WebFS, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(data)
	})
}
