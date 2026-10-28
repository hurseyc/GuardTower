// Package resilience finds single points of failure: groups whose DNS
// servers would fail together because they share a host, a power supply or
// a site, have only one member, or have lost their redundancy at runtime.
package resilience

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hurseyc/guardtower/internal/fleet"
)

// Severity of a finding.
const (
	Info     = "info"
	Warn     = "warn"
	Critical = "critical"
)

// Finding kinds.
const (
	KindSingleMember = "single_member"
	KindSharedHost   = "shared_host"
	KindSharedPower  = "shared_power"
	KindSharedSite   = "shared_site"
	KindUnplaced     = "unplaced"
	KindDegraded     = "degraded"
	KindDown         = "down"
)

// Group scores, worst first.
const (
	ScoreDown      = "down"
	ScoreSPOF      = "spof"
	ScoreAtRisk    = "at_risk"
	ScoreUnknown   = "unknown"
	ScoreRedundant = "redundant"
)

// Finding is one resilience problem.
type Finding struct {
	Severity    string  `json:"severity"`
	Kind        string  `json:"kind"`
	GroupID     int64   `json:"group_id"`
	InstanceIDs []int64 `json:"instance_ids"`
	HostID      int64   `json:"host_id,omitempty"`
	Message     string  `json:"message"`
	Fix         string  `json:"fix,omitempty"`
}

// GroupReport is one group's redundancy.
type GroupReport struct {
	GroupID  int64     `json:"group_id"`
	Name     string    `json:"name"`
	Score    string    `json:"score"`
	Members  int       `json:"members"`
	Online   int       `json:"online"`
	Hosts    int       `json:"hosts"`
	Findings []Finding `json:"findings"`
}

// Report covers the whole fleet.
type Report struct {
	Groups []GroupReport `json:"groups"`
	// SPOFHosts maps a host to the groups it alone serves.
	SPOFHosts map[int64][]int64 `json:"spof_hosts"`
	// Instances maps an instance to the worst severity among findings naming it.
	Instances map[int64]string `json:"instances"`
	Summary   struct {
		Redundant int `json:"redundant"`
		AtRisk    int `json:"at_risk"`
		SPOF      int `json:"spof"`
		Down      int `json:"down"`
		Unknown   int `json:"unknown"`
	} `json:"summary"`
}

func names(ins []*fleet.Instance) string {
	n := make([]string, len(ins))
	for i, x := range ins {
		n[i] = x.Name
	}
	return strings.Join(n, " and ")
}

func ids(ins []*fleet.Instance) []int64 {
	out := make([]int64, len(ins))
	for i, x := range ins {
		out[i] = x.ID
	}
	return out
}

func online(in *fleet.Instance) bool { return in.Status != nil && in.Status.Online }

var rank = map[string]int{Info: 1, Warn: 2, Critical: 3}

// Analyze computes the report from the fleet model. It is a pure function.
func Analyze(instances []*fleet.Instance, groups []*fleet.Group, hosts []*fleet.Host, sites []*fleet.Site) *Report {
	hostByID := map[int64]*fleet.Host{}
	for _, h := range hosts {
		hostByID[h.ID] = h
	}
	siteName := map[int64]string{}
	for _, s := range sites {
		siteName[s.ID] = s.Name
	}
	r := &Report{Groups: []GroupReport{}, SPOFHosts: map[int64][]int64{}, Instances: map[int64]string{}}
	sorted := append([]*fleet.Group(nil), groups...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, g := range sorted {
		var members []*fleet.Instance
		for _, in := range instances {
			if in.Enabled && in.GroupID == g.ID {
				members = append(members, in)
			}
		}
		gr := analyzeGroup(g, members, hostByID, siteName, len(sites))
		for _, f := range gr.Findings {
			if f.Kind == KindSharedHost && f.HostID != 0 {
				r.SPOFHosts[f.HostID] = append(r.SPOFHosts[f.HostID], g.ID)
			}
			for _, id := range f.InstanceIDs {
				if rank[f.Severity] > rank[r.Instances[id]] {
					r.Instances[id] = f.Severity
				}
			}
		}
		switch gr.Score {
		case ScoreRedundant:
			r.Summary.Redundant++
		case ScoreAtRisk:
			r.Summary.AtRisk++
		case ScoreSPOF:
			r.Summary.SPOF++
		case ScoreDown:
			r.Summary.Down++
		case ScoreUnknown:
			r.Summary.Unknown++
		}
		r.Groups = append(r.Groups, gr)
	}
	return r
}

func analyzeGroup(g *fleet.Group, members []*fleet.Instance, hostByID map[int64]*fleet.Host, siteName map[int64]string, siteCount int) GroupReport {
	gr := GroupReport{GroupID: g.ID, Name: g.Name, Members: len(members), Findings: []Finding{}}
	if len(members) == 0 {
		gr.Score = ScoreUnknown
		return gr
	}
	add := func(sev, kind string, ins []*fleet.Instance, host int64, msg, fix string) {
		gr.Findings = append(gr.Findings, Finding{Severity: sev, Kind: kind, GroupID: g.ID, InstanceIDs: ids(ins), HostID: host, Message: msg, Fix: fix})
	}
	var up []*fleet.Instance
	for _, m := range members {
		if online(m) {
			up = append(up, m)
		}
	}
	gr.Online = len(up)

	// Runtime health first: it is what hurts right now.
	switch {
	case len(up) == 0:
		add(Critical, KindDown, members, 0, fmt.Sprintf("No DNS server in %s is answering.", g.Name), "Check the instances and their hosts.")
	case len(members) > 1 && len(up) == 1:
		add(Warn, KindDegraded, up, 0, fmt.Sprintf("Only %s is answering in %s; it has no backup right now.", up[0].Name, g.Name), "Bring the other instances back online.")
	}

	if len(members) == 1 {
		add(Warn, KindSingleMember, members, members[0].HostID, fmt.Sprintf("%s has a single DNS server (%s). If it goes down, clients using %s lose DNS.", g.Name, members[0].Name, g.Name),
			"Add a second instance on a different host.")
	}

	byHost := map[int64][]*fleet.Instance{}
	var unplaced []*fleet.Instance
	for _, m := range members {
		if m.HostID == 0 || hostByID[m.HostID] == nil {
			unplaced = append(unplaced, m)
			continue
		}
		byHost[m.HostID] = append(byHost[m.HostID], m)
	}
	gr.Hosts = len(byHost)
	if len(unplaced) > 0 && len(members) > 1 {
		add(Info, KindUnplaced, unplaced, 0, fmt.Sprintf("%s %s no host recorded, so GuardTower can't tell whether %s's servers would fail together.", names(unplaced), plural(len(unplaced), "has", "have"), g.Name),
			"Set “Runs on host” for each instance under Instances.")
	}
	if len(members) > 1 && len(unplaced) == 0 {
		if len(byHost) == 1 {
			var hid int64
			for id := range byHost {
				hid = id
			}
			h := hostByID[hid]
			add(Critical, KindSharedHost, members, hid, fmt.Sprintf("All %d DNS servers of %s run on %s. If %s restarts, %s has no DNS.", len(members), g.Name, h.Name, h.Name, g.Name),
				"Move one instance to another host.")
		} else {
			power, site := map[string]bool{}, map[int64]bool{}
			allPower, allSite := true, true
			for hid := range byHost {
				h := hostByID[hid]
				if h.Power == "" {
					allPower = false
				} else {
					power[strings.ToLower(h.Power)] = true
				}
				if h.SiteID == 0 {
					allSite = false
				} else {
					site[h.SiteID] = true
				}
			}
			if allPower && len(power) == 1 {
				var p string
				for hid := range byHost {
					p = hostByID[hid].Power
				}
				add(Warn, KindSharedPower, members, 0, fmt.Sprintf("%s's servers are on different hosts, but all of them depend on power %q.", g.Name, p),
					"Put one host on a different UPS or circuit.")
			}
			if siteCount > 1 && allSite && len(site) == 1 {
				var sid int64
				for s := range site {
					sid = s
				}
				add(Info, KindSharedSite, members, 0, fmt.Sprintf("All of %s's servers are at %s.", g.Name, siteName[sid]),
					"For site-level redundancy, run one instance at another site.")
			}
		}
	}

	gr.Score = score(gr)
	return gr
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func score(gr GroupReport) string {
	has := func(kind string) bool {
		for _, f := range gr.Findings {
			if f.Kind == kind {
				return true
			}
		}
		return false
	}
	switch {
	case has(KindDown):
		return ScoreDown
	case has(KindSharedHost), has(KindSingleMember):
		return ScoreSPOF
	case has(KindSharedPower), has(KindDegraded):
		return ScoreAtRisk
	case has(KindUnplaced) && gr.Hosts < 2:
		return ScoreUnknown
	}
	return ScoreRedundant
}

// Service reads the fleet and analyzes it.
type Service struct {
	fleet *fleet.Service
}

// New creates the service.
func New(fl *fleet.Service) *Service { return &Service{fleet: fl} }

type snapshot struct {
	ins    []*fleet.Instance
	groups []*fleet.Group
	hosts  []*fleet.Host
	sites  []*fleet.Site
}

func (s *Service) load(ctx context.Context) (*snapshot, error) {
	var sn snapshot
	var err error
	if sn.ins, err = s.fleet.ListInstances(ctx); err != nil {
		return nil, err
	}
	if sn.groups, err = s.fleet.ListGroups(ctx); err != nil {
		return nil, err
	}
	if sn.hosts, err = s.fleet.ListHosts(ctx); err != nil {
		return nil, err
	}
	if sn.sites, err = s.fleet.ListSites(ctx); err != nil {
		return nil, err
	}
	return &sn, nil
}

// Report analyzes the current fleet.
func (s *Service) Report(ctx context.Context) (*Report, error) {
	sn, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	return Analyze(sn.ins, sn.groups, sn.hosts, sn.sites), nil
}

// Placement is a proposed group and host for an instance (0 = none). A zero
// InstanceID means a new instance.
type Placement struct {
	InstanceID int64  `json:"instance_id"`
	Name       string `json:"name"`
	GroupID    int64  `json:"group_id"`
	HostID     int64  `json:"host_id"`
}

// Preview returns the placement findings (shared host/power/site, single
// member) that the placement would newly create. Runtime health is ignored:
// the question is where the instance runs, not whether it is up.
func (s *Service) Preview(ctx context.Context, p Placement) ([]Finding, error) {
	sn, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	before := Analyze(withAllOnline(sn.ins), sn.groups, sn.hosts, sn.sites)
	next := make([]*fleet.Instance, 0, len(sn.ins)+1)
	found := false
	for _, in := range sn.ins {
		if in.ID == p.InstanceID && p.InstanceID != 0 {
			cp := *in
			cp.GroupID, cp.HostID = p.GroupID, p.HostID
			next = append(next, &cp)
			found = true
			continue
		}
		next = append(next, in)
	}
	if !found {
		name := p.Name
		if name == "" {
			name = "the new instance"
		}
		next = append(next, &fleet.Instance{ID: -1, Name: name, GroupID: p.GroupID, HostID: p.HostID, Enabled: true})
	}
	after := Analyze(withAllOnline(next), sn.groups, sn.hosts, sn.sites)
	seen := map[string]bool{}
	for _, g := range before.Groups {
		for _, f := range g.Findings {
			seen[key(f)] = true
		}
	}
	out := []Finding{}
	for _, g := range after.Groups {
		for _, f := range g.Findings {
			if f.Kind == KindUnplaced || seen[key(f)] {
				continue
			}
			out = append(out, f)
		}
	}
	return out, nil
}

func key(f Finding) string { return fmt.Sprintf("%s/%d/%d", f.Kind, f.GroupID, f.HostID) }

func withAllOnline(ins []*fleet.Instance) []*fleet.Instance {
	out := make([]*fleet.Instance, len(ins))
	for i, in := range ins {
		cp := *in
		cp.Status = &fleet.Status{Online: true}
		out[i] = &cp
	}
	return out
}
