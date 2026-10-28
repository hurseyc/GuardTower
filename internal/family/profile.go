package family

import (
	"context"
	"errors"
	"fmt"

	"github.com/hurseyc/guardtower/internal/config"
)

// AdultListURL is HaGeZi's NSFW list (adblock syntax), widely used and
// maintained. It is categorised as Adult by GuessListCategory.
const AdultListURL = "https://raw.githubusercontent.com/hagezi/dns-blocklists/main/adblock/nsfw.txt"

// bypassRules block well-known private-DNS resolvers, VPN and Tor sites, and
// answer Firefox's canary domain so it switches its own DNS-over-HTTPS off.
var bypassRules = []string{
	"||use-application-dns.net^",
	"||dns.google^", "||cloudflare-dns.com^", "||one.one.one.one^", "||dns.quad9.net^", "||doh.opendns.com^",
	"||dns.nextdns.io^", "||doh.cleanbrowsing.org^", "||doh.mullvad.net^",
	"||nordvpn.com^", "||expressvpn.com^", "||protonvpn.com^", "||surfshark.com^", "||torproject.org^",
}

// ProfileOptions choose what the Family profile turns on.
type ProfileOptions struct {
	Parental    bool `json:"parental"`
	SafeSearch  bool `json:"safe_search"`
	AdultList   bool `json:"adult_list"`
	BlockBypass bool `json:"block_bypass"`
}

// ErrNoMember is returned when filtering isn't managed yet and the group has
// no online member to read the current lists and rules from.
var ErrNoMember = errors.New("the group has no online instance to read its current filter lists and rules from; bring one online or manage filtering under Configuration first")

// ApplyProfile writes the Family profile into a group's settings layers,
// merging with what the group already sets. Nothing reaches AdGuard Home
// until the change is applied from Review & apply. It returns the sections
// it changed.
//
// A layer defines the whole desired value of a list, so when filtering isn't
// managed yet (neither Base nor the group sets it), the group's current
// lists and rules are first imported from memberID; otherwise adding the
// adult list would remove every list the instances already have.
func ApplyProfile(ctx context.Context, cfg *config.Service, groupID, memberID int64, o ProfileOptions, by string) ([]string, error) {
	ref := config.ScopeRef{Scope: config.ScopeGroup, ID: groupID}
	layers, err := cfg.GetLayers(ctx, ref)
	if err != nil {
		return nil, err
	}
	if o.AdultList || o.BlockBypass {
		base, err := cfg.GetLayers(ctx, config.ScopeRef{Scope: config.ScopeBase})
		if err != nil {
			return nil, err
		}
		_, inBase := base["filtering"]
		_, inGroup := layers["filtering"]
		if !inBase && !inGroup {
			if memberID == 0 {
				return nil, ErrNoMember
			}
			if err := cfg.ImportBaseline(ctx, memberID, ref, []string{"filtering"}, by); err != nil {
				return nil, fmt.Errorf("reading the current filter lists: %w", err)
			}
			if layers, err = cfg.GetLayers(ctx, ref); err != nil {
				return nil, err
			}
		}
	}
	doc := func(section string) config.Doc {
		if l, ok := layers[section]; ok && l.Doc != nil {
			return l.Doc
		}
		return config.Doc{}
	}
	var changed []string
	set := func(section string, d config.Doc) error {
		if _, err := cfg.SetLayer(ctx, ref, section, d, by); err != nil {
			return err
		}
		changed = append(changed, section)
		return nil
	}
	if o.Parental {
		d := doc("parental")
		d["enabled"] = true
		if err := set("parental", d); err != nil {
			return changed, err
		}
	}
	if o.SafeSearch {
		d := doc("safesearch")
		for _, k := range []string{"enabled", "google", "bing", "duckduckgo", "ecosia", "yandex", "pixabay", "youtube"} {
			d[k] = true
		}
		if err := set("safesearch", d); err != nil {
			return changed, err
		}
	}
	if o.AdultList || o.BlockBypass {
		d := doc("filtering")
		d["enabled"] = true
		if o.AdultList {
			ops, _ := d["filters"].(map[string]any)
			if ops == nil {
				ops = map[string]any{}
			}
			ups, _ := ops["upsert"].([]any)
			found := false
			for _, u := range ups {
				if m, ok := u.(map[string]any); ok && m["url"] == AdultListURL {
					found = true
				}
			}
			if !found {
				ups = append(ups, map[string]any{"url": AdultListURL, "name": "HaGeZi NSFW (adult content)", "enabled": true})
			}
			ops["upsert"] = ups
			d["filters"] = ops
		}
		if o.BlockBypass {
			ops, _ := d["user_rules"].(map[string]any)
			if ops == nil {
				ops = map[string]any{}
			}
			add, _ := ops["add"].([]any)
			have := map[string]bool{}
			for _, a := range add {
				if s, ok := a.(string); ok {
					have[s] = true
				}
			}
			for _, r := range bypassRules {
				if !have[r] {
					add = append(add, r)
				}
			}
			ops["add"] = add
			d["user_rules"] = ops
		}
		if err := set("filtering", d); err != nil {
			return changed, err
		}
	}
	return changed, nil
}
