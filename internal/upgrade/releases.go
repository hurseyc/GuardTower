package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hurseyc/guardtower/internal/store"
)

// DefaultReleasesURL lists AdGuard Home releases on GitHub.
const DefaultReleasesURL = "https://api.github.com/repos/AdguardTeam/AdGuardHome/releases?per_page=30"

// DefaultLatestURL redirects to the newest stable release's page. It is used
// when the API is unavailable (rate limits on shared IPs, proxies).
const DefaultLatestURL = "https://github.com/AdguardTeam/AdGuardHome/releases/latest"

// SettingCheckReleases turns release checks on GitHub on or off.
const SettingCheckReleases = "upgrades.check_releases"

// Release is an AdGuard Home release.
type Release struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	PublishedAt int64  `json:"published_at"`
	URL         string `json:"url"`
	Notes       string `json:"notes"`
	Prerelease  bool   `json:"prerelease"`
}

// Releases tracks AdGuard Home releases published on GitHub.
type Releases struct {
	st        *store.Store
	URL       string
	LatestURL string
	HTTP      *http.Client

	mu        sync.Mutex
	etag      string
	lastCheck time.Time
	lastErr   string
}

// NewReleases creates a tracker.
func NewReleases(st *store.Store) *Releases {
	return &Releases{st: st, URL: DefaultReleasesURL, LatestURL: DefaultLatestURL, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
}

// Refresh fetches the release list (conditionally, using the last ETag),
// falling back to the latest-release redirect when the API fails.
func (r *Releases) Refresh(ctx context.Context) error {
	err := r.refreshAPI(ctx)
	if err == nil || r.LatestURL == "" {
		r.setErr(err)
		return err
	}
	if ferr := r.refreshLatest(ctx); ferr != nil {
		err = fmt.Errorf("%w; fallback: %w", err, ferr)
		r.setErr(err)
		return err
	}
	r.setErr(nil)
	return nil
}

// refreshLatest reads the tag from github.com's /releases/latest redirect.
func (r *Releases) refreshLatest(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, r.LatestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "GuardTower")
	cl := *r.HTTP
	cl.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	if resp.StatusCode/100 != 3 || i < 0 {
		return fmt.Errorf("latest release redirect: HTTP %d", resp.StatusCode)
	}
	tag := loc[i+len("/tag/"):]
	if _, ok := ParseVersion(tag); !ok {
		return fmt.Errorf("latest release redirect: unexpected tag %q", tag)
	}
	_, err = r.st.DB.ExecContext(ctx, `INSERT INTO releases (tag, name, published_at, url) VALUES (?, ?, ?, ?) ON CONFLICT (tag) DO NOTHING`,
		tag, tag, 0, loc) // the redirect carries no date
	return err
}

func (r *Releases) refreshAPI(ctx context.Context) error {
	r.mu.Lock()
	etag := r.etag
	r.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "GuardTower")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		var m struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&m)
		if m.Message != "" {
			return fmt.Errorf("GitHub releases API: HTTP %d: %s", resp.StatusCode, m.Message)
		}
		return fmt.Errorf("GitHub releases API: HTTP %d", resp.StatusCode)
	}
	var list []ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return err
	}
	tx, err := r.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, g := range list {
		if g.Draft {
			continue
		}
		if _, ok := ParseVersion(g.TagName); !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO releases (tag, name, published_at, url, notes, prerelease) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (tag) DO UPDATE SET name = excluded.name, published_at = excluded.published_at, url = excluded.url, notes = excluded.notes, prerelease = excluded.prerelease`,
			g.TagName, g.Name, g.PublishedAt.UnixMilli(), g.HTMLURL, g.Body, g.Prerelease); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.mu.Lock()
	r.etag = resp.Header.Get("ETag")
	r.mu.Unlock()
	return nil
}

func (r *Releases) setErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastCheck = time.Now()
	r.lastErr = ""
	if err != nil {
		r.lastErr = err.Error()
	}
}

// State reports when releases were last checked and the last error.
func (r *Releases) State() (time.Time, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastCheck, r.lastErr
}

// List returns known releases, newest version first.
func (r *Releases) List(ctx context.Context, includePre bool) ([]Release, error) {
	rows, err := r.st.DB.QueryContext(ctx, `SELECT tag, name, published_at, url, notes, prerelease FROM releases`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Release{}
	for rows.Next() {
		var x Release
		if err := rows.Scan(&x.Tag, &x.Name, &x.PublishedAt, &x.URL, &x.Notes, &x.Prerelease); err != nil {
			return nil, err
		}
		if x.Prerelease && !includePre {
			continue
		}
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return Newer(out[i].Tag, out[j].Tag) })
	return out, rows.Err()
}

// Latest returns the newest stable release.
func (r *Releases) Latest(ctx context.Context) (*Release, error) {
	l, err := r.List(ctx, false)
	if err != nil {
		return nil, err
	}
	if len(l) == 0 {
		return nil, errors.New("no AdGuard Home releases known yet")
	}
	return &l[0], nil
}

// Enabled reports whether release checks are on (the default).
func (r *Releases) Enabled(ctx context.Context) bool {
	v, err := r.st.GetSetting(ctx, SettingCheckReleases, "true")
	return err != nil || v != "false"
}
