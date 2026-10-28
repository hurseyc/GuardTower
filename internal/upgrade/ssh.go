package upgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHConfig reaches the machine (LXC, VM, bare metal) running AdGuard Home.
type SSHConfig struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	HasKey  bool   `json:"has_key"`
	HostKey string `json:"host_key"` // pinned, authorized_keys format
	Sudo    bool   `json:"sudo"`
	AGHDir  string `json:"agh_dir"`
	Service string `json:"service"`
}

// HostKeyInfo describes a server key for the user to confirm.
type HostKeyInfo struct {
	Key         string `json:"key"` // authorized_keys format
	Fingerprint string `json:"fingerprint"`
}

func describeKey(k ssh.PublicKey) HostKeyInfo {
	sum := sha256.Sum256(k.Marshal())
	return HostKeyInfo{
		Key:         strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k))),
		Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]),
	}
}

func sshSigner(privateKey string) (ssh.Signer, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		var pm *ssh.PassphraseMissingError
		if errors.As(err, &pm) {
			return nil, errors.New("the SSH key is protected by a passphrase; use a key without one for GuardTower")
		}
		return nil, fmt.Errorf("invalid SSH private key: %w", err)
	}
	return signer, nil
}

func parseHostKey(s string) (ssh.PublicKey, error) {
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(s)))
	if err != nil {
		return nil, fmt.Errorf("invalid host key: %w", err)
	}
	return k, nil
}

func (c SSHConfig) addr() string {
	port := c.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(c.Host, strconv.Itoa(port))
}

// sshDial connects. With pin empty, any host key is accepted and returned so
// it can be shown to the user (trust on first use, confirmed explicitly).
func sshDial(ctx context.Context, cfg SSHConfig, privateKey, pin string) (*ssh.Client, HostKeyInfo, error) {
	var seen HostKeyInfo
	signer, err := sshSigner(privateKey)
	if err != nil {
		return nil, seen, err
	}
	var want ssh.PublicKey
	if pin != "" {
		if want, err = parseHostKey(pin); err != nil {
			return nil, seen, err
		}
	}
	user := cfg.User
	if user == "" {
		user = "root"
	}
	cc := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			seen = describeKey(key)
			if want != nil && !bytes.Equal(want.Marshal(), key.Marshal()) {
				return fmt.Errorf("host key mismatch: the server presented %s, but %s is pinned; if the machine was reinstalled, test the connection again to re-pin", seen.Fingerprint, describeKey(want).Fingerprint)
			}
			return nil
		},
		Timeout: 15 * time.Second,
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", cfg.addr())
	if err != nil {
		return nil, seen, fmt.Errorf("cannot connect to %s: %w", cfg.addr(), err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, cfg.addr(), cc)
	if err != nil {
		conn.Close()
		return nil, seen, fmt.Errorf("SSH login to %s failed: %w", cfg.addr(), err)
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), seen, nil
}

// TestSSH connects without a pin and reports the host key and whether the
// AdGuard Home directory exists.
func TestSSH(ctx context.Context, cfg SSHConfig, privateKey, pin string) (HostKeyInfo, string, error) {
	cl, seen, err := sshDial(ctx, cfg, privateKey, pin)
	if err != nil {
		return seen, "", err
	}
	defer cl.Close()
	out, err := runSSH(ctx, cl, cfg.Sudo, "test -x "+shellQuote(dirOr(cfg.AGHDir))+"/AdGuardHome && "+shellQuote(dirOr(cfg.AGHDir))+"/AdGuardHome --version", "")
	if err != nil {
		return seen, out, fmt.Errorf("connected, but %s/AdGuardHome was not found or not runnable: %w", dirOr(cfg.AGHDir), err)
	}
	return seen, strings.TrimSpace(out), nil
}

func dirOr(d string) string {
	if strings.TrimSpace(d) == "" {
		return "/opt/AdGuardHome"
	}
	return strings.TrimRight(d, "/")
}

func serviceOr(s string) string {
	if strings.TrimSpace(s) == "" {
		return "AdGuardHome"
	}
	return s
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func runSSH(ctx context.Context, cl *ssh.Client, sudo bool, cmd, stdin string) (string, error) {
	sess, err := cl.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	var out bytes.Buffer
	sess.Stdout, sess.Stderr = &out, &out
	if stdin != "" {
		sess.Stdin = strings.NewReader(stdin)
	}
	if sudo {
		cmd = "sudo -n sh -c " + shellQuote(cmd)
	}
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGTERM)
		return out.String(), ctx.Err()
	case err := <-done:
		if err != nil {
			tail := strings.TrimSpace(out.String())
			if len(tail) > 600 {
				tail = "…" + tail[len(tail)-600:]
			}
			if tail != "" {
				return out.String(), fmt.Errorf("%w: %s", err, tail)
			}
		}
		return out.String(), err
	}
}

// stageBackedUp is printed once the backups exist, i.e. from the point a
// rollback is meaningful.
const stageBackedUp = "GT-STAGE:backed-up"

// scriptPrelude defines helpers shared by the upgrade and rollback scripts.
const scriptPrelude = `set -eu
svc() {
  if command -v systemctl >/dev/null 2>&1; then systemctl "$1" "$SVC"
  elif command -v rc-service >/dev/null 2>&1; then rc-service "$SVC" "$1"
  else service "$SVC" "$1"; fi
}
`

// UpgradeScript downloads the official release for the machine's
// architecture, backs up the binary and configuration, and swaps the binary.
func UpgradeScript(cfg SSHConfig, version string) string {
	return "DIR=" + shellQuote(dirOr(cfg.AGHDir)) + "\nSVC=" + shellQuote(serviceOr(cfg.Service)) + "\nVER=" + shellQuote(version) + "\n" + scriptPrelude + `case "$(uname -m)" in
  x86_64|amd64) A=amd64 ;; aarch64|arm64) A=arm64 ;; armv7l|armv7) A=armv7 ;; armv6l) A=armv6 ;; i386|i686) A=386 ;;
  *) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac
rm -f "$DIR/AdGuardHome.gt-backup" "$DIR/AdGuardHome.yaml.gt-backup"
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
URL="https://github.com/AdguardTeam/AdGuardHome/releases/download/$VER/AdGuardHome_linux_$A.tar.gz"
echo "downloading $URL"
if command -v curl >/dev/null 2>&1; then curl -fsSL -o "$T/agh.tgz" "$URL"; else wget -qO "$T/agh.tgz" "$URL"; fi
tar -xzf "$T/agh.tgz" -C "$T"
test -x "$T/AdGuardHome/AdGuardHome"
cp -p "$DIR/AdGuardHome" "$DIR/AdGuardHome.gt-backup"
if [ -f "$DIR/AdGuardHome.yaml" ]; then cp -p "$DIR/AdGuardHome.yaml" "$DIR/AdGuardHome.yaml.gt-backup"; fi
echo "` + stageBackedUp + `"
svc stop
cp "$T/AdGuardHome/AdGuardHome" "$DIR/AdGuardHome.gt-new"
mv -f "$DIR/AdGuardHome.gt-new" "$DIR/AdGuardHome"
svc start
echo "installed $VER"
`
}

// RollbackScript restores the backups taken by UpgradeScript.
func RollbackScript(cfg SSHConfig) string {
	return "DIR=" + shellQuote(dirOr(cfg.AGHDir)) + "\nSVC=" + shellQuote(serviceOr(cfg.Service)) + "\n" + scriptPrelude + `test -f "$DIR/AdGuardHome.gt-backup"
svc stop || true
cp -p "$DIR/AdGuardHome.gt-backup" "$DIR/AdGuardHome"
if [ -f "$DIR/AdGuardHome.yaml.gt-backup" ]; then cp -p "$DIR/AdGuardHome.yaml.gt-backup" "$DIR/AdGuardHome.yaml"; fi
svc start
echo "restored previous version"
`
}

// SSH upgrades a binary install over SSH and can roll back.
type SSH struct {
	Cfg SSHConfig
	Key string
}

// Name implements Driver.
func (s *SSH) Name() string { return DriverSSH }

// Upgrade implements Driver.
func (s *SSH) Upgrade(ctx context.Context, target string, progress func(string)) (*Result, error) {
	if err := validTarget(target); err != nil {
		return nil, err
	}
	if s.Cfg.HostKey == "" {
		return nil, errors.New("the SSH host key is not pinned yet; use Test connection on the instance's Upgrades card first")
	}
	progress("connecting to " + s.Cfg.addr())
	cl, _, err := sshDial(ctx, s.Cfg, s.Key, s.Cfg.HostKey)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	progress("downloading and installing " + target)
	if out, err := runSSH(ctx, cl, s.Cfg.Sudo, "sh -s", UpgradeScript(s.Cfg, target)); err != nil {
		// Before the backups exist nothing was changed, so there is nothing to undo.
		if !strings.Contains(out, stageBackedUp) {
			return nil, fmt.Errorf("upgrade script failed before changing anything: %w", err)
		}
		return &Result{Undo: s.undo}, fmt.Errorf("upgrade script failed: %w", err)
	}
	return &Result{Undo: s.undo}, nil
}

func (s *SSH) undo(ctx context.Context) error {
	cl, _, err := sshDial(ctx, s.Cfg, s.Key, s.Cfg.HostKey)
	if err != nil {
		return err
	}
	defer cl.Close()
	_, err = runSSH(ctx, cl, s.Cfg.Sudo, "sh -s", RollbackScript(s.Cfg))
	return err
}
