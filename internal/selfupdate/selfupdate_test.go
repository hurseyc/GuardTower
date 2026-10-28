package selfupdate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"v1.2.0", "v1.1.9", true}, {"v1.2.0", "v1.2.0", false}, {"v1.10.0", "v1.9.0", true}, {"v2.0.0", "dev", false}, {"v2.0.0", "7056256-dirty", false}, {"v1.0.1", "v1.0.0-rc.1", true}} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestCheckAPIAndFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			_, _ = w.Write([]byte(`{"tag_name":"v9.0.0","html_url":"https://example/v9"}`))
		case "/limited":
			w.WriteHeader(http.StatusForbidden)
		case "/latest":
			http.Redirect(w, r, "https://github.com/hurseyc/GuardTower/releases/tag/v9.1.0", http.StatusFound)
		}
	}))
	defer srv.Close()
	c := New(nil)
	c.st.Current = "v1.0.0"
	c.APIURL, c.LatestURL = srv.URL+"/api", srv.URL+"/latest"
	if err := c.Check(context.Background()); err != nil || !c.Status().UpdateAvailable || c.Status().Latest != "v9.0.0" {
		t.Fatalf("api: %+v %v", c.Status(), err)
	}
	c.APIURL = srv.URL + "/limited"
	if err := c.Check(context.Background()); err != nil || c.Status().Latest != "v9.1.0" {
		t.Fatalf("fallback: %+v %v", c.Status(), err)
	}
	off := New(func(context.Context) bool { return false })
	off.APIURL = srv.URL + "/api"
	_ = off.Check(context.Background())
	if off.Status().CheckedAt != 0 {
		t.Fatal("checks ran while disabled")
	}
}

func TestSelfUpdateHelper(t *testing.T) {
	dir := t.TempDir()
	unit := filepath.Join(dir, "guardtower-update.path")
	c := New(nil)
	c.UpdateDir = filepath.Join(dir, "update")
	c.HelperUnit = unit
	c.st.Method = MethodDocker
	if err := c.RequestUpdate(); !errors.Is(err, ErrNoSelfUpdate) {
		t.Fatalf("docker install could request an update: %v", err)
	}
	c.st.Method = MethodSystemd
	if c.Status().CanSelfUpdate {
		t.Fatal("no helper installed yet")
	}
	if err := os.WriteFile(unit, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(c.UpdateDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if st := c.Status(); !st.CanSelfUpdate || st.Updating || st.Last != nil {
		t.Fatalf("fresh helper: %+v", st)
	}
	if err := c.RequestUpdate(); err != nil {
		t.Fatal(err)
	}
	if !c.Status().Updating {
		t.Fatal("not updating after a request")
	}
	// What the helper does: take the request, run, report.
	_ = os.Remove(filepath.Join(c.UpdateDir, "request"))
	_ = os.WriteFile(filepath.Join(c.UpdateDir, "started"), []byte("1"), 0o644)
	if !c.Status().Updating {
		t.Fatal("not updating while the helper runs")
	}
	later := time.Now().Add(time.Second)
	_ = os.WriteFile(filepath.Join(c.UpdateDir, "last.log"), []byte("\x1b[1;32m==>\x1b[0m GuardTower upgraded.\n"), 0o644)
	_ = os.WriteFile(filepath.Join(c.UpdateDir, "last.exit"), []byte("0\n"), 0o644)
	_ = os.Chtimes(filepath.Join(c.UpdateDir, "last.exit"), later, later)
	st := c.Status()
	if st.Updating || st.Last == nil || st.Last.ExitCode != 0 || st.Last.Log != "==> GuardTower upgraded.\n" {
		t.Fatalf("after the helper: %+v %+v", st, st.Last)
	}
	// A request nobody picks up is reported, not shown as "updating" forever.
	_ = os.WriteFile(filepath.Join(c.UpdateDir, "request"), nil, 0o600)
	old := time.Now().Add(-5 * time.Minute)
	_ = os.Chtimes(filepath.Join(c.UpdateDir, "request"), old, old)
	if st := c.Status(); st.Updating || !strings.Contains(st.Error, "didn't start") {
		t.Fatalf("stuck request: %+v", st)
	}
}
