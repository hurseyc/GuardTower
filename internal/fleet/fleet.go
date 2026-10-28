// Package fleet manages the inventory of AdGuard Home instances, their
// groups, tags and hosts, and builds API clients for them.
package fleet

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hurseyc/guardtower/internal/agh"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/store"
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("not found")

// Runtime values describe how an instance is installed.
var Runtimes = []string{"unknown", "lxc", "docker", "vm", "baremetal", "router", "homeassistant"}

// Instance is an AdGuard Home server managed by GuardTower.
type Instance struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	URL           string   `json:"url"`
	Username      string   `json:"username"`
	HasPassword   bool     `json:"has_password"`
	TLSSkipVerify bool     `json:"tls_skip_verify"`
	CAPEM         string   `json:"ca_pem"`
	DNSAddress    string   `json:"dns_address"`
	GroupID       int64    `json:"group_id,omitempty"`
	HostID        int64    `json:"host_id,omitempty"`
	Runtime       string   `json:"runtime"`
	IsCanary      bool     `json:"is_canary"`
	Enabled       bool     `json:"enabled"`
	Tags          []string `json:"tags"`
	CreatedAt     int64    `json:"created_at"`
	Status        *Status  `json:"status,omitempty"`

	passwordEnc []byte
}

// Status is the last observed state of an instance.
type Status struct {
	Online            bool     `json:"online"`
	Version           string   `json:"version"`
	ProtectionEnabled bool     `json:"protection_enabled"`
	DNSAddresses      []string `json:"dns_addresses"`
	LatencyMs         int64    `json:"latency_ms"`
	LastError         string   `json:"last_error"`
	LastSeenAt        int64    `json:"last_seen_at,omitempty"`
	CheckedAt         int64    `json:"checked_at,omitempty"`
}

// InstanceInput is the create/update payload. Password nil means "unchanged".
type InstanceInput struct {
	Name          string   `json:"name"`
	URL           string   `json:"url"`
	Username      string   `json:"username"`
	Password      *string  `json:"password"`
	TLSSkipVerify bool     `json:"tls_skip_verify"`
	CAPEM         string   `json:"ca_pem"`
	DNSAddress    string   `json:"dns_address"`
	GroupID       int64    `json:"group_id"`
	HostID        int64    `json:"host_id"`
	Runtime       string   `json:"runtime"`
	IsCanary      bool     `json:"is_canary"`
	Enabled       *bool    `json:"enabled"`
	Tags          []string `json:"tags"`
}

// Group is a settings group.
type Group struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Color       string  `json:"color"`
	CreatedAt   int64   `json:"created_at"`
	InstanceIDs []int64 `json:"instance_ids"`
}

// Host is a physical or virtual machine running one or more instances.
type Host struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	SiteID  int64  `json:"site_id,omitempty"`
	Power   string `json:"power"`
	Kind    string `json:"kind"`
	Address string `json:"address"`
	Notes   string `json:"notes"`
}

// Site is a physical location.
type Site struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Service is the fleet inventory.
type Service struct {
	st  *store.Store
	box *crypto.Box
}

// NewService creates the fleet service.
func NewService(st *store.Store, box *crypto.Box) *Service { return &Service{st: st, box: box} }

const instanceCols = `i.id, i.name, i.url, i.username, i.password_enc, i.tls_skip_verify, i.ca_pem, i.dns_address,
	COALESCE(i.group_id, 0), COALESCE(i.host_id, 0), i.runtime, i.is_canary, i.enabled, i.created_at,
	COALESCE(s.online, 0), COALESCE(s.version, ''), COALESCE(s.protection_enabled, 0), COALESCE(s.dns_addresses, '[]'),
	COALESCE(s.latency_ms, 0), COALESCE(s.last_error, ''), COALESCE(s.last_seen_at, 0), COALESCE(s.checked_at, 0)`

func scanInstance(sc interface{ Scan(...any) error }) (*Instance, error) {
	var in Instance
	var st Status
	var dns string
	if err := sc.Scan(&in.ID, &in.Name, &in.URL, &in.Username, &in.passwordEnc, &in.TLSSkipVerify, &in.CAPEM, &in.DNSAddress,
		&in.GroupID, &in.HostID, &in.Runtime, &in.IsCanary, &in.Enabled, &in.CreatedAt,
		&st.Online, &st.Version, &st.ProtectionEnabled, &dns, &st.LatencyMs, &st.LastError, &st.LastSeenAt, &st.CheckedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(dns), &st.DNSAddresses)
	in.HasPassword = len(in.passwordEnc) > 0
	in.Status = &st
	in.Tags = []string{}
	return &in, nil
}

// ListInstances returns all instances with status and tags.
func (s *Service) ListInstances(ctx context.Context) ([]*Instance, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT `+instanceCols+` FROM instances i
		LEFT JOIN instance_status s ON s.instance_id = i.id ORDER BY i.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Instance
	byID := map[int64]*Instance{}
	for rows.Next() {
		in, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
		byID[in.ID] = in
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	trows, err := s.st.DB.QueryContext(ctx, `SELECT instance_id, tag FROM instance_tags ORDER BY tag`)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var id int64
		var tag string
		if err := trows.Scan(&id, &tag); err != nil {
			return nil, err
		}
		if in := byID[id]; in != nil {
			in.Tags = append(in.Tags, tag)
		}
	}
	if out == nil {
		out = []*Instance{}
	}
	return out, trows.Err()
}

// GetInstance returns one instance.
func (s *Service) GetInstance(ctx context.Context, id int64) (*Instance, error) {
	in, err := scanInstance(s.st.DB.QueryRowContext(ctx, `SELECT `+instanceCols+` FROM instances i
		LEFT JOIN instance_status s ON s.instance_id = i.id WHERE i.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.st.DB.QueryContext(ctx, `SELECT tag FROM instance_tags WHERE instance_id = ? ORDER BY tag`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		in.Tags = append(in.Tags, t)
	}
	return in, rows.Err()
}

func (s *Service) validate(in *InstanceInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 64 {
		return errors.New("name is required (max 64 characters)")
	}
	u, err := agh.NormalizeURL(in.URL)
	if err != nil {
		return err
	}
	in.URL = u.String()
	if in.Runtime == "" {
		in.Runtime = "unknown"
	}
	ok := false
	for _, r := range Runtimes {
		if r == in.Runtime {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("runtime must be one of %s", strings.Join(Runtimes, ", "))
	}
	in.Tags = normalizeTags(in.Tags)
	in.DNSAddress = strings.TrimSpace(in.DNSAddress)
	return nil
}

func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || len(t) > 32 || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// CreateInstance adds an instance.
func (s *Service) CreateInstance(ctx context.Context, in InstanceInput) (*Instance, error) {
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	var enc []byte
	if in.Password != nil {
		var err error
		if enc, err = s.box.Seal(*in.Password); err != nil {
			return nil, err
		}
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO instances (name, url, username, password_enc, tls_skip_verify, ca_pem, dns_address,
		group_id, host_id, runtime, is_canary, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.URL, in.Username, enc, in.TLSSkipVerify, in.CAPEM, in.DNSAddress,
		store.NullInt(in.GroupID), store.NullInt(in.HostID), in.Runtime, in.IsCanary, enabled, time.Now().Unix())
	if err != nil {
		return nil, uniqueErr(err, "an instance with this name already exists")
	}
	id, _ := res.LastInsertId()
	if err := setTags(ctx, tx, id, in.Tags); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetInstance(ctx, id)
}

// UpdateInstance modifies an instance.
func (s *Service) UpdateInstance(ctx context.Context, id int64, in InstanceInput) (*Instance, error) {
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	cur, err := s.GetInstance(ctx, id)
	if err != nil {
		return nil, err
	}
	enc := cur.passwordEnc
	if in.Password != nil {
		if enc, err = s.box.Seal(*in.Password); err != nil {
			return nil, err
		}
	}
	enabled := cur.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE instances SET name = ?, url = ?, username = ?, password_enc = ?, tls_skip_verify = ?,
		ca_pem = ?, dns_address = ?, group_id = ?, host_id = ?, runtime = ?, is_canary = ?, enabled = ? WHERE id = ?`,
		in.Name, in.URL, in.Username, enc, in.TLSSkipVerify, in.CAPEM, in.DNSAddress,
		store.NullInt(in.GroupID), store.NullInt(in.HostID), in.Runtime, in.IsCanary, enabled, id)
	if err != nil {
		return nil, uniqueErr(err, "an instance with this name already exists")
	}
	if err := setTags(ctx, tx, id, in.Tags); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetInstance(ctx, id)
}

// SetInstanceGroup moves an instance to a group (0 = none).
func (s *Service) SetInstanceGroup(ctx context.Context, id, groupID int64) error {
	res, err := s.st.DB.ExecContext(ctx, `UPDATE instances SET group_id = ? WHERE id = ?`, store.NullInt(groupID), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteInstance removes an instance (its stored logs are removed by cascade
// or retention).
func (s *Service) DeleteInstance(ctx context.Context, id int64) error {
	res, err := s.st.DB.ExecContext(ctx, `DELETE FROM instances WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func setTags(ctx context.Context, tx *sql.Tx, id int64, tags []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM instance_tags WHERE instance_id = ?`, id); err != nil {
		return err
	}
	for _, t := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO instance_tags (instance_id, tag) VALUES (?, ?)`, id, t); err != nil {
			return err
		}
	}
	return nil
}

// Client builds an API client for an instance.
func (s *Service) Client(in *Instance) (*agh.Client, error) {
	pw, err := s.box.Open(in.passwordEnc)
	if err != nil {
		return nil, err
	}
	return agh.New(agh.Options{BaseURL: in.URL, Username: in.Username, Password: pw,
		TLSSkipVerify: in.TLSSkipVerify, CAPEM: in.CAPEM})
}

// ClientFromInput builds a client from unsaved input (for "test connection").
// When the input has no password and id > 0, the stored password is used.
func (s *Service) ClientFromInput(ctx context.Context, id int64, in InstanceInput) (*agh.Client, error) {
	pw := ""
	if in.Password != nil {
		pw = *in.Password
	} else if id > 0 {
		cur, err := s.GetInstance(ctx, id)
		if err != nil {
			return nil, err
		}
		if pw, err = s.box.Open(cur.passwordEnc); err != nil {
			return nil, err
		}
	}
	return agh.New(agh.Options{BaseURL: in.URL, Username: in.Username, Password: pw,
		TLSSkipVerify: in.TLSSkipVerify, CAPEM: in.CAPEM, Timeout: 8 * time.Second})
}

// SaveStatus records the outcome of a status probe.
func (s *Service) SaveStatus(ctx context.Context, id int64, st Status) error {
	dns, _ := json.Marshal(st.DNSAddresses)
	if st.DNSAddresses == nil {
		dns = []byte("[]")
	}
	now := time.Now().Unix()
	var lastSeen any
	if st.Online {
		lastSeen = now
	}
	_, err := s.st.DB.ExecContext(ctx, `INSERT INTO instance_status (instance_id, online, version, protection_enabled, dns_addresses,
		latency_ms, last_error, last_seen_at, checked_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (instance_id) DO UPDATE SET online = excluded.online,
			version = CASE WHEN excluded.online THEN excluded.version ELSE instance_status.version END,
			protection_enabled = CASE WHEN excluded.online THEN excluded.protection_enabled ELSE instance_status.protection_enabled END,
			dns_addresses = CASE WHEN excluded.online THEN excluded.dns_addresses ELSE instance_status.dns_addresses END,
			latency_ms = excluded.latency_ms, last_error = excluded.last_error,
			last_seen_at = COALESCE(excluded.last_seen_at, instance_status.last_seen_at), checked_at = excluded.checked_at`,
		id, st.Online, st.Version, st.ProtectionEnabled, string(dns), st.LatencyMs, st.LastError, lastSeen, now)
	return err
}

// ---- groups ----

// ListGroups returns all groups with member IDs.
func (s *Service) ListGroups(ctx context.Context) ([]*Group, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT id, name, description, color, created_at FROM groups ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Group{}
	byID := map[int64]*Group{}
	for rows.Next() {
		g := &Group{InstanceIDs: []int64{}}
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.Color, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
		byID[g.ID] = g
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	mrows, err := s.st.DB.QueryContext(ctx, `SELECT id, group_id FROM instances WHERE group_id IS NOT NULL ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var id, gid int64
		if err := mrows.Scan(&id, &gid); err != nil {
			return nil, err
		}
		if g := byID[gid]; g != nil {
			g.InstanceIDs = append(g.InstanceIDs, id)
		}
	}
	return out, mrows.Err()
}

// GetGroup returns one group.
func (s *Service) GetGroup(ctx context.Context, id int64) (*Group, error) {
	gs, err := s.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range gs {
		if g.ID == id {
			return g, nil
		}
	}
	return nil, ErrNotFound
}

// SaveGroup creates (id == 0) or updates a group.
func (s *Service) SaveGroup(ctx context.Context, id int64, name, desc, color string) (*Group, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return nil, errors.New("group name is required (max 64 characters)")
	}
	if id == 0 {
		res, err := s.st.DB.ExecContext(ctx, `INSERT INTO groups (name, description, color, created_at) VALUES (?, ?, ?, ?)`,
			name, desc, color, time.Now().Unix())
		if err != nil {
			return nil, uniqueErr(err, "a group with this name already exists")
		}
		id, _ = res.LastInsertId()
	} else {
		res, err := s.st.DB.ExecContext(ctx, `UPDATE groups SET name = ?, description = ?, color = ? WHERE id = ?`, name, desc, color, id)
		if err != nil {
			return nil, uniqueErr(err, "a group with this name already exists")
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
	}
	return s.GetGroup(ctx, id)
}

// DeleteGroup removes a group; members become ungrouped.
func (s *Service) DeleteGroup(ctx context.Context, id int64) error {
	res, err := s.st.DB.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- hosts & sites ----

// ListHosts returns all hosts.
func (s *Service) ListHosts(ctx context.Context) ([]*Host, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT id, name, COALESCE(site_id, 0), power, kind, address, notes FROM hosts ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Host{}
	for rows.Next() {
		h := &Host{}
		if err := rows.Scan(&h.ID, &h.Name, &h.SiteID, &h.Power, &h.Kind, &h.Address, &h.Notes); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// HostKinds are the supported host types.
var HostKinds = []string{"proxmox", "nas", "docker", "vm", "baremetal", "router", "cloud", "other"}

// SaveHost creates (id == 0) or updates a host.
func (s *Service) SaveHost(ctx context.Context, id int64, h Host) (*Host, error) {
	h.Name = strings.TrimSpace(h.Name)
	if h.Name == "" || len(h.Name) > 64 {
		return nil, errors.New("host name is required (max 64 characters)")
	}
	if h.Kind == "" {
		h.Kind = "other"
	}
	valid := false
	for _, k := range HostKinds {
		if k == h.Kind {
			valid = true
		}
	}
	if !valid {
		return nil, fmt.Errorf("host kind must be one of %s", strings.Join(HostKinds, ", "))
	}
	if id == 0 {
		res, err := s.st.DB.ExecContext(ctx, `INSERT INTO hosts (name, site_id, power, kind, address, notes) VALUES (?, ?, ?, ?, ?, ?)`,
			h.Name, store.NullInt(h.SiteID), strings.TrimSpace(h.Power), h.Kind, strings.TrimSpace(h.Address), h.Notes)
		if err != nil {
			return nil, uniqueErr(err, "a host with this name already exists")
		}
		id, _ = res.LastInsertId()
	} else {
		res, err := s.st.DB.ExecContext(ctx, `UPDATE hosts SET name = ?, site_id = ?, power = ?, kind = ?, address = ?, notes = ? WHERE id = ?`,
			h.Name, store.NullInt(h.SiteID), strings.TrimSpace(h.Power), h.Kind, strings.TrimSpace(h.Address), h.Notes, id)
		if err != nil {
			return nil, uniqueErr(err, "a host with this name already exists")
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
	}
	h.ID = id
	return &h, nil
}

// DeleteHost removes a host; its instances become unassigned.
func (s *Service) DeleteHost(ctx context.Context, id int64) error {
	res, err := s.st.DB.ExecContext(ctx, `DELETE FROM hosts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSites returns all sites.
func (s *Service) ListSites(ctx context.Context) ([]*Site, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT id, name FROM sites ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Site{}
	for rows.Next() {
		x := &Site{}
		if err := rows.Scan(&x.ID, &x.Name); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SaveSite creates (id == 0) or renames a site.
func (s *Service) SaveSite(ctx context.Context, id int64, name string) (*Site, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return nil, errors.New("site name is required (max 64 characters)")
	}
	if id == 0 {
		res, err := s.st.DB.ExecContext(ctx, `INSERT INTO sites (name) VALUES (?)`, name)
		if err != nil {
			return nil, uniqueErr(err, "a site with this name already exists")
		}
		id, _ = res.LastInsertId()
	} else if _, err := s.st.DB.ExecContext(ctx, `UPDATE sites SET name = ? WHERE id = ?`, name, id); err != nil {
		return nil, uniqueErr(err, "a site with this name already exists")
	}
	return &Site{ID: id, Name: name}, nil
}

// DeleteSite removes a site.
func (s *Service) DeleteSite(ctx context.Context, id int64) error {
	_, err := s.st.DB.ExecContext(ctx, `DELETE FROM sites WHERE id = ?`, id)
	return err
}

func uniqueErr(err error, msg string) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return errors.New(msg)
	}
	return err
}
