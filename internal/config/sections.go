package config

import (
	"context"
	"reflect"
	"sort"

	"github.com/hurseyc/guardtower/internal/agh"
)

func keyBy(field string) func(map[string]any) string {
	return func(m map[string]any) string { return str(m, field) }
}

// itemPick limits keyed items to the attributes GuardTower manages.
func itemPick(fields ...string) func(map[string]any) map[string]any {
	return func(m map[string]any) map[string]any {
		out := map[string]any{}
		for _, f := range fields {
			if v, ok := m[f]; ok {
				out[f] = v
			}
		}
		return out
	}
}

func pickItems(v any, fn func(map[string]any) map[string]any) []any {
	out := []any{}
	for _, it := range toItems(v) {
		out = append(out, fn(it))
	}
	return out
}

func init() {
	Sections = []*Section{dnsSection(), filteringSection(), rewritesSection(), clientsSection(), accessSection(),
		blockedServicesSection(), safeBrowsingSection(), parentalSection(), safeSearchSection(), queryLogSection(), statsSection()}
}

// ---- DNS ----

func dnsSection() *Section {
	s := &Section{Key: "dns", Label: "DNS", Help: "Upstream servers, caching, rate limits and how blocked queries are answered.",
		Fields: []Field{
			{Name: "upstream_dns", Label: "Upstream DNS servers", Kind: KindList, Help: "One per line, e.g. https://dns.quad9.net/dns-query or [/home.lan/]192.168.1.1"},
			{Name: "upstream_mode", Label: "Upstream mode", Kind: KindString, Help: "load_balance (default), parallel or fastest_addr"},
			{Name: "bootstrap_dns", Label: "Bootstrap DNS servers", Kind: KindList, Help: "Plain DNS servers used to resolve the upstream hostnames"},
			{Name: "fallback_dns", Label: "Fallback DNS servers", Kind: KindList},
			{Name: "local_ptr_upstreams", Label: "Private reverse DNS servers", Kind: KindList},
			{Name: "use_private_ptr_resolvers", Label: "Use private reverse DNS resolvers", Kind: KindBool},
			{Name: "resolve_clients", Label: "Resolve client hostnames", Kind: KindBool},
			{Name: "upstream_timeout", Label: "Upstream timeout (seconds)", Kind: KindInt},
			{Name: "ratelimit", Label: "Rate limit (queries/second per client, 0 = off)", Kind: KindInt},
			{Name: "ratelimit_whitelist", Label: "Rate-limit exempt IPs", Kind: KindSet},
			{Name: "blocking_mode", Label: "Blocking mode", Kind: KindEnum, Enum: []string{"default", "refused", "nxdomain", "null_ip", "custom_ip"}},
			{Name: "blocking_ipv4", Label: "Custom blocking IPv4", Kind: KindString, Help: "Only for blocking mode custom_ip"},
			{Name: "blocking_ipv6", Label: "Custom blocking IPv6", Kind: KindString, Help: "Only for blocking mode custom_ip"},
			{Name: "blocked_response_ttl", Label: "Blocked response TTL (seconds)", Kind: KindInt},
			{Name: "dnssec_enabled", Label: "Enable DNSSEC", Kind: KindBool},
			{Name: "edns_cs_enabled", Label: "Enable EDNS Client Subnet", Kind: KindBool},
			{Name: "disable_ipv6", Label: "Disable resolving of IPv6 addresses", Kind: KindBool},
			{Name: "cache_enabled", Label: "Enable cache", Kind: KindBool},
			{Name: "cache_size", Label: "Cache size (bytes)", Kind: KindInt},
			{Name: "cache_ttl_min", Label: "Minimum cache TTL (seconds)", Kind: KindInt},
			{Name: "cache_ttl_max", Label: "Maximum cache TTL (seconds)", Kind: KindInt},
			{Name: "cache_optimistic", Label: "Optimistic caching", Kind: KindBool},
		}}
	s.read = func(ctx context.Context, c *agh.Client) (Doc, error) {
		m, err := getMap(ctx, c, "dns_info")
		if err != nil {
			return nil, err
		}
		return pick(s, m), nil
	}
	s.apply = func(ctx context.Context, c *agh.Client, desired, _ Doc) error {
		// dns_config accepts partial updates: only the managed fields are sent.
		return c.Post(ctx, "dns_config", map[string]any(desired), nil)
	}
	return s
}

// ---- Filtering ----

var filterItem = itemPick("url", "name", "enabled")

func filteringSection() *Section {
	s := &Section{Key: "filtering", Label: "Filter lists & custom rules", Help: "Block lists, allow lists and your own filtering rules.",
		Fields: []Field{
			{Name: "enabled", Label: "Filtering enabled", Kind: KindBool},
			{Name: "interval", Label: "List update interval (hours)", Kind: KindInt},
			{Name: "filters", Label: "Block lists", Kind: KindKeyed, KeyLabel: "URL", keyFn: keyBy("url"),
				ItemFields: []Field{{Name: "name", Label: "Name", Kind: KindString}, {Name: "url", Label: "URL", Kind: KindString}, {Name: "enabled", Label: "Enabled", Kind: KindBool}}},
			{Name: "whitelist_filters", Label: "Allow lists", Kind: KindKeyed, KeyLabel: "URL", keyFn: keyBy("url"),
				ItemFields: []Field{{Name: "name", Label: "Name", Kind: KindString}, {Name: "url", Label: "URL", Kind: KindString}, {Name: "enabled", Label: "Enabled", Kind: KindBool}}},
			{Name: "user_rules", Label: "Custom filtering rules", Kind: KindSet, Help: "AdGuard/Adblock-style rules, one per line, e.g. ||example.com^ or @@||allowed.com^"},
		}}
	s.read = func(ctx context.Context, c *agh.Client) (Doc, error) {
		m, err := getMap(ctx, c, "filtering/status")
		if err != nil {
			return nil, err
		}
		m["filters"] = pickItems(m["filters"], filterItem)
		m["whitelist_filters"] = pickItems(m["whitelist_filters"], filterItem)
		return pick(s, m), nil
	}
	s.apply = func(ctx context.Context, c *agh.Client, desired, live Doc) error {
		if _, a := desired["enabled"]; a || desired["interval"] != nil {
			body := map[string]any{"enabled": pickVal(desired, live, "enabled"), "interval": pickVal(desired, live, "interval")}
			if err := c.Post(ctx, "filtering/config", body, nil); err != nil {
				return errf("filtering", "update settings: %w", err)
			}
		}
		for _, list := range []struct {
			field     string
			whitelist bool
		}{{"filters", false}, {"whitelist_filters", true}} {
			want, ok := desired[list.field]
			if !ok {
				continue
			}
			if err := syncFilterLists(ctx, c, toItems(want), toItems(live[list.field]), list.whitelist); err != nil {
				return err
			}
		}
		if rules, ok := desired["user_rules"]; ok {
			if err := c.Post(ctx, "filtering/set_rules", map[string]any{"rules": toStrings(rules)}, nil); err != nil {
				return errf("filtering", "set custom rules: %w", err)
			}
		}
		return nil
	}
	return s
}

func pickVal(desired, live Doc, k string) any {
	if v, ok := desired[k]; ok {
		return v
	}
	return live[k]
}

func syncFilterLists(ctx context.Context, c *agh.Client, want, have []map[string]any, whitelist bool) error {
	haveBy := map[string]map[string]any{}
	for _, h := range have {
		haveBy[str(h, "url")] = h
	}
	wantBy := map[string]bool{}
	for _, w := range want {
		url := str(w, "url")
		wantBy[url] = true
		enabled, _ := w["enabled"].(bool)
		h, exists := haveBy[url]
		if !exists {
			if err := c.Post(ctx, "filtering/add_url", map[string]any{"name": str(w, "name"), "url": url, "whitelist": whitelist}, nil); err != nil {
				return errf("filtering", "add list %s: %w", url, err)
			}
			if !enabled {
				if err := setFilterURL(ctx, c, w, whitelist); err != nil {
					return err
				}
			}
			continue
		}
		if !reflect.DeepEqual(filterItem(h), filterItem(w)) {
			if err := setFilterURL(ctx, c, w, whitelist); err != nil {
				return err
			}
		}
	}
	for url := range haveBy {
		if !wantBy[url] {
			if err := c.Post(ctx, "filtering/remove_url", map[string]any{"url": url, "whitelist": whitelist}, nil); err != nil {
				return errf("filtering", "remove list %s: %w", url, err)
			}
		}
	}
	return nil
}

func setFilterURL(ctx context.Context, c *agh.Client, w map[string]any, whitelist bool) error {
	url := str(w, "url")
	enabled, _ := w["enabled"].(bool)
	err := c.Post(ctx, "filtering/set_url", map[string]any{"url": url, "whitelist": whitelist,
		"data": map[string]any{"name": str(w, "name"), "url": url, "enabled": enabled}}, nil)
	if err != nil {
		return errf("filtering", "update list %s: %w", url, err)
	}
	return nil
}

// ---- Rewrites ----

func rewriteKey(m map[string]any) string { return str(m, "domain") + " → " + str(m, "answer") }

func rewritesSection() *Section {
	s := &Section{Key: "rewrites", Label: "DNS rewrites", Help: "Answer specific domains with your own addresses, e.g. nas.home → 192.168.1.20.",
		Fields: []Field{
			{Name: "rewrites", Label: "Rewrites", Kind: KindKeyed, KeyLabel: "Domain → answer", keyFn: rewriteKey,
				ItemFields: []Field{{Name: "domain", Label: "Domain", Kind: KindString}, {Name: "answer", Label: "Answer (IP or domain)", Kind: KindString}, {Name: "enabled", Label: "Enabled", Kind: KindBool}}},
		}}
	s.read = func(ctx context.Context, c *agh.Client) (Doc, error) {
		var list []map[string]any
		if err := c.Get(ctx, "rewrite/list", &list); err != nil {
			return nil, err
		}
		items := []any{}
		for _, r := range list {
			it := itemPick("domain", "answer", "enabled")(r)
			if _, ok := it["enabled"]; !ok {
				it["enabled"] = true // releases before the enabled flag treat every rewrite as on
			}
			items = append(items, it)
		}
		return pick(s, map[string]any{"rewrites": items}), nil
	}
	s.apply = func(ctx context.Context, c *agh.Client, desired, live Doc) error {
		want, ok := desired["rewrites"]
		if !ok {
			return nil
		}
		haveBy := map[string]map[string]any{}
		for _, h := range toItems(live["rewrites"]) {
			haveBy[rewriteKey(h)] = h
		}
		wantBy := map[string]map[string]any{}
		for _, w := range toItems(want) {
			wantBy[rewriteKey(w)] = w
		}
		for k, h := range haveBy {
			if w, keep := wantBy[k]; !keep || !reflect.DeepEqual(asJSON(w), asJSON(h)) {
				if err := c.Post(ctx, "rewrite/delete", map[string]any{"domain": h["domain"], "answer": h["answer"]}, nil); err != nil {
					return errf("rewrites", "delete %s: %w", k, err)
				}
			}
		}
		for k, w := range wantBy {
			if h, exists := haveBy[k]; exists && reflect.DeepEqual(asJSON(w), asJSON(h)) {
				continue
			}
			body := map[string]any{"domain": w["domain"], "answer": w["answer"]}
			if en, ok := w["enabled"]; ok {
				body["enabled"] = en
			}
			if err := c.Post(ctx, "rewrite/add", body, nil); err != nil {
				return errf("rewrites", "add %s: %w", k, err)
			}
		}
		return nil
	}
	return s
}

// ---- Clients ----

// clientItem normalises a persistent client so equal clients compare equal
// (nil vs empty arrays, unordered IDs and tags).
func clientItem(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	for _, k := range []string{"ids", "tags", "upstreams", "blocked_services"} {
		ss := toStrings(out[k])
		if k != "upstreams" {
			sort.Strings(ss)
		}
		arr := make([]any, len(ss))
		for i, s := range ss {
			arr[i] = s
		}
		out[k] = arr
	}
	return out
}

func clientsSection() *Section {
	s := &Section{Key: "clients", Label: "Persistent clients", Help: "Named devices with their own settings (identified by IP, MAC, CIDR or ClientID).",
		Fields: []Field{
			{Name: "clients", Label: "Clients", Kind: KindKeyed, KeyLabel: "Name", keyFn: keyBy("name"),
				ItemFields: []Field{{Name: "name", Label: "Name", Kind: KindString}, {Name: "ids", Label: "Identifiers", Kind: KindList},
					{Name: "tags", Label: "Tags", Kind: KindList}, {Name: "use_global_settings", Label: "Use global settings", Kind: KindBool},
					{Name: "use_global_blocked_services", Label: "Use global blocked services", Kind: KindBool}}},
		}}
	s.read = func(ctx context.Context, c *agh.Client) (Doc, error) {
		m, err := getMap(ctx, c, "clients")
		if err != nil {
			return nil, err
		}
		return pick(s, map[string]any{"clients": pickItems(m["clients"], clientItem)}), nil
	}
	s.apply = func(ctx context.Context, c *agh.Client, desired, live Doc) error {
		want, ok := desired["clients"]
		if !ok {
			return nil
		}
		haveBy := map[string]map[string]any{}
		for _, h := range toItems(live["clients"]) {
			haveBy[str(h, "name")] = h
		}
		wantBy := map[string]bool{}
		for _, w := range toItems(want) {
			name := str(w, "name")
			wantBy[name] = true
			h, exists := haveBy[name]
			switch {
			case !exists:
				if err := c.Post(ctx, "clients/add", w, nil); err != nil {
					return errf("clients", "add %s: %w", name, err)
				}
			case !reflect.DeepEqual(asJSON(clientItem(w)), asJSON(clientItem(h))):
				if err := c.Post(ctx, "clients/update", map[string]any{"name": name, "data": w}, nil); err != nil {
					return errf("clients", "update %s: %w", name, err)
				}
			}
		}
		for name := range haveBy {
			if !wantBy[name] {
				if err := c.Post(ctx, "clients/delete", map[string]any{"name": name}, nil); err != nil {
					return errf("clients", "delete %s: %w", name, err)
				}
			}
		}
		return nil
	}
	return s
}

// ---- Access ----

func accessSection() *Section {
	s := &Section{Key: "access", Label: "Access control", Help: "Which clients may use DNS, and domains that are never answered.",
		Fields: []Field{
			{Name: "allowed_clients", Label: "Allowed clients", Kind: KindSet, Help: "If set, only these clients may use DNS"},
			{Name: "disallowed_clients", Label: "Disallowed clients", Kind: KindSet},
			{Name: "blocked_hosts", Label: "Disallowed domains", Kind: KindSet},
		}}
	s.read = func(ctx context.Context, c *agh.Client) (Doc, error) {
		m, err := getMap(ctx, c, "access/list")
		if err != nil {
			return nil, err
		}
		return pick(s, m), nil
	}
	s.apply = func(ctx context.Context, c *agh.Client, desired, live Doc) error {
		return c.Post(ctx, "access/set", merged(live, desired), nil)
	}
	return s
}

// ---- Blocked services ----

func blockedServicesSection() *Section {
	s := &Section{Key: "blocked_services", Label: "Blocked services", Help: "Block whole services (TikTok, Roblox, …) and optionally schedule when the block applies.",
		Fields: []Field{
			{Name: "ids", Label: "Blocked services", Kind: KindSet, Help: "Service IDs, e.g. tiktok, roblox, youtube"},
			{Name: "schedule", Label: "Pause schedule", Kind: KindObject, Help: "When the blocks are lifted, per weekday (AdGuard Home format)"},
		}}
	s.read = func(ctx context.Context, c *agh.Client) (Doc, error) {
		m, err := getMap(ctx, c, "blocked_services/get")
		if err != nil {
			return nil, err
		}
		return pick(s, m), nil
	}
	s.apply = func(ctx context.Context, c *agh.Client, desired, live Doc) error {
		return c.Put(ctx, "blocked_services/update", merged(live, desired), nil)
	}
	return s
}

// ---- Safe browsing / parental / safe search ----

func toggleSection(key, label, help, path string) *Section {
	s := &Section{Key: key, Label: label, Help: help, Fields: []Field{{Name: "enabled", Label: "Enabled", Kind: KindBool}}}
	s.read = func(ctx context.Context, c *agh.Client) (Doc, error) {
		m, err := getMap(ctx, c, path+"/status")
		if err != nil {
			return nil, err
		}
		return pick(s, m), nil
	}
	s.apply = func(ctx context.Context, c *agh.Client, desired, _ Doc) error {
		on, ok := desired["enabled"].(bool)
		if !ok {
			return nil
		}
		action := "disable"
		if on {
			action = "enable"
		}
		return c.Post(ctx, path+"/"+action, nil, nil)
	}
	return s
}

func safeBrowsingSection() *Section {
	return toggleSection("safebrowsing", "Safe Browsing", "Block known malware and phishing domains using AdGuard's service.", "safebrowsing")
}

func parentalSection() *Section {
	return toggleSection("parental", "Parental control", "Block adult websites using AdGuard's adult-content service.", "parental")
}

func safeSearchSection() *Section {
	s := &Section{Key: "safesearch", Label: "Safe Search", Help: "Force safe results on search engines and YouTube.",
		Fields: []Field{
			{Name: "enabled", Label: "Enforce Safe Search", Kind: KindBool},
			{Name: "google", Label: "Google", Kind: KindBool}, {Name: "bing", Label: "Bing", Kind: KindBool},
			{Name: "duckduckgo", Label: "DuckDuckGo", Kind: KindBool}, {Name: "ecosia", Label: "Ecosia", Kind: KindBool},
			{Name: "yandex", Label: "Yandex", Kind: KindBool}, {Name: "pixabay", Label: "Pixabay", Kind: KindBool},
			{Name: "youtube", Label: "YouTube", Kind: KindBool},
		}}
	s.read = func(ctx context.Context, c *agh.Client) (Doc, error) {
		m, err := getMap(ctx, c, "safesearch/status")
		if err != nil {
			return nil, err
		}
		return pick(s, m), nil
	}
	s.apply = func(ctx context.Context, c *agh.Client, desired, live Doc) error {
		return c.Put(ctx, "safesearch/settings", merged(live, desired), nil)
	}
	return s
}

// ---- Query log & statistics settings ----

func logConfigSection(key, label, help, path string, extra ...Field) *Section {
	fields := []Field{
		{Name: "enabled", Label: "Enabled", Kind: KindBool},
		{Name: "interval", Label: "Retention", Kind: KindDuration, Help: "How long AdGuard Home keeps data, in milliseconds (e.g. 86400000 = 1 day)"},
		{Name: "ignored", Label: "Ignored domains", Kind: KindSet},
		{Name: "ignored_enabled", Label: "Use ignored-domain list", Kind: KindBool},
	}
	s := &Section{Key: key, Label: label, Help: help, Fields: append(fields, extra...)}
	s.read = func(ctx context.Context, c *agh.Client) (Doc, error) {
		m, err := getMap(ctx, c, path)
		if err != nil {
			return nil, err
		}
		return pick(s, m), nil
	}
	s.apply = func(ctx context.Context, c *agh.Client, desired, live Doc) error {
		body := merged(live, desired)
		if _, supported := live["ignored_enabled"]; !supported {
			delete(body, "ignored_enabled") // older releases reject unknown fields
		}
		return c.Put(ctx, path+"/update", body, nil)
	}
	return s
}

func queryLogSection() *Section {
	return logConfigSection("querylog", "Query log settings", "AdGuard Home's own query-log retention. GuardTower keeps its own copy separately.", "querylog/config",
		Field{Name: "anonymize_client_ip", Label: "Anonymise client IPs", Kind: KindBool})
}

func statsSection() *Section {
	return logConfigSection("stats", "Statistics settings", "AdGuard Home's built-in statistics retention.", "stats/config")
}

// Read fetches the live value of every section from an instance.
func Read(ctx context.Context, c *agh.Client) (map[string]Doc, error) {
	out := map[string]Doc{}
	for _, s := range Sections {
		d, err := s.read(ctx, c)
		if err != nil {
			return nil, errf(s.Key, "read: %w", err)
		}
		out[s.Key] = d
	}
	return out, nil
}

// ReadSection fetches one section's live value.
func ReadSection(ctx context.Context, c *agh.Client, key string) (Doc, error) {
	s := SectionByKey(key)
	if s == nil {
		return nil, errf(key, "unknown section")
	}
	return s.read(ctx, c)
}

// ApplySection writes desired values (managed fields only) to an instance.
func ApplySection(ctx context.Context, c *agh.Client, key string, desired, live Doc) error {
	s := SectionByKey(key)
	if s == nil {
		return errf(key, "unknown section")
	}
	return s.apply(ctx, c, desired, live)
}
