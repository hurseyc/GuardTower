package demo

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hurseyc/guardtower/internal/agh"
)

// Device is a simulated client on the network.
type Device struct {
	IP       string
	Hostname string
	// Weight is the device's share of the instance's traffic.
	Weight float64
	// Mix is how likely each category is, per query (missing = "general").
	Mix map[string]float64
	// Night makes the device's risky lookups cluster late in the evening.
	Night bool
}

// Profile is the traffic an instance sees.
type Profile struct {
	QPS     float64 // average queries per second
	Devices []Device
}

// Domain categories used by the generator. Lists decide what blocks them.
var catalog = map[string][]string{
	"general": {"www.google.com", "www.youtube.com", "github.com", "en.wikipedia.org", "www.apple.com", "gateway.icloud.com",
		"www.netflix.com", "api.spotify.com", "www.amazon.com", "login.microsoftonline.com", "outlook.office.com", "www.reddit.com",
		"discord.com", "steamcommunity.com", "www.roblox.com", "www.instagram.com", "www.tiktok.com", "www.facebook.com",
		"web.whatsapp.com", "www.cloudflare.com", "deb.debian.org", "www.proxmox.com", "www.minecraft.net", "www.bbc.co.uk",
		"time.apple.com", "connectivitycheck.gstatic.com", "www.twitch.tv", "open.spotify.com", "www.duckduckgo.com", "www.bing.com"},
	"ads": {"ad.doubleclick.net", "pagead2.googlesyndication.com", "www.google-analytics.com", "ads.yahoo.com",
		"adservice.google.com", "app-measurement.com", "pixel.facebook.com", "sb.scorecardresearch.com", "ads.tiktok.com",
		"analytics.twitter.com", "bat.bing.com", "cdn.taboola.com"},
	"adult":    {"www.pornhub.com", "www.xvideos.com", "onlyfans.com", "xhamster.com", "www.youporn.com", "chaturbate.com"},
	"gambling": {"www.bet365.com", "www.pokerstars.com", "www.williamhill.com"},
	"bypass":   {"dns.google", "mozilla.cloudflare-dns.com", "nordvpn.com", "www.torproject.org", "use-application-dns.net", "protonvpn.com"},
	"malware":  {"malware.testcategory.com"},
}

// services maps AdGuard Home blocked-service IDs to the domains they cover.
var services = map[string][]string{
	"tiktok": {"tiktok.com"}, "youtube": {"youtube.com"}, "roblox": {"roblox.com"}, "instagram": {"instagram.com"},
	"facebook": {"facebook.com"}, "discord": {"discord.com"}, "reddit": {"reddit.com"}, "twitch": {"twitch.tv"},
	"netflix": {"netflix.com"}, "steam": {"steamcommunity.com"}, "minecraft": {"minecraft.net"},
}

// listCategory says which generated category a filter list blocks.
func listCategory(url, name string) string {
	s := strings.ToLower(url + " " + name)
	switch {
	case strings.Contains(s, "nsfw"), strings.Contains(s, "porn"), strings.Contains(s, "adult"):
		return "adult"
	case strings.Contains(s, "gambling"):
		return "gambling"
	case strings.Contains(s, "doh"), strings.Contains(s, "vpn"), strings.Contains(s, "bypass"):
		return "bypass"
	}
	return "ads"
}

func categoryOf(domain string) string {
	for cat, ds := range catalog {
		for _, d := range ds {
			if d == domain {
				return cat
			}
		}
	}
	return "general"
}

func matches(domain, suffix string) bool {
	return domain == suffix || strings.HasSuffix(domain, "."+suffix)
}

// ruleDomain extracts d from "||d^" (and "@@||d^").
func ruleDomain(rule string) (string, bool) {
	r := strings.TrimPrefix(rule, "@@")
	if !strings.HasPrefix(r, "||") || !strings.HasSuffix(r, "^") {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(r, "||"), "^"), true
}

// resolve decides how the instance answers domain with its current settings.
// The caller holds s.mu.
func (s *Sim) resolve(e *agh.QueryLogEntry, domain string) {
	cat := categoryOf(domain)
	e.Reason = "NotFilteredNotFound"
	block := func(reason string, listID int64, rule string) {
		e.Reason = reason
		if rule != "" {
			e.Rules = []agh.QueryRule{{FilterListID: listID, Text: rule}}
		}
	}
	switch {
	case !s.protection:
	case s.userRule(e, domain):
	case s.blockedService(e, domain):
	case s.rewrite(e, domain):
	case cat == "adult" && s.toggles["parental"]:
		block("FilteredParental", 0, "")
	case cat == "malware" && s.toggles["safebrowsing"]:
		block("FilteredSafeBrowsing", 0, "")
	case s.safeSearch(domain):
		block("FilteredSafeSearch", 0, "")
	case s.filtering.Enabled:
		for _, f := range s.filtering.Filters {
			if f.Enabled && cat != "general" && cat != "malware" && listCategory(f.URL, f.Name) == cat {
				block("FilteredBlackList", f.ID, "||"+domain+"^")
				break
			}
		}
	}
	if e.Reason == "NotFilteredNotFound" || e.Reason == "NotFilteredAllowList" || e.Reason == "FilteredSafeSearch" {
		if up, ok := s.settings["dns_info"]["upstream_dns"].([]any); ok && len(up) > 0 {
			e.Upstream, _ = up[s.rng.IntN(len(up))].(string)
		}
		e.Cached = s.rng.IntN(10) < 3
		if e.Question.Type == "A" {
			e.Answer = []agh.DNSAnswer{{Type: "A", Value: fmt.Sprintf("203.0.113.%d", 1+s.rng.IntN(250)), TTL: 300}}
		}
	}
}

func (s *Sim) userRule(e *agh.QueryLogEntry, domain string) bool {
	if !s.filtering.Enabled {
		return false
	}
	for _, r := range s.filtering.UserRules {
		if d, ok := ruleDomain(r); ok && matches(domain, d) {
			e.Rules = []agh.QueryRule{{FilterListID: 0, Text: r}}
			e.Reason = "FilteredBlackList"
			if strings.HasPrefix(r, "@@") {
				e.Reason = "NotFilteredAllowList"
			}
			return true
		}
	}
	return false
}

func (s *Sim) blockedService(e *agh.QueryLogEntry, domain string) bool {
	ids, _ := s.settings["blocked_services/get"]["ids"].([]any)
	for _, id := range ids {
		sid, _ := id.(string)
		for _, d := range services[sid] {
			if matches(domain, d) {
				e.Reason, e.ServiceName = "FilteredBlockedService", sid
				e.Rules = []agh.QueryRule{{Text: "||" + d + "^"}}
				return true
			}
		}
	}
	return false
}

func (s *Sim) rewrite(e *agh.QueryLogEntry, domain string) bool {
	for _, r := range s.rewrites {
		if d, _ := r["domain"].(string); d == domain && r["enabled"] != false {
			e.Reason = "Rewrite"
			ans, _ := r["answer"].(string)
			e.Answer = []agh.DNSAnswer{{Type: "A", Value: ans, TTL: 10}}
			return true
		}
	}
	return false
}

func (s *Sim) safeSearch(domain string) bool {
	ss := s.settings["safesearch/status"]
	if ss["enabled"] != true {
		return false
	}
	for engine, d := range map[string]string{"google": "www.google.com", "youtube": "www.youtube.com", "bing": "www.bing.com", "duckduckgo": "www.duckduckgo.com"} {
		if domain == d && ss[engine] == true {
			return true
		}
	}
	return false
}

// diurnal scales traffic by time of day: quiet at night, busy in the evening.
func diurnal(t time.Time) float64 {
	h := float64(t.Hour()) + float64(t.Minute())/60
	day := math.Sin(math.Pi * math.Max(0, h-6) / 18)
	return 0.25 + 0.75*day*day + 0.35*math.Exp(-(h-20.5)*(h-20.5)/4)
}

func (s *Sim) pick(t time.Time) (Device, string) {
	devs := s.Profile.Devices
	total := 0.0
	for _, d := range devs {
		total += d.Weight
	}
	x := s.rng.Float64() * total
	dev := devs[len(devs)-1]
	for _, d := range devs {
		if x < d.Weight {
			dev = d
			break
		}
		x -= d.Weight
	}
	cat := "general"
	y := s.rng.Float64()
	for _, c := range []string{"ads", "adult", "gambling", "bypass", "malware"} {
		p := dev.Mix[c]
		if c != "ads" && dev.Night {
			// Risky lookups are rare in the daytime and peak around 23:00.
			h := float64(t.Hour())
			if h < 6 {
				h += 24
			}
			p *= 0.2 + 3*math.Exp(-(h-23)*(h-23)/3)
		}
		if y < p {
			cat = c
			break
		}
		y -= p
	}
	ds := catalog[cat]
	return dev, ds[s.rng.IntN(len(ds))]
}

// entry generates one query at t with the current settings. The caller holds s.mu.
func (s *Sim) entry(t time.Time) agh.QueryLogEntry {
	dev, domain := s.pick(t)
	qt := "A"
	if s.rng.IntN(4) == 0 {
		qt = "AAAA"
	}
	e := agh.QueryLogEntry{Time: t, Client: dev.IP, ClientProto: "", Question: agh.QueryQuestion{Class: "IN", Name: domain, Type: qt},
		Status: "NOERROR", ElapsedMs: fmt.Sprintf("%.3f", 0.2+s.rng.ExpFloat64()*14)}
	name := dev.Hostname
	for _, c := range s.clients {
		ids, _ := c["ids"].([]any)
		for _, id := range ids {
			if id == dev.IP {
				name, _ = c["name"].(string)
			}
		}
	}
	e.ClientInfo = &agh.QueryClient{Name: name}
	s.resolve(&e, domain)
	return e
}

// Generate returns queries between from and to (oldest first), without
// adding them to the instance's own log.
func (s *Sim) Generate(from, to time.Time) []agh.QueryLogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generate(from, to)
}

func (s *Sim) generate(from, to time.Time) []agh.QueryLogEntry {
	var out []agh.QueryLogEntry
	t := from
	for {
		rate := s.Profile.QPS * diurnal(t)
		t = t.Add(time.Duration(s.rng.ExpFloat64() / rate * float64(time.Second)))
		if !t.Before(to) {
			return out
		}
		out = append(out, s.entry(t))
	}
}

// Prime fills the instance's own query log with the last d of traffic.
func (s *Sim) Prime(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.log = append(s.log, s.generate(now.Add(-d), now)...)
}

// Run adds live traffic until ctx ends.
func (s *Sim) Run(ctx context.Context) {
	last := time.Now()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.mu.Lock()
			s.log = append(s.log, s.generate(last, now)...)
			if len(s.log) > maxLog {
				s.log = append([]agh.QueryLogEntry(nil), s.log[len(s.log)-maxLog:]...)
			}
			s.mu.Unlock()
			last = now
		}
	}
}
