package demo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/miekg/dns"

	"github.com/hurseyc/guardtower/internal/app"
	"github.com/hurseyc/guardtower/internal/config"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/querylog"
	"github.com/hurseyc/guardtower/internal/rollout"
)

// Demo sign-in, shown on the login page in demo mode.
const (
	User     = "demo"
	Password = "guardtower-demo"
)

const (
	simUser = "admin"
	simPass = "demo-agh-password"
)

var (
	adults = []Device{
		{IP: "192.168.1.10", Hostname: "chris-laptop.lan", Weight: 5, Mix: map[string]float64{"ads": 0.22}},
		{IP: "192.168.1.11", Hostname: "chris-phone.lan", Weight: 4, Mix: map[string]float64{"ads": 0.28}},
		{IP: "192.168.1.12", Hostname: "jo-phone.lan", Weight: 4, Mix: map[string]float64{"ads": 0.3, "gambling": 0.004}},
		{IP: "192.168.1.20", Hostname: "living-room-tv.lan", Weight: 3, Mix: map[string]float64{"ads": 0.45}},
		{IP: "192.168.1.30", Hostname: "nas.lan", Weight: 1.5, Mix: map[string]float64{"ads": 0.02}},
		{IP: "192.168.1.40", Hostname: "pve-01.lan", Weight: 1, Mix: map[string]float64{}},
	}
	kids = []Device{
		{IP: "192.168.20.21", Hostname: "sams-ipad.lan", Weight: 4, Mix: map[string]float64{"ads": 0.25, "adult": 0.012, "bypass": 0.004}, Night: true},
		{IP: "192.168.20.22", Hostname: "sams-laptop.lan", Weight: 3, Mix: map[string]float64{"ads": 0.2, "adult": 0.02, "bypass": 0.01, "gambling": 0.003}, Night: true},
		{IP: "192.168.20.31", Hostname: "alex-phone.lan", Weight: 4, Mix: map[string]float64{"ads": 0.3, "adult": 0.002}},
		{IP: "192.168.20.40", Hostname: "switch.lan", Weight: 1.5, Mix: map[string]float64{"ads": 0.1}},
		{IP: "192.168.20.50", Hostname: "", Weight: 0.8, Mix: map[string]float64{"ads": 0.2, "bypass": 0.02}},
	}
)

type spec struct {
	name, version, runtime, host, group string
	autoUpdate, canary                  bool
	tags                                []string
	profile                             Profile
}

var specs = []spec{
	{"agh-1", LatestVersion, "lxc", "pve-01", "Main network", true, true, []string{"lxc", "primary"}, Profile{QPS: 0.3, Devices: adults}},
	{"agh-2", "v0.107.72", "lxc", "pve-02", "Main network", true, false, []string{"lxc"}, Profile{QPS: 0.18, Devices: adults}},
	{"agh-3", LatestVersion, "docker", "synology", "Main network", false, false, []string{"nas", "docker"}, Profile{QPS: 0.1, Devices: adults}},
	{"agh-4", "v0.107.75", "docker", "synology", "Kids VLAN", false, true, []string{"nas", "docker"}, Profile{QPS: 0.16, Devices: kids}},
	{"agh-5", "v0.107.75", "docker", "synology", "Kids VLAN", false, false, []string{"nas", "docker"}, Profile{QPS: 0.07, Devices: kids}},
}

// history is how much generated traffic Seed stores (shortened in tests).
var history = 7 * 24 * time.Hour

// Fleet is the running simulated fleet.
type Fleet struct {
	Sims  []*Sim
	URLs  []string
	DNS   []string
	stops []func()
}

// Start runs the simulated instances on loopback ports until ctx ends.
func Start(ctx context.Context) (*Fleet, error) {
	f := &Fleet{}
	for i, sp := range specs {
		s := NewSim(sp.name, sp.version, sp.autoUpdate, sp.profile, uint64(i+1)*7919)
		if sp.group == "Kids VLAN" {
			s.toggles["parental"] = true // set up by hand before GuardTower arrived
		}
		var lc net.ListenConfig
		hl, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
		if err != nil {
			f.Close()
			return nil, err
		}
		pc, err := lc.ListenPacket(ctx, "udp", "127.0.0.1:0")
		if err != nil {
			hl.Close()
			f.Close()
			return nil, err
		}
		s.dnsAddr = pc.LocalAddr().String()
		hs := &http.Server{Handler: basicAuth(s), ReadHeaderTimeout: 5 * time.Second}
		ds := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(s.serveDNS)}
		go func() { _ = hs.Serve(hl) }()
		go func() { _ = ds.ActivateAndServe() }()
		f.stops = append(f.stops, func() { _ = hs.Close(); _ = ds.Shutdown() })
		f.Sims = append(f.Sims, s)
		f.URLs = append(f.URLs, "http://"+hl.Addr().String())
		f.DNS = append(f.DNS, s.dnsAddr)
		go s.Run(ctx)
	}
	return f, nil
}

func basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != simUser || p != simPass {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Close stops every simulated instance.
func (f *Fleet) Close() {
	for _, s := range f.stops {
		s()
	}
}

// Seed creates the demo admin, hosts, groups, instances, settings layers,
// family members and a week of query history.
func Seed(ctx context.Context, a *app.App, f *Fleet, log *slog.Logger) error {
	if need, err := a.Auth.NeedsSetup(ctx); err != nil || !need {
		return errors.Join(err, errors.New("demo mode needs an empty data directory"))
	}
	if _, err := a.Auth.CreateUser(ctx, User, Password, "admin", true); err != nil {
		return err
	}
	// The fleet is simulated, so let changes be applied straight away.
	if err := a.Store.SetSetting(ctx, rollout.SettingObserve, "false"); err != nil {
		return err
	}
	hosts := map[string]int64{}
	for _, h := range []fleet.Host{
		{Name: "pve-01", Kind: "proxmox", Address: "192.168.10.11", Power: "UPS-A"},
		{Name: "pve-02", Kind: "proxmox", Address: "192.168.10.12", Power: "UPS-B"},
		{Name: "synology", Kind: "nas", Address: "192.168.10.20", Power: "UPS-A"},
	} {
		saved, err := a.Fleet.SaveHost(ctx, 0, h)
		if err != nil {
			return err
		}
		hosts[h.Name] = saved.ID
	}
	groups := map[string]int64{}
	for _, g := range [][3]string{{"Main network", "Adults and infrastructure", "#34d399"}, {"Kids VLAN", "Strict family filtering", "#a78bfa"}} {
		saved, err := a.Fleet.SaveGroup(ctx, 0, g[0], g[1], g[2])
		if err != nil {
			return err
		}
		groups[g[0]] = saved.ID
	}
	pw := simPass
	ids := make([]int64, len(specs))
	for i, sp := range specs {
		in, err := a.Fleet.CreateInstance(ctx, fleet.InstanceInput{
			Name: sp.name, URL: f.URLs[i], Username: simUser, Password: &pw, DNSAddress: f.DNS[i],
			GroupID: groups[sp.group], HostID: hosts[sp.host], Runtime: sp.runtime, IsCanary: sp.canary, Tags: sp.tags,
		})
		if err != nil {
			return err
		}
		ids[i] = in.ID
	}

	// Shared settings live in Base; the kids' group adds Safe Search, which
	// leaves changes waiting in Review & apply.
	if err := a.Config.ImportBaseline(ctx, ids[0], config.ScopeRef{Scope: config.ScopeBase}, []string{"dns", "filtering", "access", "querylog", "stats"}, User); err != nil {
		return fmt.Errorf("import baseline: %w", err)
	}
	if err := a.Config.ImportBaseline(ctx, ids[3], config.ScopeRef{Scope: config.ScopeGroup, ID: groups["Kids VLAN"]}, []string{"parental"}, User); err != nil {
		return fmt.Errorf("import kids baseline: %w", err)
	}
	if _, err := a.Config.SetLayer(ctx, config.ScopeRef{Scope: config.ScopeGroup, ID: groups["Kids VLAN"]}, "safesearch", config.Doc{"enabled": true}, User); err != nil {
		return err
	}

	for _, p := range []struct {
		name, emoji, color string
		devices            [][2]string
	}{
		{"Sam", "🧒", "#60a5fa", [][2]string{{"192.168.20.21", "iPad"}, {"192.168.20.22", "Laptop"}}},
		{"Alex", "👧", "#f472b6", [][2]string{{"192.168.20.31", "Phone"}}},
	} {
		person, err := a.Family.SavePerson(ctx, 0, p.name, p.emoji, p.color)
		if err != nil {
			return err
		}
		for _, d := range p.devices {
			if err := a.Family.AssignDevice(ctx, d[0], d[1], person.ID); err != nil {
				return err
			}
		}
	}

	// History: the instances keep the last two hours themselves (the
	// collector picks those up); older days go straight into the warehouse.
	now := time.Now()
	total := 0
	for i, s := range f.Sims {
		s.Prime(2 * time.Hour)
		entries := s.Generate(now.Add(-history), now.Add(-2*time.Hour))
		for start := 0; start < len(entries); start += 5000 {
			end := min(start+5000, len(entries))
			rows := make([]querylog.Row, 0, end-start)
			for _, e := range entries[start:end] {
				rows = append(rows, querylog.FromEntry(ids[i], e))
			}
			if _, err := a.QueryLog.Insert(ctx, rows); err != nil {
				return err
			}
		}
		total += len(entries)
	}
	if _, err := a.QueryLog.BuildTops(ctx, int(history/time.Hour)); err != nil {
		return err
	}
	log.Info("demo fleet seeded", "instances", len(specs), "history_rows", total)
	return nil
}
