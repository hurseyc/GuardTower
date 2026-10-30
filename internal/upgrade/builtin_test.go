package upgrade

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurseyc/guardtower/internal/agh"
	"github.com/hurseyc/guardtower/internal/fleet"
)

func fakeAGH(t *testing.T, info agh.VersionInfo) (*agh.Client, *int) {
	t.Helper()
	updates := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/control/version.json":
			_ = json.NewEncoder(w).Encode(info)
		case "/control/update":
			updates++
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := agh.New(agh.Options{BaseURL: srv.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	return c, &updates
}

func TestBuiltinUpgrade(t *testing.T) {
	ctx := context.Background()
	c, n := fakeAGH(t, agh.VersionInfo{NewVersion: "v0.107.79", CanAutoUpdate: true})
	res, err := (&Builtin{C: c}).Upgrade(ctx, "v0.107.79", func(string) {})
	if err != nil || *n != 1 {
		t.Fatalf("upgrade: %v, update calls %d", err, *n)
	}
	if res.Undo != nil {
		t.Error("the built-in updater cannot undo")
	}
	if _, err := (&Builtin{C: c}).Upgrade(ctx, "v0.107.75", func(string) {}); err == nil || !strings.Contains(err.Error(), "would install v0.107.79") {
		t.Errorf("want a version mismatch error, got %v", err)
	}
	c2, n2 := fakeAGH(t, agh.VersionInfo{NewVersion: "v0.107.79", CanAutoUpdate: false})
	if _, err := (&Builtin{C: c2}).Upgrade(ctx, "", func(string) {}); err == nil || *n2 != 0 {
		t.Errorf("docker-style instance must refuse: %v", err)
	}
}

func TestEffectiveDriver(t *testing.T) {
	c := defaultConfig(1)
	if d, why := effective(c, checkRow{known: true}); d != "" || why == "" {
		t.Errorf("no updater, nothing configured: %q %q", d, why)
	}
	// AdGuard Home hasn't said yet (it only does while an update is out).
	if d, why := effective(c, checkRow{}); d != "" || why != "" {
		t.Errorf("not yet known should be neither usable nor blocked: %q %q", d, why)
	}
	if d, _ := effective(c, checkRow{canAuto: true}); d != DriverBuiltin {
		t.Errorf("auto with updater = %q", d)
	}
	c.SSH.Host, c.SSH.HasKey = "lxc", true
	if d, why := effective(c, checkRow{canAuto: true}); d != "" || !strings.Contains(why, "pin") {
		t.Errorf("unpinned ssh should block auto: %q %q", d, why)
	}
	c.SSH.HostKey = "x"
	if d, _ := effective(c, checkRow{canAuto: true}); d != DriverSSH {
		t.Errorf("pinned ssh = %q", d)
	}
	c.Docker.Endpoint, c.Docker.Container = "tcp://nas:2375", "adguard"
	if d, _ := effective(c, checkRow{}); d != DriverDocker {
		t.Errorf("docker = %q", d)
	}
	c.Driver = DriverNone
	if d, _ := effective(c, checkRow{}); d != "" {
		t.Errorf("none = %q", d)
	}
}

func TestStateUpToDateWithoutUpdaterAnswer(t *testing.T) {
	s := &Service{}
	in := &fleet.Instance{ID: 1, Name: "agh", Status: &fleet.Status{Online: true, Version: "v0.107.79"}}
	st := s.state(in, defaultConfig(1), checkRow{}, "v0.107.79", "")
	if !st.UpToDate || st.Blocked != "" || !st.DetectPending || st.UpdateAvailable {
		t.Errorf("on latest with no updater answer: %+v", st)
	}
	in.Status.Version = "v0.107.71"
	st = s.state(in, defaultConfig(1), checkRow{}, "v0.107.79", "")
	if st.UpToDate || st.Blocked == "" || st.UpdateAvailable {
		t.Errorf("behind, AdGuard Home not offering yet: %+v", st)
	}
	st = s.state(in, defaultConfig(1), checkRow{canAuto: true, known: true, builtin: "v0.107.79"}, "v0.107.79", "")
	if !st.UpdateAvailable || st.Driver != DriverBuiltin || st.Target != "v0.107.79" {
		t.Errorf("updater offering the update: %+v", st)
	}
}
