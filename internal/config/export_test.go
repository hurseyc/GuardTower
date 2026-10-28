package config

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/events"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/store"
)

func TestYAMLRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := crypto.New(make([]byte, 32))
	fl := fleet.NewService(st, box)
	s := NewService(st, fl, events.NewBus(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	g, _ := fl.SaveGroup(ctx, 0, "Kids VLAN", "", "")
	pw := "x"
	in, _ := fl.CreateInstance(ctx, fleet.InstanceInput{Name: "agh-4", URL: "http://127.0.0.1:1", Password: &pw, GroupID: g.ID})
	set := func(ref ScopeRef, sec, js string) {
		d, _ := ParseDoc([]byte(js))
		if _, err := s.SetLayer(ctx, ref, sec, d, "t"); err != nil {
			t.Fatal(err)
		}
	}
	set(ScopeRef{Scope: ScopeBase}, "dns", `{"upstream_dns": ["https://dns.quad9.net/dns-query"], "cache_size": 4194304}`)
	set(ScopeRef{Scope: ScopeGroup, ID: g.ID}, "parental", `{"enabled": true}`)
	set(ScopeRef{Scope: ScopeGroup, ID: g.ID}, "filtering", `{"user_rules": {"add": ["||tiktok.com^"], "remove": []}}`)
	set(ScopeRef{Scope: ScopeInstance, ID: in.ID}, "rewrites", `{"rewrites": {"upsert": [{"domain": "nas.home", "answer": "192.168.1.20", "enabled": true}], "remove": []}}`)

	out, err := s.ExportYAML(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"version: 1", "Kids VLAN:", "agh-4:", "||tiktok.com^", "nas.home"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("export lacks %q:\n%s", want, out)
		}
	}
	// Wipe everything, then import the file back.
	if _, err := st.DB.Exec(`DELETE FROM config_layers`); err != nil {
		t.Fatal(err)
	}
	dry, err := s.ImportYAML(ctx, out, true, false, "t")
	if err != nil || len(dry.Changes) != 4 || dry.Applied {
		t.Fatalf("dry run: %+v %v", dry, err)
	}
	var n int
	_ = st.DB.QueryRow(`SELECT COUNT(*) FROM config_layers`).Scan(&n)
	if n != 0 {
		t.Fatal("dry run wrote layers")
	}
	res, err := s.ImportYAML(ctx, out, false, false, "t")
	if err != nil || !res.Applied {
		t.Fatalf("import: %+v %v", res, err)
	}
	again, _ := s.ImportYAML(ctx, out, true, false, "t")
	if len(again.Changes) != 0 || again.Unchanged != 4 {
		t.Fatalf("re-import should change nothing: %+v", again)
	}
	// Prune removes layers the file doesn't mention.
	set(ScopeRef{Scope: ScopeBase}, "safesearch", `{"enabled": true}`)
	pr, _ := s.ImportYAML(ctx, out, true, true, "t")
	if len(pr.Changes) != 1 || pr.Changes[0].Action != "delete" || pr.Changes[0].Section != "safesearch" {
		t.Fatalf("prune: %+v", pr.Changes)
	}
	// Unknown names and sections are rejected without changing anything.
	var ie *ImportError
	if _, err := s.ImportYAML(ctx, []byte("version: 1\ngroups:\n  Nope:\n    dns: {}\n"), false, false, "t"); !errors.As(err, &ie) {
		t.Fatalf("unknown group: %v", err)
	}
	if _, err := s.ImportYAML(ctx, []byte("version: 1\nbase:\n  dnss: {}\n"), false, false, "t"); !errors.As(err, &ie) {
		t.Fatalf("unknown section: %v", err)
	}
	if _, err := s.ImportYAML(ctx, []byte("version: 1\nbase:\n  dns:\n    upstream_dns: 5\n"), false, false, "t"); !errors.As(err, &ie) {
		t.Fatalf("invalid value: %v", err)
	}
}
