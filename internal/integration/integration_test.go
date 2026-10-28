//go:build integration

// Package integration runs GuardTower against real AdGuard Home processes.
//
//	go test -tags integration ./internal/integration/...
//
// AGH binaries are cached in $GT_AGH_CACHE (default: the user cache dir).
// Set GT_AGH_VERSIONS to a comma-separated list to test several releases.
package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hurseyc/guardtower/internal/agh"
	"github.com/hurseyc/guardtower/internal/devfleet"
)

func versions() []string {
	if v := os.Getenv("GT_AGH_VERSIONS"); v != "" {
		return strings.Split(v, ",")
	}
	return []string{"latest", "v0.107.70"}
}

func cacheDir(t *testing.T) string {
	if d := os.Getenv("GT_AGH_CACHE"); d != "" {
		return d
	}
	d, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(d, "guardtower-agh")
}

// startNode starts one fresh AGH instance for the test and stops it afterwards.
func startNode(t *testing.T, ver string, slot int) (*devfleet.Node, *agh.Client) {
	t.Helper()
	ctx := context.Background()
	bin, err := devfleet.EnsureBinary(ctx, cacheDir(t), ver, t.Logf)
	if err != nil {
		t.Fatalf("download AdGuard Home %s: %v", ver, err)
	}
	dir := t.TempDir()
	n, err := devfleet.Start(ctx, dir, 0, bin, 23001+slot, 25301+slot, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { devfleet.StopAll(dir, nil); time.Sleep(200 * time.Millisecond) })
	c, err := agh.New(agh.Options{BaseURL: n.URL, Username: devfleet.User, Password: devfleet.Password})
	if err != nil {
		t.Fatal(err)
	}
	return n, c
}

func TestClientAgainstRealAdGuardHome(t *testing.T) {
	for slot, ver := range versions() {
		t.Run(ver, func(t *testing.T) {
			_, c := startNode(t, ver, slot)
			ctx := context.Background()

			st, err := c.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !st.Running || !strings.HasPrefix(st.Version, "v0.107") {
				t.Fatalf("unexpected status %+v", st)
			}

			if _, err := c.Stats(ctx); err != nil {
				t.Fatalf("stats: %v", err)
			}
			if _, err := c.QueryLog(ctx, "", 10); err != nil {
				t.Fatalf("querylog: %v", err)
			}
			if _, err := c.FilteringStatus(ctx); err != nil {
				t.Fatalf("filtering: %v", err)
			}
			v, err := c.Version(ctx, false)
			if err != nil {
				t.Fatalf("version.json: %v", err)
			}
			t.Logf("can_autoupdate=%v", v.CanAutoUpdate)

			// Protection toggles round-trip.
			if err := c.SetProtection(ctx, false, time.Minute); err != nil {
				t.Fatal(err)
			}
			if st, _ := c.Status(ctx); st.ProtectionEnabled {
				t.Fatal("protection still enabled after pause")
			}
			if err := c.SetProtection(ctx, true, 0); err != nil {
				t.Fatal(err)
			}
			if st, _ := c.Status(ctx); !st.ProtectionEnabled {
				t.Fatal("protection not re-enabled")
			}

			// Wrong credentials surface as an auth error.
			bad, _ := agh.New(agh.Options{BaseURL: c.BaseURL(), Username: devfleet.User, Password: "wrong"})
			if _, err := bad.Status(ctx); !agh.IsAuthError(err) {
				t.Fatalf("expected auth error, got %v", err)
			}
		})
	}
}
