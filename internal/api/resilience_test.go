package api_test

import (
	"context"
	"testing"

	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/resilience"
)

func TestResilienceReportAndPreview(t *testing.T) {
	a, c := newTestServer(t)
	var res struct {
		CSRF string `json:"csrf_token"`
	}
	c.do("POST", "/api/v1/setup", map[string]string{"username": "admin", "password": "correct-horse-battery"}, &res)
	c.csrf = res.CSRF
	ctx := context.Background()
	g, _ := a.Fleet.SaveGroup(ctx, 0, "Kids VLAN", "", "")
	nas, _ := a.Fleet.SaveHost(ctx, 0, fleet.Host{Name: "synology", Kind: "nas"})
	pve, _ := a.Fleet.SaveHost(ctx, 0, fleet.Host{Name: "pve-01", Kind: "proxmox"})
	pw := "x"
	in, err := a.Fleet.CreateInstance(ctx, fleet.InstanceInput{Name: "agh-4", URL: "http://10.0.0.4", Password: &pw, GroupID: g.ID, HostID: nas.ID})
	if err != nil {
		t.Fatal(err)
	}

	// A second instance on the same host would create a shared-host SPOF…
	var pv struct{ Findings []resilience.Finding }
	if code := c.do("POST", "/api/v1/resilience/preview", resilience.Placement{Name: "agh-5", GroupID: g.ID, HostID: nas.ID}, &pv); code != 200 {
		t.Fatalf("preview: %d", code)
	}
	if len(pv.Findings) != 1 || pv.Findings[0].Kind != resilience.KindSharedHost {
		t.Fatalf("same host preview = %+v", pv.Findings)
	}
	// …on another host it fixes the single-member SPOF and adds nothing.
	c.do("POST", "/api/v1/resilience/preview", resilience.Placement{Name: "agh-5", GroupID: g.ID, HostID: pve.ID}, &pv)
	if len(pv.Findings) != 0 {
		t.Fatalf("other host preview = %+v", pv.Findings)
	}

	var rep resilience.Report
	c.do("GET", "/api/v1/resilience", nil, &rep)
	if len(rep.Groups) != 1 || rep.Groups[0].Score != resilience.ScoreDown || rep.Instances[in.ID] != resilience.Critical {
		t.Fatalf("report = %+v", rep)
	}
}
