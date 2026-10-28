package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/hurseyc/guardtower/internal/app"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/store"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
}

func (c *client) do(method, path string, body any, out any) int {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			c.t.Fatalf("%s %s: decode %q: %v", method, path, data, err)
		}
	}
	return resp.StatusCode
}

func newTestServer(t *testing.T) (*app.App, *client) {
	t.Helper()
	st, err := store.OpenPath(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := crypto.New(bytes.Repeat([]byte{7}, 32))
	a := app.NewWith(app.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, st, box)
	ts := httptest.NewServer(a.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	return a, &client{t: t, base: ts.URL, http: &http.Client{Jar: jar}}
}

// fakeAGH emulates the handful of AdGuard Home endpoints the tests need.
func fakeAGH(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/control/status", func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "secret-pass" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "v0.107.79", "running": true, "protection_enabled": true, "dns_addresses": []string{"10.0.0.53"}})
	})
	mux.HandleFunc("/control/version.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"disabled": false, "new_version": "v0.107.80", "can_autoupdate": true})
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func TestSetupLoginAndCSRF(t *testing.T) {
	_, c := newTestServer(t)

	var setup map[string]bool
	c.do("GET", "/api/v1/setup", nil, &setup)
	if !setup["needs_setup"] {
		t.Fatal("expected needs_setup on fresh install")
	}
	// Anonymous access to protected endpoints is refused.
	if code := c.do("GET", "/api/v1/instances", nil, nil); code != 401 {
		t.Fatalf("anonymous instances: got %d", code)
	}
	// Weak password rejected.
	if code := c.do("POST", "/api/v1/setup", map[string]string{"username": "admin", "password": "short"}, nil); code != 400 {
		t.Fatalf("weak password: got %d", code)
	}
	var res struct {
		CSRF string `json:"csrf_token"`
	}
	if code := c.do("POST", "/api/v1/setup", map[string]string{"username": "admin", "password": "correct-horse-battery"}, &res); code != 200 {
		t.Fatalf("setup: got %d", code)
	}
	// Setup cannot be replayed.
	if code := c.do("POST", "/api/v1/setup", map[string]string{"username": "evil", "password": "correct-horse-battery"}, nil); code != 400 {
		t.Fatalf("setup replay: got %d", code)
	}
	// Mutations without CSRF token are rejected.
	if code := c.do("POST", "/api/v1/groups", map[string]string{"name": "A"}, nil); code != 403 {
		t.Fatalf("missing csrf: got %d", code)
	}
	c.csrf = res.CSRF
	if code := c.do("POST", "/api/v1/groups", map[string]string{"name": "Group A"}, nil); code != 200 {
		t.Fatalf("create group: got %d", code)
	}

	// Logout then login again.
	c.do("POST", "/api/v1/auth/logout", nil, nil)
	c.csrf = ""
	if code := c.do("GET", "/api/v1/me", nil, nil); code != 401 {
		t.Fatalf("after logout: got %d", code)
	}
	if code := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong-password!"}, nil); code != 401 {
		t.Fatalf("bad login: got %d", code)
	}
	if code := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "ADMIN", "password": "correct-horse-battery"}, &res); code != 200 {
		t.Fatalf("login: got %d", code)
	}
}

func TestViewerCannotMutate(t *testing.T) {
	a, c := newTestServer(t)
	var res struct {
		CSRF string `json:"csrf_token"`
	}
	c.do("POST", "/api/v1/setup", map[string]string{"username": "admin", "password": "correct-horse-battery"}, &res)
	if _, err := a.Auth.CreateUser(context.Background(), "viewer", "viewer-password", "viewer", false); err != nil {
		t.Fatal(err)
	}
	c.do("POST", "/api/v1/auth/logout", nil, nil)
	c.do("POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": "viewer-password"}, &res)
	c.csrf = res.CSRF
	if code := c.do("GET", "/api/v1/instances", nil, nil); code != 200 {
		t.Fatalf("viewer list: got %d", code)
	}
	if code := c.do("POST", "/api/v1/groups", map[string]string{"name": "X"}, nil); code != 403 {
		t.Fatalf("viewer create group: got %d", code)
	}
}

func TestInstanceLifecycle(t *testing.T) {
	a, c := newTestServer(t)
	agh := fakeAGH(t)
	var res struct {
		CSRF string `json:"csrf_token"`
	}
	c.do("POST", "/api/v1/setup", map[string]string{"username": "admin", "password": "correct-horse-battery"}, &res)
	c.csrf = res.CSRF

	// Connection test with wrong password reports a friendly error.
	var test map[string]any
	c.do("POST", "/api/v1/instances/test", map[string]any{"url": agh.URL, "username": "admin", "password": "nope"}, &test)
	if test["ok"] != false {
		t.Fatalf("expected failed test, got %v", test)
	}
	c.do("POST", "/api/v1/instances/test", map[string]any{"url": agh.URL, "username": "admin", "password": "secret-pass"}, &test)
	if test["ok"] != true || test["version"] != "v0.107.79" {
		t.Fatalf("expected ok test, got %v", test)
	}

	var g struct{ ID int64 }
	c.do("POST", "/api/v1/groups", map[string]string{"name": "Group A"}, &g)
	var inst struct {
		ID          int64    `json:"id"`
		HasPassword bool     `json:"has_password"`
		Tags        []string `json:"tags"`
		GroupID     int64    `json:"group_id"`
	}
	code := c.do("POST", "/api/v1/instances", map[string]any{
		"name": "agh-1", "url": agh.URL + "/control/", "username": "admin", "password": "secret-pass",
		"group_id": g.ID, "runtime": "lxc", "tags": []string{"LXC", "lxc", " upstairs "},
	}, &inst)
	if code != 201 || !inst.HasPassword || len(inst.Tags) != 2 || inst.GroupID != g.ID {
		t.Fatalf("create instance: %d %+v", code, inst)
	}
	// Duplicate name.
	if code := c.do("POST", "/api/v1/instances", map[string]any{"name": "agh-1", "url": agh.URL}, nil); code != 400 {
		t.Fatalf("duplicate: got %d", code)
	}
	// The password never comes back from the API.
	var raw []map[string]any
	c.do("GET", "/api/v1/instances", nil, &raw)
	if _, leaked := raw[0]["password"]; leaked {
		t.Fatal("password leaked in API response")
	}
	// Probe stores status.
	st := a.Poller.Probe(context.Background(), mustGet(t, a, inst.ID))
	if !st.Online || st.Version != "v0.107.79" {
		t.Fatalf("probe: %+v", st)
	}
	// Update without password keeps the stored one.
	c.do("PUT", "/api/v1/instances/1", map[string]any{"name": "agh-1", "url": agh.URL, "username": "admin", "tags": []string{}}, nil)
	if st := a.Poller.Probe(context.Background(), mustGet(t, a, inst.ID)); !st.Online {
		t.Fatalf("password lost on update: %+v", st)
	}
	if code := c.do("DELETE", "/api/v1/instances/1", nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	var audit []map[string]any
	c.do("GET", "/api/v1/audit", nil, &audit)
	if len(audit) < 4 {
		t.Fatalf("expected audit entries, got %d", len(audit))
	}
}

func mustGet(t *testing.T, a *app.App, id int64) *fleetInstance {
	t.Helper()
	in, err := a.Fleet.GetInstance(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func TestConfigLayersAPI(t *testing.T) {
	a, c := newTestServer(t)
	var res struct {
		CSRF string `json:"csrf_token"`
	}
	c.do("POST", "/api/v1/setup", map[string]string{"username": "admin", "password": "correct-horse-battery"}, &res)
	c.csrf = res.CSRF

	var schema []map[string]any
	if code := c.do("GET", "/api/v1/config/schema", nil, &schema); code != 200 || len(schema) < 10 {
		t.Fatalf("schema: %d, %d sections", code, len(schema))
	}
	// Valid base layer.
	var saved map[string]any
	if code := c.do("PUT", "/api/v1/config/layers/base/0/dns", map[string]any{"ratelimit": 25, "upstream_dns": []string{"https://dns.quad9.net/dns-query"}}, &saved); code != 200 {
		t.Fatalf("put layer: %d", code)
	}
	// Invalid field values are rejected with a useful message.
	var errBody map[string]string
	if code := c.do("PUT", "/api/v1/config/layers/base/0/dns", map[string]any{"ratelimit": "lots"}, &errBody); code != 400 || errBody["error"] == "" {
		t.Fatalf("invalid layer: %d %v", code, errBody)
	}
	if code := c.do("PUT", "/api/v1/config/layers/base/0/nonsense", map[string]any{"x": 1}, nil); code != 400 {
		t.Fatalf("unknown section: %d", code)
	}
	if code := c.do("PUT", "/api/v1/config/layers/group/0/dns", map[string]any{"ratelimit": 1}, nil); code != 400 {
		t.Fatalf("group without id: %d", code)
	}
	var layers map[string]map[string]any
	c.do("GET", "/api/v1/config/layers?scope=base", nil, &layers)
	if _, ok := layers["dns"]; !ok {
		t.Fatalf("layer not stored: %v", layers)
	}
	// An empty document removes the layer.
	c.do("PUT", "/api/v1/config/layers/base/0/dns", map[string]any{}, nil)
	layers = nil
	c.do("GET", "/api/v1/config/layers?scope=base", nil, &layers)
	if _, ok := layers["dns"]; ok {
		t.Fatal("layer not deleted")
	}
	// Viewers may read but not write.
	if _, err := a.Auth.CreateUser(context.Background(), "viewer", "viewer-password", "viewer", false); err != nil {
		t.Fatal(err)
	}
	c.do("POST", "/api/v1/auth/logout", nil, nil)
	c.do("POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": "viewer-password"}, &res)
	c.csrf = res.CSRF
	if code := c.do("GET", "/api/v1/config/layers?scope=base", nil, nil); code != 200 {
		t.Fatalf("viewer read: %d", code)
	}
	if code := c.do("PUT", "/api/v1/config/layers/base/0/dns", map[string]any{"ratelimit": 1}, nil); code != 403 {
		t.Fatalf("viewer write: %d", code)
	}
}
