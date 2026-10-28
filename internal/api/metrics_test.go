package api_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurseyc/guardtower/internal/app"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/store"
)

func TestMetrics(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := crypto.New(bytes.Repeat([]byte{7}, 32))
	get := func(token, auth string) (int, string) {
		a := app.NewWith(app.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), MetricsToken: token}, st, box)
		ts := httptest.NewServer(a.Handler())
		defer ts.Close()
		req, _ := http.NewRequest("GET", ts.URL+"/metrics", nil)
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, _ := get("", ""); code != 404 {
		t.Fatalf("metrics without a token configured: %d", code)
	}
	if code, _ := get("scrape-me", "wrong"); code != 401 {
		t.Fatalf("wrong token: %d", code)
	}
	fl := fleet.NewService(st, box)
	pw := "x"
	g, _ := fl.SaveGroup(ctx, 0, "Kids \"VLAN\"", "", "")
	if _, err := fl.CreateInstance(ctx, fleet.InstanceInput{Name: "agh-1", URL: "http://127.0.0.1:1", Password: &pw, GroupID: g.ID}); err != nil {
		t.Fatal(err)
	}
	code, body := get("scrape-me", "scrape-me")
	if code != 200 {
		t.Fatalf("metrics: %d %s", code, body)
	}
	for _, want := range []string{
		"# TYPE guardtower_instance_up gauge",
		`guardtower_instance_up{instance="agh-1",group="Kids \"VLAN\""} 0`,
		`guardtower_group_redundancy{group="Kids \"VLAN\"",score="down"} 1`,
		"guardtower_observe_mode 1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "family") {
		t.Error("family data must not be exported")
	}
}
