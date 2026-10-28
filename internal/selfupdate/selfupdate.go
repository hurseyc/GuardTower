// Package selfupdate checks whether a newer GuardTower release exists and
// how this copy was installed, so the UI can say how to update it.
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hurseyc/guardtower/internal/version"
)

// Install methods.
const (
	MethodDocker  = "docker"
	MethodSystemd = "systemd"
	MethodPackage = "package"
	MethodBinary  = "binary"
)

// Status is what the UI shows.
type Status struct {
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	URL             string `json:"url,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	Method          string `json:"method"`
	Command         string `json:"command,omitempty"`
	CheckedAt       int64  `json:"checked_at,omitempty"`
	Error           string `json:"error,omitempty"`
}

// Checker polls GitHub for the latest GuardTower release.
type Checker struct {
	APIURL    string
	LatestURL string
	HTTP      *http.Client
	// Enabled reports whether release checks are allowed (privacy setting).
	Enabled func(ctx context.Context) bool

	mu sync.Mutex
	st Status
}

// New creates a checker for the GuardTower repository.
func New(enabled func(ctx context.Context) bool) *Checker {
	c := &Checker{
		APIURL:    "https://api.github.com/repos/hurseyc/GuardTower/releases/latest",
		LatestURL: "https://github.com/hurseyc/GuardTower/releases/latest",
		HTTP:      &http.Client{Timeout: 20 * time.Second},
		Enabled:   enabled,
	}
	c.st = Status{Current: version.Version, Method: DetectMethod()}
	c.st.Command = command(c.st.Method)
	return c
}

// DetectMethod guesses how GuardTower was installed.
func DetectMethod() string {
	if _, err := os.Stat("/.dockerenv"); err == nil || os.Getenv("container") == "docker" {
		return MethodDocker
	}
	if exe, err := os.Executable(); err == nil && exe == "/usr/bin/guardtower" {
		return MethodPackage // .deb/.rpm install to /usr/bin; install.sh uses /usr/local/bin
	}
	if os.Getenv("INVOCATION_ID") != "" { // set by systemd for every service
		return MethodSystemd
	}
	return MethodBinary
}

func command(method string) string {
	switch method {
	case MethodDocker:
		return "docker compose pull && docker compose up -d"
	case MethodSystemd:
		return "curl -fsSL https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/install.sh | sudo bash -s -- --upgrade"
	case MethodPackage:
		return "sudo apt install ./guardtower_<version>_<arch>.deb   # or: sudo dnf install ./guardtower-<version>.<arch>.rpm"
	}
	return ""
}

// Status returns the last check result.
func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st
}

// Check asks GitHub for the latest release.
func (c *Checker) Check(ctx context.Context) error {
	if c.Enabled != nil && !c.Enabled(ctx) {
		return nil
	}
	tag, url, err := c.latest(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.st.CheckedAt = time.Now().UnixMilli()
	if err != nil {
		c.st.Error = err.Error()
		return err
	}
	c.st.Error, c.st.Latest, c.st.URL = "", tag, url
	c.st.UpdateAvailable = newer(tag, c.st.Current)
	return nil
}

func (c *Checker) latest(ctx context.Context) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "GuardTower/"+version.Version)
	if resp, err := c.HTTP.Do(req); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var r struct {
				Tag string `json:"tag_name"`
				URL string `json:"html_url"`
			}
			if json.NewDecoder(resp.Body).Decode(&r) == nil && r.Tag != "" {
				return r.Tag, r.URL, nil
			}
		}
	}
	// Fall back to the redirect, which isn't rate limited like the API.
	req, err = http.NewRequestWithContext(ctx, http.MethodHead, c.LatestURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "GuardTower/"+version.Version)
	cl := *c.HTTP
	cl.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := cl.Do(req)
	if err != nil {
		return "", "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	if resp.StatusCode/100 != 3 || i < 0 {
		return "", "", fmt.Errorf("GuardTower release check: HTTP %d", resp.StatusCode)
	}
	return loc[i+len("/tag/"):], loc, nil
}

// Run checks now and every 12 hours until ctx ends.
func (c *Checker) Run(ctx context.Context) {
	_ = c.Check(ctx)
	t := time.NewTicker(12 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = c.Check(ctx)
		}
	}
}

// newer compares vX.Y.Z tags; development builds ("dev", commit hashes)
// never report an update.
func newer(latest, current string) bool {
	a, ok1 := parse(latest)
	b, ok2 := parse(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n := 0
		if p == "" {
			return out, false
		}
		for _, ch := range p {
			if ch < '0' || ch > '9' {
				return out, false
			}
			n = n*10 + int(ch-'0')
		}
		out[i] = n
	}
	return out, true
}
