package upgrade

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hurseyc/guardtower/internal/agh"
	"github.com/hurseyc/guardtower/internal/config"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/events"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/health"
	"github.com/hurseyc/guardtower/internal/rollout"
	"github.com/hurseyc/guardtower/internal/store"
)

// Config is how one instance is upgraded.
type Config struct {
	InstanceID    int64        `json:"instance_id"`
	Driver        string       `json:"driver"`
	PinnedVersion string       `json:"pinned_version"`
	Docker        DockerConfig `json:"docker"`
	SSH           SSHConfig    `json:"ssh"`
	UpdatedAt     int64        `json:"updated_at,omitempty"`
}

// ConfigInput is the update payload. SSHKey nil keeps the stored key, ""
// removes it.
type ConfigInput struct {
	Config
	SSHKey *string `json:"ssh_key"`
}

// Service manages AdGuard Home upgrades.
type Service struct {
	st       *store.Store
	fleet    *fleet.Service
	box      *crypto.Box
	ro       *rollout.Service
	bus      *events.Bus
	log      *slog.Logger
	Releases *Releases

	// WaitTimeout bounds how long an upgraded instance has to come back.
	WaitTimeout time.Duration
	// PollEvery is the status polling interval while waiting.
	PollEvery time.Duration
	// OnChanged is called after an instance was upgraded or rolled back.
	OnChanged func()
	// Now is for tests.
	Now func() time.Time
}

// New creates the upgrade service.
func New(st *store.Store, fl *fleet.Service, box *crypto.Box, ro *rollout.Service, bus *events.Bus, log *slog.Logger) *Service {
	return &Service{st: st, fleet: fl, box: box, ro: ro, bus: bus, log: log, Releases: NewReleases(st),
		WaitTimeout: 3 * time.Minute, PollEvery: 2 * time.Second, Now: time.Now}
}

// ---- configuration ----

func defaultConfig(id int64) *Config {
	return &Config{InstanceID: id, Driver: DriverAuto,
		Docker: DockerConfig{ConfPath: "/opt/adguardhome/conf/AdGuardHome.yaml"},
		SSH:    SSHConfig{Port: 22, User: "root", AGHDir: "/opt/AdGuardHome", Service: "AdGuardHome"}}
}

func (s *Service) load(ctx context.Context, id int64) (*Config, []byte, error) {
	c := defaultConfig(id)
	var key []byte
	err := s.st.DB.QueryRowContext(ctx, `SELECT driver, pinned_version, docker_endpoint, docker_container, docker_image, docker_tls_skip_verify, docker_ca_pem,
		docker_conf_path, ssh_host, ssh_port, ssh_user, ssh_key_enc, ssh_host_key, ssh_sudo, ssh_agh_dir, ssh_service, updated_at
		FROM upgrade_config WHERE instance_id = ?`, id).Scan(&c.Driver, &c.PinnedVersion, &c.Docker.Endpoint, &c.Docker.Container, &c.Docker.Image,
		&c.Docker.TLSSkipVerify, &c.Docker.CAPEM, &c.Docker.ConfPath, &c.SSH.Host, &c.SSH.Port, &c.SSH.User, &key, &c.SSH.HostKey, &c.SSH.Sudo,
		&c.SSH.AGHDir, &c.SSH.Service, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	c.SSH.HasKey = len(key) > 0
	return c, key, nil
}

// GetConfig returns an instance's upgrade configuration (never the key).
func (s *Service) GetConfig(ctx context.Context, id int64) (*Config, error) {
	if _, err := s.fleet.GetInstance(ctx, id); err != nil {
		return nil, err
	}
	c, _, err := s.load(ctx, id)
	return c, err
}

// BadInput is a validation error.
type BadInput struct{ Msg string }

func (e *BadInput) Error() string { return e.Msg }

func bad(format string, a ...any) error { return &BadInput{Msg: fmt.Sprintf(format, a...)} }

// SaveConfig validates and stores an instance's upgrade configuration.
func (s *Service) SaveConfig(ctx context.Context, id int64, in ConfigInput) (*Config, error) {
	if _, err := s.fleet.GetInstance(ctx, id); err != nil {
		return nil, err
	}
	cur, curKey, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	c := in.Config
	c.InstanceID = id
	switch c.Driver {
	case DriverAuto, DriverBuiltin, DriverDocker, DriverSSH, DriverNone:
	case "":
		c.Driver = DriverAuto
	default:
		return nil, bad("unknown driver %q", c.Driver)
	}
	c.PinnedVersion = strings.TrimSpace(c.PinnedVersion)
	if c.PinnedVersion != "" {
		if !strings.HasPrefix(c.PinnedVersion, "v") {
			c.PinnedVersion = "v" + c.PinnedVersion
		}
		if err := validTarget(c.PinnedVersion); err != nil {
			return nil, bad("pinned version: %v", err)
		}
	}
	c.Docker.Endpoint = strings.TrimSpace(c.Docker.Endpoint)
	c.Docker.Container = strings.TrimSpace(c.Docker.Container)
	c.Docker.Image = strings.TrimSpace(c.Docker.Image)
	if c.Docker.ConfPath == "" {
		c.Docker.ConfPath = "/opt/adguardhome/conf/AdGuardHome.yaml"
	}
	if c.Docker.Endpoint != "" {
		if _, err := NewDockerAPI(c.Docker); err != nil {
			return nil, bad("%v", err)
		}
	}
	if c.Driver == DriverDocker && (c.Docker.Endpoint == "" || c.Docker.Container == "") {
		return nil, bad("the Docker driver needs an endpoint and a container name")
	}
	c.SSH.Host = strings.TrimSpace(c.SSH.Host)
	if c.SSH.Port == 0 {
		c.SSH.Port = 22
	}
	if c.SSH.Port < 1 || c.SSH.Port > 65535 {
		return nil, bad("SSH port must be 1–65535")
	}
	if c.SSH.User == "" {
		c.SSH.User = "root"
	}
	c.SSH.AGHDir, c.SSH.Service = dirOr(c.SSH.AGHDir), serviceOr(c.SSH.Service)
	// The pin belongs to the machine; it is set by PinHostKey, never by the form.
	c.SSH.HostKey = cur.SSH.HostKey
	if c.SSH.Host != cur.SSH.Host || c.SSH.Port != cur.SSH.Port {
		c.SSH.HostKey = ""
	}
	key := curKey
	if in.SSHKey != nil {
		k := strings.TrimSpace(*in.SSHKey)
		if k == "" {
			key = nil
		} else {
			if _, err := sshSigner(k); err != nil {
				return nil, bad("%v", err)
			}
			if key, err = s.box.Seal(k + "\n"); err != nil {
				return nil, err
			}
		}
	}
	if c.Driver == DriverSSH && (c.SSH.Host == "" || len(key) == 0) {
		return nil, bad("the SSH driver needs a host and a private key")
	}
	_, err = s.st.DB.ExecContext(ctx, `INSERT INTO upgrade_config (instance_id, driver, pinned_version, docker_endpoint, docker_container, docker_image,
		docker_tls_skip_verify, docker_ca_pem, docker_conf_path, ssh_host, ssh_port, ssh_user, ssh_key_enc, ssh_host_key, ssh_sudo, ssh_agh_dir, ssh_service, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (instance_id) DO UPDATE SET driver = excluded.driver, pinned_version = excluded.pinned_version, docker_endpoint = excluded.docker_endpoint,
		docker_container = excluded.docker_container, docker_image = excluded.docker_image, docker_tls_skip_verify = excluded.docker_tls_skip_verify,
		docker_ca_pem = excluded.docker_ca_pem, docker_conf_path = excluded.docker_conf_path, ssh_host = excluded.ssh_host, ssh_port = excluded.ssh_port,
		ssh_user = excluded.ssh_user, ssh_key_enc = excluded.ssh_key_enc, ssh_host_key = excluded.ssh_host_key, ssh_sudo = excluded.ssh_sudo,
		ssh_agh_dir = excluded.ssh_agh_dir, ssh_service = excluded.ssh_service, updated_at = excluded.updated_at`,
		id, c.Driver, c.PinnedVersion, c.Docker.Endpoint, c.Docker.Container, c.Docker.Image, c.Docker.TLSSkipVerify, c.Docker.CAPEM, c.Docker.ConfPath,
		c.SSH.Host, c.SSH.Port, c.SSH.User, key, c.SSH.HostKey, c.SSH.Sudo, c.SSH.AGHDir, c.SSH.Service, s.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	return s.GetConfig(ctx, id)
}

// PinHostKey stores the SSH host key the user confirmed.
func (s *Service) PinHostKey(ctx context.Context, id int64, key string) error {
	if _, err := parseHostKey(key); err != nil {
		return bad("%v", err)
	}
	res, err := s.st.DB.ExecContext(ctx, `UPDATE upgrade_config SET ssh_host_key = ? WHERE instance_id = ?`, strings.TrimSpace(key), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return bad("save the SSH settings first")
	}
	return nil
}

// TestResult reports a connection test.
type TestResult struct {
	OK      bool         `json:"ok"`
	Message string       `json:"message"`
	HostKey *HostKeyInfo `json:"host_key,omitempty"`
	Pinned  bool         `json:"pinned"`
}

// Test checks the Docker or SSH settings in in (unsaved), using the stored
// SSH key when none is given.
func (s *Service) Test(ctx context.Context, id int64, kind string, in ConfigInput) (*TestResult, error) {
	cur, curKey, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	switch kind {
	case DriverDocker:
		api, err := NewDockerAPI(in.Docker)
		if err != nil {
			return &TestResult{Message: err.Error()}, nil
		}
		ver, err := api.Ping(ctx)
		if err != nil {
			return &TestResult{Message: err.Error()}, nil
		}
		if in.Docker.Container == "" {
			return &TestResult{OK: true, Message: "Docker " + ver + " answered. Enter the container name to finish."}, nil
		}
		ci, err := api.Inspect(ctx, in.Docker.Container)
		if err != nil {
			return &TestResult{Message: fmt.Sprintf("Docker %s answered, but container %q: %v", ver, in.Docker.Container, err)}, nil
		}
		state := "stopped"
		if ci.State.Running {
			state = "running"
		}
		return &TestResult{OK: true, Message: fmt.Sprintf("Docker %s: container %s is %s from image %s.", ver, strings.TrimPrefix(ci.Name, "/"), state, ci.ImageRef())}, nil
	case DriverSSH:
		key := ""
		if in.SSHKey != nil && strings.TrimSpace(*in.SSHKey) != "" {
			key = strings.TrimSpace(*in.SSHKey) + "\n"
		} else if len(curKey) > 0 {
			if key, err = s.box.Open(curKey); err != nil {
				return nil, err
			}
		} else {
			return &TestResult{Message: "paste the private key GuardTower should use"}, nil
		}
		pin := ""
		if in.SSH.Host == cur.SSH.Host && in.SSH.Port == cur.SSH.Port {
			pin = cur.SSH.HostKey
		}
		hk, out, err := TestSSH(ctx, in.SSH, key, pin)
		res := &TestResult{Pinned: pin != ""}
		if hk.Key != "" {
			res.HostKey = &hk
		}
		if err != nil {
			res.Message = err.Error()
			return res, nil
		}
		res.OK, res.Message = true, "Connected. Found "+out+"."
		return res, nil
	}
	return nil, bad("unknown test %q", kind)
}

// ---- status ----

// InstanceState is one instance on the Upgrades page.
type InstanceState struct {
	InstanceID    int64  `json:"instance_id"`
	Name          string `json:"name"`
	GroupID       int64  `json:"group_id,omitempty"`
	Online        bool   `json:"online"`
	Version       string `json:"version"`
	Configured    string `json:"configured"`
	Driver        string `json:"driver"`
	CanAutoUpdate bool   `json:"can_autoupdate"`
	// DetectPending: AdGuard Home hasn't said yet whether it can update
	// itself, because it only does so while a newer version is on offer.
	DetectPending   bool   `json:"detect_pending,omitempty"`
	BuiltinVersion  string `json:"builtin_version,omitempty"`
	Pinned          string `json:"pinned,omitempty"`
	Target          string `json:"target,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	UpToDate        bool   `json:"up_to_date"`
	Blocked         string `json:"blocked,omitempty"`
	CheckError      string `json:"check_error,omitempty"`
	CheckedAt       int64  `json:"checked_at,omitempty"`
}

type checkRow struct {
	driver, builtin, errText string
	// canAuto is only meaningful when known: AdGuard Home answers
	// can_autoupdate truthfully only while it offers a newer version.
	canAuto, known bool
	at             int64
}

func (s *Service) checks(ctx context.Context) (map[int64]checkRow, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT instance_id, driver, can_autoupdate, autoupdate_known, builtin_version, error, checked_at FROM upgrade_status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]checkRow{}
	for rows.Next() {
		var id int64
		var r checkRow
		if err := rows.Scan(&id, &r.driver, &r.canAuto, &r.known, &r.builtin, &r.errText, &r.at); err != nil {
			return nil, err
		}
		out[id] = r
	}
	return out, rows.Err()
}

// effective picks the driver for an instance; "" means none is usable, with
// the reason.
func effective(c *Config, chk checkRow) (string, string) {
	dockerOK := c.Docker.Endpoint != "" && c.Docker.Container != ""
	sshOK := c.SSH.Host != "" && c.SSH.HasKey
	switch c.Driver {
	case DriverNone:
		return "", "Upgrades are turned off for this instance."
	case DriverBuiltin:
		if !chk.canAuto && chk.known {
			return "", "AdGuard Home's built-in updater is not available on this instance."
		}
		return DriverBuiltin, ""
	case DriverDocker:
		if !dockerOK {
			return "", "Set the Docker endpoint and container name."
		}
		return DriverDocker, ""
	case DriverSSH:
		if !sshOK {
			return "", "Set the SSH host and key."
		}
		if c.SSH.HostKey == "" {
			return "", "Test the SSH connection to pin the host key."
		}
		return DriverSSH, ""
	}
	switch {
	case dockerOK:
		return DriverDocker, ""
	case sshOK && c.SSH.HostKey != "":
		return DriverSSH, ""
	case sshOK:
		return "", "Test the SSH connection to pin the host key."
	case chk.canAuto:
		return DriverBuiltin, ""
	case !chk.known:
		// Not known yet: neither usable nor blocked.
		return "", ""
	}
	return "", "No upgrade method: AdGuard Home can't update itself here (typical for Docker). Configure the Docker or SSH driver."
}

// state works out what an upgrade to want ("" = latest) would do.
func (s *Service) state(in *fleet.Instance, c *Config, chk checkRow, latest, want string) InstanceState {
	st := InstanceState{InstanceID: in.ID, Name: in.Name, GroupID: in.GroupID, Configured: c.Driver, Pinned: c.PinnedVersion,
		CanAutoUpdate: chk.canAuto, BuiltinVersion: chk.builtin, CheckError: chk.errText, CheckedAt: chk.at}
	if in.Status != nil {
		st.Online, st.Version = in.Status.Online, in.Status.Version
	}
	st.Driver, st.Blocked = effective(c, chk)
	st.UpToDate = latest != "" && st.Version != "" && !Newer(latest, st.Version)
	target := want
	if target == "" {
		target = latest
	}
	if st.Driver == "" && st.Blocked == "" {
		st.DetectPending = true
		if target != "" && Newer(target, st.Version) {
			st.Blocked = "AdGuard Home hasn't offered " + target + " to this instance yet, so GuardTower can't tell whether its built-in updater works here. It checks again every few hours; configure Docker or SSH to upgrade now."
		}
		return st
	}
	if c.PinnedVersion != "" {
		if want != "" && !SameVersion(want, c.PinnedVersion) {
			st.Blocked = "Pinned to " + c.PinnedVersion + "."
			return st
		}
		target = c.PinnedVersion
	}
	if st.Driver == DriverBuiltin {
		if chk.builtin == "" {
			target = ""
		} else if target == "" || SameVersion(target, chk.builtin) || (want == "" && c.PinnedVersion == "") {
			target = chk.builtin
		} else {
			st.Blocked = "The built-in updater can only install " + chk.builtin + "; configure Docker or SSH to choose a version."
			return st
		}
	}
	if target == "" && st.Driver != "" && st.Driver != DriverBuiltin && st.Blocked == "" {
		st.Blocked = "The latest AdGuard Home version isn't known yet (see the release check on the Upgrades page); pick a version to upgrade."
		return st
	}
	if target != "" && Newer(target, st.Version) {
		st.Target, st.UpdateAvailable = target, true
	}
	return st
}

// Overview is the Upgrades page.
type Overview struct {
	Latest        *Release        `json:"latest,omitempty"`
	Releases      []Release       `json:"releases"`
	Instances     []InstanceState `json:"instances"`
	Policies      []Policy        `json:"policies"`
	ChecksEnabled bool            `json:"checks_enabled"`
	CheckedAt     int64           `json:"checked_at,omitempty"`
	CheckError    string          `json:"check_error,omitempty"`
	Observe       bool            `json:"observe"`
}

// Overview returns releases and every instance's upgrade state.
func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	ins, err := s.fleet.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	rels, err := s.Releases.List(ctx, false)
	if err != nil {
		return nil, err
	}
	chk, err := s.checks(ctx)
	if err != nil {
		return nil, err
	}
	o := &Overview{Releases: rels, Instances: []InstanceState{}, ChecksEnabled: s.Releases.Enabled(ctx), Observe: s.ro.Observing(ctx)}
	if len(rels) > 10 {
		o.Releases = rels[:10]
	}
	latest := ""
	if len(rels) > 0 {
		o.Latest = &rels[0]
		latest = rels[0].Tag
	}
	at, e := s.Releases.State()
	if !at.IsZero() {
		o.CheckedAt = at.UnixMilli()
	}
	o.CheckError = e
	for _, in := range ins {
		c, _, err := s.load(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		o.Instances = append(o.Instances, s.state(in, c, chk[in.ID], latest, ""))
	}
	if o.Policies, err = s.Policies(ctx); err != nil {
		return nil, err
	}
	return o, nil
}

// CheckInstance asks an instance what its built-in updater offers.
func (s *Service) CheckInstance(ctx context.Context, in *fleet.Instance, recheck bool) {
	row := checkRow{at: s.Now().UnixMilli()}
	prev, _ := s.checks(ctx)
	row.canAuto, row.known = prev[in.ID].canAuto, prev[in.ID].known
	c, err := s.fleet.Client(in)
	if err == nil {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		var v *agh.VersionInfo
		v, err = c.Version(cctx, recheck)
		cancel()
		if err == nil {
			offered := Newer(v.NewVersion, statusVersion(in))
			// Without an update on offer AdGuard Home always says it can't
			// update itself; keep the last real answer instead.
			if offered || v.Disabled {
				row.canAuto, row.known = v.CanAutoUpdate && !v.Disabled, true
			}
			if offered {
				row.builtin = v.NewVersion
			}
		}
	}
	if err != nil {
		row.errText = err.Error()
	}
	cfg, _, lerr := s.load(ctx, in.ID)
	if lerr == nil {
		row.driver, _ = effective(cfg, row)
	}
	if _, err := s.st.DB.ExecContext(ctx, `INSERT INTO upgrade_status (instance_id, driver, can_autoupdate, autoupdate_known, builtin_version, error, checked_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (instance_id) DO UPDATE SET driver = excluded.driver, can_autoupdate = excluded.can_autoupdate, autoupdate_known = excluded.autoupdate_known,
		builtin_version = excluded.builtin_version, error = excluded.error, checked_at = excluded.checked_at`, in.ID, row.driver, row.canAuto, row.known, row.builtin, row.errText, row.at); err != nil {
		s.log.Warn("upgrade: save check", "err", err)
	}
}

func statusVersion(in *fleet.Instance) string {
	if in.Status == nil {
		return ""
	}
	return in.Status.Version
}

// CheckAll refreshes releases (when enabled) and every online instance.
func (s *Service) CheckAll(ctx context.Context, recheck bool) error {
	var relErr error
	if s.Releases.Enabled(ctx) {
		relErr = s.Releases.Refresh(ctx)
	}
	ins, err := s.fleet.ListInstances(ctx)
	if err != nil {
		return err
	}
	for _, in := range ins {
		if in.Enabled && in.Status != nil && in.Status.Online {
			s.CheckInstance(ctx, in, recheck)
		}
	}
	s.bus.Publish("upgrade.update", map[string]any{"checked": true})
	return relErr
}

// ---- plan & run ----

// PlanItem is one instance in an upgrade plan.
type PlanItem struct {
	InstanceID int64  `json:"instance_id"`
	Name       string `json:"name"`
	Canary     bool   `json:"canary"`
	Online     bool   `json:"online"`
	Driver     string `json:"driver"`
	From       string `json:"from"`
	To         string `json:"to,omitempty"`
	Skip       string `json:"skip,omitempty"`
}

// Plan is a dry run of an upgrade.
type Plan struct {
	Target   string     `json:"target"`
	Version  string     `json:"version,omitempty"`
	Items    []PlanItem `json:"items"`
	Total    int        `json:"total"`
	Observe  bool       `json:"observe"`
	Warnings []string   `json:"warnings"`
}

// MakePlan works out what upgrading target to version ("" = latest) would do.
func (s *Service) MakePlan(ctx context.Context, t rollout.Target, version string) (*Plan, error) {
	version = strings.TrimSpace(version)
	if version != "" {
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		if err := validTarget(version); err != nil {
			return nil, bad("%v", err)
		}
	}
	ins, desc, err := s.ro.Resolve(ctx, t)
	if err != nil {
		return nil, err
	}
	latest := ""
	if l, err := s.Releases.Latest(ctx); err == nil {
		latest = l.Tag
	}
	chk, err := s.checks(ctx)
	if err != nil {
		return nil, err
	}
	all, err := s.fleet.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	p := &Plan{Target: desc, Version: version, Items: []PlanItem{}, Observe: s.ro.Observing(ctx), Warnings: []string{}}
	if latest == "" && version == "" {
		p.Warnings = append(p.Warnings, "GuardTower doesn't know the latest AdGuard Home release yet (release checks may be off). Only instances whose built-in updater offers a version can be upgraded.")
	}
	groupSize := map[int64]int{}
	for _, in := range all {
		if in.Enabled && in.GroupID != 0 {
			groupSize[in.GroupID]++
		}
	}
	warnedGroup := map[int64]bool{}
	for _, in := range ins {
		c, _, err := s.load(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		st := s.state(in, c, chk[in.ID], latest, version)
		it := PlanItem{InstanceID: in.ID, Name: in.Name, Canary: in.IsCanary, Online: st.Online, Driver: st.Driver, From: st.Version, To: st.Target}
		switch {
		case !st.Online:
			it.Skip = "offline"
		case st.Blocked != "":
			it.Skip = st.Blocked
		case !st.UpdateAvailable:
			continue // already up to date
		}
		if it.Skip == "" {
			p.Total++
			if in.GroupID != 0 && groupSize[in.GroupID] == 1 && !warnedGroup[in.GroupID] {
				warnedGroup[in.GroupID] = true
				p.Warnings = append(p.Warnings, in.Name+" is the only instance in its group: clients using it lose DNS for a few seconds while it restarts.")
			}
		}
		p.Items = append(p.Items, it)
	}
	return p, nil
}

// Start runs an upgrade rollout in the background and returns its ID.
func (s *Service) Start(ctx context.Context, t rollout.Target, version, by string) (int64, error) {
	p, err := s.MakePlan(ctx, t, version)
	if err != nil {
		return 0, err
	}
	if p.Total == 0 {
		return 0, bad("nothing to upgrade: every selected instance is up to date or can't be upgraded")
	}
	items := make([]rollout.JobItem, 0, len(p.Items))
	for _, it := range p.Items {
		items = append(items, rollout.JobItem{InstanceID: it.InstanceID, Canary: it.Canary, Skip: it.Skip,
			Changes: []config.Change{{Section: "upgrade", Field: "version", Label: "AdGuard Home version", Op: "set", From: it.From, To: it.To}}})
	}
	title := "Upgrade " + p.Target
	if version != "" {
		title += " to " + version
	}
	return s.ro.StartJob(ctx, "upgrade", title, by, items, s.runStep)
}

func (s *Service) driver(ctx context.Context, in *fleet.Instance, c *agh.Client) (Driver, error) {
	cfg, key, err := s.load(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	chk, _ := s.checks(ctx)
	name, why := effective(cfg, chk[in.ID])
	switch name {
	case DriverBuiltin:
		return &Builtin{C: c}, nil
	case DriverDocker:
		api, err := NewDockerAPI(cfg.Docker)
		if err != nil {
			return nil, err
		}
		return &Docker{API: api, Cfg: cfg.Docker}, nil
	case DriverSSH:
		k, err := s.box.Open(key)
		if err != nil {
			return nil, err
		}
		return &SSH{Cfg: cfg.SSH, Key: k}, nil
	}
	return nil, errors.New(why)
}

// runStep upgrades one instance: guard → upgrade → wait for the new version →
// health probe, rolling back on failure where the driver can.
func (s *Service) runStep(ctx context.Context, st rollout.Step, update func(status, detail string)) error {
	if len(st.Changes) == 0 {
		update("failed", "missing target version")
		return errors.New("missing target version")
	}
	target, _ := st.Changes[0].To.(string)
	in, err := s.fleet.GetInstance(ctx, st.InstanceID)
	if err != nil {
		update("failed", err.Error())
		return err
	}
	// Never take down the last healthy member of a group.
	if in.GroupID != 0 {
		all, err := s.fleet.ListInstances(ctx)
		if err == nil {
			peers, healthy := 0, 0
			for _, o := range all {
				if o.ID != in.ID && o.Enabled && o.GroupID == in.GroupID {
					peers++
					if o.Status != nil && o.Status.Online {
						healthy++
					}
				}
			}
			if peers > 0 && healthy == 0 {
				update("skipped", "skipped: no other member of its group is online, so upgrading it would leave the group without DNS")
				return nil
			}
		}
	}
	c, err := s.fleet.Client(in)
	if err != nil {
		update("failed", err.Error())
		return err
	}
	before, err := c.Status(ctx)
	if err != nil {
		update("failed", "instance not reachable: "+err.Error())
		return err
	}
	if !Newer(target, before.Version) {
		update("succeeded", "already on "+before.Version)
		return nil
	}
	drv, err := s.driver(ctx, in, c)
	if err != nil {
		update("skipped", err.Error())
		return nil
	}
	progress := func(msg string) { update("applying", drv.Name()+": "+msg) }
	res, cause := drv.Upgrade(ctx, target, progress)
	if cause == nil {
		update("verifying", "waiting for AdGuard Home "+target+" to answer and resolve DNS")
		cause = s.waitHealthy(ctx, in, c, target)
	}
	if cause == nil {
		detail := "upgraded " + before.Version + " → " + target + ", healthy"
		if res != nil && res.Finalize != nil {
			if err := res.Finalize(ctx); err != nil {
				detail += " (cleanup: " + err.Error() + ")"
			}
		}
		update("succeeded", detail)
		s.changed()
		return nil
	}
	if res == nil || res.Undo == nil {
		hint := ""
		if drv.Name() == DriverBuiltin {
			hint = " AdGuard Home keeps the previous version in agh-backup/ next to its binary; see the upgrades docs to restore it by hand."
		}
		update("failed", cause.Error()+"."+hint)
		s.changed()
		return cause
	}
	update("applying", "rolling back to "+before.Version+" after: "+cause.Error())
	if err := res.Undo(ctx); err != nil {
		update("failed", fmt.Sprintf("%v; rolling back also failed: %v", cause, err))
		s.changed()
		return fmt.Errorf("%w; rollback failed: %w", cause, err)
	}
	if err := s.waitHealthy(ctx, in, c, before.Version); err != nil {
		update("failed", fmt.Sprintf("%v; after rolling back, %v", cause, err))
		s.changed()
		return fmt.Errorf("%w; after rollback: %w", cause, err)
	}
	update("rolled_back", "rolled back to "+before.Version+" after: "+cause.Error())
	s.changed()
	return fmt.Errorf("%w: %w", rollout.ErrRolledBack, cause)
}

func (s *Service) changed() {
	if s.OnChanged != nil {
		s.OnChanged()
	}
	s.bus.Publish("upgrade.update", map[string]any{"changed": true})
}

// waitHealthy waits until the instance reports version want and passes a DNS
// health probe.
func (s *Service) waitHealthy(ctx context.Context, in *fleet.Instance, c *agh.Client, want string) error {
	deadline := time.Now().Add(s.WaitTimeout)
	var last string
	for {
		sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		st, err := c.Status(sctx)
		cancel()
		switch {
		case err != nil:
			last = err.Error()
		case !SameVersion(st.Version, want):
			last = "still reports " + st.Version
		case !st.Running:
			last = "DNS server not running"
		default:
			h := health.Probe(ctx, in, c)
			if h.DNSOK {
				return nil
			}
			last = h.Error
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("AdGuard Home did not come back healthy on %s within %s (%s)", want, s.WaitTimeout, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.PollEvery):
		}
	}
}

// ---- policies & scheduler ----

// Policy is a group's upgrade policy.
type Policy struct {
	GroupID  int64  `json:"group_id"`
	Policy   string `json:"policy"` // manual | auto
	Window   Window `json:"window"`
	LastAuto int64  `json:"last_auto,omitempty"`
}

// Policies returns every group's policy (manual by default).
func (s *Service) Policies(ctx context.Context) ([]Policy, error) {
	groups, err := s.fleet.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Policy, 0, len(groups))
	for _, g := range groups {
		p := Policy{GroupID: g.ID, Policy: "manual", Window: Window{Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "03:00", End: "05:00"}}
		var days string
		err := s.st.DB.QueryRowContext(ctx, `SELECT policy, days, start_time, end_time, last_auto FROM upgrade_policy WHERE group_id = ?`, g.ID).
			Scan(&p.Policy, &days, &p.Window.Start, &p.Window.End, &p.LastAuto)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			_ = json.Unmarshal([]byte(days), &p.Window.Days)
		}
		out = append(out, p)
	}
	return out, nil
}

// SetPolicy stores a group's policy.
func (s *Service) SetPolicy(ctx context.Context, groupID int64, p Policy) error {
	if _, err := s.fleet.GetGroup(ctx, groupID); err != nil {
		return err
	}
	if p.Policy != "manual" && p.Policy != "auto" {
		return bad("policy must be manual or auto")
	}
	if err := p.Window.Validate(); err != nil {
		return bad("maintenance window: %v", err)
	}
	days, _ := json.Marshal(p.Window.Days)
	_, err := s.st.DB.ExecContext(ctx, `INSERT INTO upgrade_policy (group_id, policy, days, start_time, end_time) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (group_id) DO UPDATE SET policy = excluded.policy, days = excluded.days, start_time = excluded.start_time, end_time = excluded.end_time`,
		groupID, p.Policy, string(days), p.Window.Start, p.Window.End)
	return err
}

// RunAuto starts automatic upgrades for groups whose maintenance window is
// open. A group is upgraded at most once per 20 hours.
func (s *Service) RunAuto(ctx context.Context) {
	if s.ro.Observing(ctx) || s.ro.ActiveID() != 0 {
		return
	}
	pols, err := s.Policies(ctx)
	if err != nil {
		s.log.Warn("upgrade: policies", "err", err)
		return
	}
	now := s.Now()
	for _, p := range pols {
		if p.Policy != "auto" || !p.Window.Contains(now) || now.Sub(time.UnixMilli(p.LastAuto)) < 20*time.Hour {
			continue
		}
		t := rollout.Target{Kind: "group", ID: p.GroupID}
		plan, err := s.MakePlan(ctx, t, "")
		if err != nil || plan.Total == 0 {
			continue
		}
		id, err := s.Start(ctx, t, "", "maintenance window")
		if err != nil {
			s.log.Warn("upgrade: automatic upgrade", "group", p.GroupID, "err", err)
			continue
		}
		_, _ = s.st.DB.ExecContext(ctx, `UPDATE upgrade_policy SET last_auto = ? WHERE group_id = ?`, now.UnixMilli(), p.GroupID)
		s.log.Info("upgrade: started automatic upgrade", "group", p.GroupID, "rollout", id)
		return // one rollout at a time
	}
}

// Run checks releases and instances every 6 hours and opens maintenance
// windows every 5 minutes until ctx ends.
func (s *Service) Run(ctx context.Context) {
	if err := s.CheckAll(ctx, false); err != nil {
		s.log.Info("upgrade: release check", "err", err)
	}
	lastCheck := time.Now()
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if time.Since(lastCheck) >= 6*time.Hour {
				lastCheck = time.Now()
				if err := s.CheckAll(ctx, false); err != nil {
					s.log.Info("upgrade: release check", "err", err)
				}
			}
			s.RunAuto(ctx)
		}
	}
}
