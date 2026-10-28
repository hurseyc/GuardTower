package family

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
)

// Filter selects sessions.
type Filter struct {
	PersonID   int64  // 0 = everyone; -1 = unassigned devices only
	Device     string // exact device
	Categories []string
	From, To   int64 // unix ms
	Limit      int
}

func (f Filter) where() (string, []any) {
	conds := []string{"e.first_ts >= ?", "e.first_ts < ?"}
	args := []any{f.From, f.To}
	switch {
	case f.PersonID > 0:
		conds = append(conds, "pd.person_id = ?")
		args = append(args, f.PersonID)
	case f.PersonID < 0:
		conds = append(conds, "pd.person_id IS NULL")
	}
	if f.Device != "" {
		conds = append(conds, "e.device = ?")
		args = append(args, f.Device)
	}
	if len(f.Categories) > 0 {
		conds = append(conds, "e.category IN ("+strings.Repeat("?,", len(f.Categories)-1)+"?)")
		for _, c := range f.Categories {
			args = append(args, c)
		}
	}
	return strings.Join(conds, " AND "), args
}

const eventCols = `e.id, e.device, COALESCE(NULLIF(pd.label, ''), e.device_name), COALESCE(pd.person_id, 0), COALESCE(p.name, ''), e.instance_id,
	e.category, e.domain, e.first_ts, e.last_ts, e.count, e.blocked`

const eventJoin = ` FROM family_events e LEFT JOIN person_devices pd ON pd.device = e.device LEFT JOIN people p ON p.id = pd.person_id `

func (s *Service) masking(ctx context.Context) bool {
	return s.setting(ctx, SettingMask, "true") != "false"
}

func (s *Service) bedtime(ctx context.Context) *Clock {
	c, err := ParseClock(s.setting(ctx, SettingBedtime, DefaultBedtime))
	if err != nil {
		return nil
	}
	return c
}

func scanEvent(sc interface{ Scan(...any) error }) (Event, error) {
	var e Event
	err := sc.Scan(&e.ID, &e.Device, &e.DeviceName, &e.PersonID, &e.Person, &e.InstanceID, &e.Category, &e.Domain, &e.FirstTS, &e.LastTS, &e.Count, &e.Blocked)
	return e, err
}

func (s *Service) finish(ctx context.Context, e *Event, mask bool, bed *Clock) {
	if mask && (e.Category == Adult || e.Category == Gambling) {
		e.Domain, e.Masked = Mask(e.Domain), true
	}
	e.LateNight = bed.Contains(time.UnixMilli(e.FirstTS))
	_ = ctx
}

// Events lists sessions, newest first.
func (s *Service) Events(ctx context.Context, f Filter) ([]Event, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 200
	}
	where, args := f.where()
	rows, err := s.st.DB.QueryContext(ctx, `SELECT `+eventCols+eventJoin+`WHERE `+where+` ORDER BY e.first_ts DESC LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mask, bed := s.masking(ctx), s.bedtime(ctx)
	out := []Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		s.finish(ctx, &e, mask, bed)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Reveal returns the unmasked domain of a session.
func (s *Service) Reveal(ctx context.Context, id int64) (*Event, error) {
	e, err := scanEvent(s.st.DB.QueryRowContext(ctx, `SELECT `+eventCols+eventJoin+`WHERE e.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.finish(ctx, &e, false, s.bedtime(ctx))
	return &e, nil
}

// CategoryCount is sessions and hits for one category.
type CategoryCount struct {
	Category string `json:"category"`
	Attempts int64  `json:"attempts"` // sessions
	Hits     int64  `json:"hits"`     // individual queries
	Allowed  int64  `json:"allowed"`  // sessions that were NOT blocked
}

// Summary describes one person (or the unassigned devices) over a range.
type Summary struct {
	PersonID     int64           `json:"person_id"`
	Name         string          `json:"name"`
	Emoji        string          `json:"emoji,omitempty"`
	Color        string          `json:"color,omitempty"`
	Devices      int             `json:"devices"`
	Categories   []CategoryCount `json:"categories"`
	Adult        int64           `json:"adult"`
	LateNight    int64           `json:"late_night"`
	LastAttempt  int64           `json:"last_attempt,omitempty"`
	LastCategory string          `json:"last_category,omitempty"`
}

// Overview is the Family DNS home page.
type Overview struct {
	From       int64     `json:"from"`
	To         int64     `json:"to"`
	People     []Summary `json:"people"`
	Unassigned Summary   `json:"unassigned"`
	Signals    []Signal  `json:"signals"`
	Bedtime    string    `json:"bedtime"`
	Masked     bool      `json:"masked"`
	Totals     struct {
		Adult      int64 `json:"adult"`
		AdultHits  int64 `json:"adult_hits"`
		Allowed    int64 `json:"allowed"`
		LateNight  int64 `json:"late_night"`
		SafeSearch int64 `json:"safe_search"`
	} `json:"totals"`
}

// Overview summarises everyone over [from, to).
func (s *Service) Overview(ctx context.Context, from, to int64) (*Overview, error) {
	ppl, err := s.People(ctx)
	if err != nil {
		return nil, err
	}
	bed := s.bedtime(ctx)
	o := &Overview{From: from, To: to, People: []Summary{}, Signals: []Signal{}, Bedtime: s.setting(ctx, SettingBedtime, DefaultBedtime), Masked: s.masking(ctx)}
	byID := map[int64]*Summary{}
	for _, p := range ppl {
		o.People = append(o.People, Summary{PersonID: p.ID, Name: p.Name, Emoji: p.Emoji, Color: p.Color, Devices: len(p.Devices), Categories: []CategoryCount{}})
	}
	for i := range o.People {
		byID[o.People[i].PersonID] = &o.People[i]
	}
	o.Unassigned = Summary{Name: "Unassigned devices", Categories: []CategoryCount{}}
	rows, err := s.st.DB.QueryContext(ctx, `SELECT COALESCE(pd.person_id, 0), e.category, e.first_ts, e.count, e.blocked`+eventJoin+`WHERE e.first_ts >= ? AND e.first_ts < ?`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type acc struct{ m map[string]*CategoryCount }
	accs := map[int64]*acc{}
	devs := map[string]bool{}
	for rows.Next() {
		var pid, ts, count int64
		var cat string
		var blocked bool
		if err := rows.Scan(&pid, &cat, &ts, &count, &blocked); err != nil {
			return nil, err
		}
		sum := byID[pid]
		if sum == nil {
			sum = &o.Unassigned
			pid = 0
		}
		a := accs[pid]
		if a == nil {
			a = &acc{m: map[string]*CategoryCount{}}
			accs[pid] = a
		}
		cc := a.m[cat]
		if cc == nil {
			cc = &CategoryCount{Category: cat}
			a.m[cat] = cc
		}
		cc.Attempts++
		cc.Hits += count
		if !blocked {
			cc.Allowed++
		}
		if cat == SafeSearch {
			o.Totals.SafeSearch++
			continue
		}
		late := bed.Contains(time.UnixMilli(ts))
		if cat == Adult {
			sum.Adult++
			o.Totals.Adult++
			o.Totals.AdultHits += count
			if !blocked {
				o.Totals.Allowed++
			}
		}
		if late {
			sum.LateNight++
			o.Totals.LateNight++
		}
		if ts > sum.LastAttempt {
			sum.LastAttempt, sum.LastCategory = ts, cat
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for pid, a := range accs {
		sum := byID[pid]
		if sum == nil {
			sum = &o.Unassigned
		}
		for _, c := range Categories {
			if cc := a.m[c]; cc != nil {
				sum.Categories = append(sum.Categories, *cc)
			}
		}
		if cc := a.m[SafeSearch]; cc != nil {
			sum.Categories = append(sum.Categories, *cc)
		}
	}
	_ = devs
	var unassigned int
	_ = s.st.DB.QueryRowContext(ctx, `SELECT COUNT(DISTINCT e.device)`+eventJoin+`WHERE pd.person_id IS NULL AND e.first_ts >= ? AND e.first_ts < ?`, from, to).Scan(&unassigned)
	o.Unassigned.Devices = unassigned
	sort.SliceStable(o.People, func(i, j int) bool { return o.People[i].Adult > o.People[j].Adult })
	if o.Signals, err = s.Signals(ctx, from, to); err != nil {
		return nil, err
	}
	return o, nil
}

// Detail is one person's (or the unassigned devices') page.
type Detail struct {
	Summary
	Devices []DeviceStat `json:"devices"`
	// Heatmap[weekday][hour] counts sessions of the selected categories.
	Heatmap [][]int64   `json:"heatmap"`
	Daily   []DayCount  `json:"daily"`
	Top     []TopDomain `json:"top"`
	Events  []Event     `json:"events"`
	Signals []Signal    `json:"signals"`
}

// DeviceStat is one device's activity.
type DeviceStat struct {
	Device   string `json:"device"`
	Label    string `json:"label"`
	Attempts int64  `json:"attempts"`
	LastSeen int64  `json:"last_seen,omitempty"`
}

// DayCount is sessions per category on one local day.
type DayCount struct {
	Day   int64            `json:"day"` // unix ms of local midnight
	Count map[string]int64 `json:"count"`
}

// TopDomain is a frequently attempted site.
type TopDomain struct {
	EventID  int64  `json:"event_id"` // latest session, for reveal
	Domain   string `json:"domain"`
	Masked   bool   `json:"masked"`
	Category string `json:"category"`
	Attempts int64  `json:"attempts"`
	Hits     int64  `json:"hits"`
	Allowed  int64  `json:"allowed"` // attempts that were not blocked
}

// Detail returns one person's page. personID -1 = unassigned devices.
func (s *Service) Detail(ctx context.Context, personID int64, categories []string, from, to int64) (*Detail, error) {
	o, err := s.Overview(ctx, from, to)
	if err != nil {
		return nil, err
	}
	d := &Detail{Devices: []DeviceStat{}, Daily: []DayCount{}, Top: []TopDomain{}, Signals: []Signal{}}
	if personID < 0 {
		d.Summary = o.Unassigned
	} else {
		found := false
		for _, p := range o.People {
			if p.PersonID == personID {
				d.Summary, found = p, true
			}
		}
		if !found {
			return nil, ErrNotFound
		}
	}
	for _, sg := range o.Signals {
		if (personID > 0 && sg.PersonID == personID) || (personID < 0 && sg.PersonID == 0) {
			d.Signals = append(d.Signals, sg)
		}
	}
	if len(categories) == 0 {
		categories = []string{Adult}
	}
	f := Filter{PersonID: personID, Categories: categories, From: from, To: to, Limit: 300}
	if d.Events, err = s.Events(ctx, f); err != nil {
		return nil, err
	}
	// Devices: the person's assigned devices, plus any device with events.
	if personID > 0 {
		ppl, _ := s.People(ctx)
		for _, p := range ppl {
			if p.ID == personID {
				for _, dv := range p.Devices {
					d.Devices = append(d.Devices, DeviceStat{Device: dv.Device, Label: dv.Label})
				}
			}
		}
	}
	devIdx := map[string]int{}
	for i, dv := range d.Devices {
		devIdx[dv.Device] = i
	}
	d.Heatmap = make([][]int64, 7)
	for i := range d.Heatmap {
		d.Heatmap[i] = make([]int64, 24)
	}
	daily := map[int64]map[string]int64{}
	top := map[string]*TopDomain{}
	where, args := f.where()
	rows, err := s.st.DB.QueryContext(ctx, `SELECT `+eventCols+eventJoin+`WHERE `+where+` ORDER BY e.first_ts DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mask := s.masking(ctx)
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		t := time.UnixMilli(e.FirstTS)
		d.Heatmap[int(t.Weekday())][t.Hour()]++
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).UnixMilli()
		if daily[day] == nil {
			daily[day] = map[string]int64{}
		}
		daily[day][e.Category]++
		i, ok := devIdx[e.Device]
		if !ok {
			i = len(d.Devices)
			devIdx[e.Device] = i
			d.Devices = append(d.Devices, DeviceStat{Device: e.Device, Label: e.DeviceName})
		}
		d.Devices[i].Attempts++
		if e.LastTS > d.Devices[i].LastSeen {
			d.Devices[i].LastSeen = e.LastTS
		}
		k := e.Category + "|" + e.Domain
		td := top[k]
		if td == nil {
			td = &TopDomain{EventID: e.ID, Domain: e.Domain, Category: e.Category}
			if mask && (e.Category == Adult || e.Category == Gambling) {
				td.Domain, td.Masked = Mask(e.Domain), true
			}
			top[k] = td
		}
		td.Attempts++
		td.Hits += e.Count
		if !e.Blocked {
			td.Allowed++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for day := time.UnixMilli(from); day.UnixMilli() < to; day = day.AddDate(0, 0, 1) {
		k := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location()).UnixMilli()
		c := daily[k]
		if c == nil {
			c = map[string]int64{}
		}
		d.Daily = append(d.Daily, DayCount{Day: k, Count: c})
	}
	for _, td := range top {
		d.Top = append(d.Top, *td)
	}
	sort.Slice(d.Top, func(i, j int) bool { return d.Top[i].Attempts > d.Top[j].Attempts })
	if len(d.Top) > 15 {
		d.Top = d.Top[:15]
	}
	return d, nil
}
