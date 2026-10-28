package upgrade

import (
	"bufio"
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
	"path"
	"sort"
	"strings"
	"time"
)

// DockerConfig locates an AdGuard Home container through the Docker Engine
// API, normally exposed by a socket proxy on the Docker host.
type DockerConfig struct {
	Endpoint      string `json:"endpoint"`  // tcp://host:2375, https://host:2376 or unix:///var/run/docker.sock
	Container     string `json:"container"` // name or ID
	Image         string `json:"image"`     // repository override, e.g. adguard/adguardhome
	TLSSkipVerify bool   `json:"tls_skip_verify"`
	CAPEM         string `json:"ca_pem"`
	ConfPath      string `json:"conf_path"` // AdGuardHome.yaml inside the container
}

// DockerAPI is a minimal Docker Engine API client.
type DockerAPI struct {
	base string
	http *http.Client
}

// NewDockerAPI builds a client for the endpoint.
func NewDockerAPI(cfg DockerConfig) (*DockerAPI, error) {
	ep := strings.TrimSpace(cfg.Endpoint)
	if ep == "" {
		return nil, errors.New("no Docker endpoint configured")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil // the Docker host is on the LAN; never send it through an HTTP proxy
	base := ""
	switch {
	case strings.HasPrefix(ep, "unix://"):
		sock := strings.TrimPrefix(ep, "unix://")
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		}
		base = "http://docker"
	case strings.HasPrefix(ep, "tcp://"):
		base = "http://" + strings.TrimPrefix(ep, "tcp://")
	case strings.HasPrefix(ep, "http://"), strings.HasPrefix(ep, "https://"):
		base = ep
	default:
		base = "http://" + ep
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid Docker endpoint %q", cfg.Endpoint)
	}
	if u.Scheme == "https" {
		tc := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.TLSSkipVerify} //nolint:gosec // explicit per-endpoint opt-in for self-signed hosts.
		if strings.TrimSpace(cfg.CAPEM) != "" {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM([]byte(cfg.CAPEM)) {
				return nil, errors.New("the Docker CA certificate is not valid PEM")
			}
			tc.RootCAs = pool
		}
		tr.TLSClientConfig = tc
	}
	return &DockerAPI{base: strings.TrimSuffix(u.String(), "/"), http: &http.Client{Transport: tr, Timeout: 10 * time.Minute}}, nil
}

// DockerError is a non-2xx answer from the Docker API.
type DockerError struct {
	Status int
	Msg    string
}

func (e *DockerError) Error() string {
	switch e.Status {
	case http.StatusForbidden:
		return "Docker API refused the request (403); allow it on the socket proxy: " + e.Msg
	case http.StatusNotFound:
		return "not found: " + e.Msg
	}
	return fmt.Sprintf("Docker API HTTP %d: %s", e.Status, e.Msg)
}

func (d *DockerAPI) do(ctx context.Context, method, p string, q url.Values, body io.Reader, ctype string) (*http.Response, error) {
	u := d.base + p
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the Docker API: %w", err)
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotModified {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(b))
		var m struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(b, &m) == nil && m.Message != "" {
			msg = m.Message
		}
		return nil, &DockerError{Status: resp.StatusCode, Msg: msg}
	}
	return resp, nil
}

func (d *DockerAPI) json(ctx context.Context, method, p string, q url.Values, in, out any) error {
	var body io.Reader
	ctype := ""
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body, ctype = bytes.NewReader(b), "application/json"
	}
	resp, err := d.do(ctx, method, p, q, body, ctype)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Ping checks the API answers and returns the Docker version.
func (d *DockerAPI) Ping(ctx context.Context) (string, error) {
	var v struct {
		Version    string `json:"Version"`
		APIVersion string `json:"ApiVersion"`
	}
	if err := d.json(ctx, http.MethodGet, "/version", nil, nil, &v); err != nil {
		return "", err
	}
	return v.Version, nil
}

// ContainerInfo is the part of /containers/{id}/json GuardTower needs. Config
// and HostConfig are kept raw so they are copied to the new container intact.
type ContainerInfo struct {
	ID              string                     `json:"Id"`
	Name            string                     `json:"Name"`
	Image           string                     `json:"Image"`
	Config          map[string]json.RawMessage `json:"Config"`
	HostConfig      map[string]json.RawMessage `json:"HostConfig"`
	NetworkSettings struct {
		Networks map[string]map[string]json.RawMessage `json:"Networks"`
	} `json:"NetworkSettings"`
	State struct {
		Running bool   `json:"Running"`
		Status  string `json:"Status"`
	} `json:"State"`
}

// ImageRef returns Config.Image (e.g. adguard/adguardhome:v0.107.70).
func (c *ContainerInfo) ImageRef() string {
	var s string
	_ = json.Unmarshal(c.Config["Image"], &s)
	return s
}

// Inspect returns a container's configuration.
func (d *DockerAPI) Inspect(ctx context.Context, id string) (*ContainerInfo, error) {
	var ci ContainerInfo
	if err := d.json(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/json", nil, nil, &ci); err != nil {
		return nil, err
	}
	return &ci, nil
}

// Pull pulls repo:tag, returning the first error reported in the progress stream.
func (d *DockerAPI) Pull(ctx context.Context, repo, tag string) error {
	resp, err := d.do(ctx, http.MethodPost, "/images/create", url.Values{"fromImage": {repo}, "tag": {tag}}, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var m struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &m) == nil && m.Error != "" {
			return fmt.Errorf("pulling %s:%s: %s", repo, tag, m.Error)
		}
	}
	return sc.Err()
}

func (d *DockerAPI) post(ctx context.Context, p string, q url.Values) error {
	return d.json(ctx, http.MethodPost, p, q, nil, nil)
}

// Stop stops a container (already stopped is fine).
func (d *DockerAPI) Stop(ctx context.Context, id string) error {
	return d.post(ctx, "/containers/"+url.PathEscape(id)+"/stop", url.Values{"t": {"30"}})
}

// Start starts a container (already running is fine).
func (d *DockerAPI) Start(ctx context.Context, id string) error {
	return d.post(ctx, "/containers/"+url.PathEscape(id)+"/start", nil)
}

// Rename renames a container.
func (d *DockerAPI) Rename(ctx context.Context, id, name string) error {
	return d.post(ctx, "/containers/"+url.PathEscape(id)+"/rename", url.Values{"name": {name}})
}

// Remove force-removes a container (not its volumes).
func (d *DockerAPI) Remove(ctx context.Context, id string) error {
	return d.json(ctx, http.MethodDelete, "/containers/"+url.PathEscape(id), url.Values{"force": {"1"}}, nil, nil)
}

// Create creates a container and returns its ID.
func (d *DockerAPI) Create(ctx context.Context, name string, body any) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	if err := d.json(ctx, http.MethodPost, "/containers/create", url.Values{"name": {name}}, body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// Connect attaches a container to a network.
func (d *DockerAPI) Connect(ctx context.Context, network, id string, endpoint any) error {
	return d.json(ctx, http.MethodPost, "/networks/"+url.PathEscape(network)+"/connect", nil,
		map[string]any{"Container": id, "EndpointConfig": endpoint}, nil)
}

// GetArchive copies a path out of a container as a tar stream.
func (d *DockerAPI) GetArchive(ctx context.Context, id, p string) ([]byte, error) {
	resp, err := d.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/archive", url.Values{"path": {p}}, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

// PutArchive extracts a tar stream into dir inside a container.
func (d *DockerAPI) PutArchive(ctx context.Context, id, dir string, tarball []byte) error {
	resp, err := d.do(ctx, http.MethodPut, "/containers/"+url.PathEscape(id)+"/archive", url.Values{"path": {dir}}, bytes.NewReader(tarball), "application/x-tar")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// splitImage splits "repo:tag" / "repo@digest" into repo and tag.
func splitImage(ref string) (repo, tag string) {
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	slash := strings.LastIndex(ref, "/")
	if i := strings.LastIndex(ref, ":"); i > slash {
		return ref[:i], ref[i+1:]
	}
	return ref, "latest"
}

// Docker upgrades an AdGuard Home container by recreating it from a newer
// image with an identical configuration, the way `docker compose pull && up`
// would, but keeping the old container until the new one is healthy.
type Docker struct {
	API *DockerAPI
	Cfg DockerConfig
	// Now is for tests.
	Now func() time.Time
}

// Name implements Driver.
func (d *Docker) Name() string { return DriverDocker }

// endpointSettings keeps only the fields that may be passed when creating or
// connecting: fixed IPs, aliases and MAC address (important on macvlan).
func endpointSettings(raw map[string]json.RawMessage, oldID string) map[string]any {
	out := map[string]any{}
	if v, ok := raw["IPAMConfig"]; ok && string(v) != "null" {
		out["IPAMConfig"] = v
	}
	if v, ok := raw["MacAddress"]; ok {
		var mac string
		if json.Unmarshal(v, &mac) == nil && mac != "" {
			out["MacAddress"] = mac
		}
	}
	if v, ok := raw["Aliases"]; ok {
		var aliases []string
		_ = json.Unmarshal(v, &aliases)
		keep := aliases[:0]
		for _, a := range aliases {
			// Docker adds the short container ID as an alias; it must not carry over.
			if oldID != "" && strings.HasPrefix(oldID, a) {
				continue
			}
			keep = append(keep, a)
		}
		if len(keep) > 0 {
			out["Aliases"] = keep
		}
	}
	return out
}

// Upgrade implements Driver.
func (d *Docker) Upgrade(ctx context.Context, target string, progress func(string)) (*Result, error) {
	if err := validTarget(target); err != nil {
		return nil, err
	}
	if strings.TrimSpace(d.Cfg.Container) == "" {
		return nil, errors.New("no container name configured")
	}
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	progress("inspecting container " + d.Cfg.Container)
	old, err := d.API.Inspect(ctx, d.Cfg.Container)
	if err != nil {
		return nil, fmt.Errorf("inspecting %s: %w", d.Cfg.Container, err)
	}
	name := strings.TrimPrefix(old.Name, "/")
	repo, _ := splitImage(old.ImageRef())
	if d.Cfg.Image != "" {
		repo, _ = splitImage(d.Cfg.Image)
	}
	newRef := repo + ":" + target

	confPath := d.Cfg.ConfPath
	if confPath == "" {
		confPath = "/opt/adguardhome/conf/AdGuardHome.yaml"
	}
	progress("backing up " + confPath)
	backup, berr := d.API.GetArchive(ctx, old.ID, confPath)
	if berr != nil {
		progress("could not back up the configuration (" + berr.Error() + "); continuing")
		backup = nil
	}

	progress("pulling " + newRef)
	if err := d.API.Pull(ctx, repo, target); err != nil {
		return nil, err
	}

	// Build the new container from the old one's settings.
	cfg := map[string]json.RawMessage{}
	for k, v := range old.Config {
		cfg[k] = v
	}
	img, _ := json.Marshal(newRef)
	cfg["Image"] = img
	// Labels baked into the old image (version, revision) would go stale.
	if raw, ok := cfg["Labels"]; ok {
		var labels map[string]string
		if json.Unmarshal(raw, &labels) == nil {
			for k := range labels {
				if strings.HasPrefix(k, "org.opencontainers.image.") {
					delete(labels, k)
				}
			}
			cfg["Labels"], _ = json.Marshal(labels)
		}
	}
	delete(cfg, "Hostname") // Docker set it to the old container ID unless the user chose one
	if h, ok := old.Config["Hostname"]; ok {
		var hn string
		_ = json.Unmarshal(h, &hn)
		if hn != "" && !strings.HasPrefix(old.ID, hn) {
			cfg["Hostname"] = h
		}
	}
	body := map[string]any{}
	for k, v := range cfg {
		body[k] = v
	}
	body["HostConfig"] = old.HostConfig
	nets := make([]string, 0, len(old.NetworkSettings.Networks))
	for n := range old.NetworkSettings.Networks {
		nets = append(nets, n)
	}
	sort.Strings(nets)
	var mode string
	_ = json.Unmarshal(old.HostConfig["NetworkMode"], &mode)
	first := ""
	if mode != "" && mode != "default" && !strings.HasPrefix(mode, "container:") && mode != "host" && mode != "none" {
		if _, ok := old.NetworkSettings.Networks[mode]; ok {
			first = mode
		}
	}
	if first == "" && len(nets) > 0 && mode != "host" && mode != "none" && !strings.HasPrefix(mode, "container:") {
		first = nets[0]
	}
	if first != "" {
		body["NetworkingConfig"] = map[string]any{"EndpointsConfig": map[string]any{first: endpointSettings(old.NetworkSettings.Networks[first], old.ID)}}
	}

	progress("stopping the old container")
	if err := d.API.Stop(ctx, old.ID); err != nil {
		return nil, fmt.Errorf("stopping %s: %w", name, err)
	}
	oldName := fmt.Sprintf("%s-gt-old-%d", name, now().Unix())
	restartOld := func(ctx context.Context) error {
		if err := d.API.Rename(ctx, old.ID, name); err != nil {
			return fmt.Errorf("renaming the old container back: %w", err)
		}
		if backup != nil {
			if err := d.API.PutArchive(ctx, old.ID, path.Dir(confPath), backup); err != nil {
				return fmt.Errorf("restoring the configuration backup: %w", err)
			}
		}
		return d.API.Start(ctx, old.ID)
	}
	if err := d.API.Rename(ctx, old.ID, oldName); err != nil {
		_ = d.API.Start(ctx, old.ID)
		return nil, fmt.Errorf("renaming %s: %w", name, err)
	}
	progress("creating " + name + " from " + newRef)
	newID, err := d.API.Create(ctx, name, body)
	if err != nil {
		if rerr := restartOld(ctx); rerr != nil {
			return nil, fmt.Errorf("creating the new container: %w; restarting the old one also failed: %w", err, rerr)
		}
		return nil, fmt.Errorf("creating the new container (the old one was restarted): %w", err)
	}
	undo := func(ctx context.Context) error {
		_ = d.API.Stop(ctx, newID)
		if err := d.API.Remove(ctx, newID); err != nil {
			return fmt.Errorf("removing the new container: %w", err)
		}
		return restartOld(ctx)
	}
	for _, n := range nets {
		if n == first || first == "" {
			continue
		}
		if err := d.API.Connect(ctx, n, newID, endpointSettings(old.NetworkSettings.Networks[n], old.ID)); err != nil {
			if uerr := undo(ctx); uerr != nil {
				return nil, fmt.Errorf("connecting network %s: %w; undo failed: %w", n, err, uerr)
			}
			return nil, fmt.Errorf("connecting network %s (rolled back): %w", n, err)
		}
	}
	progress("starting " + name)
	if err := d.API.Start(ctx, newID); err != nil {
		// Leave it to the caller's rollback path so it is recorded consistently.
		return &Result{Undo: undo}, fmt.Errorf("starting the new container: %w", err)
	}
	return &Result{
		Undo:     undo,
		Finalize: func(ctx context.Context) error { return d.API.Remove(ctx, old.ID) },
	}, nil
}
