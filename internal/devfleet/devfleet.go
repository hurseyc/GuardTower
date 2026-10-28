// Package devfleet runs real AdGuard Home processes locally for development
// and integration tests. It downloads release binaries from GitHub, starts
// them on loopback ports and completes the setup wizard via the API.
package devfleet

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hurseyc/guardtower/internal/agh"
)

// Default credentials for every dev instance.
const (
	User     = "admin"
	Password = "devfleet-password"
)

// Node is a running AdGuard Home process.
type Node struct {
	Index   int
	URL     string
	DNSAddr string
	WorkDir string
	PID     int
}

// Logf receives progress messages; nil discards them.
type Logf func(format string, args ...any)

// EnsureBinary downloads (once) and caches an AdGuard Home release binary.
func EnsureBinary(ctx context.Context, dir, ver string, logf Logf) (string, error) {
	dest := filepath.Join(dir, "bin", ver, "AdGuardHome")
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	arch := runtime.GOARCH
	if arch == "arm" {
		arch = "armv7"
	}
	asset := fmt.Sprintf("AdGuardHome_%s_%s.tar.gz", runtime.GOOS, arch)
	url := "https://github.com/AdguardTeam/AdGuardHome/releases/latest/download/" + asset
	if ver != "latest" {
		url = fmt.Sprintf("https://github.com/AdguardTeam/AdGuardHome/releases/download/%s/%s", ver, asset)
	}
	if logf != nil {
		logf("downloading %s", url)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", errors.New("AdGuardHome binary not found in archive")
		}
		if err != nil {
			return "", err
		}
		if filepath.Base(h.Name) == "AdGuardHome" && h.Typeflag == tar.TypeReg {
			tmp := dest + ".tmp"
			f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755) //nolint:gosec // cache path we control.
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // trusted release archive.
				f.Close()
				return "", err
			}
			if err := f.Close(); err != nil {
				return "", err
			}
			return dest, os.Rename(tmp, dest)
		}
	}
}

// Start launches instance i (0-based) from bin with its state under dir,
// listening on webPort and dnsPort, and completes the setup wizard.
func Start(ctx context.Context, dir string, i int, bin string, webPort, dnsPort int, logf Logf) (*Node, error) {
	wd, _ := filepath.Abs(filepath.Join(dir, "agh"+strconv.Itoa(i+1)))
	if err := os.MkdirAll(wd, 0o750); err != nil {
		return nil, err
	}
	port := webPort
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	node := &Node{Index: i, URL: url, DNSAddr: fmt.Sprintf("127.0.0.1:%d", dnsPort), WorkDir: wd}
	if c, _ := agh.New(agh.Options{BaseURL: url, Username: User, Password: Password, Timeout: 2 * time.Second}); c != nil {
		if _, err := c.Status(ctx); err == nil {
			if logf != nil {
				logf("agh%d already running at %s", i+1, url)
			}
			return node, nil
		}
	}
	logFile, err := os.Create(filepath.Join(wd, "agh.log")) //nolint:gosec // path we control.
	if err != nil {
		return nil, err
	}
	defer logFile.Close()
	cmd := exec.Command(bin, //nolint:gosec // dev tool runs the downloaded AGH binary.
		"-w", wd, "-h", "127.0.0.1", "-p", strconv.Itoa(port), "--no-check-update")
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	node.PID = cmd.Process.Pid
	if err := os.WriteFile(filepath.Join(wd, "pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		return nil, err
	}
	_ = cmd.Process.Release()

	c, _ := agh.New(agh.Options{BaseURL: url, Username: User, Password: Password, Timeout: 3 * time.Second})
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := c.Status(ctx); err == nil {
			if logf != nil {
				logf("agh%d ready at %s (DNS %s)", i+1, url, node.DNSAddr)
			}
			return node, nil
		} else if strings.Contains(err.Error(), "setup wizard") {
			if err := c.InstallConfigure(ctx, "127.0.0.1", port, "127.0.0.1", dnsPort, User, Password); err != nil {
				return nil, fmt.Errorf("configure: %w", err)
			}
		}
		time.Sleep(400 * time.Millisecond)
	}
	return nil, errors.New("timed out waiting for AdGuard Home to start (see " + logFile.Name() + ")")
}

// StopAll terminates every instance started under dir.
func StopAll(dir string, logf Logf) {
	matches, _ := filepath.Glob(filepath.Join(dir, "agh*", "pid"))
	for _, m := range matches {
		b, err := os.ReadFile(m) //nolint:gosec // glob within our dir.
		if err != nil {
			continue
		}
		pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		if p, err := os.FindProcess(pid); err == nil && pid > 0 {
			_ = p.Signal(syscall.SIGTERM)
			if logf != nil {
				logf("stopped %s", filepath.Base(filepath.Dir(m)))
			}
		}
		_ = os.Remove(m)
	}
}
