package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
)

// gtClient is a minimal GuardTower API client for seeding.
type gtClient struct {
	base string
	http *http.Client
	csrf string
}

func newGTClient(base string) *gtClient {
	jar, _ := cookiejar.New(nil)
	return &gtClient{base: base, http: &http.Client{Jar: jar}}
}

func (c *gtClient) do(method, path string, body any) (map[string]any, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+"/api/v1"+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("%s %s: HTTP %d: %v", method, path, resp.StatusCode, out["error"])
	}
	return out, nil
}

func (c *gtClient) login(user, pass string) error {
	st, err := c.do("GET", "/setup", nil)
	if err != nil {
		return err
	}
	path := "/auth/login"
	if st["needs_setup"] == true {
		path = "/setup"
	}
	out, err := c.do("POST", path, map[string]string{"username": user, "password": pass})
	if err != nil {
		return err
	}
	c.csrf, _ = out["csrf_token"].(string)
	return nil
}

// create POSTs and returns the new record's id (or the existing one's on conflict).
func (c *gtClient) create(path string, body map[string]any) (int64, error) {
	out, err := c.do("POST", path, body)
	if err != nil {
		// Look up by name so seeding is idempotent.
		lr, lerr := c.list(path)
		if lerr == nil {
			for _, item := range lr {
				if item["name"] == body["name"] {
					return int64(item["id"].(float64)), nil
				}
			}
		}
		return 0, err
	}
	id, _ := out["id"].(float64)
	return int64(id), nil
}

func (c *gtClient) list(path string) ([]map[string]any, error) {
	req, _ := http.NewRequest("GET", c.base+"/api/v1"+path, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out []map[string]any
	return out, json.NewDecoder(resp.Body).Decode(&out)
}
