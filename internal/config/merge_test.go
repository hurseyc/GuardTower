package config

import (
	"encoding/json"
	"testing"
)

func doc(t *testing.T, s string) Doc {
	t.Helper()
	d, err := ParseDoc([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMergeLayers(t *testing.T) {
	sec := SectionByKey("filtering")
	base := doc(t, `{
		"enabled": true, "interval": 24,
		"filters": {"upsert": [
			{"url": "https://a.example/list.txt", "name": "A", "enabled": true},
			{"url": "https://b.example/list.txt", "name": "B", "enabled": true}
		]},
		"user_rules": {"add": ["||ads.example^", "||track.example^"]}
	}`)
	group := doc(t, `{
		"filters": {"upsert": [{"url": "https://nsfw.example/list.txt", "name": "NSFW", "enabled": true}], "remove": ["https://b.example/list.txt"]},
		"user_rules": {"add": ["||tiktok.com^"], "remove": ["||track.example^"]}
	}`)
	inst := doc(t, `{"interval": 12}`)
	eff, orig := Merge(sec, []Layer{
		{Scope: ScopeBase, Name: "Base", Doc: base},
		{Scope: ScopeGroup, Name: "Kids", Doc: group},
		{Scope: ScopeInstance, Name: "agh-4", Doc: inst},
	})
	if eff["interval"] != float64(12) || orig["interval"].Scope != ScopeInstance {
		t.Fatalf("instance override: %v %v", eff["interval"], orig["interval"])
	}
	if eff["enabled"] != true || orig["enabled"].Scope != ScopeBase {
		t.Fatalf("inherited scalar: %v %v", eff["enabled"], orig["enabled"])
	}
	filters := toItems(eff["filters"])
	if len(filters) != 2 {
		t.Fatalf("filters: %v", filters)
	}
	urls := []string{str(filters[0], "url"), str(filters[1], "url")}
	if urls[0] != "https://a.example/list.txt" || urls[1] != "https://nsfw.example/list.txt" {
		t.Fatalf("filter urls: %v", urls)
	}
	if o := orig[ItemKey("filters", "https://nsfw.example/list.txt")]; o.Scope != ScopeGroup || o.Name != "Kids" {
		t.Fatalf("filter origin: %+v", o)
	}
	rules := toStrings(eff["user_rules"])
	if len(rules) != 2 || rules[0] != "||ads.example^" || rules[1] != "||tiktok.com^" {
		t.Fatalf("rules: %v", rules)
	}
	if _, ok := orig[ItemKey("user_rules", "||track.example^")]; ok {
		t.Fatal("removed rule kept an origin")
	}
	// Unmanaged fields are absent.
	if _, ok := eff["whitelist_filters"]; ok {
		t.Fatal("unmanaged field appeared in effective config")
	}
}

func TestDiff(t *testing.T) {
	sec := SectionByKey("filtering")
	desired, _ := Merge(sec, []Layer{{Scope: ScopeBase, Doc: doc(t, `{
		"enabled": true,
		"filters": {"upsert": [{"url": "u1", "name": "One", "enabled": true}, {"url": "u2", "name": "Two", "enabled": false}]},
		"user_rules": {"add": ["r1", "r2"]}
	}`)}})
	live := doc(t, `{
		"enabled": false, "interval": 24, "whitelist_filters": [],
		"filters": [{"url": "u1", "name": "One", "enabled": true}, {"url": "u2", "name": "Two", "enabled": true}, {"url": "u3", "name": "Three", "enabled": true}],
		"user_rules": ["r2", "r3"]
	}`)
	changes := Diff(sec, desired, live)
	got := map[string]bool{}
	for _, c := range changes {
		got[c.Field+":"+c.Op+":"+c.Item] = true
	}
	for _, want := range []string{"enabled:set:", "filters:update:u2", "filters:remove:u3", "user_rules:add:r1", "user_rules:remove:r3"} {
		if !got[want] {
			t.Errorf("missing change %s (got %v)", want, got)
		}
	}
	if len(changes) != 5 {
		b, _ := json.MarshalIndent(changes, "", " ")
		t.Fatalf("expected 5 changes, got %d: %s", len(changes), b)
	}
	// interval is unmanaged, so it never shows up.
	for _, c := range changes {
		if c.Field == "interval" {
			t.Fatal("unmanaged field diffed")
		}
	}
	// Same rules in a different order are not a change (sets are unordered).
	d2, _ := Merge(sec, []Layer{{Scope: ScopeBase, Doc: doc(t, `{"user_rules": {"add": ["b", "a"]}}`)}})
	if cs := Diff(sec, d2, doc(t, `{"user_rules": ["a", "b"]}`)); len(cs) != 0 {
		t.Fatalf("order-only difference reported: %+v", cs)
	}
}

func TestLayerFromLiveRoundTrip(t *testing.T) {
	for _, sec := range Sections {
		live := Doc{}
		for _, f := range sec.Fields {
			switch f.Kind {
			case KindBool:
				live[f.Name] = true
			case KindInt, KindDuration:
				live[f.Name] = float64(7)
			case KindString:
				live[f.Name] = "x"
			case KindEnum:
				live[f.Name] = f.Enum[0]
			case KindList, KindSet:
				live[f.Name] = []any{"one", "two"}
			case KindObject:
				live[f.Name] = map[string]any{"k": "v"}
			case KindKeyed:
				item := map[string]any{"name": "n", "url": "u", "domain": "d.example", "answer": "1.2.3.4", "enabled": true}
				live[f.Name] = []any{item}
			}
		}
		layer := LayerFromLive(sec, live)
		if _, err := ValidateLayer(sec, layer); err != nil {
			t.Fatalf("%s: imported layer invalid: %v", sec.Key, err)
		}
		eff, _ := Merge(sec, []Layer{{Scope: ScopeBase, Doc: layer}})
		if cs := Diff(sec, eff, Doc(asJSON(live).(map[string]any))); len(cs) != 0 {
			t.Fatalf("%s: import then diff should be clean, got %+v", sec.Key, cs)
		}
	}
}

func TestValidateLayer(t *testing.T) {
	sec := SectionByKey("dns")
	bad := []string{
		`{"nope": 1}`,
		`{"ratelimit": "fast"}`,
		`{"ratelimit": 1.5}`,
		`{"blocking_mode": "explode"}`,
		`{"upstream_dns": "1.1.1.1"}`,
		`{"ratelimit_whitelist": ["1.2.3.4"]}`,
	}
	for _, b := range bad {
		if _, err := ValidateLayer(sec, doc(t, b)); err == nil {
			t.Errorf("expected error for %s", b)
		}
	}
	ok := `{"upstream_dns": ["https://dns.quad9.net/dns-query"], "ratelimit": 20, "blocking_mode": "nxdomain", "ratelimit_whitelist": {"add": ["10.0.0.1"]}}`
	if _, err := ValidateLayer(sec, doc(t, ok)); err != nil {
		t.Fatalf("valid layer rejected: %v", err)
	}
	if _, err := ValidateLayer(SectionByKey("rewrites"), doc(t, `{"rewrites": {"upsert": [{"answer": "1.2.3.4"}]}}`)); err == nil {
		t.Fatal("rewrite without domain accepted")
	}
}
