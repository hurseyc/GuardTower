// Command gtdev is a developer tool: it runs a local fleet of real AdGuard
// Home processes (no Docker needed), seeds GuardTower with them and generates
// DNS traffic so every screen has realistic data.
//
//	go run ./cmd/gtdev fleet up      # start 5 AGH instances on 127.0.0.1
//	go run ./cmd/gtdev seed          # register them with a running GuardTower
//	go run ./cmd/gtdev traffic       # send a realistic query mix
//	go run ./cmd/gtdev docker        # fake Docker API for trying Docker upgrades
//	go run ./cmd/gtdev fleet down
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/miekg/dns"

	"github.com/hurseyc/guardtower/internal/devfleet"
)

const (
	basePort    = 13001
	baseDNSPort = 15301
)

func logln(format string, args ...any) { fmt.Printf(format+"\n", args...) }

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "fleet":
		if len(os.Args) < 3 {
			usage()
		}
		switch os.Args[2] {
		case "up":
			err = fleetUp(os.Args[3:])
		case "down":
			devfleet.StopAll(workDir(), logln)
		default:
			usage()
		}
	case "seed":
		err = seed(os.Args[2:])
	case "traffic":
		err = traffic(os.Args[2:])
	case "docker":
		err = fakeDocker(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtdev fleet up [-n 5] [-version v0.107.79] | fleet down | seed | traffic [-duration 30s] | docker [-instance 5]")
	os.Exit(2)
}

func workDir() string {
	if d := os.Getenv("GTDEV_DIR"); d != "" {
		return d
	}
	return filepath.Join(".", "tmp", "devfleet")
}

func fleetUp(args []string) error {
	fs := flag.NewFlagSet("fleet up", flag.ExitOnError)
	n := fs.Int("n", 5, "number of instances")
	ver := fs.String("version", "latest", "AdGuard Home release tag, e.g. v0.107.79")
	oldest := fs.String("old-version", "", "run the last instance on this older version (to exercise upgrades)")
	_ = fs.Parse(args)

	dir := workDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	bin, err := devfleet.EnsureBinary(context.Background(), dir, *ver, logln)
	if err != nil {
		return err
	}
	oldBin := bin
	if *oldest != "" {
		if oldBin, err = devfleet.EnsureBinary(context.Background(), dir, *oldest, logln); err != nil {
			return err
		}
	}
	ctx := context.Background()
	for i := 0; i < *n; i++ {
		b := bin
		if i == *n-1 && *oldest != "" {
			b = oldBin
		}
		if _, err := devfleet.Start(ctx, dir, i, b, basePort+i, baseDNSPort+i, logln); err != nil {
			return fmt.Errorf("instance %d: %w", i+1, err)
		}
	}
	fmt.Printf("Fleet is up. Credentials for every instance: %s / %s\n", devfleet.User, devfleet.Password)
	return nil
}

// seed registers the dev fleet with a running GuardTower as a 3+2 split
// across three hosts, with one deliberate single point of failure.
func seed(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	gt := fs.String("url", "http://127.0.0.1:8080", "GuardTower URL")
	user := fs.String("user", "admin", "GuardTower admin username")
	pass := fs.String("pass", "devfleet-admin-pw", "GuardTower admin password (created if setup is pending)")
	n := fs.Int("n", 5, "number of instances")
	_ = fs.Parse(args)
	c := newGTClient(*gt)
	if err := c.login(*user, *pass); err != nil {
		return err
	}
	hostA, _ := c.create("/hosts", map[string]any{"name": "pve-01", "kind": "proxmox", "address": "192.168.10.11"})
	hostB, _ := c.create("/hosts", map[string]any{"name": "pve-02", "kind": "proxmox", "address": "192.168.10.12"})
	hostC, _ := c.create("/hosts", map[string]any{"name": "synology", "kind": "nas", "address": "192.168.10.20"})
	gMain, _ := c.create("/groups", map[string]any{"name": "Main network", "description": "Adults and infrastructure", "color": "#34d399"})
	gKids, _ := c.create("/groups", map[string]any{"name": "Kids VLAN", "description": "Strict family filtering", "color": "#a78bfa"})
	type spec struct {
		group, host int64
		runtime     string
		tags        []string
		canary      bool
	}
	specs := []spec{
		{gMain, hostA, "lxc", []string{"lxc", "primary"}, true},
		{gMain, hostB, "lxc", []string{"lxc"}, false},
		{gMain, hostC, "docker", []string{"nas", "docker"}, false},
		{gKids, hostC, "docker", []string{"nas", "docker"}, true},
		{gKids, hostC, "docker", []string{"nas", "docker"}, false}, // same host as previous: SPOF.
	}
	for i := 0; i < *n && i < len(specs); i++ {
		s := specs[i]
		_, err := c.create("/instances", map[string]any{
			"name": fmt.Sprintf("agh-%d", i+1), "url": fmt.Sprintf("http://127.0.0.1:%d", basePort+i),
			"username": devfleet.User, "password": devfleet.Password, "group_id": s.group, "host_id": s.host,
			"runtime": s.runtime, "tags": s.tags, "is_canary": s.canary,
			"dns_address": fmt.Sprintf("127.0.0.1:%d", baseDNSPort+i),
		})
		if err != nil {
			fmt.Println("instance", i+1, ":", err)
		}
	}
	fmt.Println("seeded GuardTower at", *gt)
	return nil
}

var domains = []string{
	"google.com", "youtube.com", "github.com", "wikipedia.org", "apple.com", "icloud.com", "netflix.com", "spotify.com",
	"amazon.com", "microsoft.com", "office.com", "reddit.com", "discord.com", "steamcommunity.com", "roblox.com",
	"instagram.com", "tiktok.com", "facebook.com", "whatsapp.net", "cloudflare.com", "debian.org", "proxmox.com",
	"doubleclick.net", "googlesyndication.com", "google-analytics.com", "ads.yahoo.com", "adservice.google.com",
	"app-measurement.com", "graph.facebook.com", "pixel.facebook.com", "telemetry.microsoft.com", "scorecardresearch.com",
	"pornhub.com", "xvideos.com", "onlyfans.com", "bet365.com", "pokerstars.com", "dns.google", "mozilla.cloudflare-dns.com",
	"nordvpn.com", "torproject.org",
}

func traffic(args []string) error {
	fs := flag.NewFlagSet("traffic", flag.ExitOnError)
	n := fs.Int("n", 5, "number of instances")
	dur := fs.Duration("duration", 20*time.Second, "how long to send queries")
	qps := fs.Int("qps", 25, "queries per second across the fleet")
	_ = fs.Parse(args)
	cl := &dns.Client{Timeout: 2 * time.Second}
	end := time.Now().Add(*dur)
	tick := time.NewTicker(time.Second / time.Duration(*qps))
	defer tick.Stop()
	sent := 0
	for time.Now().Before(end) {
		<-tick.C
		// Instances get unequal load so the charts have something to show.
		i := int(rand.ExpFloat64()*1.2) % *n
		d := domains[rand.IntN(len(domains))]
		m := new(dns.Msg)
		qt := dns.TypeA
		if rand.IntN(4) == 0 {
			qt = dns.TypeAAAA
		}
		m.SetQuestion(dns.Fqdn(d), qt)
		go func(addr string) { _, _, _ = cl.Exchange(m, addr) }(fmt.Sprintf("127.0.0.1:%d", baseDNSPort+i))
		sent++
	}
	fmt.Println("sent", sent, "queries")
	return nil
}
