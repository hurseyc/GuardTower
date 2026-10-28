package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/hurseyc/guardtower/internal/agh"
	"github.com/hurseyc/guardtower/internal/devfleet"
	"github.com/hurseyc/guardtower/internal/upgrade/dockerfake"
)

// brokenTag is an image tag whose container exits immediately, to exercise
// automatic rollback.
const brokenTag = "v9.9.9-broken"

// fleetRuntime runs one dev-fleet instance as a "container".
type fleetRuntime struct {
	dir string
	i   int
}

func (r *fleetRuntime) Start(c *dockerfake.Container) error {
	_, tag, _ := strings.Cut(c.Image, ":")
	if tag == brokenTag {
		return fmt.Errorf("container %s exited immediately (exit code 1)", c.Name)
	}
	bin, err := devfleet.EnsureBinary(context.Background(), r.dir, tag, logln)
	if err != nil {
		return err
	}
	_, err = devfleet.Start(context.Background(), r.dir, r.i, bin, basePort+r.i, baseDNSPort+r.i, logln)
	return err
}

func (r *fleetRuntime) Stop(*dockerfake.Container) error {
	devfleet.Stop(r.dir, r.i)
	c, _ := agh.New(agh.Options{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", basePort+r.i), Timeout: time.Second})
	for range 50 {
		if _, err := c.Status(context.Background()); err != nil && !strings.Contains(err.Error(), "401") {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("AdGuard Home did not stop")
}

func (r *fleetRuntime) path(p string) (string, error) {
	if p != "/opt/adguardhome/conf/AdGuardHome.yaml" {
		return "", fmt.Errorf("no such file %s", p)
	}
	return filepath.Join(r.dir, fmt.Sprintf("agh%d", r.i+1), "AdGuardHome.yaml"), nil
}

func (r *fleetRuntime) ReadFile(_ *dockerfake.Container, p string) ([]byte, error) {
	f, err := r.path(p)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(f) //nolint:gosec // dev fleet dir.
}

func (r *fleetRuntime) WriteFile(_ *dockerfake.Container, p string, data []byte) error {
	f, err := r.path(p)
	if err != nil {
		return err
	}
	return os.WriteFile(f, data, 0o600)
}

// fakeDocker serves a fake Docker API that manages one dev-fleet instance as
// container "adguardhome", so the Docker upgrade method can be tried locally.
func fakeDocker(args []string) error {
	fs := flag.NewFlagSet("docker", flag.ExitOnError)
	n := fs.Int("instance", 5, "dev-fleet instance number (1-based) to manage")
	listen := fs.String("listen", "127.0.0.1:12375", "address for the fake Docker API")
	_ = fs.Parse(args)
	i := *n - 1
	c, err := agh.New(agh.Options{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", basePort+i), Username: devfleet.User, Password: devfleet.Password})
	if err != nil {
		return err
	}
	st, err := c.Status(context.Background())
	if err != nil {
		return fmt.Errorf("instance %d is not running (gtdev fleet up first): %w", *n, err)
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	srv := dockerfake.NewUnstarted(&fleetRuntime{dir: workDir(), i: i})
	srv.Listener = ln
	srv.Add(&dockerfake.Container{Name: "adguardhome", Image: "adguard/adguardhome:" + st.Version, Running: true})
	srv.Start()
	defer srv.Close()
	logln("Fake Docker API on tcp://%s managing agh%d (%s) as container \"adguardhome\".", *listen, *n, st.Version)
	logln("Image tag %s fails to start, to try automatic rollback. Ctrl-C to stop.", brokenTag)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	<-ctx.Done()
	return nil
}
