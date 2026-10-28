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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/hurseyc/guardtower/internal/agh"
	"github.com/hurseyc/guardtower/internal/app"
	"github.com/hurseyc/guardtower/internal/config"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/devfleet"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/querylog"
	"github.com/hurseyc/guardtower/internal/rollout"
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
	// AdGuard Home logs a query when it completes (a slow upstream can take
	// seconds) and flushes its buffer asynchronously, so keep collecting
	// until all three are stored.
	waitFor(t, func() bool {
		if _, err := a.Collector.CollectOne(ctx, in); err != nil {
			return false
		}
		var n int
		_ = st.DB.QueryRow(`SELECT COUNT(*) FROM query_log WHERE domain IN ('one.example', 'two.example', 'three.example')`).Scan(&n)
		return n == 3
	})
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

// TestConfigSyncAllSections imports a baseline from one instance, layers
// group changes for every section on top, applies them to instances of each
// supported AdGuard Home version and checks nothing is left to change.
func TestConfigSyncAllSections(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := crypto.New(make([]byte, 32))
	a := app.NewWith(app.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, st, box)
	pw := devfleet.Password
	g, err := a.Fleet.SaveGroup(ctx, 0, "Kids", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i, ver := range versions() {
		node, _ := startNode(t, ver, 10+i)
		in, err := a.Fleet.CreateInstance(ctx, fleet.InstanceInput{Name: "agh-" + ver, URL: node.URL, Username: devfleet.User, Password: &pw, GroupID: g.ID})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, in.ID)
	}
	// Baseline: everything the first instance currently has.
	if err := a.Config.ImportBaseline(ctx, ids[0], config.ScopeRef{Scope: config.ScopeBase}, nil, "test"); err != nil {
		t.Fatal(err)
	}
	// Group layer touching every section.
	group := config.ScopeRef{Scope: config.ScopeGroup, ID: g.ID}
	layers := map[string]string{
		"dns":              `{"upstream_dns": ["https://dns.quad9.net/dns-query", "https://cloudflare-dns.com/dns-query"], "ratelimit": 30, "blocking_mode": "nxdomain", "cache_optimistic": true, "ratelimit_whitelist": {"add": ["10.0.0.9"]}}`,
		"filtering":        `{"interval": 12, "filters": {"upsert": [{"url": "https://adguardteam.github.io/HostlistsRegistry/assets/filter_1.txt", "name": "AdGuard DNS filter", "enabled": false}]}, "user_rules": {"add": ["||tiktok.com^", "@@||school.example^"]}}`,
		"rewrites":         `{"rewrites": {"upsert": [{"domain": "nas.home", "answer": "192.168.1.20", "enabled": true}, {"domain": "printer.home", "answer": "192.168.1.30", "enabled": true}]}}`,
		"clients":          `{"clients": {"upsert": [{"name": "Sam iPad", "ids": ["192.168.1.50"], "tags": ["user_child"], "use_global_settings": true, "use_global_blocked_services": true}]}}`,
		"access":           `{"disallowed_clients": {"add": ["192.168.66.6"]}, "blocked_hosts": {"add": ["evil.example"]}}`,
		"blocked_services": `{"ids": {"add": ["tiktok", "roblox"]}}`,
		"safebrowsing":     `{"enabled": true}`,
		"parental":         `{"enabled": true}`,
		"safesearch":       `{"enabled": true, "youtube": true}`,
		"querylog":         `{"interval": 86400000, "ignored": {"add": ["ignored.example"]}}`,
		"stats":            `{"interval": 86400000}`,
	}
	for sec, js := range layers {
		d, err := config.ParseDoc([]byte(js))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Config.SetLayer(ctx, group, sec, d, "test"); err != nil {
			t.Fatalf("%s: %v", sec, err)
		}
	}
	for i, id := range ids {
		t.Run(versions()[i], func(t *testing.T) {
			before, err := a.Config.Effective(ctx, id, true)
			if err != nil {
				t.Fatal(err)
			}
			pending := 0
			for _, s := range before {
				if s.LiveError != "" {
					t.Fatalf("%s: %s", s.Section, s.LiveError)
				}
				pending += len(s.Changes)
			}
			if pending == 0 {
				t.Fatal("expected pending changes before apply")
			}
			if _, err := a.Config.ApplyInstance(ctx, id, nil); err != nil {
				t.Fatal(err)
			}
			after, err := a.Config.Effective(ctx, id, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range after {
				if len(s.Changes) > 0 {
					b, _ := json.MarshalIndent(s.Changes, "", "  ")
					t.Errorf("%s still differs after apply: %s", s.Section, b)
				}
			}
		})
	}
}

func waitRollout(t *testing.T, a *app.App, id int64) *rollout.Rollout {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		r, err := a.Rollout.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != "running" && a.Rollout.ActiveID() == 0 {
			return r
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("rollout did not finish")
	return nil
}

// TestRolloutCanaryRollbackAndRestore covers the M4 exit criteria on real
// AdGuard Home: a good change rolls out canary-first; a change that breaks
// DNS (an unreachable upstream) is detected on the canary, rolled back
// automatically and never reaches the other instance; snapshots restore.
func TestRolloutCanaryRollbackAndRestore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := crypto.New(make([]byte, 32))
	a := app.NewWith(app.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, st, box)
	a.Rollout.SetRoot(ctx)
	a.Rollout.SettleDelay = 500 * time.Millisecond
	pw := devfleet.Password
	g, _ := a.Fleet.SaveGroup(ctx, 0, "G", "", "")
	var ids []int64
	for i, canary := range []bool{true, false} {
		node, _ := startNode(t, "latest", 20+i)
		in, err := a.Fleet.CreateInstance(ctx, fleet.InstanceInput{Name: fmt.Sprintf("agh-%d", i+1), URL: node.URL, Username: devfleet.User,
			Password: &pw, GroupID: g.ID, IsCanary: canary, DNSAddress: node.DNSAddr})
		if err != nil {
			t.Fatal(err)
		}
		// Mark online (normally done by the status poller).
		_ = a.Fleet.SaveStatus(ctx, in.ID, a.Poller.Probe(ctx, in))
		ids = append(ids, in.ID)
	}
	if err := a.Config.ImportBaseline(ctx, ids[0], config.ScopeRef{Scope: config.ScopeBase}, []string{"dns", "rewrites"}, "test"); err != nil {
		t.Fatal(err)
	}
	target := rollout.Target{Kind: "group", ID: g.ID}
	group := config.ScopeRef{Scope: config.ScopeGroup, ID: g.ID}
	setLayer := func(sec, js string) {
		d, _ := config.ParseDoc([]byte(js))
		if _, err := a.Config.SetLayer(ctx, group, sec, d, "test"); err != nil {
			t.Fatal(err)
		}
	}

	// Observe mode (the default) refuses to apply.
	setLayer("rewrites", `{"rewrites": {"upsert": [{"domain": "nas.home", "answer": "192.168.1.20", "enabled": true}]}}`)
	if _, err := a.Rollout.Start(ctx, target, nil, "test"); !errors.Is(err, rollout.ErrObserveMode) {
		t.Fatalf("expected observe-mode refusal, got %v", err)
	}
	_ = st.SetSetting(ctx, rollout.SettingObserve, "false")

	// 1. A good change: canary first, both succeed, both in sync.
	plan, err := a.Rollout.MakePlan(ctx, target, nil)
	if err != nil || plan.Total != 2 || !plan.Items[0].Canary {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	id, err := a.Rollout.Start(ctx, target, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	r := waitRollout(t, a, id)
	if r.Status != "succeeded" || len(r.Steps) != 2 || r.Steps[0].InstanceID != ids[0] {
		t.Fatalf("good rollout: %+v", r)
	}
	for _, s := range r.Steps {
		if s.Status != "succeeded" || s.SnapshotID == 0 {
			t.Fatalf("step: %+v", s)
		}
	}

	// 2. A bad upstream: the canary breaks, is rolled back, agh-2 is untouched.
	before, _ := a.Config.Effective(ctx, ids[0], true)
	var upstreamBefore any
	for _, s := range before {
		if s.Section == "dns" {
			upstreamBefore = s.Live["upstream_dns"]
		}
	}
	setLayer("dns", `{"upstream_dns": ["127.0.0.1:1"], "upstream_timeout": 1}`)
	id, err = a.Rollout.Start(ctx, target, []string{"dns"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	r = waitRollout(t, a, id)
	if r.Status != "rolled_back" {
		t.Fatalf("bad rollout should roll back, got %s (%s)", r.Status, r.Error)
	}
	if r.Steps[0].Status != "rolled_back" || r.Steps[1].Status != "skipped" {
		t.Fatalf("steps: canary=%s (%s) other=%s", r.Steps[0].Status, r.Steps[0].Detail, r.Steps[1].Status)
	}
	after, _ := a.Config.Effective(ctx, ids[0], true)
	for _, s := range after {
		if s.Section == "dns" && !reflect.DeepEqual(s.Live["upstream_dns"], upstreamBefore) {
			t.Fatalf("canary upstreams not restored: %v vs %v", s.Live["upstream_dns"], upstreamBefore)
		}
	}
	other, _ := a.Config.Effective(ctx, ids[1], true)
	for _, s := range other {
		if s.Section == "dns" && reflect.DeepEqual(s.Live["upstream_dns"], []any{"127.0.0.1:1"}) {
			t.Fatal("bad change reached the non-canary instance")
		}
	}

	// 3. Restore agh-2 to its snapshot from before the good rollout.
	setLayer("dns", `{}`)
	snaps, _ := a.Rollout.ListSnapshots(ctx, ids[1], 50)
	var pre int64
	for _, s := range snaps {
		if s.Kind == "pre-apply" {
			pre = s.ID // oldest pre-apply wins (list is newest first)
		}
	}
	rid, err := a.Rollout.Restore(ctx, pre, "test")
	if err != nil {
		t.Fatal(err)
	}
	if r := waitRollout(t, a, rid); r.Status != "succeeded" {
		t.Fatalf("restore: %s %s", r.Status, r.Error)
	}
	restored, _ := a.Config.Effective(ctx, ids[1], true)
	for _, s := range restored {
		if s.Section == "rewrites" && len(s.Changes) != 1 {
			t.Fatalf("after restore the rewrite should be pending again, changes=%+v", s.Changes)
		}
	}
}
