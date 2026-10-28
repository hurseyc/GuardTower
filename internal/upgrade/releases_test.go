package upgrade

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurseyc/guardtower/internal/store"
)

func TestReleasesRefreshUsesETag(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	calls, notModified := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("If-None-Match") == `"v1"` {
			notModified++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(`[
		 {"tag_name":"v0.108.0-b.3","name":"beta","published_at":"2026-10-01T00:00:00Z","prerelease":true},
		 {"tag_name":"v0.107.79","name":"v0.107.79","published_at":"2026-09-20T00:00:00Z","html_url":"https://example/79","body":"notes"},
		 {"tag_name":"v0.107.70","published_at":"2026-01-02T00:00:00Z"},
		 {"tag_name":"draft","draft":true,"published_at":"2026-01-02T00:00:00Z"}]`))
	}))
	defer srv.Close()
	r := NewReleases(st)
	r.URL, r.LatestURL = srv.URL, ""
	for i := 0; i < 2; i++ {
		if err := r.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 || notModified != 1 {
		t.Errorf("calls=%d notModified=%d", calls, notModified)
	}
	l, err := r.Latest(ctx)
	if err != nil || l.Tag != "v0.107.79" || l.Notes != "notes" {
		t.Fatalf("latest = %+v %v", l, err)
	}
	all, _ := r.List(ctx, true)
	if len(all) != 3 || all[0].Tag != "v0.108.0-b.3" {
		t.Errorf("with prereleases = %+v", all)
	}
}

func TestReleasesFallBackToLatestRedirect(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			http.Redirect(w, r, "https://github.com/AdguardTeam/AdGuardHome/releases/tag/v0.107.80", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer srv.Close()
	r := NewReleases(st)
	r.URL, r.LatestURL = srv.URL+"/api", srv.URL+"/latest"
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if l, err := r.Latest(ctx); err != nil || l.Tag != "v0.107.80" {
		t.Fatalf("latest = %+v %v", l, err)
	}
	r.LatestURL = ""
	if err := r.Refresh(ctx); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("want the API message in the error, got %v", err)
	}
}
