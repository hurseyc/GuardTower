package resilience

import (
	"testing"

	"github.com/hurseyc/guardtower/internal/fleet"
)

func inst(id int64, name string, group, host int64, up bool) *fleet.Instance {
	return &fleet.Instance{ID: id, Name: name, GroupID: group, HostID: host, Enabled: true, Status: &fleet.Status{Online: up}}
}

var (
	groups = []*fleet.Group{{ID: 1, Name: "Main network"}, {ID: 2, Name: "Kids VLAN"}}
	hosts  = []*fleet.Host{{ID: 10, Name: "pve-01", Power: "ups-a"}, {ID: 11, Name: "pve-02", Power: "ups-a"}, {ID: 12, Name: "synology"}}
)

func kinds(g GroupReport) map[string]bool {
	m := map[string]bool{}
	for _, f := range g.Findings {
		m[f.Kind] = true
	}
	return m
}

func byName(r *Report, n string) GroupReport {
	for _, g := range r.Groups {
		if g.Name == n {
			return g
		}
	}
	return GroupReport{}
}

func TestAnalyzeSeededFleet(t *testing.T) {
	ins := []*fleet.Instance{
		inst(1, "agh-1", 1, 10, true), inst(2, "agh-2", 1, 11, true), inst(3, "agh-3", 1, 12, true),
		inst(4, "agh-4", 2, 12, true), inst(5, "agh-5", 2, 12, true),
	}
	r := Analyze(ins, groups, hosts, nil)
	main, kids := byName(r, "Main network"), byName(r, "Kids VLAN")
	if main.Score != ScoreRedundant || len(main.Findings) != 0 {
		t.Errorf("main = %+v", main)
	}
	if kids.Score != ScoreSPOF || !kinds(kids)[KindSharedHost] {
		t.Errorf("kids = %+v", kids)
	}
	if got := r.SPOFHosts[12]; len(got) != 1 || got[0] != 2 {
		t.Errorf("spof hosts = %v", r.SPOFHosts)
	}
	if r.Instances[4] != Critical || r.Instances[1] != "" {
		t.Errorf("instance severities = %v", r.Instances)
	}
	if r.Summary.SPOF != 1 || r.Summary.Redundant != 1 {
		t.Errorf("summary = %+v", r.Summary)
	}
}

func TestAnalyzeCases(t *testing.T) {
	cases := []struct {
		name  string
		ins   []*fleet.Instance
		score string
		kind  string
	}{
		{"single member", []*fleet.Instance{inst(1, "a", 1, 10, true)}, ScoreSPOF, KindSingleMember},
		{"shared power", []*fleet.Instance{inst(1, "a", 1, 10, true), inst(2, "b", 1, 11, true)}, ScoreAtRisk, KindSharedPower},
		{"degraded", []*fleet.Instance{inst(1, "a", 1, 10, true), inst(2, "b", 1, 12, false)}, ScoreAtRisk, KindDegraded},
		{"down", []*fleet.Instance{inst(1, "a", 1, 10, false), inst(2, "b", 1, 12, false)}, ScoreDown, KindDown},
		{"unplaced", []*fleet.Instance{inst(1, "a", 1, 0, true), inst(2, "b", 1, 0, true)}, ScoreUnknown, KindUnplaced},
		{"redundant", []*fleet.Instance{inst(1, "a", 1, 10, true), inst(2, "b", 1, 12, true)}, ScoreRedundant, ""},
	}
	for _, c := range cases {
		g := byName(Analyze(c.ins, groups[:1], hosts, nil), "Main network")
		if g.Score != c.score || (c.kind != "" && !kinds(g)[c.kind]) {
			t.Errorf("%s: score=%s findings=%+v", c.name, g.Score, g.Findings)
		}
	}
	// Disabled instances don't count as redundancy.
	off := inst(2, "b", 1, 12, true)
	off.Enabled = false
	if g := byName(Analyze([]*fleet.Instance{inst(1, "a", 1, 10, true), off}, groups[:1], hosts, nil), "Main network"); g.Score != ScoreSPOF {
		t.Errorf("disabled member counted: %+v", g)
	}
}

func TestSharedSiteOnlyWithSeveralSites(t *testing.T) {
	hs := []*fleet.Host{{ID: 10, Name: "a", SiteID: 1}, {ID: 11, Name: "b", SiteID: 1}}
	ins := []*fleet.Instance{inst(1, "x", 1, 10, true), inst(2, "y", 1, 11, true)}
	if g := Analyze(ins, groups[:1], hs, []*fleet.Site{{ID: 1, Name: "Home"}}).Groups[0]; len(g.Findings) != 0 {
		t.Errorf("one site should not be flagged: %+v", g.Findings)
	}
	sites := []*fleet.Site{{ID: 1, Name: "Home"}, {ID: 2, Name: "Cabin"}}
	if g := Analyze(ins, groups[:1], hs, sites).Groups[0]; !kinds(g)[KindSharedSite] || g.Score != ScoreRedundant {
		t.Errorf("shared site: %+v", g)
	}
}
