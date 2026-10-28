//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hurseyc/guardtower/internal/agh"
	"github.com/hurseyc/guardtower/internal/app"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/devfleet"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/rollout"
	"github.com/hurseyc/guardtower/internal/store"
	"github.com/hurseyc/guardtower/internal/upgrade"
	"github.com/hurseyc/guardtower/internal/upgrade/dockerfake"
)

// aghRuntime makes the fake Docker API run real AdGuard Home binaries: the
// image tag selects the release, and the "volume" is one work directory
// shared by every container, like a bind-mounted conf directory.
type aghRuntime struct {
	t        *testing.T
	dir      string
	web, dns int
}

const brokenTag = "v9.9.9-broken"

func (r *aghRuntime) Start(c *dockerfake.Container) error {
	_, tag, _ := strings.Cut(c.Image, ":")
	if tag == brokenTag {
		return fmt.Errorf("container %s exited immediately (exit code 1)", c.Name)
	}
	bin, err := devfleet.EnsureBinary(context.Background(), cacheDir(r.t), tag, r.t.Logf)
	if err != nil {
		return err
	}
	_, err = devfleet.Start(context.Background(), r.dir, 0, bin, r.web, r.dns, r.t.Logf)
	return err
}

func (r *aghRuntime) Stop(*dockerfake.Container) error {
	devfleet.StopAll(r.dir, nil)
	c, _ := agh.New(agh.Options{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", r.web), Timeout: time.Second})
	for i := 0; i < 50; i++ {
		if _, err := c.Status(context.Background()); err != nil && !strings.Contains(err.Error(), "401") {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("AdGuard Home did not stop")
}

func (r *aghRuntime) file(p string) (string, error) {
	if p != "/opt/adguardhome/conf/AdGuardHome.yaml" {
		return "", fmt.Errorf("no such file %s", p)
	}
	return filepath.Join(r.dir, "agh1", "AdGuardHome.yaml"), nil
}

func (r *aghRuntime) ReadFile(_ *dockerfake.Container, p string) ([]byte, error) {
	f, err := r.file(p)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(f) //nolint:gosec // test temp dir.
}

func (r *aghRuntime) WriteFile(_ *dockerfake.Container, p string, data []byte) error {
	f, err := r.file(p)
	if err != nil {
		return err
	}
	return os.WriteFile(f, data, 0o600)
}

// TestDockerUpgradeAndRollback covers the M5 exit criteria: an AdGuard Home
// container is upgraded through the Docker API with health gates, and a
// broken image is rolled back automatically to the previous container.
func TestDockerUpgradeAndRollback(t *testing.T) {
	const from, to = "v0.107.70", "v0.107.79"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, v := range []string{from, to} {
		if _, err := devfleet.EnsureBinary(ctx, cacheDir(t), v, t.Logf); err != nil {
			t.Fatalf("download %s: %v", v, err)
		}
	}
	rt := &aghRuntime{t: t, dir: t.TempDir(), web: 23051, dns: 25351}
	t.Cleanup(func() { devfleet.StopAll(rt.dir, nil); time.Sleep(200 * time.Millisecond) })
	docker := dockerfake.New(rt)
	defer docker.Close()
	ctr := &dockerfake.Container{Name: "adguard", Image: "adguard/adguardhome:" + from}
	docker.Add(ctr)
	if err := rt.Start(ctr); err != nil {
		t.Fatal(err)
	}
	ctr.Running = true

	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := crypto.New(make([]byte, 32))
	a := app.NewWith(app.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, st, box)
	a.Rollout.SetRoot(ctx)
	a.Upgrade.WaitTimeout, a.Upgrade.PollEvery = 60*time.Second, 500*time.Millisecond
	pw := devfleet.Password
	in, err := a.Fleet.CreateInstance(ctx, fleet.InstanceInput{Name: "nas-agh", URL: fmt.Sprintf("http://127.0.0.1:%d", rt.web),
		Username: devfleet.User, Password: &pw, DNSAddress: fmt.Sprintf("127.0.0.1:%d", rt.dns)})
	if err != nil {
		t.Fatal(err)
	}
	refresh := func() *fleet.Instance {
		cur, _ := a.Fleet.GetInstance(ctx, in.ID)
		_ = a.Fleet.SaveStatus(ctx, in.ID, a.Poller.Probe(ctx, cur))
		cur, _ = a.Fleet.GetInstance(ctx, in.ID)
		return cur
	}
	in = refresh()
	if !upgrade.SameVersion(in.Status.Version, from) {
		t.Fatalf("started on %s, want %s", in.Status.Version, from)
	}
	if _, err := a.Upgrade.SaveConfig(ctx, in.ID, upgrade.ConfigInput{Config: upgrade.Config{Driver: upgrade.DriverDocker,
		Docker: upgrade.DockerConfig{Endpoint: docker.URL, Container: "adguard"}}}); err != nil {
		t.Fatal(err)
	}
	res, err := a.Upgrade.Test(ctx, in.ID, upgrade.DriverDocker, upgrade.ConfigInput{Config: upgrade.Config{Docker: upgrade.DockerConfig{Endpoint: docker.URL, Container: "adguard"}}})
	if err != nil || !res.OK {
		t.Fatalf("docker test: %+v %v", res, err)
	}
	target := rollout.Target{Kind: "instance", ID: in.ID}

	// Observe mode refuses.
	if _, err := a.Upgrade.Start(ctx, target, to, "test"); err == nil {
		t.Fatal("expected observe-mode refusal")
	}
	_ = st.SetSetting(ctx, rollout.SettingObserve, "false")

	// 1. A broken image: the new container never comes up, the old one is restored.
	id, err := a.Upgrade.Start(ctx, target, brokenTag, "test")
	if err != nil {
		t.Fatal(err)
	}
	r := waitRollout(t, a, id)
	if r.Status != "rolled_back" || r.Steps[0].Status != "rolled_back" {
		t.Fatalf("broken image should roll back: %s / %s: %s", r.Status, r.Steps[0].Status, r.Steps[0].Detail)
	}
	if in = refresh(); !in.Status.Online || !upgrade.SameVersion(in.Status.Version, from) {
		t.Fatalf("after rollback: online=%v version=%s", in.Status.Online, in.Status.Version)
	}
	if n := len(docker.List()); n != 1 {
		t.Fatalf("containers after rollback = %d", n)
	}

	// 2. A real upgrade.
	plan, err := a.Upgrade.MakePlan(ctx, target, to)
	if err != nil || plan.Total != 1 || plan.Items[0].From != from || plan.Items[0].To != to {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	id, err = a.Upgrade.Start(ctx, target, to, "test")
	if err != nil {
		t.Fatal(err)
	}
	r = waitRollout(t, a, id)
	if r.Status != "succeeded" {
		t.Fatalf("upgrade: %s / %s", r.Status, r.Steps[0].Detail)
	}
	if in = refresh(); !upgrade.SameVersion(in.Status.Version, to) {
		t.Fatalf("version after upgrade = %s", in.Status.Version)
	}
	cs := docker.List()
	if len(cs) != 1 || cs[0].Name != "adguard" || cs[0].Image != "adguard/adguardhome:"+to {
		t.Fatalf("containers after upgrade: %+v", cs)
	}

	// 3. Up to date now: nothing to plan.
	if plan, _ := a.Upgrade.MakePlan(ctx, target, to); plan.Total != 0 {
		t.Fatalf("expected nothing to upgrade, got %+v", plan)
	}
}
