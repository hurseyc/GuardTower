package fleet

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hurseyc/guardtower/internal/events"
)

// Poller probes every enabled instance's /control/status on an interval.
type Poller struct {
	svc      *Service
	bus      *events.Bus
	log      *slog.Logger
	interval time.Duration
	kick     chan struct{}
}

// NewPoller creates a status poller.
func NewPoller(svc *Service, bus *events.Bus, log *slog.Logger, interval time.Duration) *Poller {
	return &Poller{svc: svc, bus: bus, log: log, interval: interval, kick: make(chan struct{}, 1)}
}

// Kick triggers an immediate poll (e.g. after an instance is added).
func (p *Poller) Kick() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

// Run polls until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	t := time.NewTicker(p.interval)
	defer t.Stop()
	p.pollAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-p.kick:
		}
		p.pollAll(ctx)
	}
}

func (p *Poller) pollAll(ctx context.Context) {
	insts, err := p.svc.ListInstances(ctx)
	if err != nil {
		p.log.Error("list instances", "err", err)
		return
	}
	var wg sync.WaitGroup
	for _, in := range insts {
		if !in.Enabled {
			continue
		}
		wg.Add(1)
		go func(in *Instance) {
			defer wg.Done()
			st := p.Probe(ctx, in)
			if err := p.svc.SaveStatus(ctx, in.ID, st); err != nil && ctx.Err() == nil {
				p.log.Error("save status", "instance", in.Name, "err", err)
			}
			wasOnline := in.Status != nil && in.Status.Online
			if wasOnline != st.Online && in.Status != nil && in.Status.CheckedAt != 0 {
				p.bus.Publish("instance.availability", map[string]any{"id": in.ID, "name": in.Name, "online": st.Online, "error": st.LastError})
			}
			p.bus.Publish("instance.status", map[string]any{"id": in.ID, "status": st})
		}(in)
	}
	wg.Wait()
}

// Probe fetches status for one instance.
func (p *Poller) Probe(ctx context.Context, in *Instance) Status {
	c, err := p.svc.Client(in)
	if err != nil {
		return Status{LastError: err.Error()}
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	start := time.Now()
	s, err := c.Status(cctx)
	lat := time.Since(start).Milliseconds()
	if err != nil {
		return Status{LastError: err.Error(), LatencyMs: lat}
	}
	return Status{Online: true, Version: s.Version, ProtectionEnabled: s.ProtectionEnabled, DNSAddresses: s.DNSAddresses, LatencyMs: lat}
}
