package api_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestYAMLAndSelfUpdateAPI(t *testing.T) {
	_, c := newTestServer(t)
	var res struct {
		CSRF string `json:"csrf_token"`
	}
	c.do("POST", "/api/v1/setup", map[string]string{"username": "admin", "password": "correct-horse-battery"}, &res)
	c.csrf = res.CSRF

	var upd struct {
		Current         string `json:"current"`
		Method          string `json:"method"`
		UpdateAvailable bool   `json:"update_available"`
	}
	if code := c.do("GET", "/api/v1/updates/guardtower", nil, &upd); code != 200 || upd.Current == "" || upd.Method == "" || upd.UpdateAvailable {
		t.Fatalf("self-update status: %d %+v", code, upd)
	}

	raw := func(path, body string) (int, string) {
		req, _ := http.NewRequest("POST", c.base+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/yaml")
		req.Header.Set("X-CSRF-Token", c.csrf)
		resp, err := c.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	file := "version: 1\nbase:\n  parental:\n    enabled: true\n"
	code, body := raw("/api/v1/config/yaml?dry_run=1", file)
	if code != 200 || !strings.Contains(body, `"action":"create"`) || !strings.Contains(body, `"applied":false`) {
		t.Fatalf("dry run: %d %s", code, body)
	}
	if code, body = raw("/api/v1/config/yaml", file); code != 200 || !strings.Contains(body, `"applied":true`) {
		t.Fatalf("import: %d %s", code, body)
	}
	if code, body = raw("/api/v1/config/yaml", "version: 1\ngroups:\n  Nope:\n    parental: {enabled: true}\n"); code != 400 || !strings.Contains(body, "Nope") {
		t.Fatalf("bad import: %d %s", code, body)
	}

	resp, err := c.http.Get(c.base + "/api/v1/config/yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "parental:") {
		t.Fatalf("export: %d %s", resp.StatusCode, b)
	}
}
