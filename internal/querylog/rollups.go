package querylog

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	topsPerHour     = 100
	settingTopsDone = "rollup.tops_done_until" // last hour (unix s) whose tops are built
)

// BuildTops computes per-hour top-N rollups for every complete hour that
// hasn't been rolled up yet. It is cheap to call often: each hour is only
// processed once. maxHours bounds the work per call.
func (s *Store) BuildTops(ctx context.Context, maxHours int) (int, error) {
	done, err := s.topsDoneUntil(ctx)
	if err != nil {
		return 0, err
	}
	current := time.Now().Unix() / 3600 * 3600
	next := done + 3600
	if done == 0 {
		// First run: start at the oldest raw row.
		var minTS *int64
		if err := s.st.DB.QueryRowContext(ctx, `SELECT MIN(ts) FROM query_log`).Scan(&minTS); err != nil {
			return 0, err
		}
		if minTS == nil {
			return 0, nil
		}
		next = *minTS / 1000 / 3600 * 3600
	}
	n := 0
	for h := next; h < current && n < maxHours; h += 3600 {
		if err := s.buildHour(ctx, h); err != nil {
			return n, err
		}
		if err := s.st.SetSetting(ctx, settingTopsDone, strconv.FormatInt(h, 10)); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Store) topsDoneUntil(ctx context.Context) (int64, error) {
	v, err := s.st.GetSetting(ctx, settingTopsDone, "0")
	if err != nil {
		return 0, err
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return n, nil
}

func (s *Store) buildHour(ctx context.Context, hour int64) error {
	from, to := hour*1000, (hour+3600)*1000
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM stats_hourly_top WHERE hour = ?`, hour); err != nil {
		return err
	}
	for _, q := range []struct{ kind, key, label, where string }{
		{"domain", "domain", "''", ""},
		{"blocked", "domain", "''", " AND blocked = 1"},
		{"client", "client", "MAX(client_name)", ""},
	} {
		_, err := tx.ExecContext(ctx, `INSERT INTO stats_hourly_top (hour, instance_id, kind, key, label, count)
			SELECT ?, instance_id, ?, k, l, c FROM (
				SELECT instance_id, `+q.key+` AS k, `+q.label+` AS l, COUNT(*) AS c,
					ROW_NUMBER() OVER (PARTITION BY instance_id ORDER BY COUNT(*) DESC) AS rn
				FROM query_log WHERE ts >= ? AND ts < ?`+q.where+` GROUP BY instance_id, `+q.key+`
			) WHERE rn <= ?`, hour, q.kind, from, to, topsPerHour)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// summarizeRollups fills totals, series, instances, statuses and the heatmap
// from stats_hourly, and top lists from stats_hourly_top plus raw rows for
// hours that aren't rolled up yet.
func (s *Store) summarizeRollups(ctx context.Context, f Filter, sum *Summary, topN int) error {
	if err := s.rollupSummary(ctx, f, sum); err != nil {
		return err
	}
	fromH, toH := f.From/1000/3600*3600, f.To/1000
	instWhere, instArgs := "", []any{}
	if len(f.InstanceIDs) > 0 {
		instWhere = " AND instance_id IN (" + placeholders(len(f.InstanceIDs)) + ")"
		for _, id := range f.InstanceIDs {
			instArgs = append(instArgs, id)
		}
	}
	// Statuses and heatmap.
	var st [7]int64
	args := append([]any{fromH, toH}, instArgs...)
	if err := s.st.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(allowed),0), COALESCE(SUM(allowlisted),0), COALESCE(SUM(filtered),0),
		COALESCE(SUM(safebrowsing),0), COALESCE(SUM(parental),0), COALESCE(SUM(safesearch),0), COALESCE(SUM(rewritten),0)
		FROM stats_hourly WHERE hour >= ? AND hour < ?`+instWhere, args...).
		Scan(&st[0], &st[1], &st[2], &st[3], &st[4], &st[5], &st[6]); err != nil {
		return err
	}
	sum.TopStatuses = []TopItem{}
	for i, name := range Statuses {
		if st[i] > 0 {
			sum.TopStatuses = append(sum.TopStatuses, TopItem{Key: name, Count: st[i]})
		}
	}
	sort.Slice(sum.TopStatuses, func(i, j int) bool { return sum.TopStatuses[i].Count > sum.TopStatuses[j].Count })

	_, off := time.Now().Zone()
	rows, err := s.st.DB.QueryContext(ctx, `SELECT CAST(strftime('%w', hour + ?, 'unixepoch') AS INTEGER), CAST(strftime('%H', hour + ?, 'unixepoch') AS INTEGER), SUM(queries)
		FROM stats_hourly WHERE hour >= ? AND hour < ?`+instWhere+` GROUP BY 1, 2`, append([]any{off, off}, args...)...)
	if err != nil {
		return err
	}
	sum.Hourly = emptyGrid()
	for rows.Next() {
		var d, h int
		var c int64
		if err := rows.Scan(&d, &h, &c); err != nil {
			rows.Close()
			return err
		}
		if d >= 0 && d < 7 && h >= 0 && h < 24 {
			sum.Hourly[d][h] = c
		}
	}
	rows.Close()

	// Top lists: rolled-up hours + the raw tail that isn't rolled up yet.
	done, err := s.topsDoneUntil(ctx)
	if err != nil {
		return err
	}
	tailFrom := f.From
	if done > 0 && (done+3600)*1000 > tailFrom {
		tailFrom = (done + 3600) * 1000
	}
	merged := map[string]map[string]*TopItem{"domain": {}, "blocked": {}, "client": {}}
	add := func(kind, key, label string, c int64) {
		m := merged[kind]
		it := m[key]
		if it == nil {
			it = &TopItem{Key: key}
			m[key] = it
		}
		it.Count += c
		if label != "" {
			it.Label = label
		}
	}
	if done > 0 && done >= fromH {
		trows, err := s.st.DB.QueryContext(ctx, `SELECT kind, key, MAX(label), SUM(count) FROM stats_hourly_top
			WHERE hour >= ? AND hour <= ?`+instWhere+` GROUP BY kind, key`, append([]any{fromH, done}, instArgs...)...)
		if err != nil {
			return err
		}
		for trows.Next() {
			var kind, key, label string
			var c int64
			if err := trows.Scan(&kind, &key, &label, &c); err != nil {
				trows.Close()
				return err
			}
			add(kind, key, label, c)
		}
		trows.Close()
	}
	if tailFrom < f.To {
		tail := f
		tail.From = tailFrom
		where, targs := tail.where()
		for _, q := range []struct{ kind, cols, extra string }{
			{"domain", "domain, ''", ""},
			{"blocked", "domain, ''", " AND blocked = 1"},
			{"client", "client, MAX(client_name)", ""},
		} {
			key := strings.SplitN(q.cols, ",", 2)[0]
			r, err := s.st.DB.QueryContext(ctx, `SELECT `+q.cols+`, COUNT(*) FROM query_log WHERE `+where+q.extra+` GROUP BY `+key, targs...)
			if err != nil {
				return err
			}
			for r.Next() {
				var k, l string
				var c int64
				if err := r.Scan(&k, &l, &c); err != nil {
					r.Close()
					return err
				}
				add(q.kind, k, l, c)
			}
			r.Close()
		}
	}
	sum.TopDomains = topOf(merged["domain"], topN)
	sum.TopBlocked = topOf(merged["blocked"], topN)
	sum.TopClients = topOf(merged["client"], topN)
	sum.Totals.Clients = int64(len(merged["client"]))
	return nil
}

func topOf(m map[string]*TopItem, n int) []TopItem {
	out := make([]TopItem, 0, len(m))
	for _, it := range m {
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func emptyGrid() [][]int64 {
	g := make([][]int64, 7)
	for i := range g {
		g[i] = make([]int64, 24)
	}
	return g
}
