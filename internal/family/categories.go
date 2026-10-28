// Package family turns the query log into something a parent can read: who
// tried to reach adult (and other notable) sites, from which device, and
// when, with bursts collapsed into attempts, masking, bypass signals and
// alerts.
package family

import (
	"strings"

	"golang.org/x/net/publicsuffix"

	"github.com/hurseyc/guardtower/internal/querylog"
)

// Categories, in display order.
const (
	Adult      = "adult"
	Gambling   = "gambling"
	Bypass     = "bypass" // DoH/DoT resolvers, VPNs, proxies, Tor
	Social     = "social"
	Gaming     = "gaming"
	Malware    = "malware"
	SafeSearch = "safesearch" // a search was forced to safe results
	Ads        = "ads"        // ads & tracking: recognised, never shown as a family event
)

// Categories lists the categories a list can be mapped to.
var Categories = []string{Adult, Gambling, Bypass, Social, Gaming, Malware, Ads}

// CategoryLabels are human names.
var CategoryLabels = map[string]string{ //nolint:gosec // display labels, not credentials
	Adult: "Adult", Gambling: "Gambling", Bypass: "VPN, proxy & private DNS", Social: "Social media",
	Gaming: "Gaming", Malware: "Malware & phishing", SafeSearch: "Safe search enforced", Ads: "Ads & tracking",
}

// Built-in domain lists, matched on the registrable domain and its subdomains.
// They catch the common cases even when no categorised filter list exists,
// including queries that were *not* blocked (a filter gap worth knowing).
var domainCategories = map[string]string{}

func init() {
	add := func(cat string, ds ...string) {
		for _, d := range ds {
			domainCategories[d] = cat
		}
	}
	add(Adult, "pornhub.com", "xvideos.com", "xnxx.com", "xhamster.com", "onlyfans.com", "redtube.com", "youporn.com",
		"chaturbate.com", "stripchat.com", "spankbang.com", "brazzers.com", "fansly.com", "livejasmin.com", "rule34.xxx", "e-hentai.org", "nhentai.net")
	add(Gambling, "bet365.com", "pokerstars.com", "williamhill.com", "draftkings.com", "fanduel.com", "betfair.com", "paddypower.com",
		"888casino.com", "stake.com", "bovada.lv", "betway.com", "unibet.com", "ladbrokes.com", "skybet.com")
	add(Bypass, "dns.google", "cloudflare-dns.com", "mozilla.cloudflare-dns.com", "one.one.one.one", "dns.quad9.net", "doh.opendns.com",
		"dns.nextdns.io", "nextdns.io", "doh.cleanbrowsing.org", "dns.adguard-dns.com", "doh.mullvad.net",
		"nordvpn.com", "expressvpn.com", "protonvpn.com", "surfshark.com", "privateinternetaccess.com", "cyberghostvpn.com",
		"windscribe.com", "hotspotshield.com", "tunnelbear.com", "torproject.org", "psiphon.ca", "hide.me", "ultrasurf.us")
	add(Social, "tiktok.com", "tiktokcdn.com", "instagram.com", "snapchat.com", "facebook.com", "discord.com", "discord.gg",
		"reddit.com", "x.com", "twitter.com", "twitch.tv", "pinterest.com", "tumblr.com", "bereal.com")
	add(Gaming, "roblox.com", "rbxcdn.com", "fortnite.com", "epicgames.com", "minecraft.net", "steampowered.com", "steamcommunity.com",
		"ea.com", "blizzard.com", "battle.net", "riotgames.com", "leagueoflegends.com", "xbox.com", "playstation.com")
}

// Blocked-service IDs (AdGuard Home's catalogue) that map to a category.
var serviceCategories = map[string]string{
	"tiktok": Social, "instagram": Social, "snapchat": Social, "facebook": Social, "discord": Social, "reddit": Social,
	"twitter": Social, "twitch": Social, "pinterest": Social, "tumblr": Social, "onlyfans": Adult,
	"roblox": Gaming, "fortnite": Gaming, "epic_games": Gaming, "minecraft": Gaming, "steam": Gaming, "battle_net": Gaming,
	"riot_games": Gaming, "leagueoflegends": Gaming, "xboxlive": Gaming, "playstation": Gaming, "blizzard_entertainment": Gaming,
	"betfair": Gambling, "betway": Gambling, "bet365": Gambling, "pokerstars": Gambling, "tor": Bypass, "nordvpn": Bypass,
	"protonvpn": Bypass, "expressvpn": Bypass,
}

// GuessListCategory maps a filter list to a category from its URL or name.
// Well-known adult lists (HaGeZi NSFW, OISD NSFW, StevenBlack porn) are
// recognised; "" means the list is general-purpose (counted as ads).
func GuessListCategory(url, name string) string {
	s := strings.ToLower(url + " " + name)
	switch {
	case strings.Contains(s, "nsfw"), strings.Contains(s, "porn"), strings.Contains(s, "adult"), strings.Contains(s, "xxx"):
		return Adult
	case strings.Contains(s, "gambling"), strings.Contains(s, "casino"):
		return Gambling
	case strings.Contains(s, "doh"), strings.Contains(s, "vpn"), strings.Contains(s, "proxy"), strings.Contains(s, "bypass"):
		return Bypass
	case strings.Contains(s, "social"), strings.Contains(s, "tiktok"), strings.Contains(s, "facebook"):
		return Social
	case strings.Contains(s, "gaming"), strings.Contains(s, "games"):
		return Gaming
	case strings.Contains(s, "malware"), strings.Contains(s, "phishing"), strings.Contains(s, "threat"), strings.Contains(s, "scam"):
		return Malware
	}
	return ""
}

// BaseDomain returns the registrable domain ("www.pornhub.com" → "pornhub.com").
func BaseDomain(d string) string {
	d = strings.TrimSuffix(strings.ToLower(d), ".")
	if b, err := publicsuffix.EffectiveTLDPlusOne(d); err == nil {
		return b
	}
	return d
}

func domainCategory(d string) string {
	d = strings.TrimSuffix(strings.ToLower(d), ".")
	for {
		if c, ok := domainCategories[d]; ok {
			return c
		}
		i := strings.IndexByte(d, '.')
		if i < 0 {
			return ""
		}
		d = d[i+1:]
	}
}

// ListLookup returns the category of filter list filterID on an instance
// ("" = uncategorised).
type ListLookup func(instanceID, filterID int64) string

// Categorize decides whether a query matters to the family section and how.
// It returns "" for everything else (including ordinary ad blocking).
func Categorize(r *querylog.Row, lists ListLookup) string {
	switch r.Status {
	case querylog.StatusParental:
		return Adult
	case querylog.StatusSafeBrowsing:
		return Malware
	case querylog.StatusSafeSearch:
		return SafeSearch
	}
	if r.Blocked {
		if r.Service != "" {
			if c, ok := serviceCategories[strings.ToLower(r.Service)]; ok {
				return c
			}
		}
		if r.FilterID != 0 && lists != nil {
			if c := lists(r.InstanceID, r.FilterID); c != "" {
				if c == Ads {
					return domainOrEmpty(r.Domain)
				}
				return c
			}
		}
	}
	return domainOrEmpty(r.Domain)
}

// domainOrEmpty categorises by the built-in lists; ads stay out.
func domainOrEmpty(d string) string {
	return domainCategory(d)
}

// Mask hides most of a domain: "pornhub.com" → "p***hub.com".
func Mask(domain string) string {
	b := BaseDomain(domain)
	label, rest, ok := strings.Cut(b, ".")
	if !ok {
		rest = ""
	} else {
		rest = "." + rest
	}
	r := []rune(label)
	switch {
	case len(r) <= 2:
		return string(r[:1]) + "***" + rest
	case len(r) <= 5:
		return string(r[:1]) + "***" + string(r[len(r)-1:]) + rest
	default:
		return string(r[:1]) + "***" + string(r[len(r)-3:]) + rest
	}
}
