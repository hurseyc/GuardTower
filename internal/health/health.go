// Package health checks that an AdGuard Home instance is actually serving:
// its API answers and it resolves a DNS query.
package health

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/hurseyc/guardtower/internal/agh"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/fleet"
)

// ProbeZone is where DNS health probes look up a random, never-cached name,
// so every probe exercises the instance's upstream path. NXDOMAIN (or a
// block) proves the resolver is serving; SERVFAIL, REFUSED or silence don't.
const ProbeZone = "guardtower-health.example."

// DNSAddress picks the address used for DNS probes: the configured override,
// else the first listener the instance reports, else the URL host on port 53.
func DNSAddress(in *fleet.Instance, status *agh.Status, urlHost string) string {
	if a := strings.TrimSpace(in.DNSAddress); a != "" {
		return withPort(a, 53)
	}
	if status != nil {
		for _, a := range status.DNSAddresses {
			host, port := splitHostPort(a, status.DNSPort)
			ip := net.ParseIP(host)
			if ip == nil || ip.IsUnspecified() {
				continue
			}
			return net.JoinHostPort(host, strconv.Itoa(port))
		}
		if status.DNSPort > 0 {
			return net.JoinHostPort(urlHost, strconv.Itoa(status.DNSPort))
		}
	}
	return net.JoinHostPort(urlHost, "53")
}

func withPort(a string, def int) string {
	if _, _, err := net.SplitHostPort(a); err == nil {
		return a
	}
	return net.JoinHostPort(strings.Trim(a, "[]"), strconv.Itoa(def))
}

func splitHostPort(a string, def int) (string, int) {
	host, p, err := net.SplitHostPort(a)
	if err != nil {
		if def == 0 {
			def = 53
		}
		return strings.Trim(a, "[]"), def
	}
	n, _ := strconv.Atoi(p)
	return host, n
}

// Result describes a probe.
type Result struct {
	APIOK      bool   `json:"api_ok"`
	DNSOK      bool   `json:"dns_ok"`
	DNSAddress string `json:"dns_address"`
	Rcode      string `json:"rcode,omitempty"`
	LatencyMs  int64  `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
}

// Probe checks the API and DNS service, retrying DNS a few times to ride out
// the brief restart some settings changes cause.
func Probe(ctx context.Context, in *fleet.Instance, c *agh.Client) Result {
	r := Result{}
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	st, err := c.Status(sctx)
	cancel()
	if err != nil {
		r.Error = "API: " + err.Error()
		return r
	}
	if !st.Running {
		r.Error = "AdGuard Home reports its DNS server is not running"
		return r
	}
	r.APIOK = true
	r.DNSAddress = DNSAddress(in, st, c.Host())
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				r.Error = ctx.Err().Error()
				return r
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		start := time.Now()
		rcode, err := Query(ctx, r.DNSAddress)
		if err == nil {
			r.DNSOK, r.Rcode, r.LatencyMs = true, rcode, time.Since(start).Milliseconds()
			return r
		}
		lastErr = err
	}
	r.Error = fmt.Sprintf("DNS: no answer from %s: %v", r.DNSAddress, lastErr)
	return r
}

// Query sends one A query (UDP, falling back to TCP) and returns the rcode.
func Query(ctx context.Context, addr string) (string, error) {
	m := new(dns.Msg)
	m.SetQuestion("gt-"+crypto.RandomToken(6)+"."+ProbeZone, dns.TypeA)
	m.RecursionDesired = true
	for _, netw := range []string{"udp", "tcp"} {
		cl := &dns.Client{Net: netw, Timeout: 3 * time.Second}
		resp, _, err := cl.ExchangeContext(ctx, m, addr)
		if err != nil {
			continue
		}
		if resp == nil {
			return "", errors.New("empty response")
		}
		switch resp.Rcode {
		case dns.RcodeRefused:
			return "", errors.New("query refused (is GuardTower's address blocked by the instance's access settings?)")
		case dns.RcodeServerFailure:
			return "", errors.New("SERVFAIL: the instance could not reach its upstream DNS servers")
		}
		return dns.RcodeToString[resp.Rcode], nil
	}
	return "", fmt.Errorf("no response over UDP or TCP")
}
