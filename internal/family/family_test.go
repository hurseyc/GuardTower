package family

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hurseyc/guardtower/internal/config"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/events"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/querylog"
	"github.com/hurseyc/guardtower/internal/store"
)

func TestCategorize(t *testing.T) {
	lists := func(inst, id int64) string {
		return map[int64]string{1: Ads, 7: Adult, 9: Gambling}[id]
	}
	row := func(domain, reason string, filter int64, service string) *querylog.Row {
		r := &querylog.Row{InstanceID: 1, Domain: domain, Reason: reason, FilterID: filter, Service: service}
		r.Status, r.Blocked = querylog.Classify(reason)
		return r
	}
	cases := []struct {
		r    *querylog.Row
		want string
	}{
		{row("anything.example", "FilteredParental", 0, ""), Adult},
		{row("cdn.pornhub.com", "NotFilteredNotFound", 0, ""), Adult}, // not blocked, still adult
		{row("adult.example", "FilteredBlackList", 7, ""), Adult},
		{row("casino.example", "FilteredBlackList", 9, ""), Gambling},
		{row("doubleclick.net", "FilteredBlackList", 1, ""), ""}, // ordinary ad block
		{row("www.tiktok.com", "FilteredBlockedService", 0, "tiktok"), Social},
		{row("dns.google", "NotFilteredNotFound", 0, ""), Bypass},
		{row("phish.example", "FilteredSafeBrowsing", 0, ""), Malware},
		{row("www.google.com", "FilteredSafeSearch", 0, ""), SafeSearch},
		{row("example.org", "NotFilteredNotFound", 0, ""), ""},
	}
	for _, c := range cases {
		if got := Categorize(c.r, lists); got != c.want {
			t.Errorf("%s (%s): got %q want %q", c.r.Domain, c.r.Reason, got, c.want)
		}
	}
	if GuessListCategory("https://raw.githubusercontent.com/hagezi/dns-blocklists/main/adblock/nsfw.txt", "") != Adult ||
		GuessListCategory("https://example/StevenBlack/hosts/alternates/porn/hosts", "") != Adult ||
		GuessListCategory("https://adguardteam.github.io/HostlistsRegistry/assets/filter_1.txt", "AdGuard DNS filter") != "" {
		t.Error("list guesses")
	}
}

func TestMaskAndClock(t *testing.T) {
	for in, want := range map[string]string{"pornhub.com": "p***hub.com", "www.xvideos.com": "x***eos.com", "x.com": "x***.com", "bet365.co.uk": "b***365.co.uk", "abcd.net": "a***d.net"} {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%s) = %s, want %s", in, got, want)
		}
	}
	c, err := ParseClock("22:00-06:00")
	if err != nil {
		t.Fatal(err)
	}
	at := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.Local) }
	if !c.Contains(at(23, 30)) || !c.Contains(at(5, 59)) || c.Contains(at(6, 0)) || c.Contains(at(12, 0)) {
		t.Error("bedtime window")
	}
	if _, err := ParseClock("25:00-01:00"); err == nil {
		t.Error("invalid window accepted")
	}
	if wd, m, on, err := ParseDigest("Sun 18:30"); err != nil || !on || wd != time.Sunday || m != 18*60+30 {
		t.Errorf("digest parse: %v %v %v %v", wd, m, on, err)
	}
}

func newService(t *testing.T) (*Service, *store.Store, *querylog.Store) {
	t.Helper()
	ctx := context.Background()
	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := crypto.New(make([]byte, 32))
	fl := fleet.NewService(st, box)
	pw := "x"
	if _, err := fl.CreateInstance(ctx, fleet.InstanceInput{Name: "agh-1", URL: "http://127.0.0.1:1", Password: &pw}); err != nil {
		t.Fatal(err)
	}
	s := New(st, fl, events.NewBus(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return s, st, querylog.New(st)
}

func hit(ts time.Time, client, domain, reason string) querylog.Row {
	r := querylog.Row{InstanceID: 1, TS: ts.UnixMilli(), Client: client, Domain: domain, QType: "A", Reason: reason}
	r.Status, r.Blocked = querylog.Classify(reason)
	return r
}

func TestBuildSessionsAndOverview(t *testing.T) {
	ctx := context.Background()
	s, _, ql := newService(t)
	now := time.Now().Truncate(time.Minute)
	s.Now = func() time.Time { return now }
	base := now.Add(-2 * time.Hour)
	rows := []querylog.Row{
		// A burst of 40 queries in two minutes = one attempt.
		hit(base, "10.0.0.5", "www.pornhub.com", "FilteredParental"),
		hit(base.Add(30*time.Second), "10.0.0.5", "cdn.pornhub.com", "FilteredParental"),
		hit(base.Add(2*time.Minute), "10.0.0.5", "pornhub.com", "FilteredParental"),
		// Twenty minutes later = a second attempt.
		hit(base.Add(22*time.Minute), "10.0.0.5", "pornhub.com", "FilteredParental"),
		// A different site, not blocked.
		hit(base.Add(23*time.Minute), "10.0.0.5", "xvideos.com", "NotFilteredNotFound"),
		// Private DNS from another device.
		hit(base.Add(24*time.Minute), "10.0.0.9", "dns.google", "NotFilteredNotFound"),
		// Noise.
		hit(base.Add(25*time.Minute), "10.0.0.5", "example.org", "NotFilteredNotFound"),
		hit(base.Add(26*time.Minute), "10.0.0.5", "doubleclick.net", "FilteredBlackList"),
	}
	for i := 0; i < 37; i++ {
		rows = append(rows, hit(base.Add(time.Duration(i)*time.Second+time.Second), "10.0.0.5", "pornhub.com", "FilteredParental"))
	}
	if _, err := ql.Insert(ctx, rows); err != nil {
		t.Fatal(err)
	}
	created, err := s.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 4 {
		t.Fatalf("created %d sessions, want 4: %+v", len(created), created)
	}
	// Running again finds nothing new.
	if again, _ := s.Build(ctx); len(again) != 0 {
		t.Fatalf("second build created %d", len(again))
	}

	p, err := s.SavePerson(ctx, 0, "Sam", "🧒", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AssignDevice(ctx, "10.0.0.5", "Sam's iPad", p.ID); err != nil {
		t.Fatal(err)
	}
	o, err := s.Overview(ctx, now.Add(-24*time.Hour).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(o.People) != 1 || o.People[0].Adult != 3 || o.Totals.Adult != 3 || o.Totals.Allowed != 1 || o.Totals.AdultHits != 42 {
		t.Fatalf("overview: %+v totals %+v", o.People, o.Totals)
	}
	kinds := map[string]bool{}
	for _, sg := range o.Signals {
		kinds[sg.Kind] = true
	}
	if !kinds[SignalBypass] || !kinds[SignalAllowed] {
		t.Fatalf("signals: %+v", o.Signals)
	}

	d, err := s.Detail(ctx, p.ID, nil, now.Add(-24*time.Hour).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Events) != 3 || d.Events[0].Domain != "x***eos.com" || !d.Events[0].Masked || d.Events[0].Person != "Sam" {
		t.Fatalf("events: %+v", d.Events)
	}
	if len(d.Top) != 2 || d.Top[0].Domain != "p***hub.com" || d.Top[0].Attempts != 2 || d.Top[0].Hits != 41 {
		t.Fatalf("top: %+v", d.Top)
	}
	if len(d.Devices) != 1 || d.Devices[0].Label != "Sam's iPad" || d.Devices[0].Attempts != 3 {
		t.Fatalf("devices: %+v", d.Devices)
	}
	r, err := s.Reveal(ctx, d.Events[0].ID)
	if err != nil || r.Domain != "xvideos.com" || r.Masked {
		t.Fatalf("reveal: %+v %v", r, err)
	}

	m, err := s.Digest(ctx, now)
	if err != nil || !strings.Contains(m.Body, "Sam: 3 attempts") || !strings.Contains(m.Body, "1 not blocked") {
		t.Fatalf("digest: %q %v", m.Body, err)
	}
	clients, err := s.Clients(ctx, 7)
	if err != nil || len(clients) != 2 || clients[0].Device != "10.0.0.9" || clients[0].PersonID != 0 {
		t.Fatalf("clients (unassigned with events first): %+v %v", clients, err)
	}
}

func TestApplyProfileWritesValidLayers(t *testing.T) {
	ctx := context.Background()
	s, st, _ := newService(t)
	g, err := s.fleet.SaveGroup(ctx, 0, "Kids", "", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.NewService(st, s.fleet, events.NewBus(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	opts := ProfileOptions{Parental: true, SafeSearch: true, AdultList: true, BlockBypass: true}
	// Filtering isn't managed and there is no member to read it from.
	if _, err := ApplyProfile(ctx, cfg, g.ID, 0, opts, "test"); !errors.Is(err, ErrNoMember) {
		t.Fatalf("want ErrNoMember, got %v", err)
	}
	// With filtering managed in Base, the group layer only adds to it.
	base, _ := config.ParseDoc([]byte(`{"filters": {"upsert": [{"url": "https://example/ads.txt", "name": "Ads", "enabled": true}]}}`))
	if _, err := cfg.SetLayer(ctx, config.ScopeRef{Scope: config.ScopeBase}, "filtering", base, "test"); err != nil {
		t.Fatal(err)
	}
	changed, err := ApplyProfile(ctx, cfg, g.ID, 0, opts, "test")
	if err != nil || len(changed) != 3 {
		t.Fatalf("apply: %v %v", changed, err)
	}
	// Applying twice doesn't duplicate list entries or rules.
	if _, err := ApplyProfile(ctx, cfg, g.ID, 0, opts, "test"); err != nil {
		t.Fatal(err)
	}
	layers, _ := cfg.GetLayers(ctx, config.ScopeRef{Scope: config.ScopeGroup, ID: g.ID})
	f := layers["filtering"].Doc
	ups := f["filters"].(map[string]any)["upsert"].([]any)
	rules := f["user_rules"].(map[string]any)["add"].([]any)
	if len(ups) != 1 || len(rules) != len(bypassRules) {
		t.Fatalf("filtering layer: %d lists, %d rules", len(ups), len(rules))
	}
	if layers["parental"].Doc["enabled"] != true || layers["safesearch"].Doc["youtube"] != true {
		t.Fatalf("layers: %+v", layers)
	}
}
