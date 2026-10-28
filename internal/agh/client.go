// Package agh is a client for the AdGuard Home control API (/control/*).
package agh

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Options configures a Client.
type Options struct {
	BaseURL       string // e.g. https://10.0.0.53:3000 (path prefix allowed)
	Username      string
	Password      string
	TLSSkipVerify bool
	CAPEM         string
	Timeout       time.Duration
}

// Client talks to one AdGuard Home instance.
type Client struct {
	base *url.URL
	user string
	pass string
	http *http.Client
}

// APIError is a non-2xx response from AdGuard Home.
type APIError struct {
	Status int
	Body   string
	Path   string
}

func (e *APIError) Error() string {
	msg := strings.TrimSpace(e.Body)
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Sprintf("authentication failed (HTTP %d) – check username/password", e.Status)
	}
	return fmt.Sprintf("%s: HTTP %d: %s", e.Path, e.Status, msg)
}

// IsAuthError reports whether err is a 401/403 from AdGuard Home.
func IsAuthError(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && (ae.Status == 401 || ae.Status == 403)
}

// New builds a client. It validates the URL and TLS settings up front.
func New(o Options) (*Client, error) {
	u, err := NormalizeURL(o.BaseURL)
	if err != nil {
		return nil, err
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if o.TLSSkipVerify {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // explicit per-instance opt-in for self-signed certs.
	}
	if strings.TrimSpace(o.CAPEM) != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(o.CAPEM)) {
			return nil, errors.New("custom CA: no valid PEM certificates found")
		}
		tlsCfg.RootCAs = pool
	}
	timeout := o.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	tr := &http.Transport{
		Proxy:                 nil, // instances live on the LAN; never route via HTTP(S)_PROXY.
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       tlsCfg,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	return &Client{
		base: u,
		user: o.Username,
		pass: o.Password,
		http: &http.Client{Transport: tr, Timeout: timeout,
			// AdGuard Home redirects to /login.html or /install.html; surface those as errors.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

// NormalizeURL parses an instance URL, defaulting the scheme to http and
// stripping a trailing slash or /control suffix users sometimes paste.
func NormalizeURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("URL is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("URL scheme must be http or https")
	}
	if u.Host == "" {
		return nil, errors.New("URL must include a host")
	}
	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/control")
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

// Host returns the hostname (without port) of the instance URL.
func (c *Client) Host() string { return c.base.Hostname() }

// BaseURL returns the normalised instance URL.
func (c *Client) BaseURL() string { return c.base.String() }

// Do performs a request against /control/<path>. in is JSON-encoded when
// non-nil; out is JSON-decoded when non-nil.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	u := *c.base
	p, q, _ := strings.Cut(path, "?")
	u.Path = u.Path + "/control/" + strings.TrimPrefix(p, "/")
	u.RawQuery = q
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GuardTower")
	if c.user != "" || c.pass != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return friendlyNetErr(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		if strings.Contains(loc, "install") {
			return errors.New("instance has not completed the AdGuard Home setup wizard")
		}
		return &APIError{Status: http.StatusUnauthorized, Path: path, Body: "redirected to " + loc}
	}
	if resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Body: string(data), Path: path}
	}
	if out == nil {
		return nil
	}
	if raw, ok := out.(*json.RawMessage); ok {
		*raw = append((*raw)[:0], data...)
		return nil
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: decode response: %w", path, err)
	}
	return nil
}

func friendlyNetErr(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		err = uerr.Err
	}
	var cerr *tls.CertificateVerificationError
	if errors.As(err, &cerr) {
		return fmt.Errorf("TLS certificate not trusted (%w) – upload the CA or enable 'skip TLS verification' for this instance", cerr.Err)
	}
	var herr x509.HostnameError
	if errors.As(err, &herr) {
		return fmt.Errorf("TLS certificate does not match host: %w", herr)
	}
	var operr *net.OpError
	if errors.As(err, &operr) {
		return fmt.Errorf("cannot connect: %w", operr.Err)
	}
	return err
}

// Get is shorthand for a GET request.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out)
}

// Post is shorthand for a POST request.
func (c *Client) Post(ctx context.Context, path string, in, out any) error {
	return c.Do(ctx, http.MethodPost, path, in, out)
}

// Put is shorthand for a PUT request.
func (c *Client) Put(ctx context.Context, path string, in, out any) error {
	return c.Do(ctx, http.MethodPut, path, in, out)
}
