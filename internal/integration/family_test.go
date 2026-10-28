//go:build integration

package integration

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/hurseyc/guardtower/internal/app"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/devfleet"
	"github.com/hurseyc/guardtower/internal/family"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/store"
)

// TestFamilyFromRealQueries sends real DNS queries through AdGuard Home, with
// an adult site blocked by a custom rule, and checks they become one Adult
// attempt for the right person plus a bypass signal for a DoH resolver.
func TestFamilyFromRealQueries(t *testing.T) {
	node, c := startNode(t, "latest", 7)
	ctx := context.Background()
	if err := c.Post(ctx, "filtering/set_rules", map[string]any{"rules": []string{"||pornhub.com^"}}, nil); err != nil {
		t.Fatal(err)
	}
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
	_ = a.Fleet.SaveStatus(ctx, in.ID, a.Poller.Probe(ctx, in))

	cl := &dns.Client{Timeout: 3 * time.Second}
	for _, n := range []string{"www.pornhub.com", "pornhub.com", "cdn.pornhub.com", "dns.google"} {
		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(n), dns.TypeA)
		_, _, _ = cl.Exchange(m, node.DNSAddr)
	}
	waitFor(t, func() bool {
		_, _ = a.Collector.CollectOne(ctx, in)
		var n int
		_ = st.DB.QueryRow(`SELECT COUNT(*) FROM query_log`).Scan(&n)
		return n >= 4
	})
	a.Family.SyncLists(ctx)
	if _, err := a.Family.Build(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := a.Family.SavePerson(ctx, 0, "Sam", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Family.AssignDevice(ctx, "127.0.0.1", "test", p.ID); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	o, err := a.Family.Overview(ctx, now.Add(-time.Hour).UnixMilli(), now.Add(time.Minute).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(o.People) != 1 || o.People[0].Adult != 1 || o.Totals.Allowed != 0 {
		t.Fatalf("overview: people=%+v totals=%+v", o.People, o.Totals)
	}
	bypass := false
	for _, s := range o.Signals {
		bypass = bypass || s.Kind == family.SignalBypass
	}
	if !bypass {
		t.Fatalf("expected a bypass signal for dns.google: %+v", o.Signals)
	}
	evs, _ := a.Family.Events(ctx, family.Filter{PersonID: p.ID, Categories: []string{family.Adult}, From: now.Add(-time.Hour).UnixMilli(), To: now.Add(time.Minute).UnixMilli()})
	if len(evs) != 1 || evs[0].Count != 3 || !evs[0].Blocked || evs[0].Domain != "p***hub.com" {
		t.Fatalf("events: %+v", evs)
	}
}

// TestFamilyProfileKeepsExistingFiltering applies the Family profile to a
// group whose filtering isn't managed yet and checks the plan only adds:
// the instance's existing lists and rules must survive.
func TestFamilyProfileKeepsExistingFiltering(t *testing.T) {
	_, c := startNode(t, "latest", 8)
	ctx := context.Background()
	if err := c.Post(ctx, "filtering/set_rules", map[string]any{"rules": []string{"||tiktok.com^"}}, nil); err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := crypto.New(make([]byte, 32))
	a := app.NewWith(app.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, st, box)
	pw := devfleet.Password
	g, _ := a.Fleet.SaveGroup(ctx, 0, "Kids", "", "")
	in, err := a.Fleet.CreateInstance(ctx, fleet.InstanceInput{Name: "kid-agh", URL: c.BaseURL(), Username: devfleet.User, Password: &pw, GroupID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	_ = a.Fleet.SaveStatus(ctx, in.ID, a.Poller.Probe(ctx, in))
	if _, err := family.ApplyProfile(ctx, a.Config, g.ID, in.ID, family.ProfileOptions{Parental: true, AdultList: true, BlockBypass: true}, "test"); err != nil {
		t.Fatal(err)
	}
	states, err := a.Config.Effective(ctx, in.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	adds := 0
	for _, s := range states {
		for _, ch := range s.Changes {
			if ch.Op == "remove" {
				t.Errorf("profile would remove %s %s: %v", s.Section, ch.Field, ch.Item)
			}
			if ch.Op == "add" {
				adds++
			}
		}
	}
	if adds == 0 {
		t.Fatal("expected the profile to add the adult list and bypass rules")
	}
}
