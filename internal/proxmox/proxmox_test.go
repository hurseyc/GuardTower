package proxmox

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/store"
)

type fakePVE struct {
	mu    sync.Mutex
	nodes map[int64]string // vmid → node
}

func (f *fakePVE) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "PVEAPIToken=gt@pve!ro=s3cret" {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var data any
	switch p := strings.TrimPrefix(r.URL.Path, "/api2/json/"); {
	case p == "version":
		data = map[string]string{"version": "8.4.1"}
	case p == "cluster/resources":
		data = []map[string]any{
			{"vmid": 101, "name": "agh-1", "node": f.nodes[101], "type": "lxc", "status": "running"},
			{"vmid": 102, "name": "dns-two", "node": f.nodes[102], "type": "lxc", "status": "running"},
			{"vmid": 201, "name": "agh-3", "node": f.nodes[201], "type": "qemu", "status": "running"},
			{"vmid": 900, "name": "tmpl", "node": "pve-01", "type": "lxc", "status": "stopped", "template": 1},
		}
	case p == "cluster/status":
		data = []map[string]any{{"type": "cluster", "name": "home"}, {"type": "node", "name": "pve-01", "ip": "10.0.0.11"}, {"type": "node", "name": "pve-02", "ip": "10.0.0.12"}}
	case strings.HasSuffix(p, "/lxc/101/interfaces"):
		data = []map[string]any{{"name": "lo", "inet": "127.0.0.1/8"}, {"name": "eth0", "inet": "192.168.1.53/24"}}
	case strings.HasSuffix(p, "/lxc/102/interfaces"):
		http.Error(w, "forbidden", http.StatusForbidden) // falls back to the static config
		return
	case strings.HasSuffix(p, "/lxc/102/config"):
		data = map[string]any{"hostname": "dns-two", "net0": "name=eth0,bridge=vmbr0,ip=192.168.1.54/24,gw=192.168.1.1"}
	case strings.HasSuffix(p, "/qemu/201/agent/network-get-interfaces"):
		http.Error(w, "no agent", http.StatusInternalServerError) // matched by name instead
		return
	default:
		http.NotFound(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func TestSyncPlacesInstances(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := crypto.New(bytes.Repeat([]byte{7}, 32))
	fl := fleet.NewService(st, box)
	pve := &fakePVE{nodes: map[int64]string{101: "pve-01", 102: "pve-01", 201: "pve-02"}}
	ts := httptest.NewServer(pve)
	defer ts.Close()

	pw := "x"
	nas, _ := fl.SaveHost(ctx, 0, fleet.Host{Name: "synology", Kind: "nas"})
	mk := func(name, url string, host int64) *fleet.Instance {
		in, err := fl.CreateInstance(ctx, fleet.InstanceInput{Name: name, URL: url, Password: &pw, HostID: host})
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	a := mk("agh-1", "http://192.168.1.53:3000", nas.ID) // wrongly placed by hand
	b := mk("agh-2", "http://dns-two.lan", 0)            // found through DNS
	c := mk("agh-3", "https://10.9.9.9", 0)              // found by guest name
	d := mk("agh-4", "http://192.168.7.7", nas.ID)       // not on Proxmox: left alone

	s := New(st, box, fl, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Resolve = func(_ context.Context, host string) ([]string, error) {
		if host == "dns-two.lan" {
			return []string{"192.168.1.54"}, nil
		}
		return nil, &net404{}
	}
	secret := "s3cret"
	in := ClusterInput{Name: "home", URL: ts.URL, TokenID: "gt@pve!ro", Secret: &secret}
	if v, n, err := s.Test(ctx, 0, in); err != nil || v != "8.4.1" || n != 4 {
		t.Fatalf("test: %q %d %v", v, n, err)
	}
	bad := "wrong"
	if _, _, err := s.Test(ctx, 0, ClusterInput{Name: "home", URL: ts.URL, TokenID: "gt@pve!ro", Secret: &bad}); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("bad secret: %v", err)
	}
	cl, err := s.Save(ctx, 0, in)
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.Sync(ctx, cl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Guests != 3 || res.Matched != 3 || len(res.Moves) != 3 {
		t.Fatalf("sync: %+v", res)
	}
	place := func(in *fleet.Instance) (string, string) {
		got, _ := fl.GetInstance(ctx, in.ID)
		hs, _ := fl.ListHosts(ctx)
		for _, h := range hs {
			if h.ID == got.HostID {
				return h.Name, got.Runtime
			}
		}
		return "", got.Runtime
	}
	for _, want := range []struct {
		in            *fleet.Instance
		host, runtime string
	}{{a, "pve-01", "lxc"}, {b, "pve-01", "lxc"}, {c, "pve-02", "vm"}, {d, "synology", "unknown"}} {
		if h, rt := place(want.in); h != want.host || rt != want.runtime {
			t.Fatalf("%s placed on %s/%s, want %s/%s", want.in.Name, h, rt, want.host, want.runtime)
		}
	}
	hs, _ := fl.ListHosts(ctx)
	for _, h := range hs {
		if h.Name == "pve-02" && (h.Kind != "proxmox" || h.Address != "10.0.0.12") {
			t.Fatalf("created host: %+v", h)
		}
	}

	// A live migration moves the instance on the next sync; nothing else changes.
	pve.mu.Lock()
	pve.nodes[102] = "pve-02"
	pve.mu.Unlock()
	if res, err = s.Sync(ctx, cl.ID); err != nil || len(res.Moves) != 1 || res.Moves[0].Instance != "agh-2" || res.Moves[0].To != "pve-02" {
		t.Fatalf("after migration: %+v %v", res, err)
	}
	gs, _ := s.Guests(ctx)
	if len(gs) != 3 || gs[0].InstanceName == "" {
		t.Fatalf("guests: %+v", gs)
	}
	audit, _ := st.ListAudit(ctx, 10, 0)
	if len(audit) != 4 || audit[0].Action != "proxmox.placement" {
		t.Fatalf("audit: %+v", audit)
	}
}

type net404 struct{}

func (*net404) Error() string { return "no such host" }
