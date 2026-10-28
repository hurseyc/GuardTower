// Package proxmox reads Proxmox VE (with a read-only API token) to learn
// which node runs each AdGuard Home container or VM, so placement and
// single-point-of-failure checks stay right after migrations.
package proxmox

import (
	"context"
	"crypto/tls"
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

// Client talks to the Proxmox VE API.
type Client struct {
	base  *url.URL
	auth  string
	httpc *http.Client
}

// NewClient creates a client for https://host:8006 with an API token
// ("user@realm!name") and its secret.
func NewClient(rawURL, tokenID, secret string, skipVerify bool) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawURL), "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, errors.New("the Proxmox URL must look like https://pve.lan:8006")
	}
	u.Path = strings.TrimSuffix(u.Path, "/api2/json")
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: skipVerify} //nolint:gosec // explicit opt-in for Proxmox's self-signed certificate.
	return &Client{base: u, auth: "PVEAPIToken=" + tokenID + "=" + secret, httpc: &http.Client{Timeout: 20 * time.Second, Transport: tr}}, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.String()+"/api2/json/"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.auth)
	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("the API token was rejected by Proxmox (check the token ID and secret)")
	case resp.StatusCode == http.StatusForbidden:
		return &PermissionError{Path: path}
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("reading %s from Proxmox: HTTP %d %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return fmt.Errorf("reading %s from Proxmox: %w", path, err)
	}
	return json.Unmarshal(env.Data, out)
}

// PermissionError means the token lacks a privilege for one call.
type PermissionError struct{ Path string }

func (e *PermissionError) Error() string {
	return "the API token isn't allowed to read " + e.Path + " (give it the PVEAuditor role on /)"
}

// Version returns the Proxmox VE version (a connection test).
func (c *Client) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
		Release string `json:"release"`
	}
	if err := c.get(ctx, "version", &v); err != nil {
		return "", err
	}
	return v.Version, nil
}

// Resource is a guest from /cluster/resources.
type Resource struct {
	VMID     int64  `json:"vmid"`
	Name     string `json:"name"`
	Node     string `json:"node"`
	Type     string `json:"type"` // lxc | qemu
	Status   string `json:"status"`
	Template int    `json:"template"`
}

// Guests lists every container and VM in the cluster.
func (c *Client) Guests(ctx context.Context) ([]Resource, error) {
	var out []Resource
	return out, c.get(ctx, "cluster/resources?type=vm", &out)
}

// NodeAddresses maps node names to their cluster IPs (empty when the token
// can't read cluster status).
func (c *Client) NodeAddresses(ctx context.Context) map[string]string {
	var st []struct {
		Type string `json:"type"`
		Name string `json:"name"`
		IP   string `json:"ip"`
	}
	out := map[string]string{}
	if c.get(ctx, "cluster/status", &st) == nil {
		for _, s := range st {
			if s.Type == "node" {
				out[s.Name] = s.IP
			}
		}
	}
	return out
}

// GuestIPs returns a running guest's addresses: from the container's
// interfaces, its static network config, or the VM's guest agent.
func (c *Client) GuestIPs(ctx context.Context, g Resource) []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if i := strings.IndexByte(s, '/'); i >= 0 {
			s = s[:i]
		}
		if ip := net.ParseIP(s); ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			out = append(out, ip.String())
		}
	}
	base := fmt.Sprintf("nodes/%s/%s/%d/", url.PathEscape(g.Node), g.Type, g.VMID)
	switch g.Type {
	case "lxc":
		var ifs []struct {
			Name  string `json:"name"`
			Inet  string `json:"inet"`
			Inet6 string `json:"inet6"`
		}
		if c.get(ctx, base+"interfaces", &ifs) == nil {
			for _, i := range ifs {
				add(i.Inet)
				add(i.Inet6)
			}
		}
		if len(out) == 0 {
			var cfg map[string]any
			if c.get(ctx, base+"config", &cfg) == nil {
				for k, v := range cfg {
					s, _ := v.(string)
					if !strings.HasPrefix(k, "net") {
						continue
					}
					for _, kv := range strings.Split(s, ",") {
						if ip, ok := strings.CutPrefix(kv, "ip="); ok {
							add(ip)
						}
						if ip, ok := strings.CutPrefix(kv, "ip6="); ok {
							add(ip)
						}
					}
				}
			}
		}
	case "qemu":
		var r struct {
			Result []struct {
				Name string `json:"name"`
				IPs  []struct {
					IP string `json:"ip-address"`
				} `json:"ip-addresses"`
			} `json:"result"`
		}
		if c.get(ctx, base+"agent/network-get-interfaces", &r) == nil {
			for _, i := range r.Result {
				for _, a := range i.IPs {
					add(a.IP)
				}
			}
		}
	}
	return out
}
