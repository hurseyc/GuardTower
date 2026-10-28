package upgrade

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/hurseyc/guardtower/internal/agh"
)

// Driver names.
const (
	DriverAuto    = "auto"
	DriverBuiltin = "builtin"
	DriverDocker  = "docker"
	DriverSSH     = "ssh"
	DriverNone    = "none"
)

// Result is what a driver hands back after starting the new version.
type Result struct {
	// Undo puts the previous version back. Nil when the driver can't.
	Undo func(ctx context.Context) error
	// Finalize cleans up once the new version is healthy (e.g. removes the
	// old container). Nil when there is nothing to do.
	Finalize func(ctx context.Context) error
}

// Driver upgrades one AdGuard Home instance.
type Driver interface {
	Name() string
	// Upgrade installs target and returns once the new version is starting.
	// progress receives short human-readable updates. On error the Result may
	// still be non-nil with an Undo when the old version must be put back.
	Upgrade(ctx context.Context, target string, progress func(string)) (*Result, error)
}

var versionRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)

// validTarget guards against anything but a plain release tag reaching a
// shell command or an image reference.
func validTarget(v string) error {
	if !versionRe.MatchString(v) {
		return fmt.Errorf("invalid version %q (expected e.g. v0.107.79)", v)
	}
	return nil
}

// ---- built-in updater ----

// Builtin uses AdGuard Home's own updater (POST /control/update). It works
// for binary installs (LXC, bare metal) where AdGuard Home can replace its
// own executable, and always installs the latest version AdGuard Home offers.
type Builtin struct {
	C *agh.Client
}

// Name implements Driver.
func (b *Builtin) Name() string { return DriverBuiltin }

// Upgrade implements Driver.
func (b *Builtin) Upgrade(ctx context.Context, target string, progress func(string)) (*Result, error) {
	progress("asking AdGuard Home for its latest version")
	v, err := b.C.Version(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("checking for updates: %w", err)
	}
	if v.Disabled {
		return nil, errors.New("update checks are disabled on this instance (it was started with --no-check-update)")
	}
	if !v.CanAutoUpdate {
		return nil, errors.New("AdGuard Home says it cannot update itself here (common in Docker); configure the Docker or SSH driver instead")
	}
	if v.NewVersion == "" {
		return nil, errors.New("AdGuard Home reports no newer version")
	}
	if target != "" && !SameVersion(v.NewVersion, target) {
		return nil, fmt.Errorf("the built-in updater would install %s, not %s; use the Docker or SSH driver to choose a version", v.NewVersion, target)
	}
	progress("starting AdGuard Home's updater for " + v.NewVersion)
	if err := b.C.Post(ctx, "update", nil, nil); err != nil {
		// AdGuard Home may restart before the response is complete.
		if !isConnReset(err) {
			return nil, fmt.Errorf("starting the update: %w", err)
		}
	}
	return &Result{}, nil
}

func isConnReset(err error) bool {
	s := err.Error()
	return strings.Contains(s, "EOF") || strings.Contains(s, "connection reset") || strings.Contains(s, "connection refused")
}
