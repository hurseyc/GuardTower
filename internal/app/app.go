// Package app wires GuardTower's services together.
package app

import (
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/hurseyc/guardtower/internal/api"
	"github.com/hurseyc/guardtower/internal/auth"
	"github.com/hurseyc/guardtower/internal/collector"
	"github.com/hurseyc/guardtower/internal/config"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/events"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/querylog"
	"github.com/hurseyc/guardtower/internal/store"
)

// Config configures the application.
type Config struct {
	DataDir       string
	Log           *slog.Logger
	TrustedProxy  []*net.IPNet
	SecureCookies bool
	WebFS         fs.FS
	DocsFS        fs.FS
	// StatusInterval overrides the status poll interval (tests).
	StatusInterval time.Duration
	// CollectInterval overrides the query-log poll interval (tests).
	CollectInterval time.Duration
}

// App holds the running services.
type App struct {
	cfg       Config
	Store     *store.Store
	Box       *crypto.Box
	Auth      *auth.Service
	Fleet     *fleet.Service
	Poller    *fleet.Poller
	QueryLog  *querylog.Store
	Collector *collector.Collector
	Config    *config.Service
	syncKick  chan struct{}
	Bus       *events.Bus
	server    *api.Server
}

// New opens storage and constructs every service.
func New(ctx context.Context, cfg Config) (*App, error) {
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	box, err := crypto.LoadOrCreate(cfg.DataDir)
	if err != nil {
		st.Close()
		return nil, err
	}
	return NewWith(cfg, st, box), nil
}

// NewWith builds the app from an already-open store (used by tests).
func NewWith(cfg Config, st *store.Store, box *crypto.Box) *App {
	if cfg.StatusInterval == 0 {
		cfg.StatusInterval = 15 * time.Second
	}
	if cfg.CollectInterval == 0 {
		cfg.CollectInterval = 10 * time.Second
	}
	bus := events.NewBus()
	fl := fleet.NewService(st, box)
	ql := querylog.New(st)
	cs := config.NewService(st, fl, bus, cfg.Log)
	kick := make(chan struct{}, 1)
	a := &App{
		cfg:       cfg,
		Store:     st,
		Box:       box,
		Auth:      auth.NewService(st, cfg.SecureCookies),
		Fleet:     fl,
		Poller:    fleet.NewPoller(fl, bus, cfg.Log, cfg.StatusInterval),
		Bus:       bus,
		QueryLog:  ql,
		Collector: collector.New(fl, ql, st, bus, cfg.Log, cfg.CollectInterval),
		Config:    cs,
		syncKick:  kick,
	}
	a.server = api.New(api.Deps{
		Log: cfg.Log, Store: st, Auth: a.Auth, Fleet: fl, Poller: a.Poller, QueryLog: ql, Config: cs, SyncKick: kick, Bus: bus,
		WebFS: cfg.WebFS, DocsFS: cfg.DocsFS, Proxies: cfg.TrustedProxy,
		Features: map[string]bool{},
	})
	return a
}

// Start launches background workers.
func (a *App) Start(ctx context.Context) {
	go a.Poller.Run(ctx)
	go a.Collector.Run(ctx)
	go a.Config.RunChecks(ctx, 2*time.Minute, a.syncKick)
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.Auth.PurgeExpired(ctx)
			}
		}
	}()
}

// Handler returns the HTTP handler.
func (a *App) Handler() http.Handler { return a.server }

// Close releases resources.
func (a *App) Close() error { return a.Store.Close() }
