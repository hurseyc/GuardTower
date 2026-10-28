package upgrade

import (
	"context"
	"strings"
	"testing"

	"github.com/hurseyc/guardtower/internal/upgrade/dockerfake"
)

const confPath = "/opt/adguardhome/conf/AdGuardHome.yaml"

func newFakeDocker(t *testing.T) (*dockerfake.Server, *dockerfake.MemRuntime, *Docker) {
	t.Helper()
	rt := &dockerfake.MemRuntime{Files: map[string][]byte{confPath: []byte("schema_version: 29\n")}, Broken: map[string]bool{}}
	srv := dockerfake.New(rt)
	t.Cleanup(srv.Close)
	srv.Add(&dockerfake.Container{
		ID: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", Name: "adguard", Image: "adguard/adguardhome:v0.107.70", Running: true,
		Config: map[string]any{
			"Hostname": "abcdef123456",
			"Env":      []string{"TZ=Europe/London"},
			"Labels":   map[string]any{"com.docker.compose.project": "dns", "org.opencontainers.image.version": "0.107.70"},
		},
		HostConfig: map[string]any{"NetworkMode": "lan", "RestartPolicy": map[string]any{"Name": "unless-stopped"},
			"Binds": []string{"/volume1/docker/adguard/conf:/opt/adguardhome/conf"}},
		Networks: map[string]map[string]any{
			"lan":  {"IPAMConfig": map[string]any{"IPv4Address": "192.168.1.53"}, "MacAddress": "02:42:c0:a8:01:35", "Aliases": []string{"abcdef123456", "dns"}},
			"mgmt": {"Aliases": []string{"adguard"}},
		},
	})
	api, err := NewDockerAPI(DockerConfig{Endpoint: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return srv, rt, &Docker{API: api, Cfg: DockerConfig{Endpoint: srv.URL, Container: "adguard", ConfPath: confPath}}
}

func TestDockerUpgradeRecreatesContainer(t *testing.T) {
	ctx := context.Background()
	srv, rt, d := newFakeDocker(t)
	var steps []string
	res, err := d.Upgrade(ctx, "v0.107.79", func(s string) { steps = append(steps, s) })
	if err != nil {
		t.Fatalf("upgrade: %v (steps %v)", err, steps)
	}
	nc := srv.Get("adguard")
	if nc == nil || nc.Image != "adguard/adguardhome:v0.107.79" || !nc.Running {
		t.Fatalf("new container = %+v", nc)
	}
	if _, ok := nc.Config["Hostname"]; ok {
		t.Error("the old container-ID hostname must not carry over")
	}
	labels, _ := nc.Config["Labels"].(map[string]any)
	if labels["com.docker.compose.project"] != "dns" || labels["org.opencontainers.image.version"] != nil {
		t.Errorf("labels = %v", labels)
	}
	if nc.HostConfig["NetworkMode"] != "lan" {
		t.Errorf("network mode = %v", nc.HostConfig["NetworkMode"])
	}
	lan := nc.Networks["lan"]
	if lan == nil || lan["MacAddress"] != "02:42:c0:a8:01:35" {
		t.Errorf("lan endpoint = %v", lan)
	}
	if ip, _ := lan["IPAMConfig"].(map[string]any); ip["IPv4Address"] != "192.168.1.53" {
		t.Errorf("fixed IP lost: %v", lan["IPAMConfig"])
	}
	if a, _ := lan["Aliases"].([]any); len(a) != 1 || a[0] != "dns" {
		t.Errorf("aliases = %v (the short-ID alias must be dropped)", lan["Aliases"])
	}
	if nc.Networks["mgmt"] == nil {
		t.Error("second network not connected")
	}
	if len(srv.List()) != 2 {
		t.Fatalf("expected the old container to be kept until finalize, have %d", len(srv.List()))
	}
	if err := res.Finalize(ctx); err != nil {
		t.Fatal(err)
	}
	if len(srv.List()) != 1 {
		t.Errorf("old container not removed: %d left", len(srv.List()))
	}
	if strings.Join(rt.Starts, ",") != "adguard/adguardhome:v0.107.79" {
		t.Errorf("starts = %v", rt.Starts)
	}
}

func TestDockerUndoRestoresOldContainerAndConfig(t *testing.T) {
	ctx := context.Background()
	srv, rt, d := newFakeDocker(t)
	res, err := d.Upgrade(ctx, "v0.107.79", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	// The new version migrates the shared config forward…
	_ = rt.WriteFile(nil, confPath, []byte("schema_version: 30\n"))
	// …and then fails its health check, so we undo.
	if err := res.Undo(ctx); err != nil {
		t.Fatal(err)
	}
	c := srv.Get("adguard")
	if c == nil || c.Image != "adguard/adguardhome:v0.107.70" || !c.Running {
		t.Fatalf("after undo: %+v", c)
	}
	if len(srv.List()) != 1 {
		t.Errorf("new container not removed: %d containers", len(srv.List()))
	}
	if got := string(rt.Files[confPath]); got != "schema_version: 29\n" {
		t.Errorf("config not restored: %q", got)
	}
}

func TestDockerStartFailureReturnsUndo(t *testing.T) {
	ctx := context.Background()
	srv, rt, d := newFakeDocker(t)
	rt.Broken["adguard/adguardhome:v0.107.79"] = true
	res, err := d.Upgrade(ctx, "v0.107.79", func(string) {})
	if err == nil || res == nil || res.Undo == nil {
		t.Fatalf("want an error with undo, got %v %v", res, err)
	}
	if err := res.Undo(ctx); err != nil {
		t.Fatal(err)
	}
	if c := srv.Get("adguard"); c == nil || !c.Running || c.Image != "adguard/adguardhome:v0.107.70" {
		t.Fatalf("old container not back: %+v", c)
	}
}

func TestDockerPullFailureChangesNothing(t *testing.T) {
	ctx := context.Background()
	srv, _, d := newFakeDocker(t)
	srv.Pullable = func(_, tag string) bool { return tag != "v9.9.9" }
	if _, err := d.Upgrade(ctx, "v9.9.9", func(string) {}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want a pull error, got %v", err)
	}
	c := srv.Get("adguard")
	if c == nil || !c.Running || len(srv.List()) != 1 {
		t.Fatalf("container changed after a failed pull: %+v", c)
	}
	for _, call := range srv.Calls {
		if strings.Contains(call, "/stop") {
			t.Errorf("container was stopped before the pull succeeded")
		}
	}
}

func TestInvalidTargetRejected(t *testing.T) {
	_, _, d := newFakeDocker(t)
	if _, err := d.Upgrade(context.Background(), "v1.0.0;rm -rf /", func(string) {}); err == nil {
		t.Fatal("expected an invalid version error")
	}
	if got := splitImageJoin("registry.lan:5000/adguard/adguardhome:v1"); got != "registry.lan:5000/adguard/adguardhome|v1" {
		t.Errorf("splitImage = %s", got)
	}
	if got := splitImageJoin("adguard/adguardhome"); got != "adguard/adguardhome|latest" {
		t.Errorf("splitImage = %s", got)
	}
}

func splitImageJoin(s string) string {
	r, t := splitImage(s)
	return r + "|" + t
}
