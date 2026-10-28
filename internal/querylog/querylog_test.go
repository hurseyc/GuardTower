package querylog

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hurseyc/guardtower/internal/store"
)

func newStore(t *testing.T) (*Store, *store.Store) {
	t.Helper()
	st, err := store.OpenPath(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, n := range []string{"a", "b"} {
		if _, err := st.DB.Exec(`INSERT INTO instances (name, url, created_at) VALUES (?, 'http://x', 0)`, n); err != nil {
			t.Fatal(err)
		}
	}
	return New(st), st
}

func row(inst int64, ts int64, client, domain, reason string) Row {
	r := Row{InstanceID: inst, TS: ts, Client: client, Domain: domain, QType: "A", Reason: reason, ElapsedMs: 2}
	r.Status, r.Blocked = Classify(reason)
	return r
}

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"FilteredBlackList": StatusBlocked, "FilteredParental": StatusParental, "FilteredSafeSearch": StatusSafeSearch,
		"NotFilteredNotFound": StatusAllowed, "NotFilteredAllowList": StatusAllowlisted, "Rewrite": StatusRewritten,
		"FilteredBlockedService": StatusBlocked, "FilteredSafeBrowsing": StatusSafeBrowsing,
	}
	for reason, want := range cases {
		if got, _ := Classify(reason); got != want {
			t.Errorf("%s: got %s want %s", reason, got, want)
		}
	}
	if _, b := Classify("FilteredParental"); !b {
		t.Error("parental should count as blocked")
	}
	if _, b := Classify("FilteredSafeSearch"); b {
		t.Error("safe search rewrites are not blocks")
	}
}

func TestInsertDedupeAndRollups(t *testing.T) {
	q, st := newStore(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	rows := []Row{
		row(1, now, "10.0.0.2", "www.google.com", "NotFilteredNotFound"),
		row(1, now+1, "10.0.0.2", "ads.doubleclick.net", "FilteredBlackList"),
		row(2, now+2, "10.0.0.3", "google.com", "NotFilteredNotFound"),
	}
	ins, err := q.Insert(ctx, rows)
	if err != nil || len(ins) != 3 {
		t.Fatalf("insert: %d %v", len(ins), err)
	}
	// Re-inserting the same rows (overlapping pages) stores nothing new.
	ins, err = q.Insert(ctx, rows)
	if err != nil || len(ins) != 0 {
		t.Fatalf("dedupe: %d %v", len(ins), err)
	}
	var queries, blocked int64
	if err := st.DB.QueryRow(`SELECT SUM(queries), SUM(blocked) FROM stats_hourly`).Scan(&queries, &blocked); err != nil {
		t.Fatal(err)
	}
	if queries != 3 || blocked != 1 {
		t.Fatalf("rollups: queries=%d blocked=%d", queries, blocked)
	}
}

func TestSearchFilters(t *testing.T) {
	q, _ := newStore(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour).UnixMilli()
	var rows []Row
	for i := 0; i < 50; i++ {
		rows = append(rows, row(1+int64(i%2), base+int64(i), fmt.Sprintf("10.0.0.%d", i%5), fmt.Sprintf("host%d.example.com", i), "NotFilteredNotFound"))
	}
	rows = append(rows, row(1, base+100, "10.0.0.9", "example.com", "FilteredBlackList"))
	rows = append(rows, row(1, base+101, "10.0.0.9", "notexample.com", "FilteredBlackList"))
	if _, err := q.Insert(ctx, rows); err != nil {
		t.Fatal(err)
	}

	// Domain suffix: example.com and its subdomains, but not notexample.com.
	p, err := q.Search(ctx, Filter{Domain: "example.com"}, nil, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 51 {
		t.Fatalf("suffix search: %d rows", len(p.Rows))
	}
	for _, r := range p.Rows {
		if r.Domain == "notexample.com" {
			t.Fatal("suffix search matched notexample.com")
		}
	}
	// Newest first.
	if p.Rows[0].Domain != "example.com" {
		t.Fatalf("ordering: first row %s", p.Rows[0].Domain)
	}
	// Pagination covers everything exactly once.
	seen := map[int64]bool{}
	var cur *Cursor
	for {
		pg, err := q.Search(ctx, Filter{}, cur, 7)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range pg.Rows {
			if seen[r.ID] {
				t.Fatalf("row %d returned twice", r.ID)
			}
			seen[r.ID] = true
		}
		if pg.Next == nil {
			break
		}
		cur = pg.Next
	}
	if len(seen) != 52 {
		t.Fatalf("pagination saw %d rows", len(seen))
	}
	// Status, client and instance filters.
	p, _ = q.Search(ctx, Filter{BlockedOnly: true}, nil, 100)
	if len(p.Rows) != 2 {
		t.Fatalf("blocked filter: %d", len(p.Rows))
	}
	p, _ = q.Search(ctx, Filter{Client: "10.0.0.9"}, nil, 100)
	if len(p.Rows) != 2 {
		t.Fatalf("client filter: %d", len(p.Rows))
	}
	p, _ = q.Search(ctx, Filter{InstanceIDs: []int64{2}}, nil, 100)
	if len(p.Rows) != 25 {
		t.Fatalf("instance filter: %d", len(p.Rows))
	}
	p, _ = q.Search(ctx, Filter{Contains: "st4"}, nil, 100)
	if len(p.Rows) != 11 { // host4, host40-49
		t.Fatalf("contains filter: %d", len(p.Rows))
	}
}

func TestSummarize(t *testing.T) {
	q, _ := newStore(t)
	ctx := context.Background()
	now := time.Now()
	var rows []Row
	for i := 0; i < 100; i++ {
		reason := "NotFilteredNotFound"
		if i%4 == 0 {
			reason = "FilteredBlackList"
		}
		rows = append(rows, row(1+int64(i%2), now.Add(-time.Duration(i)*time.Minute).UnixMilli(), "10.0.0.1", fmt.Sprintf("d%d.com", i%3), reason))
	}
	if _, err := q.Insert(ctx, rows); err != nil {
		t.Fatal(err)
	}
	if _, err := q.BuildTops(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	for _, span := range []time.Duration{3 * time.Hour, 7 * 24 * time.Hour} {
		s, err := q.Summarize(ctx, Filter{From: now.Add(-span).UnixMilli(), To: now.Add(time.Minute).UnixMilli()}, 5)
		if err != nil {
			t.Fatal(err)
		}
		if s.Totals.Queries != 100 || s.Totals.Blocked != 25 || s.Totals.BlockedPct != 25 {
			t.Fatalf("span %v totals: %+v", span, s.Totals)
		}
		var seriesSum int64
		for _, p := range s.Series {
			seriesSum += p.Queries
		}
		if seriesSum != 100 {
			t.Fatalf("span %v series sum %d", span, seriesSum)
		}
		if len(s.Instances) != 2 || len(s.TopDomains) != 3 || s.Totals.Clients != 1 {
			t.Fatalf("span %v breakdowns: %d instances, %d domains, %d clients", span, len(s.Instances), len(s.TopDomains), s.Totals.Clients)
		}
		if (span > 24*time.Hour) != s.Totals.FromRollups {
			t.Fatalf("span %v from_rollups=%v", span, s.Totals.FromRollups)
		}
		// Rolled-up hours plus the raw tail must add up exactly.
		var topSum, blockedSum int64
		for _, d := range s.TopDomains {
			topSum += d.Count
		}
		for _, d := range s.TopBlocked {
			blockedSum += d.Count
		}
		if topSum != 100 || blockedSum != 25 {
			t.Fatalf("span %v top sums: domains=%d blocked=%d", span, topSum, blockedSum)
		}
		var statusSum, heat int64
		for _, st := range s.TopStatuses {
			statusSum += st.Count
		}
		for _, d := range s.Hourly {
			for _, c := range d {
				heat += c
			}
		}
		if statusSum != 100 || heat != 100 {
			t.Fatalf("span %v statuses=%d heatmap=%d", span, statusSum, heat)
		}
	}
}

func TestPrune(t *testing.T) {
	q, st := newStore(t)
	ctx := context.Background()
	old := time.Now().Add(-40 * 24 * time.Hour).UnixMilli()
	if _, err := q.Insert(ctx, []Row{row(1, old, "c", "old.com", ""), row(1, time.Now().UnixMilli(), "c", "new.com", "")}); err != nil {
		t.Fatal(err)
	}
	n, err := q.Prune(ctx, 30, 400)
	if err != nil || n != 1 {
		t.Fatalf("prune: %d %v", n, err)
	}
	var left int
	_ = st.DB.QueryRow(`SELECT COUNT(*) FROM query_log`).Scan(&left)
	if left != 1 {
		t.Fatalf("rows left %d", left)
	}
}

// TestSearchPerformance checks the M2 target (fleet search < 300 ms) on a
// realistic volume. It is skipped unless GT_PERF=1 because it inserts 1M rows.
func TestSearchPerformance(t *testing.T) {
	if os.Getenv("GT_PERF") != "1" {
		t.Skip("set GT_PERF=1 to run")
	}
	dir := t.TempDir()
	st, err := store.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := 0; i < 5; i++ {
		_, _ = st.DB.Exec(`INSERT INTO instances (name, url, created_at) VALUES (?, 'http://x', 0)`, fmt.Sprintf("i%d", i))
	}
	q := New(st)
	ctx := context.Background()
	start := time.Now().Add(-24 * time.Hour).UnixMilli()
	const total = 1_000_000
	batch := make([]Row, 0, 5000)
	for i := 0; i < total; i++ {
		reason := "NotFilteredNotFound"
		if i%7 == 0 {
			reason = "FilteredBlackList"
		}
		batch = append(batch, row(1+int64(i%5), start+int64(i)*86, fmt.Sprintf("192.168.1.%d", i%60), fmt.Sprintf("s%d.site%d.com", i%13, i%5000), reason))
		if len(batch) == cap(batch) {
			if _, err := q.Insert(ctx, batch); err != nil {
				t.Fatal(err)
			}
			batch = batch[:0]
		}
	}
	for name, f := range map[string]Filter{
		"latest":        {},
		"domain suffix": {Domain: "site1234.com"},
		"client":        {Client: "192.168.1.7"},
		"blocked":       {BlockedOnly: true},
		"contains":      {Contains: "site4999"},
	} {
		t0 := time.Now()
		if _, err := q.Search(ctx, f, nil, 100); err != nil {
			t.Fatal(err)
		}
		d := time.Since(t0)
		t.Logf("%-14s %v", name, d)
		if d > 300*time.Millisecond {
			t.Errorf("%s search took %v (target < 300ms)", name, d)
		}
	}
	if _, err := q.BuildTops(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	for _, span := range []time.Duration{time.Hour, 24 * time.Hour, 7 * 24 * time.Hour} {
		t0 := time.Now()
		now := time.Now().UnixMilli()
		if _, err := q.Summarize(ctx, Filter{From: now - span.Milliseconds(), To: now}, 10); err != nil {
			t.Fatal(err)
		}
		d := time.Since(t0)
		t.Logf("summary %-6v %v", span, d)
		if d > time.Second {
			t.Errorf("summary over %v took %v (target < 1s)", span, d)
		}
	}
}
