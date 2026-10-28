// Package demo runs GuardTower against a simulated fleet: in-process
// AdGuard Home look-alikes that speak enough of the /control API (and DNS)
// for every GuardTower feature, with generated traffic and history.
package demo

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/hurseyc/guardtower/internal/agh"
)

// LatestVersion is the AdGuard Home version simulated instances update to.
const LatestVersion = "v0.107.79"

const maxLog = 40000

// Sim is one simulated AdGuard Home instance.
type Sim struct {
	Name string
	// Profile shapes the generated traffic (see traffic.go).
	Profile Profile

	mu            sync.Mutex
	version       string
	canAutoUpdate bool
	protection    bool
	started       time.Time
	dnsAddr       string
	settings      map[string]map[string]any // dns_info, access/list, blocked_services/get, querylog/config, stats/config, safesearch/status
	toggles       map[string]bool           // safebrowsing, parental
	filtering     filteringState
	rewrites      []map[string]any
	clients       []map[string]any
	log           []agh.QueryLogEntry // oldest first
	nextFilterID  int64
	rng           *rand.Rand
}

type filteringState struct {
	Enabled   bool
	Interval  int
	Filters   []agh.Filter
	Allow     []agh.Filter
	UserRules []string
}

// NewSim creates a simulated instance with typical out-of-the-box settings.
func NewSim(name, version string, canAutoUpdate bool, p Profile, seed uint64) *Sim {
	s := &Sim{Name: name, Profile: p, version: version, canAutoUpdate: canAutoUpdate, protection: true, started: time.Now(),
		toggles: map[string]bool{"safebrowsing": false, "parental": false}, nextFilterID: 3,
		rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))} //nolint:gosec // simulated traffic, not security
	s.settings = map[string]map[string]any{
		"dns_info": {
			"upstream_dns": []any{"https://dns10.quad9.net/dns-query"}, "upstream_mode": "load_balance",
			"bootstrap_dns": []any{"9.9.9.10", "149.112.112.10"}, "fallback_dns": []any{}, "local_ptr_upstreams": []any{},
			"use_private_ptr_resolvers": true, "resolve_clients": true, "upstream_timeout": 10.0, "ratelimit": 20.0,
			"ratelimit_whitelist": []any{}, "blocking_mode": "default", "blocking_ipv4": "", "blocking_ipv6": "",
			"blocked_response_ttl": 10.0, "dnssec_enabled": false, "edns_cs_enabled": false, "disable_ipv6": false,
			"cache_enabled": true, "cache_size": 4194304.0, "cache_ttl_min": 0.0, "cache_ttl_max": 0.0, "cache_optimistic": false,
		},
		"access/list":          {"allowed_clients": []any{}, "disallowed_clients": []any{}, "blocked_hosts": []any{"version.bind", "id.server", "hostname.bind"}},
		"blocked_services/get": {"ids": []any{}, "schedule": map[string]any{"time_zone": "Local"}},
		"querylog/config":      {"enabled": true, "interval": 7776000000.0, "anonymize_client_ip": false, "ignored": []any{}, "ignored_enabled": true},
		"stats/config":         {"enabled": true, "interval": 86400000.0, "ignored": []any{}, "ignored_enabled": true},
		"safesearch/status": {"enabled": false, "bing": true, "duckduckgo": true, "ecosia": true, "google": true,
			"pixabay": true, "yandex": true, "youtube": true},
	}
	s.filtering = filteringState{Enabled: true, Interval: 24, Filters: []agh.Filter{
		{ID: 1, URL: "https://adguardteam.github.io/HostlistsRegistry/assets/filter_1.txt", Name: "AdGuard DNS filter", Enabled: true, RulesCount: 62480},
		{ID: 2, URL: "https://adguardteam.github.io/HostlistsRegistry/assets/filter_2.txt", Name: "AdAway Default Blocklist", Enabled: false, RulesCount: 6540},
	}, UserRules: []string{}}
	s.rewrites = []map[string]any{}
	s.clients = []map[string]any{}
	return s
}

// Version returns the simulated AdGuard Home version.
func (s *Sim) Version() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.version
}

// ServeHTTP implements the subset of /control/* GuardTower uses.
func (s *Sim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/control/")
	var body map[string]any
	if r.Method != http.MethodGet && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out, code := s.route(r.Method, path, r.URL.Query(), body)
	if code == 0 {
		code = http.StatusOK
	}
	if code != http.StatusOK {
		http.Error(w, fmt.Sprint(out), code)
		return
	}
	if out == nil {
		_, _ = w.Write([]byte("OK\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Sim) route(method, path string, q map[string][]string, body map[string]any) (any, int) {
	switch {
	case path == "status":
		return map[string]any{"version": s.version, "language": "en", "dns_addresses": []string{s.dnsHost()}, "dns_port": s.dnsPort(),
			"http_port": 80, "protection_enabled": s.protection, "running": true, "start_time": float64(s.started.UnixMilli())}, 0
	case path == "version.json":
		v := map[string]any{"disabled": false, "can_autoupdate": s.canAutoUpdate, "new_version": ""}
		if newer(LatestVersion, s.version) {
			v["new_version"] = LatestVersion
			v["announcement"] = "AdGuard Home " + LatestVersion + " is now available!"
			v["announcement_url"] = "https://github.com/AdguardTeam/AdGuardHome/releases/tag/" + LatestVersion
		}
		return v, 0
	case path == "update" && method == http.MethodPost:
		if !s.canAutoUpdate || !newer(LatestVersion, s.version) {
			return "no update available", http.StatusBadRequest
		}
		go func() { // the real updater restarts AdGuard Home a few seconds later
			time.Sleep(3 * time.Second)
			s.mu.Lock()
			s.version, s.started = LatestVersion, time.Now()
			s.mu.Unlock()
		}()
		return nil, 0
	case path == "protection":
		s.protection, _ = body["enabled"].(bool)
		return nil, 0
	case path == "stats":
		return s.stats(), 0
	case path == "querylog":
		return s.queryLog(first(q["older_than"]), first(q["limit"])), 0
	case path == "dns_info", path == "access/list", path == "blocked_services/get", path == "querylog/config",
		path == "stats/config", path == "safesearch/status":
		return s.settings[path], 0
	case path == "dns_config":
		for k, v := range body {
			s.settings["dns_info"][k] = v
		}
		return nil, 0
	case path == "access/set":
		s.settings["access/list"] = body
		return nil, 0
	case path == "blocked_services/update":
		s.settings["blocked_services/get"] = body
		return nil, 0
	case path == "safesearch/settings":
		s.settings["safesearch/status"] = body
		return nil, 0
	case path == "querylog/config/update", path == "stats/config/update":
		s.settings[strings.TrimSuffix(path, "/update")] = body
		return nil, 0
	case strings.HasSuffix(path, "/status") && (strings.HasPrefix(path, "safebrowsing") || strings.HasPrefix(path, "parental")):
		return map[string]any{"enabled": s.toggles[strings.TrimSuffix(path, "/status")]}, 0
	case strings.HasPrefix(path, "safebrowsing/") || strings.HasPrefix(path, "parental/"):
		k, action, _ := strings.Cut(path, "/")
		s.toggles[k] = action == "enable"
		return nil, 0
	case strings.HasPrefix(path, "filtering/"):
		return s.filteringRoute(strings.TrimPrefix(path, "filtering/"), body)
	case path == "rewrite/list":
		return s.rewrites, 0
	case path == "rewrite/add":
		if _, ok := body["enabled"]; !ok {
			body["enabled"] = true
		}
		s.rewrites = append(s.rewrites, body)
		return nil, 0
	case path == "rewrite/delete":
		s.rewrites = filterItems(s.rewrites, func(m map[string]any) bool { return m["domain"] == body["domain"] && m["answer"] == body["answer"] })
		return nil, 0
	case path == "clients":
		return map[string]any{"clients": s.clients, "auto_clients": []any{}, "supported_tags": []string{"device_phone", "device_tablet", "device_laptop", "user_child", "user_admin"}}, 0
	case path == "clients/add":
		s.clients = append(s.clients, body)
		return nil, 0
	case path == "clients/update":
		name := body["name"]
		data, _ := body["data"].(map[string]any)
		for i, c := range s.clients {
			if c["name"] == name {
				s.clients[i] = data
			}
		}
		return nil, 0
	case path == "clients/delete":
		s.clients = filterItems(s.clients, func(m map[string]any) bool { return m["name"] == body["name"] })
		return nil, 0
	}
	return "not found: /control/" + path, http.StatusNotFound
}

func (s *Sim) filteringRoute(op string, body map[string]any) (any, int) {
	f := &s.filtering
	list := func(white bool) *[]agh.Filter {
		if white {
			return &f.Allow
		}
		return &f.Filters
	}
	white, _ := body["whitelist"].(bool)
	switch op {
	case "status":
		return agh.FilteringStatus{Enabled: f.Enabled, Interval: f.Interval, Filters: nonNil(f.Filters), WhitelistFilters: nonNil(f.Allow), UserRules: f.UserRules}, 0
	case "config":
		f.Enabled, _ = body["enabled"].(bool)
		if v, ok := body["interval"].(float64); ok {
			f.Interval = int(v)
		}
	case "add_url":
		u, _ := body["url"].(string)
		name, _ := body["name"].(string)
		for _, x := range *list(white) {
			if x.URL == u {
				return "filter URL already added", http.StatusBadRequest
			}
		}
		*list(white) = append(*list(white), agh.Filter{ID: s.nextFilterID, URL: u, Name: name, Enabled: true, RulesCount: int64(1000 + s.rng.IntN(90000))})
		s.nextFilterID++
	case "remove_url":
		u, _ := body["url"].(string)
		l := list(white)
		out := (*l)[:0]
		for _, x := range *l {
			if x.URL != u {
				out = append(out, x)
			}
		}
		*l = out
	case "set_url":
		u, _ := body["url"].(string)
		data, _ := body["data"].(map[string]any)
		for i, x := range *list(white) {
			if x.URL == u {
				x.Name, _ = data["name"].(string)
				x.Enabled, _ = data["enabled"].(bool)
				if nu, ok := data["url"].(string); ok && nu != "" {
					x.URL = nu
				}
				(*list(white))[i] = x
			}
		}
	case "set_rules":
		f.UserRules = []string{}
		if rr, ok := body["rules"].([]any); ok {
			for _, r := range rr {
				if s, ok := r.(string); ok {
					f.UserRules = append(f.UserRules, s)
				}
			}
		}
	default:
		return "not found: /control/filtering/" + op, http.StatusNotFound
	}
	return nil, 0
}

func (s *Sim) queryLog(olderThan, limit string) agh.QueryLogPage {
	n, _ := strconv.Atoi(limit)
	if n <= 0 || n > 1000 {
		n = 500
	}
	end := len(s.log)
	if olderThan != "" {
		if t, err := time.Parse(time.RFC3339Nano, olderThan); err == nil {
			end = sort.Search(len(s.log), func(i int) bool { return !s.log[i].Time.Before(t) })
		}
	}
	page := agh.QueryLogPage{Data: []agh.QueryLogEntry{}}
	for i := end - 1; i >= 0 && len(page.Data) < n; i-- {
		page.Data = append(page.Data, s.log[i])
	}
	if len(page.Data) > 0 {
		page.Oldest = page.Data[len(page.Data)-1].Time.Format(time.RFC3339Nano)
	}
	return page
}

func (s *Sim) stats() map[string]any {
	now := time.Now()
	hours := make([]int64, 24)
	blockedH := make([]int64, 24)
	parentalH := make([]int64, 24)
	sbH := make([]int64, 24)
	var total, blocked, parental, sb, ss int64
	var elapsed float64
	domains, blockedDomains, clients, upstreams := map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}
	for i := len(s.log) - 1; i >= 0; i-- {
		e := s.log[i]
		age := now.Sub(e.Time)
		if age > 24*time.Hour {
			break
		}
		h := 23 - int(age/time.Hour)
		total++
		hours[h]++
		ms, _ := strconv.ParseFloat(e.ElapsedMs, 64)
		elapsed += ms
		d := strings.TrimSuffix(e.Question.Name, ".")
		domains[d]++
		clients[e.Client]++
		if e.Upstream != "" {
			upstreams[e.Upstream]++
		}
		switch e.Reason {
		case "FilteredBlackList", "FilteredBlockedService":
			blocked++
			blockedH[h]++
			blockedDomains[d]++
		case "FilteredParental":
			parental++
			parentalH[h]++
			blockedDomains[d]++
		case "FilteredSafeBrowsing":
			sb++
			sbH[h]++
			blockedDomains[d]++
		case "FilteredSafeSearch":
			ss++
		}
	}
	avg := 0.0
	if total > 0 {
		avg = elapsed / float64(total) / 1000
	}
	up := []map[string]float64{}
	for u := range upstreams {
		up = append(up, map[string]float64{u: 0.012 + s.rng.Float64()*0.02})
	}
	return map[string]any{
		"time_units": "hours", "num_dns_queries": total, "num_blocked_filtering": blocked, "num_replaced_safebrowsing": sb,
		"num_replaced_safesearch": ss, "num_replaced_parental": parental, "avg_processing_time": avg,
		"dns_queries": hours, "blocked_filtering": blockedH, "replaced_safebrowsing": sbH, "replaced_parental": parentalH,
		"top_queried_domains": top(domains), "top_blocked_domains": top(blockedDomains), "top_clients": top(clients),
		"top_upstreams_responses": top(upstreams), "top_upstreams_avg_time": up,
	}
}

func top(m map[string]int64) []map[string]int64 {
	const n = 10
	type kv struct {
		k string
		v int64
	}
	var l []kv
	for k, v := range m {
		l = append(l, kv{k, v})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].v > l[j].v || (l[i].v == l[j].v && l[i].k < l[j].k) })
	out := []map[string]int64{}
	for i := 0; i < len(l) && i < n; i++ {
		out = append(out, map[string]int64{l[i].k: l[i].v})
	}
	return out
}

func (s *Sim) dnsHost() string {
	h, _, _ := net.SplitHostPort(s.dnsAddr)
	return h
}

func (s *Sim) dnsPort() int {
	_, p, _ := net.SplitHostPort(s.dnsAddr)
	n, _ := strconv.Atoi(p)
	return n
}

// serveDNS answers queries GuardTower's health probes send: names in the
// probe zone get NXDOMAIN, everything else a documentation address.
func (s *Sim) serveDNS(w dns.ResponseWriter, req *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(req)
	if len(req.Question) == 1 {
		q := req.Question[0]
		if strings.HasSuffix(q.Name, ".example.") {
			m.Rcode = dns.RcodeNameError
		} else if q.Qtype == dns.TypeA {
			m.Answer = append(m.Answer, &dns.A{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.IPv4(192, 0, 2, 10)})
		}
	}
	_ = w.WriteMsg(m)
}

func filterItems(l []map[string]any, drop func(map[string]any) bool) []map[string]any {
	out := []map[string]any{}
	for _, m := range l {
		if !drop(m) {
			out = append(out, m)
		}
	}
	return out
}

func nonNil(f []agh.Filter) []agh.Filter {
	if f == nil {
		return []agh.Filter{}
	}
	return f
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// newer compares vX.Y.Z versions.
func newer(a, b string) bool {
	pa, pb := parts(a), parts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parts(v string) [3]int {
	var out [3]int
	for i, p := range strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}
