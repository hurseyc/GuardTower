package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
