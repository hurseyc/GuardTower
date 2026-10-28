// Command guardtower runs the GuardTower server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/hurseyc/guardtower"
	"github.com/hurseyc/guardtower/internal/app"
	"github.com/hurseyc/guardtower/internal/auth"
	"github.com/hurseyc/guardtower/internal/store"
	"github.com/hurseyc/guardtower/internal/version"
)

func main() {
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "version":
		fmt.Printf("GuardTower %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date)
	case "healthcheck":
		err = healthcheck(args)
	case "reset-password":
		err = resetPassword(args)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`GuardTower – fleet manager for AdGuard Home

Usage:
  guardtower [serve] [flags]        run the server (default)
  guardtower version                print version
  guardtower healthcheck            exit 0 if the local server is healthy
  guardtower reset-password USER    set a new password for USER (offline)

Run "guardtower serve -h" for server flags. Every flag can also be set with an
environment variable, shown in brackets.
`)
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func defaultDataDir() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "/data"
	}
	if os.Geteuid() == 0 {
		return "/var/lib/guardtower"
	}
	return "./data"
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := fs.String("listen", env("GT_LISTEN", ":8080"), "listen address [GT_LISTEN]")
	dataDir := fs.String("data-dir", env("GT_DATA_DIR", defaultDataDir()), "data directory [GT_DATA_DIR]")
	logLevel := fs.String("log-level", env("GT_LOG_LEVEL", "info"), "debug, info, warn or error [GT_LOG_LEVEL]")
	logJSON := fs.Bool("log-json", envBool("GT_LOG_JSON", false), "log as JSON [GT_LOG_JSON]")
	proxies := fs.String("trusted-proxies", env("GT_TRUSTED_PROXIES", ""), "comma-separated CIDRs of reverse proxies whose X-Forwarded-For is trusted [GT_TRUSTED_PROXIES]")
	secure := fs.Bool("secure-cookies", envBool("GT_SECURE_COOKIES", false), "always mark cookies Secure (set when behind HTTPS) [GT_SECURE_COOKIES]")
	tlsCert := fs.String("tls-cert", env("GT_TLS_CERT", ""), "TLS certificate file to serve HTTPS directly [GT_TLS_CERT]")
	tlsKey := fs.String("tls-key", env("GT_TLS_KEY", ""), "TLS private key file [GT_TLS_KEY]")
	_ = fs.Parse(args)

	log := newLogger(*logLevel, *logJSON)
	cidrs, err := parseCIDRs(*proxies)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*dataDir, 0o750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.New(ctx, app.Config{
		DataDir:       *dataDir,
		Log:           log,
		TrustedProxy:  cidrs,
		SecureCookies: *secure || *tlsCert != "",
		WebFS:         guardtower.WebFS(),
		DocsFS:        guardtower.DocsFS(),
	})
	if err != nil {
		return err
	}
	defer a.Close()
	a.Start(ctx)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("GuardTower listening", "addr", *listen, "version", version.Version, "data_dir", *dataDir, "https", *tlsCert != "")
		if *tlsCert != "" {
			errCh <- srv.ListenAndServeTLS(*tlsCert, *tlsKey)
		} else {
			errCh <- srv.ListenAndServe()
		}
	}()
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutCtx)
}

func newLogger(level string, asJSON bool) *slog.Logger {
	var lv slog.Level
	_ = lv.UnmarshalText([]byte(level))
	opts := &slog.HandlerOptions{Level: lv}
	if asJSON {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func parseCIDRs(s string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			if strings.Contains(part, ":") {
				part += "/128"
			} else {
				part += "/32"
			}
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy %q: %w", part, err)
		}
		out = append(out, n)
	}
	return out, nil
}

func healthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ExitOnError)
	listen := fs.String("listen", env("GT_LISTEN", ":8080"), "server listen address [GT_LISTEN]")
	_ = fs.Parse(args)
	host, port, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	scheme := "http"
	if env("GT_TLS_CERT", "") != "" {
		scheme = "https"
	}
	c := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, scheme+"://"+net.JoinHostPort(host, port)+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("unhealthy: HTTP %d", resp.StatusCode)
	}
	return nil
}

func resetPassword(args []string) error {
	fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
	dataDir := fs.String("data-dir", env("GT_DATA_DIR", defaultDataDir()), "data directory [GT_DATA_DIR]")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: guardtower reset-password [-data-dir DIR] USERNAME")
	}
	user := fs.Arg(0)
	pw := os.Getenv("GT_NEW_PASSWORD")
	if pw == "" {
		fmt.Print("New password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("read password (or set GT_NEW_PASSWORD): %w", err)
		}
		pw = string(b)
	}
	if err := auth.ValidatePassword(pw); err != nil {
		return err
	}
	ctx := context.Background()
	st, err := store.Open(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	res, err := st.DB.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE username = ?`, hash, user)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no user named %q", user)
	}
	_, _ = st.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE username = ?)`, user)
	_ = st.Audit(ctx, store.AuditEntry{Action: "user.password_reset_cli", Target: user})
	fmt.Printf("Password for %s updated; existing sessions were signed out.\n", user)
	return nil
}
