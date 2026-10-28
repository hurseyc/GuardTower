// Package collector pulls query logs from every instance into the warehouse.
package collector

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/hurseyc/guardtower/internal/events"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/querylog"
	"github.com/hurseyc/guardtower/internal/store"
)

// Settings keys and defaults.
const (
	SettingRetentionDays       = "retention.querylog_days"
	SettingRollupRetentionDays = "retention.rollup_days"
	DefaultRetentionDays       = 30
	DefaultRollupRetentionDays = 400

	pageSize        = 500
	maxPagesPerRun  = 200   // 100k rows: catches up after long outages without hogging a run
	initialBackfill = 20000 // rows fetched on first contact with an instance
	liveRowsPerPush = 100
)

// Collector polls instances on an interval.
type Collector struct {
	fleet    *fleet.Service
	ql       *querylog.Store
	st       *store.Store
	bus      *events.Bus
	log      *slog.Logger
	interval time.Duration

	mu      sync.Mutex
	backoff map[int64]backoffState
	kick    chan struct{}
}

type backoffState struct {
	failures int
	until    time.Time
}

// New creates a collector.
func New(fl *fleet.Service, ql *querylog.Store, st *store.Store, bus *events.Bus, log *slog.Logger, interval time.Duration) *Collector {
	return &Collector{fleet: fl, ql: ql, st: st, bus: bus, log: log, interval: interval,
		backoff: map[int64]backoffState{}, kick: make(chan struct{}, 1)}
}

// Kick triggers an immediate collection.
func (c *Collector) Kick() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

// Run collects until ctx is cancelled. Retention pruning runs hourly.
func (c *Collector) Run(ctx context.Context) {
	t := time.NewTicker(c.interval)
	defer t.Stop()
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	c.prune(ctx)
	c.collectAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-c.kick:
		case <-prune.C:
			c.prune(ctx)
			continue
		}
		c.collectAll(ctx)
		if _, err := c.ql.BuildTops(ctx, 24); err != nil && ctx.Err() == nil {
			c.log.Error("collector: build hourly rollups", "err", err)
		}
	}
}

func (c *Collector) collectAll(ctx context.Context) {
	insts, err := c.fleet.ListInstances(ctx)
	if err != nil {
		c.log.Error("collector: list instances", "err", err)
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, in := range insts {
		if !in.Enabled || c.inBackoff(in.ID) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(in *fleet.Instance) {
			defer wg.Done()
			defer func() { <-sem }()
			n, err := c.CollectOne(ctx, in)
			c.recordResult(in.ID, err)
			if err != nil && ctx.Err() == nil {
				c.log.Debug("collector: instance failed", "instance", in.Name, "err", err)
			} else if n > 0 {
				c.log.Debug("collector: stored rows", "instance", in.Name, "rows", n)
			}
		}(in)
	}
	wg.Wait()
}

// CollectOne fetches new query-log rows for one instance and returns how
// many were stored.
func (c *Collector) CollectOne(ctx context.Context, in *fleet.Instance) (int, error) {
	state, err := c.ql.GetState(ctx, in.ID)
	if err != nil {
		return 0, err
	}
	cl, err := c.fleet.Client(in)
	if err != nil {
		_ = c.ql.SaveState(ctx, in.ID, state.LastTS, 0, err)
		return 0, err
	}
	cursor := ""
	var batch []querylog.Row
	complete := false // reached rows we already had (or the start of the log)
	for page := 0; page < maxPagesPerRun; page++ {
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		p, err := cl.QueryLog(pctx, cursor, pageSize)
		cancel()
		if err != nil {
			// Store what we fetched but keep the old cursor: the unfetched rows
			// between here and LastTS are picked up (and deduplicated) next run.
			stored, ierr := c.store(ctx, in.ID, batch)
			if ierr != nil {
				return 0, ierr
			}
			_ = c.ql.SaveState(ctx, in.ID, state.LastTS, int64(len(stored)), err)
			return len(stored), err
		}
		for _, e := range p.Data {
			// Rows with ts == LastTS may be partly stored; dedupe handles the overlap.
			if state.LastTS > 0 && e.Time.UnixMilli() < state.LastTS {
				complete = true
				break
			}
			batch = append(batch, querylog.FromEntry(in.ID, e))
		}
		if complete || len(p.Data) < pageSize || p.Oldest == "" {
			complete = true
			break
		}
		if state.LastTS == 0 && len(batch) >= initialBackfill {
			complete = true // first contact: a recent window is enough
			break
		}
		cursor = p.Oldest
	}
	if !complete {
		c.log.Warn("collector: query-log backlog larger than one run; older rows skipped", "instance", in.Name, "rows", len(batch))
	}
	stored, err := c.store(ctx, in.ID, batch)
	if err != nil {
		return 0, err
	}
	if err := c.ql.SaveState(ctx, in.ID, maxTS(batch, state.LastTS), int64(len(stored)), nil); err != nil {
		return len(stored), err
	}
	return len(stored), nil
}

func (c *Collector) store(ctx context.Context, instanceID int64, batch []querylog.Row) ([]querylog.Row, error) {
	if len(batch) == 0 {
		return nil, nil
	}
	var stored []querylog.Row
	// Chunk inserts to keep transactions (and the write lock) short.
	for i := 0; i < len(batch); i += 2000 {
		end := i + 2000
		if end > len(batch) {
			end = len(batch)
		}
		ins, err := c.ql.Insert(ctx, batch[i:end])
		if err != nil {
			return stored, err
		}
		stored = append(stored, ins...)
	}
	if len(stored) > 0 {
		live := stored
		sort.Slice(live, func(i, j int) bool { return live[i].TS > live[j].TS })
		if len(live) > liveRowsPerPush {
			live = live[:liveRowsPerPush]
		}
		c.bus.Publish("querylog.rows", map[string]any{"instance_id": instanceID, "count": len(stored), "rows": live})
	}
	return stored, nil
}

func maxTS(rows []querylog.Row, cur int64) int64 {
	for _, r := range rows {
		if r.TS > cur {
			cur = r.TS
		}
	}
	return cur
}

func (c *Collector) inBackoff(id int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Before(c.backoff[id].until)
}

func (c *Collector) recordResult(id int64, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		delete(c.backoff, id)
		return
	}
	b := c.backoff[id]
	b.failures++
	d := c.interval * time.Duration(1<<min(b.failures, 6)) // up to 64× the interval
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	b.until = time.Now().Add(d)
	c.backoff[id] = b
}

func (c *Collector) prune(ctx context.Context) {
	raw := c.intSetting(ctx, SettingRetentionDays, DefaultRetentionDays)
	roll := c.intSetting(ctx, SettingRollupRetentionDays, DefaultRollupRetentionDays)
	n, err := c.ql.Prune(ctx, raw, roll)
	if err != nil {
		c.log.Error("collector: prune", "err", err)
		return
	}
	if n > 0 {
		c.log.Info("pruned old query-log rows", "rows", n, "retention_days", raw)
	}
}

func (c *Collector) intSetting(ctx context.Context, key string, def int) int {
	v, err := c.st.GetSetting(ctx, key, "")
	if err != nil || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}
