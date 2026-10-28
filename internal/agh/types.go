package agh

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// Status is GET /control/status.
type Status struct {
	Version                    string   `json:"version"`
	Language                   string   `json:"language"`
	DNSAddresses               []string `json:"dns_addresses"`
	DNSPort                    int      `json:"dns_port"`
	HTTPPort                   int      `json:"http_port"`
	ProtectionDisabledDuration int64    `json:"protection_disabled_duration"`
	ProtectionEnabled          bool     `json:"protection_enabled"`
	DHCPAvailable              bool     `json:"dhcp_available"`
	Running                    bool     `json:"running"`
	StartTime                  float64  `json:"start_time"`
}

// Status fetches the instance status.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var s Status
	return &s, c.Get(ctx, "status", &s)
}

// VersionInfo is POST /control/version.json.
type VersionInfo struct {
	Disabled        bool   `json:"disabled"`
	NewVersion      string `json:"new_version"`
	Announcement    string `json:"announcement"`
	AnnouncementURL string `json:"announcement_url"`
	CanAutoUpdate   bool   `json:"can_autoupdate"`
}

// Version asks the instance about available updates.
func (c *Client) Version(ctx context.Context, recheck bool) (*VersionInfo, error) {
	var v VersionInfo
	return &v, c.Post(ctx, "version.json", map[string]bool{"recheck_now": recheck}, &v)
}

// SetProtection enables or disables protection, optionally for a duration.
func (c *Client) SetProtection(ctx context.Context, enabled bool, d time.Duration) error {
	body := map[string]any{"enabled": enabled}
	if !enabled && d > 0 {
		body["duration"] = d.Milliseconds()
	}
	return c.Post(ctx, "protection", body, nil)
}

// Stats is GET /control/stats. Top-N lists are arrays of single-key objects.
type Stats struct {
	TimeUnits             string               `json:"time_units"`
	NumDNSQueries         int64                `json:"num_dns_queries"`
	NumBlockedFiltering   int64                `json:"num_blocked_filtering"`
	NumReplacedSafebrows  int64                `json:"num_replaced_safebrowsing"`
	NumReplacedSafesearch int64                `json:"num_replaced_safesearch"`
	NumReplacedParental   int64                `json:"num_replaced_parental"`
	AvgProcessingTime     float64              `json:"avg_processing_time"`
	DNSQueries            []int64              `json:"dns_queries"`
	BlockedFiltering      []int64              `json:"blocked_filtering"`
	ReplacedSafebrowsing  []int64              `json:"replaced_safebrowsing"`
	ReplacedParental      []int64              `json:"replaced_parental"`
	TopQueriedDomains     []map[string]int64   `json:"top_queried_domains"`
	TopClients            []map[string]int64   `json:"top_clients"`
	TopBlockedDomains     []map[string]int64   `json:"top_blocked_domains"`
	TopUpstreamsResponses []map[string]int64   `json:"top_upstreams_responses"`
	TopUpstreamsAvgTime   []map[string]float64 `json:"top_upstreams_avg_time"`
}

// Stats fetches aggregated statistics.
func (c *Client) Stats(ctx context.Context) (*Stats, error) {
	var s Stats
	return &s, c.Get(ctx, "stats", &s)
}

// QueryLogEntry is one row of GET /control/querylog.
type QueryLogEntry struct {
	Answer         []DNSAnswer     `json:"answer"`
	OriginalAnswer []DNSAnswer     `json:"original_answer"`
	Upstream       string          `json:"upstream"`
	AnswerDNSSEC   bool            `json:"answer_dnssec"`
	Cached         bool            `json:"cached"`
	Client         string          `json:"client"`
	ClientID       string          `json:"client_id"`
	ClientInfo     *QueryClient    `json:"client_info"`
	ClientProto    string          `json:"client_proto"`
	ElapsedMs      string          `json:"elapsedMs"`
	Question       QueryQuestion   `json:"question"`
	Reason         string          `json:"reason"`
	Rules          []QueryRule     `json:"rules"`
	ServiceName    string          `json:"service_name"`
	Status         string          `json:"status"`
	Time           time.Time       `json:"time"`
	Extra          json.RawMessage `json:"-"`
}

// DNSAnswer is one answer record.
type DNSAnswer struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
	TTL   int64  `json:"ttl"`
}

// QueryClient is resolved client info.
type QueryClient struct {
	Name           string `json:"name"`
	DisallowedRule string `json:"disallowed_rule"`
	Disallowed     bool   `json:"disallowed"`
}

// QueryQuestion is the DNS question.
type QueryQuestion struct {
	Class       string `json:"class"`
	Name        string `json:"name"`
	UnicodeName string `json:"unicode_name"`
	Type        string `json:"type"`
}

// QueryRule is a matched filtering rule.
type QueryRule struct {
	FilterListID int64  `json:"filter_list_id"`
	Text         string `json:"text"`
}

// QueryLogPage is the GET /control/querylog response.
type QueryLogPage struct {
	Data   []QueryLogEntry `json:"data"`
	Oldest string          `json:"oldest"`
}

// QueryLog fetches one page of the query log, newest first. olderThan is an
// RFC3339 timestamp cursor (empty for the newest page).
func (c *Client) QueryLog(ctx context.Context, olderThan string, limit int) (*QueryLogPage, error) {
	v := url.Values{}
	if olderThan != "" {
		v.Set("older_than", olderThan)
	}
	if limit > 0 {
		v.Set("limit", strconv.Itoa(limit))
	}
	var p QueryLogPage
	path := "querylog"
	if len(v) > 0 {
		path += "?" + v.Encode()
	}
	return &p, c.Get(ctx, path, &p)
}

// FilteringStatus is GET /control/filtering/status.
type FilteringStatus struct {
	Enabled          bool     `json:"enabled"`
	Interval         int      `json:"interval"`
	Filters          []Filter `json:"filters"`
	WhitelistFilters []Filter `json:"whitelist_filters"`
	UserRules        []string `json:"user_rules"`
}

// Filter is a filter subscription.
type Filter struct {
	ID          int64  `json:"id"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	RulesCount  int64  `json:"rules_count"`
	LastUpdated string `json:"last_updated,omitempty"`
}

// FilteringStatus fetches filter lists and user rules.
func (c *Client) FilteringStatus(ctx context.Context) (*FilteringStatus, error) {
	var f FilteringStatus
	return &f, c.Get(ctx, "filtering/status", &f)
}

// Update triggers AdGuard Home's built-in self-updater.
func (c *Client) Update(ctx context.Context) error {
	return c.Post(ctx, "update", nil, nil)
}

// InstallConfigure completes the setup wizard on a fresh instance (used by the
// dev fleet and integration tests).
func (c *Client) InstallConfigure(ctx context.Context, webIP string, webPort int, dnsIP string, dnsPort int, user, pass string) error {
	return c.Post(ctx, "install/configure", map[string]any{
		"web":      map[string]any{"ip": webIP, "port": webPort},
		"dns":      map[string]any{"ip": dnsIP, "port": dnsPort},
		"username": user,
		"password": pass,
	}, nil)
}
