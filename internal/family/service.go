package family

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hurseyc/guardtower/internal/events"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/notify"
	"github.com/hurseyc/guardtower/internal/querylog"
	"github.com/hurseyc/guardtower/internal/store"
)

// Settings.
const (
	SettingRetentionDays = "retention.family_days"
	SettingBedtime       = "family.bedtime"      // "22:00-06:00", "" = none
	SettingMask          = "family.mask_domains" // "true" | "false"
	SettingAlerts        = "family.alerts"       // off | adult | all
	SettingQuietHours    = "family.quiet_hours"  // "07:00-08:00", "" = none
	SettingDigest        = "family.digest"       // "off" | "sun 18:00"

	DefaultRetentionDays = 365
	DefaultBedtime       = "22:00-06:00"

	settingCursor     = "family.cursor"
	settingLastDigest = "family.last_digest"

	// SessionGap merges hits on the same site closer together than this.
	SessionGap = 5 * time.Minute
)

// Service is the family DNS engine.
type Service struct {
	st     *store.Store
	fleet  *fleet.Service
	bus    *events.Bus
	notify *notify.Service
	log    *slog.Logger
	// Now is for tests.
	Now func() time.Time

	mu       sync.Mutex
	cooldown map[string]time.Time // device|category -> last alert
	protect  map[int64]protectState
}

// New creates the service.
func New(st *store.Store, fl *fleet.Service, bus *events.Bus, n *notify.Service, log *slog.Logger) *Service {
	return &Service{st: st, fleet: fl, bus: bus, notify: n, log: log, Now: time.Now, cooldown: map[string]time.Time{}, protect: map[int64]protectState{}}
}

func (s *Service) setting(ctx context.Context, key, def string) string {
	v, err := s.st.GetSetting(ctx, key, def)
	if err != nil {
		return def
	}
	return v
}

// ---- filter list categories ----

// SyncLists records which filter lists each online instance has, so
// filter_id in the query log can be mapped to a list URL and category.
func (s *Service) SyncLists(ctx context.Context) {
	ins, err := s.fleet.ListInstances(ctx)
	if err != nil {
		return
	}
	for _, in := range ins {
		if !in.Enabled || in.Status == nil || !in.Status.Online {
			continue
		}
		c, err := s.fleet.Client(in)
		if err != nil {
			continue
		}
		var st struct {
			Filters []struct {
				ID   int64  `json:"id"`
				URL  string `json:"url"`
				Name string `json:"name"`
			} `json:"filters"`
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = c.Get(cctx, "filtering/status", &st)
		cancel()
		if err != nil {
			continue
		}
		for _, f := range st.Filters {
			_, _ = s.st.DB.ExecContext(ctx, `INSERT INTO instance_lists (instance_id, filter_id, url, name) VALUES (?, ?, ?, ?)
				ON CONFLICT (instance_id, filter_id) DO UPDATE SET url = excluded.url, name = excluded.name`, in.ID, f.ID, f.URL, f.Name)
		}
	}
}

// ListInfo is one filter list and its category.
type ListInfo struct {
	URL       string  `json:"url"`
	Name      string  `json:"name"`
	Category  string  `json:"category"` // effective
	Explicit  bool    `json:"explicit"` // set by a user rather than guessed
	Instances []int64 `json:"instances"`
}

// Lists returns every known filter list with its category.
func (s *Service) Lists(ctx context.Context) ([]ListInfo, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT l.url, MAX(l.name), GROUP_CONCAT(l.instance_id), COALESCE(c.category, '')
		FROM instance_lists l LEFT JOIN list_categories c ON c.url = l.url GROUP BY l.url ORDER BY MAX(l.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ListInfo{}
	for rows.Next() {
		var li ListInfo
		var ids, explicit string
		if err := rows.Scan(&li.URL, &li.Name, &ids, &explicit); err != nil {
			return nil, err
		}
		for _, p := range strings.Split(ids, ",") {
			if id, err := strconv.ParseInt(p, 10, 64); err == nil {
				li.Instances = append(li.Instances, id)
			}
		}
		li.Category, li.Explicit = explicit, explicit != ""
		if !li.Explicit {
			if li.Category = GuessListCategory(li.URL, li.Name); li.Category == "" {
				li.Category = Ads
			}
		}
		out = append(out, li)
	}
	return out, rows.Err()
}

// SetListCategory overrides a list's category ("" = back to the guess).
func (s *Service) SetListCategory(ctx context.Context, url, category string) error {
	if category == "" {
		_, err := s.st.DB.ExecContext(ctx, `DELETE FROM list_categories WHERE url = ?`, url)
		return err
	}
	ok := false
	for _, c := range Categories {
		ok = ok || c == category
	}
	if !ok {
		return &BadInput{"unknown category " + category}
	}
	_, err := s.st.DB.ExecContext(ctx, `INSERT INTO list_categories (url, category) VALUES (?, ?) ON CONFLICT (url) DO UPDATE SET category = excluded.category`, url, category)
	return err
}

func (s *Service) listLookup(ctx context.Context) (ListLookup, error) {
	lists, err := s.Lists(ctx)
	if err != nil {
		return nil, err
	}
	byURL := map[string]string{}
	for _, l := range lists {
		byURL[l.URL] = l.Category
	}
	rows, err := s.st.DB.QueryContext(ctx, `SELECT instance_id, filter_id, url FROM instance_lists`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[[2]int64]string{}
	for rows.Next() {
		var i, f int64
		var u string
		if err := rows.Scan(&i, &f, &u); err != nil {
			return nil, err
		}
		m[[2]int64{i, f}] = byURL[u]
	}
	return func(instanceID, filterID int64) string { return m[[2]int64{instanceID, filterID}] }, rows.Err()
}

// BadInput is a validation error.
type BadInput struct{ Msg string }

func (e *BadInput) Error() string { return e.Msg }

// ErrNotFound is returned for unknown people or events.
var ErrNotFound = errors.New("not found")

// ---- session builder ----

// Event is one session.
type Event struct {
	ID         int64  `json:"id"`
	Device     string `json:"device"`
	DeviceName string `json:"device_name"`
	PersonID   int64  `json:"person_id,omitempty"`
	Person     string `json:"person,omitempty"`
	InstanceID int64  `json:"instance_id"`
	Category   string `json:"category"`
	Domain     string `json:"domain"` // masked unless revealed
	Masked     bool   `json:"masked"`
	FirstTS    int64  `json:"first_ts"`
	LastTS     int64  `json:"last_ts"`
	Count      int64  `json:"count"`
	Blocked    bool   `json:"blocked"`
	LateNight  bool   `json:"late_night"`
}

type openSession struct {
	id              int64
	first, last     int64
	count           int64
	blocked, exists bool
}

// Build turns new query-log rows into sessions and returns the sessions it
// created (not those it extended).
func (s *Service) Build(ctx context.Context) ([]Event, error) {
	lookup, err := s.listLookup(ctx)
	if err != nil {
		return nil, err
	}
	cursor, _ := strconv.ParseInt(s.setting(ctx, settingCursor, "0"), 10, 64)
	if cursor == 0 {
		// First run: start from the last week rather than all history.
		var minID sql.NullInt64
		_ = s.st.DB.QueryRowContext(ctx, `SELECT MIN(id) - 1 FROM query_log WHERE ts >= ?`, s.Now().Add(-7*24*time.Hour).UnixMilli()).Scan(&minID)
		cursor = minID.Int64
	}
	var created []Event
	for {
		n, last, evs, err := s.buildBatch(ctx, cursor, lookup)
		if err != nil {
			return created, err
		}
		created = append(created, evs...)
		if last > cursor {
			cursor = last
			if err := s.st.SetSetting(ctx, settingCursor, strconv.FormatInt(cursor, 10)); err != nil {
				return created, err
			}
		}
		if n < batchSize || ctx.Err() != nil {
			return created, nil
		}
	}
}

const batchSize = 5000

func (s *Service) buildBatch(ctx context.Context, after int64, lookup ListLookup) (int, int64, []Event, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT id, instance_id, ts, client, client_name, client_id, domain, status, blocked, filter_id, service
		FROM query_log WHERE id > ? ORDER BY id LIMIT ?`, after, batchSize)
	if err != nil {
		return 0, after, nil, err
	}
	type hit struct {
		r      querylog.Row
		cat    string
		device string
	}
	var hits []hit
	n, last := 0, after
	for rows.Next() {
		var r querylog.Row
		if err := rows.Scan(&r.ID, &r.InstanceID, &r.TS, &r.Client, &r.ClientName, &r.ClientID, &r.Domain, &r.Status, &r.Blocked, &r.FilterID, &r.Service); err != nil {
			rows.Close()
			return n, last, nil, err
		}
		n++
		last = r.ID
		cat := Categorize(&r, lookup)
		if cat == "" || cat == Ads {
			continue
		}
		dev := r.ClientID
		if dev == "" {
			dev = r.Client
		}
		hits = append(hits, hit{r: r, cat: cat, device: dev})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return n, last, nil, err
	}
	if len(hits) == 0 {
		return n, last, nil, nil
	}

	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return n, after, nil, err
	}
	defer tx.Rollback()
	// Sessions touched in this batch, per device/category/site. Rows are not
	// strictly in time order (several instances), so a hit joins whichever
	// session it falls within, not just the latest.
	open := map[string][]*openSession{}
	var created []Event
	gap := SessionGap.Milliseconds()
	for _, h := range hits {
		dom := BaseDomain(h.r.Domain)
		key := h.device + "\x00" + h.cat + "\x00" + dom
		ts := h.r.TS
		var os *openSession
		for _, o := range open[key] {
			if ts <= o.last+gap && ts >= o.first-gap {
				os = o
				break
			}
		}
		if os == nil {
			o := &openSession{}
			err := tx.QueryRowContext(ctx, `SELECT id, first_ts, last_ts, count, blocked FROM family_events WHERE device = ? AND category = ? AND domain = ?
				AND last_ts >= ? AND first_ts <= ? ORDER BY last_ts DESC LIMIT 1`, h.device, h.cat, dom, ts-gap, ts+gap).Scan(&o.id, &o.first, &o.last, &o.count, &o.blocked)
			switch {
			case errors.Is(err, sql.ErrNoRows):
			case err != nil:
				return n, after, nil, err
			default:
				o.exists = true
				os = o
				open[key] = append(open[key], o)
			}
		}
		if os != nil {
			if ts > os.last {
				os.last = ts
			}
			if ts < os.first {
				os.first = ts
			}
			os.count++
			os.blocked = os.blocked || h.r.Blocked
			if _, err := tx.ExecContext(ctx, `UPDATE family_events SET first_ts = ?, last_ts = ?, count = ?, blocked = ? WHERE id = ?`,
				os.first, os.last, os.count, os.blocked, os.id); err != nil {
				return n, after, nil, err
			}
			continue
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO family_events (device, device_name, instance_id, category, domain, first_ts, last_ts, count, blocked)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)`, h.device, h.r.ClientName, h.r.InstanceID, h.cat, dom, ts, ts, h.r.Blocked)
		if err != nil {
			return n, after, nil, err
		}
		id, _ := res.LastInsertId()
		open[key] = append(open[key], &openSession{id: id, first: ts, last: ts, count: 1, blocked: h.r.Blocked, exists: true})
		created = append(created, Event{ID: id, Device: h.device, DeviceName: h.r.ClientName, InstanceID: h.r.InstanceID, Category: h.cat,
			Domain: dom, FirstTS: ts, LastTS: ts, Count: 1, Blocked: h.r.Blocked})
	}
	if err := tx.Commit(); err != nil {
		return n, after, nil, err
	}
	return n, last, created, nil
}

// Prune drops sessions older than the family retention.
func (s *Service) Prune(ctx context.Context) {
	days, err := strconv.Atoi(s.setting(ctx, SettingRetentionDays, strconv.Itoa(DefaultRetentionDays)))
	if err != nil || days < 1 {
		days = DefaultRetentionDays
	}
	_, _ = s.st.DB.ExecContext(ctx, `DELETE FROM family_events WHERE last_ts < ?`, s.Now().Add(-time.Duration(days)*24*time.Hour).UnixMilli())
}

// Run builds sessions every interval, sends alerts and the weekly digest,
// syncs filter lists hourly and prunes daily, until ctx ends.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	s.SyncLists(ctx)
	t := time.NewTicker(every)
	defer t.Stop()
	lastSync, lastPrune := time.Now(), time.Time{}
	for {
		created, err := s.Build(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.Error("family: build sessions", "err", err)
		}
		if len(created) > 0 {
			s.alert(ctx, created)
		}
		s.maybeDigest(ctx)
		if time.Since(lastSync) > time.Hour {
			s.SyncLists(ctx)
			lastSync = time.Now()
		}
		if time.Since(lastPrune) > 24*time.Hour {
			s.Prune(ctx)
			lastPrune = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ---- helpers ----

// Clock is a daily time window such as 22:00-06:00 (may wrap midnight).
type Clock struct{ from, to int }

// ParseClock parses "HH:MM-HH:MM"; "" is no window.
func ParseClock(s string) (*Clock, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return nil, fmt.Errorf("window %q must look like 22:00-06:00", s)
	}
	p := func(x string) (int, error) {
		h, m, ok := strings.Cut(strings.TrimSpace(x), ":")
		hh, e1 := strconv.Atoi(h)
		mm, e2 := strconv.Atoi(m)
		if !ok || e1 != nil || e2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
			return 0, fmt.Errorf("window %q must look like 22:00-06:00", s)
		}
		return hh*60 + mm, nil
	}
	f, err := p(a)
	if err != nil {
		return nil, err
	}
	t, err := p(b)
	if err != nil {
		return nil, err
	}
	if f == t {
		return nil, fmt.Errorf("window %q is empty", s)
	}
	return &Clock{f, t}, nil
}

// Contains reports whether t's local time of day is inside the window.
func (c *Clock) Contains(t time.Time) bool {
	if c == nil {
		return false
	}
	m := t.Hour()*60 + t.Minute()
	if c.from < c.to {
		return m >= c.from && m < c.to
	}
	return m >= c.from || m < c.to
}
