// Package app wires GuardTower's services together.
package app

import (
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/hurseyc/guardtower/internal/api"
	"github.com/hurseyc/guardtower/internal/auth"
	"github.com/hurseyc/guardtower/internal/collector"
	"github.com/hurseyc/guardtower/internal/config"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/events"
	"github.com/hurseyc/guardtower/internal/family"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/notify"
	"github.com/hurseyc/guardtower/internal/proxmox"
	"github.com/hurseyc/guardtower/internal/querylog"
	"github.com/hurseyc/guardtower/internal/resilience"
	"github.com/hurseyc/guardtower/internal/rollout"
	"github.com/hurseyc/guardtower/internal/selfupdate"
	"github.com/hurseyc/guardtower/internal/store"
	"github.com/hurseyc/guardtower/internal/upgrade"
)

// Config configures the application.
type Config struct {
	DataDir       string
	Log           *slog.Logger
	TrustedProxy  []*net.IPNet
	SecureCookies bool
	WebFS         fs.FS
	DocsFS        fs.FS
	MetricsToken  string
	// OIDC configures single sign-on (disabled when Issuer is empty).
	OIDC auth.OIDCConfig
	// Demo marks a demo-mode server (the login page shows the demo sign-in).
	Demo bool
	// StatusInterval overrides the status poll interval (tests).
	StatusInterval time.Duration
	// CollectInterval overrides the query-log poll interval (tests).
	CollectInterval time.Duration
}

// App holds the running services.
type App struct {
	cfg        Config
	Store      *store.Store
	Box        *crypto.Box
	Auth       *auth.Service
	Fleet      *fleet.Service
	Poller     *fleet.Poller
	QueryLog   *querylog.Store
	Collector  *collector.Collector
	Config     *config.Service
	Rollout    *rollout.Service
	Upgrade    *upgrade.Service
	SelfUpdate *selfupdate.Checker
	Proxmox    *proxmox.Service
	Family     *family.Service
	Notify     *notify.Service
	Bus        *events.Bus
	server     *api.Server
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
		Rollout:   rollout.New(st, fl, cs, bus, cfg.Log),
	}
	a.Upgrade = upgrade.New(st, fl, box, a.Rollout, bus, cfg.Log)
	a.Upgrade.OnChanged = a.Poller.Kick
	a.Notify = notify.New(st, box)
	a.SelfUpdate = selfupdate.New(a.Upgrade.Releases.Enabled)
	if cfg.DataDir != "" {
		a.SelfUpdate.UpdateDir = filepath.Join(cfg.DataDir, "update")
	}
	a.Proxmox = proxmox.New(st, box, fl, cfg.Log)
	a.Proxmox.OnMoved = func(m []proxmox.Move) { bus.Publish("placement.changed", m) }
	a.Family = family.New(st, fl, bus, a.Notify, cfg.Log)
	a.server = api.New(api.Deps{
		Log: cfg.Log, Store: st, Auth: a.Auth, Fleet: fl, Poller: a.Poller, QueryLog: ql, Config: cs, Rollout: a.Rollout, Upgrade: a.Upgrade, Resilience: resilience.New(fl), Family: a.Family, Notify: a.Notify, SelfUpdate: a.SelfUpdate, OIDC: oidcFor(cfg), Proxmox: a.Proxmox, Bus: bus,
		WebFS: cfg.WebFS, DocsFS: cfg.DocsFS, Proxies: cfg.TrustedProxy,
		Features: map[string]bool{"demo": cfg.Demo}, MetricsToken: cfg.MetricsToken,
	})
	return a
}

// Start launches background workers.
func (a *App) Start(ctx context.Context) {
	a.Rollout.SetRoot(ctx)
	go a.Poller.Run(ctx)
	go a.Collector.Run(ctx)
	go a.Config.RunChecks(ctx, 2*time.Minute)
	go a.Upgrade.Run(ctx)
	go a.SelfUpdate.Run(ctx)
	go a.Proxmox.Run(ctx, 5*time.Minute)
	go a.Family.Run(ctx, 30*time.Second)
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

func oidcFor(cfg Config) *auth.OIDC {
	o := auth.NewOIDC(cfg.OIDC)
	if o != nil {
		o.Secure = cfg.SecureCookies
	}
	return o
}
