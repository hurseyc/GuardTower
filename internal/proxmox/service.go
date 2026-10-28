package proxmox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/store"
)

// Cluster is a configured Proxmox VE connection.
type Cluster struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	TokenID       string `json:"token_id"`
	HasSecret     bool   `json:"has_secret"`
	TLSSkipVerify bool   `json:"tls_skip_verify"`
	CreatedAt     int64  `json:"created_at"`
	LastSyncAt    int64  `json:"last_sync_at,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

// ClusterInput is the create/update payload. Secret nil keeps the stored one.
type ClusterInput struct {
	Name          string  `json:"name"`
	URL           string  `json:"url"`
	TokenID       string  `json:"token_id"`
	Secret        *string `json:"secret"`
	TLSSkipVerify bool    `json:"tls_skip_verify"`
}

// Guest is a container or VM seen at the last sync.
type Guest struct {
	ClusterID    int64    `json:"cluster_id"`
	VMID         int64    `json:"vmid"`
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Node         string   `json:"node"`
	Status       string   `json:"status"`
	IPs          []string `json:"ips"`
	InstanceID   int64    `json:"instance_id,omitempty"`
	InstanceName string   `json:"instance_name,omitempty"`
}

// Move is an instance placed on a different host by a sync.
type Move struct {
	InstanceID int64  `json:"instance_id"`
	Instance   string `json:"instance"`
	From       string `json:"from"`
	To         string `json:"to"`
}

// SyncResult reports one sync.
type SyncResult struct {
	Guests  int    `json:"guests"`
	Matched int    `json:"matched"`
	Moves   []Move `json:"moves"`
}

// BadInput is a validation error.
type BadInput struct{ Msg string }

func (e *BadInput) Error() string { return e.Msg }

// ErrNotFound is returned for unknown clusters.
var ErrNotFound = errors.New("no such Proxmox connection")

// Service stores connections and syncs placement.
type Service struct {
	st    *store.Store
	box   *crypto.Box
	fleet *fleet.Service
	log   *slog.Logger
	// OnMoved runs after a sync moved instances to other hosts.
	OnMoved func([]Move)
	// Resolve looks up instance hostnames; replaceable in tests.
	Resolve func(ctx context.Context, host string) ([]string, error)
}

// New creates the service.
func New(st *store.Store, box *crypto.Box, fl *fleet.Service, log *slog.Logger) *Service {
	return &Service{st: st, box: box, fleet: fl, log: log, Resolve: net.DefaultResolver.LookupHost}
}

type row struct {
	Cluster
	secret []byte
}

func (s *Service) rows(ctx context.Context, where string, args ...any) ([]row, error) {
	rs, err := s.st.DB.QueryContext(ctx, `SELECT id, name, url, token_id, token_secret_enc, tls_skip_verify, created_at, last_sync_at, last_error
		FROM proxmox_clusters `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	out := []row{}
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.ID, &r.Name, &r.URL, &r.TokenID, &r.secret, &r.TLSSkipVerify, &r.CreatedAt, &r.LastSyncAt, &r.LastError); err != nil {
			return nil, err
		}
		r.HasSecret = len(r.secret) > 0
		out = append(out, r)
	}
	return out, rs.Err()
}

// List returns every connection (never secrets).
func (s *Service) List(ctx context.Context) ([]Cluster, error) {
	rs, err := s.rows(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]Cluster, len(rs))
	for i, r := range rs {
		out[i] = r.Cluster
	}
	return out, nil
}

func (s *Service) get(ctx context.Context, id int64) (*row, error) {
	rs, err := s.rows(ctx, "WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return nil, ErrNotFound
	}
	return &rs[0], nil
}

func validate(in *ClusterInput) error {
	in.Name, in.URL, in.TokenID = strings.TrimSpace(in.Name), strings.TrimSpace(in.URL), strings.TrimSpace(in.TokenID)
	if in.Name == "" {
		return &BadInput{"name is required"}
	}
	if u, err := url.Parse(in.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return &BadInput{"URL must look like https://pve.lan:8006"}
	}
	if !strings.Contains(in.TokenID, "@") || !strings.Contains(in.TokenID, "!") {
		return &BadInput{`token ID must look like guardtower@pve!readonly (user@realm!token-name)`}
	}
	return nil
}

// Save creates (id 0) or updates a connection.
func (s *Service) Save(ctx context.Context, id int64, in ClusterInput) (*Cluster, error) {
	if err := validate(&in); err != nil {
		return nil, err
	}
	var enc []byte
	if in.Secret != nil {
		var err error
		if enc, err = s.box.Seal(strings.TrimSpace(*in.Secret)); err != nil {
			return nil, err
		}
	} else if id == 0 {
		return nil, &BadInput{"the token secret is required"}
	}
	if id == 0 {
		res, err := s.st.DB.ExecContext(ctx, `INSERT INTO proxmox_clusters (name, url, token_id, token_secret_enc, tls_skip_verify, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`, in.Name, in.URL, in.TokenID, enc, in.TLSSkipVerify, time.Now().UnixMilli())
		if err != nil {
			return nil, uniqueErr(err)
		}
		id, _ = res.LastInsertId()
	} else {
		q := `UPDATE proxmox_clusters SET name = ?, url = ?, token_id = ?, tls_skip_verify = ?`
		args := []any{in.Name, in.URL, in.TokenID, in.TLSSkipVerify}
		if enc != nil {
			q += `, token_secret_enc = ?`
			args = append(args, enc)
		}
		res, err := s.st.DB.ExecContext(ctx, q+` WHERE id = ?`, append(args, id)...)
		if err != nil {
			return nil, uniqueErr(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
	}
	r, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &r.Cluster, nil
}

func uniqueErr(err error) error {
	if strings.Contains(err.Error(), "UNIQUE") {
		return &BadInput{"a Proxmox connection with this name already exists"}
	}
	return err
}

// Delete removes a connection and its guest list.
func (s *Service) Delete(ctx context.Context, id int64) error {
	res, err := s.st.DB.ExecContext(ctx, `DELETE FROM proxmox_clusters WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) client(r *row) (*Client, error) {
	secret, err := s.box.Open(r.secret)
	if err != nil {
		return nil, err
	}
	return NewClient(r.URL, r.TokenID, secret, r.TLSSkipVerify)
}

// Test checks a connection before it is saved (Secret nil uses the stored
// secret of id) and returns the Proxmox version and guest count.
func (s *Service) Test(ctx context.Context, id int64, in ClusterInput) (string, int, error) {
	if err := validate(&in); err != nil {
		return "", 0, err
	}
	secret := ""
	if in.Secret != nil {
		secret = strings.TrimSpace(*in.Secret)
	} else if id != 0 {
		r, err := s.get(ctx, id)
		if err != nil {
			return "", 0, err
		}
		if secret, err = s.box.Open(r.secret); err != nil {
			return "", 0, err
		}
	}
	c, err := NewClient(in.URL, in.TokenID, secret, in.TLSSkipVerify)
	if err != nil {
		return "", 0, &BadInput{err.Error()}
	}
	v, err := c.Version(ctx)
	if err != nil {
		return "", 0, err
	}
	gs, err := c.Guests(ctx)
	if err != nil {
		return v, 0, err
	}
	return v, len(gs), nil
}

// Guests lists the guests seen at the last syncs.
func (s *Service) Guests(ctx context.Context) ([]Guest, error) {
	rs, err := s.st.DB.QueryContext(ctx, `SELECT g.cluster_id, g.vmid, g.kind, g.name, g.node, g.status, g.ips, COALESCE(g.instance_id, 0), COALESCE(i.name, '')
		FROM proxmox_guests g LEFT JOIN instances i ON i.id = g.instance_id ORDER BY g.instance_id IS NULL, g.node, g.vmid`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	out := []Guest{}
	for rs.Next() {
		var g Guest
		var ips string
		if err := rs.Scan(&g.ClusterID, &g.VMID, &g.Kind, &g.Name, &g.Node, &g.Status, &ips, &g.InstanceID, &g.InstanceName); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(ips), &g.IPs)
		out = append(out, g)
	}
	return out, rs.Err()
}

// instanceAddrs returns the IPs an instance is reached on and the names it
// might have as a guest.
func (s *Service) instanceAddrs(ctx context.Context, in *fleet.Instance) (ips map[string]bool, names []string) {
	ips = map[string]bool{}
	hosts := []string{}
	if u, err := url.Parse(in.URL); err == nil && u.Hostname() != "" {
		hosts = append(hosts, u.Hostname())
	}
	if in.DNSAddress != "" {
		h := in.DNSAddress
		if hh, _, err := net.SplitHostPort(h); err == nil {
			h = hh
		}
		hosts = append(hosts, strings.Trim(h, "[]"))
	}
	names = []string{strings.ToLower(in.Name)}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			ips[ip.String()] = true
			continue
		}
		names = append(names, strings.ToLower(strings.SplitN(h, ".", 2)[0]))
		rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		addrs, err := s.Resolve(rctx, h)
		cancel()
		if err == nil {
			for _, a := range addrs {
				if ip := net.ParseIP(a); ip != nil {
					ips[ip.String()] = true
				}
			}
		}
	}
	return ips, names
}

// match picks the guest an instance runs on: by IP first, then by name.
func match(ips map[string]bool, names []string, guests []Guest) int {
	for i, g := range guests {
		for _, ip := range g.IPs {
			if ips[ip] {
				return i
			}
		}
	}
	for i, g := range guests {
		for _, n := range names {
			if n != "" && strings.EqualFold(g.Name, n) {
				return i
			}
		}
	}
	return -1
}

// Sync reads one cluster and places every matching instance on the host
// named after its Proxmox node, creating the host when needed.
func (s *Service) Sync(ctx context.Context, id int64) (*SyncResult, error) {
	r, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	res, err := s.sync(ctx, r)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	_, _ = s.st.DB.ExecContext(ctx, `UPDATE proxmox_clusters SET last_sync_at = ?, last_error = ? WHERE id = ?`, time.Now().UnixMilli(), msg, id)
	if err == nil && len(res.Moves) > 0 && s.OnMoved != nil {
		s.OnMoved(res.Moves)
	}
	return res, err
}

func (s *Service) sync(ctx context.Context, r *row) (*SyncResult, error) {
	c, err := s.client(r)
	if err != nil {
		return nil, err
	}
	res, err := c.Guests(ctx)
	if err != nil {
		return nil, err
	}
	nodeIPs := c.NodeAddresses(ctx)
	guests := make([]Guest, 0, len(res))
	for _, g := range res {
		if g.Template == 1 || (g.Type != "lxc" && g.Type != "qemu") {
			continue
		}
		gg := Guest{ClusterID: r.ID, VMID: g.VMID, Kind: g.Type, Name: g.Name, Node: g.Node, Status: g.Status, IPs: []string{}}
		if g.Status == "running" {
			gg.IPs = append(gg.IPs, c.GuestIPs(ctx, g)...)
		}
		guests = append(guests, gg)
	}

	ins, err := s.fleet.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	hosts, err := s.fleet.ListHosts(ctx)
	if err != nil {
		return nil, err
	}
	hostByName, hostName := map[string]*fleet.Host{}, map[int64]string{}
	for _, h := range hosts {
		hostByName[strings.ToLower(h.Name)] = h
		hostName[h.ID] = h.Name
	}
	out := &SyncResult{Guests: len(guests), Moves: []Move{}}
	for _, in := range ins {
		ips, names := s.instanceAddrs(ctx, in)
		i := match(ips, names, guests)
		if i < 0 {
			continue
		}
		g := &guests[i]
		g.InstanceID = in.ID
		out.Matched++
		h := hostByName[strings.ToLower(g.Node)]
		if h == nil {
			if h, err = s.fleet.SaveHost(ctx, 0, fleet.Host{Name: g.Node, Kind: "proxmox", Address: nodeIPs[g.Node],
				Notes: "Added from Proxmox " + r.Name}); err != nil {
				return nil, fmt.Errorf("create host %s: %w", g.Node, err)
			}
			hostByName[strings.ToLower(g.Node)], hostName[h.ID] = h, h.Name
		}
		runtime := "lxc"
		if g.Kind == "qemu" {
			runtime = "vm"
		}
		if in.HostID == h.ID && in.Runtime == runtime {
			continue
		}
		if err := s.fleet.SetPlacement(ctx, in.ID, h.ID, runtime); err != nil {
			return nil, err
		}
		if in.HostID != h.ID {
			from := hostName[in.HostID]
			if from == "" {
				from = "(none)"
			}
			mv := Move{InstanceID: in.ID, Instance: in.Name, From: from, To: h.Name}
			out.Moves = append(out.Moves, mv)
			detail, _ := json.Marshal(mv)
			_ = s.st.Audit(ctx, store.AuditEntry{Username: "proxmox:" + r.Name, Action: "proxmox.placement", Target: in.Name, Detail: string(detail)})
			s.log.Info("proxmox: instance placed", "instance", in.Name, "from", from, "to", h.Name)
		}
	}

	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM proxmox_guests WHERE cluster_id = ?`, r.ID); err != nil {
		return nil, err
	}
	for _, g := range guests {
		ips, _ := json.Marshal(g.IPs)
		if _, err := tx.ExecContext(ctx, `INSERT INTO proxmox_guests (cluster_id, vmid, kind, name, node, status, ips, instance_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, g.VMID, g.Kind, g.Name, g.Node, g.Status, string(ips), store.NullInt(g.InstanceID)); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}

// Run syncs every connection now and then every interval.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		cs, err := s.List(ctx)
		if err == nil {
			for _, c := range cs {
				if _, err := s.Sync(ctx, c.ID); err != nil && ctx.Err() == nil {
					s.log.Warn("proxmox: sync", "cluster", c.Name, "err", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
