//go:build integration

// Package integration runs GuardTower against real AdGuard Home processes.
//
//	go test -tags integration ./internal/integration/...
//
// AGH binaries are cached in $GT_AGH_CACHE (default: the user cache dir).
// Set GT_AGH_VERSIONS to a comma-separated list to test several releases.
package integration

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/hurseyc/guardtower/internal/agh"
	"github.com/hurseyc/guardtower/internal/app"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/devfleet"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/querylog"
	"github.com/hurseyc/guardtower/internal/store"
)

func versions() []string {
	if v := os.Getenv("GT_AGH_VERSIONS"); v != "" {
		return strings.Split(v, ",")
	}
	return []string{"latest", "v0.107.70"}
}

func cacheDir(t *testing.T) string {
	if d := os.Getenv("GT_AGH_CACHE"); d != "" {
		return d
	}
	d, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(d, "guardtower-agh")
}

// startNode starts one fresh AGH instance for the test and stops it afterwards.
func startNode(t *testing.T, ver string, slot int) (*devfleet.Node, *agh.Client) {
	t.Helper()
	ctx := context.Background()
	bin, err := devfleet.EnsureBinary(ctx, cacheDir(t), ver, t.Logf)
	if err != nil {
		t.Fatalf("download AdGuard Home %s: %v", ver, err)
	}
	dir := t.TempDir()
	n, err := devfleet.Start(ctx, dir, 0, bin, 23001+slot, 25301+slot, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { devfleet.StopAll(dir, nil); time.Sleep(200 * time.Millisecond) })
	c, err := agh.New(agh.Options{BaseURL: n.URL, Username: devfleet.User, Password: devfleet.Password})
	if err != nil {
		t.Fatal(err)
	}
	return n, c
}

func TestClientAgainstRealAdGuardHome(t *testing.T) {
	for slot, ver := range versions() {
		t.Run(ver, func(t *testing.T) {
			_, c := startNode(t, ver, slot)
			ctx := context.Background()

			st, err := c.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !st.Running || !strings.HasPrefix(st.Version, "v0.107") {
				t.Fatalf("unexpected status %+v", st)
			}

			if _, err := c.Stats(ctx); err != nil {
				t.Fatalf("stats: %v", err)
			}
			if _, err := c.QueryLog(ctx, "", 10); err != nil {
				t.Fatalf("querylog: %v", err)
			}
			if _, err := c.FilteringStatus(ctx); err != nil {
				t.Fatalf("filtering: %v", err)
			}
			v, err := c.Version(ctx, false)
			if err != nil {
				t.Fatalf("version.json: %v", err)
			}
			t.Logf("can_autoupdate=%v", v.CanAutoUpdate)

			// Protection toggles round-trip.
			if err := c.SetProtection(ctx, false, time.Minute); err != nil {
				t.Fatal(err)
			}
			if st, _ := c.Status(ctx); st.ProtectionEnabled {
				t.Fatal("protection still enabled after pause")
			}
			if err := c.SetProtection(ctx, true, 0); err != nil {
				t.Fatal(err)
			}
			if st, _ := c.Status(ctx); !st.ProtectionEnabled {
				t.Fatal("protection not re-enabled")
			}

			// Wrong credentials surface as an auth error.
			bad, _ := agh.New(agh.Options{BaseURL: c.BaseURL(), Username: devfleet.User, Password: "wrong"})
			if _, err := bad.Status(ctx); !agh.IsAuthError(err) {
				t.Fatalf("expected auth error, got %v", err)
			}
		})
	}
}

func TestCollectorAgainstRealAdGuardHome(t *testing.T) {
	node, _ := startNode(t, "latest", 5)
	ctx := context.Background()
	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := crypto.New(make([]byte, 32))
	a := app.NewWith(app.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, st, box)
	pw := devfleet.Password
	in, err := a.Fleet.CreateInstance(ctx, fleet.InstanceInput{Name: "agh", URL: node.URL, Username: devfleet.User, Password: &pw})
	if err != nil {
		t.Fatal(err)
	}

	query := func(names ...string) {
		cl := &dns.Client{Timeout: 3 * time.Second}
		for _, n := range names {
			m := new(dns.Msg)
			m.SetQuestion(dns.Fqdn(n), dns.TypeA)
			_, _, _ = cl.Exchange(m, node.DNSAddr) // upstream may be unreachable; the query is still logged
		}
	}
	query("one.example", "two.example", "three.example")
	// AdGuard Home flushes its in-memory query-log buffer asynchronously.
	waitFor(t, func() bool {
		n, err := a.Collector.CollectOne(ctx, in)
		return err == nil && n > 0
	})
	var first int
	_ = st.DB.QueryRow(`SELECT COUNT(*) FROM query_log`).Scan(&first)
	if first < 3 {
		t.Fatalf("expected at least 3 rows, got %d", first)
	}
	// A second run without new traffic stores nothing (dedupe + cursor).
	if n, err := a.Collector.CollectOne(ctx, in); err != nil || n != 0 {
		t.Fatalf("second run: n=%d err=%v", n, err)
	}
	query("four.example")
	waitFor(t, func() bool {
		var c int
		_, _ = a.Collector.CollectOne(ctx, in)
		_ = st.DB.QueryRow(`SELECT COUNT(*) FROM query_log WHERE domain = 'four.example'`).Scan(&c)
		return c == 1
	})
	page, err := a.QueryLog.Search(ctx, querylog.Filter{Domain: "example"}, nil, 50)
	if err != nil || len(page.Rows) < 4 {
		t.Fatalf("search: %d rows, %v", len(page.Rows), err)
	}
	if page.Rows[0].Domain != "four.example" {
		t.Fatalf("newest row should be four.example, got %s", page.Rows[0].Domain)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("condition not met within 20s")
}
