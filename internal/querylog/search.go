package querylog

import (
	"context"
	"strings"
	"time"
)

// Filter narrows searches and statistics.
type Filter struct {
	InstanceIDs []int64  // empty = all
	Client      string   // exact IP or ClientID, or substring of client name
	Domain      string   // matches the domain and its subdomains ("google.com")
	Contains    string   // substring anywhere in the domain (slower)
	Statuses    []string // empty = any
	BlockedOnly bool
	From, To    int64 // unix ms; 0 = unbounded

	// resolved is set by resolve(): the domains matching Contains.
	resolved   bool
	containsIn []string
}

// maxContainsDomains caps how many distinct domains a substring search expands to.
const maxContainsDomains = 2000

// resolve expands a substring (Contains) search into the matching distinct
// domains via the domain dictionary, so the main query can use the index.
func (s *Store) resolve(ctx context.Context, f Filter) (Filter, error) {
	c := strings.TrimSpace(strings.ToLower(f.Contains))
	if c == "" || f.resolved {
		return f, nil
	}
	rows, err := s.st.DB.QueryContext(ctx, `SELECT domain FROM domains WHERE domain LIKE ? ESCAPE '\' ORDER BY last_seen DESC LIMIT ?`,
		"%"+escapeLike(c)+"%", maxContainsDomains)
	if err != nil {
		return f, err
	}
	defer rows.Close()
	f.containsIn = []string{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return f, err
		}
		f.containsIn = append(f.containsIn, ReverseDomain(d))
	}
	f.resolved = true
	return f, rows.Err()
}

// where builds the SQL predicate and arguments for f.
func (f Filter) where() (string, []any) {
	var conds []string
	var args []any
	if len(f.InstanceIDs) > 0 {
		conds = append(conds, "instance_id IN ("+placeholders(len(f.InstanceIDs))+")")
		for _, id := range f.InstanceIDs {
			args = append(args, id)
		}
	}
	if c := strings.TrimSpace(f.Client); c != "" {
		conds = append(conds, "(client = ? OR client_id = ? OR client_name LIKE ? ESCAPE '\\')")
		args = append(args, c, c, "%"+escapeLike(c)+"%")
	}
	if d := normDomain(f.Domain); d != "" {
		rd := ReverseDomain(d)
		// Indexed: exact match or any subdomain, via the reversed-label column.
		conds = append(conds, "(rdomain = ? OR (rdomain >= ? AND rdomain < ?))")
		args = append(args, rd, rd+".", rd+"/") // '/' sorts right after '.'
	}
	if c := strings.TrimSpace(strings.ToLower(f.Contains)); c != "" {
		switch {
		case !f.resolved:
			conds = append(conds, "domain LIKE ? ESCAPE '\\'")
			args = append(args, "%"+escapeLike(c)+"%")
		case len(f.containsIn) == 0:
			conds = append(conds, "0 = 1")
		default:
			conds = append(conds, "rdomain IN ("+placeholders(len(f.containsIn))+")")
			for _, d := range f.containsIn {
				args = append(args, d)
			}
		}
	}
	if len(f.Statuses) > 0 {
		conds = append(conds, "status IN ("+placeholders(len(f.Statuses))+")")
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if f.BlockedOnly {
		conds = append(conds, "blocked = 1")
	}
	if f.From > 0 {
		conds = append(conds, "ts >= ?")
		args = append(args, f.From)
	}
	if f.To > 0 {
		conds = append(conds, "ts < ?")
		args = append(args, f.To)
	}
	if len(conds) == 0 {
		return "1=1", nil
	}
	return strings.Join(conds, " AND "), args
}

func normDomain(d string) string {
	d = strings.TrimSpace(strings.ToLower(d))
	d = strings.TrimPrefix(d, "*.")
	d = strings.TrimSuffix(d, ".")
	if strings.ContainsAny(d, " %*/") {
		return ""
	}
	return d
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// Cursor is a keyset pagination position (newest first).
type Cursor struct {
	TS int64 `json:"ts"`
	ID int64 `json:"id"`
}

// Page is a page of search results.
type Page struct {
	Rows []Row   `json:"rows"`
	Next *Cursor `json:"next,omitempty"`
}

const rowCols = `id, instance_id, ts, client, client_name, client_id, domain, qtype, status, blocked, reason, filter_id, rule, service, upstream, elapsed_ms, cached, proto, answer`

// Search returns rows matching f, newest first.
func (s *Store) Search(ctx context.Context, f Filter, after *Cursor, limit int) (*Page, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	f, err := s.resolve(ctx, f)
	if err != nil {
		return nil, err
	}
	where, args := f.where()
	if after != nil {
		where += " AND (ts < ? OR (ts = ? AND id < ?))"
		args = append(args, after.TS, after.TS, after.ID)
	}
	args = append(args, limit+1)
	rows, err := s.st.DB.QueryContext(ctx, `SELECT `+rowCols+` FROM query_log WHERE `+where+` ORDER BY ts DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &Page{Rows: []Row{}}
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.ID, &r.InstanceID, &r.TS, &r.Client, &r.ClientName, &r.ClientID, &r.Domain, &r.QType, &r.Status, &r.Blocked,
			&r.Reason, &r.FilterID, &r.Rule, &r.Service, &r.Upstream, &r.ElapsedMs, &r.Cached, &r.Proto, &r.Answer); err != nil {
			return nil, err
		}
		out.Rows = append(out.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out.Rows) > limit {
		out.Rows = out.Rows[:limit]
		last := out.Rows[limit-1]
		out.Next = &Cursor{TS: last.TS, ID: last.ID}
	}
	return out, nil
}

// Totals are aggregate counts for a range.
type Totals struct {
	Queries     int64   `json:"queries"`
	Blocked     int64   `json:"blocked"`
	Cached      int64   `json:"cached"`
	AvgElapsed  float64 `json:"avg_elapsed_ms"`
	BlockedPct  float64 `json:"blocked_pct"`
	CachedPct   float64 `json:"cached_pct"`
	Clients     int64   `json:"clients"`
	FromRollups bool    `json:"from_rollups"`
}

// Point is one timeseries bucket.
type Point struct {
	T       int64 `json:"t"` // unix ms, bucket start
	Queries int64 `json:"queries"`
	Blocked int64 `json:"blocked"`
}

// InstanceStat is per-instance traffic for a range.
type InstanceStat struct {
	InstanceID int64   `json:"instance_id"`
	Queries    int64   `json:"queries"`
	Blocked    int64   `json:"blocked"`
	AvgElapsed float64 `json:"avg_elapsed_ms"`
}

// TopItem is an entry in a top-N list.
type TopItem struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
	Count int64  `json:"count"`
}

// Summary is the analytics payload for a range.
type Summary struct {
	From        int64          `json:"from"`
	To          int64          `json:"to"`
	BucketMs    int64          `json:"bucket_ms"`
	Totals      Totals         `json:"totals"`
	Series      []Point        `json:"series"`
	Instances   []InstanceStat `json:"instances"`
	TopDomains  []TopItem      `json:"top_domains"`
	TopBlocked  []TopItem      `json:"top_blocked"`
	TopClients  []TopItem      `json:"top_clients"`
	TopStatuses []TopItem      `json:"statuses"`
	Hourly      [][]int64      `json:"heatmap"` // [weekday 0-6][hour 0-23] queries
}

// Summarize computes analytics for f over [f.From, f.To). Totals, series and
// per-instance numbers use hourly rollups for ranges of a day or more (so they
// survive raw-log retention) unless a client/domain/status filter requires raw rows.
func (s *Store) Summarize(ctx context.Context, f Filter, topN int) (*Summary, error) {
	if f.To == 0 {
		f.To = time.Now().UnixMilli()
	}
	if f.From == 0 {
		f.From = f.To - 24*3600*1000
	}
	if topN <= 0 {
		topN = 10
	}
	span := f.To - f.From
	sum := &Summary{From: f.From, To: f.To}
	rawOnly := f.Client != "" || f.Domain != "" || f.Contains != "" || len(f.Statuses) > 0 || f.BlockedOnly
	useRollups := !rawOnly && span >= 24*3600*1000

	switch {
	case span <= 3*3600*1000:
		sum.BucketMs = 5 * 60 * 1000
	case span <= 24*3600*1000:
		sum.BucketMs = 15 * 60 * 1000
	case span <= 8*24*3600*1000:
		sum.BucketMs = 3600 * 1000
	default:
		sum.BucketMs = 6 * 3600 * 1000
	}

	f, err := s.resolve(ctx, f)
	if err != nil {
		return nil, err
	}
	if useRollups {
		if err := s.summarizeRollups(ctx, f, sum, topN); err != nil {
			return nil, err
		}
		finish(sum, f)
		return sum, nil
	}
	where, args := f.where()
	{
		// Totals.
		var avg *float64
		if err := s.st.DB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(blocked),0), COALESCE(SUM(cached),0), AVG(elapsed_ms)
			FROM query_log WHERE `+where, args...).Scan(&sum.Totals.Queries, &sum.Totals.Blocked, &sum.Totals.Cached, &avg); err != nil {
			return nil, err
		}
		if avg != nil {
			sum.Totals.AvgElapsed = *avg
		}
		// Series.
		rows, err := s.st.DB.QueryContext(ctx, `SELECT (ts / ?) * ?, COUNT(*), SUM(blocked) FROM query_log WHERE `+where+` GROUP BY 1 ORDER BY 1`,
			append([]any{sum.BucketMs, sum.BucketMs}, args...)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var p Point
			if err := rows.Scan(&p.T, &p.Queries, &p.Blocked); err != nil {
				rows.Close()
				return nil, err
			}
			sum.Series = append(sum.Series, p)
		}
		rows.Close()
		// Per instance.
		irows, err := s.st.DB.QueryContext(ctx, `SELECT instance_id, COUNT(*), SUM(blocked), AVG(elapsed_ms) FROM query_log WHERE `+where+` GROUP BY instance_id ORDER BY 2 DESC`, args...)
		if err != nil {
			return nil, err
		}
		for irows.Next() {
			var is InstanceStat
			if err := irows.Scan(&is.InstanceID, &is.Queries, &is.Blocked, &is.AvgElapsed); err != nil {
				irows.Close()
				return nil, err
			}
			sum.Instances = append(sum.Instances, is)
		}
		irows.Close()
	}
	finish(sum, f)

	// Raw path: top lists, distinct clients and heatmap from raw rows.
	if err = s.st.DB.QueryRowContext(ctx, `SELECT COUNT(DISTINCT client) FROM query_log WHERE `+where, args...).Scan(&sum.Totals.Clients); err != nil {
		return nil, err
	}
	if sum.TopDomains, err = s.top(ctx, `domain, ''`, where, args, topN); err != nil {
		return nil, err
	}
	bf := f
	bf.BlockedOnly = true
	bwhere, bargs := bf.where()
	if sum.TopBlocked, err = s.top(ctx, `domain, ''`, bwhere, bargs, topN); err != nil {
		return nil, err
	}
	if sum.TopClients, err = s.top(ctx, `client, MAX(client_name)`, where, args, topN); err != nil {
		return nil, err
	}
	if sum.TopStatuses, err = s.top(ctx, `status, ''`, where, args, 20); err != nil {
		return nil, err
	}
	if sum.Hourly, err = s.heatmap(ctx, where, args); err != nil {
		return nil, err
	}
	return sum, nil
}

func (s *Store) rollupSummary(ctx context.Context, f Filter, sum *Summary) error {
	conds := []string{"hour >= ?", "hour < ?"}
	args := []any{f.From / 1000 / 3600 * 3600, f.To / 1000}
	if len(f.InstanceIDs) > 0 {
		conds = append(conds, "instance_id IN ("+placeholders(len(f.InstanceIDs))+")")
		for _, id := range f.InstanceIDs {
			args = append(args, id)
		}
	}
	where := strings.Join(conds, " AND ")
	var esum float64
	if err := s.st.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(queries),0), COALESCE(SUM(blocked),0), COALESCE(SUM(cached),0), COALESCE(SUM(elapsed_sum),0)
		FROM stats_hourly WHERE `+where, args...).Scan(&sum.Totals.Queries, &sum.Totals.Blocked, &sum.Totals.Cached, &esum); err != nil {
		return err
	}
	if sum.Totals.Queries > 0 {
		sum.Totals.AvgElapsed = esum / float64(sum.Totals.Queries)
	}
	sum.Totals.FromRollups = true
	bucketS := sum.BucketMs / 1000
	rows, err := s.st.DB.QueryContext(ctx, `SELECT (hour / ?) * ? * 1000, SUM(queries), SUM(blocked) FROM stats_hourly WHERE `+where+` GROUP BY 1 ORDER BY 1`,
		append([]any{bucketS, bucketS}, args...)...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.T, &p.Queries, &p.Blocked); err != nil {
			rows.Close()
			return err
		}
		sum.Series = append(sum.Series, p)
	}
	rows.Close()
	irows, err := s.st.DB.QueryContext(ctx, `SELECT instance_id, SUM(queries), SUM(blocked), CASE WHEN SUM(queries) > 0 THEN SUM(elapsed_sum) / SUM(queries) ELSE 0 END
		FROM stats_hourly WHERE `+where+` GROUP BY instance_id ORDER BY 2 DESC`, args...)
	if err != nil {
		return err
	}
	defer irows.Close()
	for irows.Next() {
		var is InstanceStat
		if err := irows.Scan(&is.InstanceID, &is.Queries, &is.Blocked, &is.AvgElapsed); err != nil {
			return err
		}
		sum.Instances = append(sum.Instances, is)
	}
	return irows.Err()
}

func (s *Store) top(ctx context.Context, cols, where string, args []any, n int) ([]TopItem, error) {
	key := strings.SplitN(cols, ",", 2)[0]
	rows, err := s.st.DB.QueryContext(ctx, `SELECT `+cols+`, COUNT(*) AS c FROM query_log WHERE `+where+` GROUP BY `+key+` ORDER BY c DESC LIMIT ?`,
		append(append([]any{}, args...), n)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TopItem{}
	for rows.Next() {
		var t TopItem
		if err := rows.Scan(&t.Key, &t.Label, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// heatmap returns query counts by local weekday (0=Sunday) and hour, using
// the server's time zone.
func (s *Store) heatmap(ctx context.Context, where string, args []any) ([][]int64, error) {
	_, off := time.Now().Zone()
	rows, err := s.st.DB.QueryContext(ctx, `SELECT CAST(strftime('%w', (ts / 1000) + ?, 'unixepoch') AS INTEGER),
		CAST(strftime('%H', (ts / 1000) + ?, 'unixepoch') AS INTEGER), COUNT(*) FROM query_log WHERE `+where+` GROUP BY 1, 2`,
		append([]any{off, off}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grid := make([][]int64, 7)
	for i := range grid {
		grid[i] = make([]int64, 24)
	}
	for rows.Next() {
		var d, h int
		var c int64
		if err := rows.Scan(&d, &h, &c); err != nil {
			return nil, err
		}
		if d >= 0 && d < 7 && h >= 0 && h < 24 {
			grid[d][h] = c
		}
	}
	return grid, rows.Err()
}

// finish fills series gaps and derives percentages.
func finish(sum *Summary, f Filter) {
	sum.Series = fillSeries(sum.Series, f.From, f.To, sum.BucketMs)
	if sum.Totals.Queries > 0 {
		sum.Totals.BlockedPct = float64(sum.Totals.Blocked) * 100 / float64(sum.Totals.Queries)
		sum.Totals.CachedPct = float64(sum.Totals.Cached) * 100 / float64(sum.Totals.Queries)
	}
	if sum.Instances == nil {
		sum.Instances = []InstanceStat{}
	}
}

// fillSeries inserts zero buckets so charts render gaps correctly.
func fillSeries(in []Point, from, to, bucket int64) []Point {
	byT := make(map[int64]Point, len(in))
	for _, p := range in {
		byT[p.T] = p
	}
	out := []Point{}
	for t := from / bucket * bucket; t < to; t += bucket {
		if p, ok := byT[t]; ok {
			out = append(out, p)
		} else {
			out = append(out, Point{T: t})
		}
	}
	return out
}
