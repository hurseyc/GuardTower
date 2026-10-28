package upgrade

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// fakeSSH accepts one client key and records every exec'd command with its
// stdin. exit decides each command's exit status from its stdin.
type fakeSSH struct {
	addr    string
	hostKey ssh.PublicKey
	mu      sync.Mutex
	runs    []string
	exit    func(cmd, stdin string) (string, uint32)
}

func newFakeSSH(t *testing.T, client ssh.PublicKey) *fakeSSH {
	t.Helper()
	_, hk, _ := ed25519.GenerateKey(rand.Reader)
	hs, _ := ssh.NewSignerFromKey(hk)
	f := &fakeSSH{hostKey: hs.PublicKey(), exit: func(string, string) (string, uint32) { return "ok\n", 0 }}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if string(k.Marshal()) == string(client.Marshal()) {
			return nil, nil
		}
		return nil, io.EOF
	}}
	cfg.AddHostKey(hs)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f.addr = ln.Addr().String()
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(nc, cfg)
		}
	}()
	return f
}

func (f *fakeSSH) serve(nc net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		ch, creqs, err := nch.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer ch.Close()
			for req := range creqs {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				n := binary.BigEndian.Uint32(req.Payload[:4])
				cmd := string(req.Payload[4 : 4+n])
				_ = req.Reply(true, nil)
				in := ""
				if strings.Contains(cmd, "sh -s") {
					b, _ := io.ReadAll(ch)
					in = string(b)
				}
				f.mu.Lock()
				f.runs = append(f.runs, cmd+"\n"+in)
				f.mu.Unlock()
				out, code := f.exit(cmd, in)
				_, _ = ch.Write([]byte(out))
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{code}))
				return
			}
		}()
	}
}

func clientKey(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	blk, err := ssh.MarshalPrivateKey(k, "")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := ssh.NewSignerFromKey(k)
	return string(pem.EncodeToMemory(blk)), s.PublicKey()
}

func sshCfg(addr string) SSHConfig {
	h, p, _ := net.SplitHostPort(addr)
	var port int
	for _, c := range p {
		port = port*10 + int(c-'0')
	}
	return SSHConfig{Host: h, Port: port, User: "root", AGHDir: "/opt/AdGuardHome", Service: "AdGuardHome"}
}

func TestSSHTestReportsHostKeyAndPinIsEnforced(t *testing.T) {
	ctx := context.Background()
	priv, pub := clientKey(t)
	srv := newFakeSSH(t, pub)
	cfg := sshCfg(srv.addr)
	hk, _, err := TestSSH(ctx, cfg, priv, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hk.Fingerprint, "SHA256:") || hk.Key == "" {
		t.Fatalf("host key info = %+v", hk)
	}
	// The correct pin works…
	if _, _, err := TestSSH(ctx, cfg, priv, hk.Key); err != nil {
		t.Fatalf("pinned connect: %v", err)
	}
	// …a different one is refused.
	_, otherPub := clientKey(t)
	other := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(otherPub)))
	if _, _, err := TestSSH(ctx, cfg, priv, other); err == nil || !strings.Contains(err.Error(), "host key mismatch") {
		t.Fatalf("want host key mismatch, got %v", err)
	}
}

func TestSSHUpgradeAndRollbackScripts(t *testing.T) {
	ctx := context.Background()
	priv, pub := clientKey(t)
	srv := newFakeSSH(t, pub)
	cfg := sshCfg(srv.addr)
	hk, _, err := TestSSH(ctx, cfg, priv, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg.HostKey, cfg.Sudo = hk.Key, true
	d := &SSH{Cfg: cfg, Key: priv}
	if _, err := d.Upgrade(ctx, "v0.107.79", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Upgrade(ctx, "latest; reboot", func(string) {}); err == nil {
		t.Fatal("an invalid version must be rejected before connecting")
	}
	srv.mu.Lock()
	last := srv.runs[len(srv.runs)-1]
	srv.mu.Unlock()
	if !strings.HasPrefix(last, "sudo -n sh -c 'sh -s'") {
		t.Errorf("command = %q", strings.SplitN(last, "\n", 2)[0])
	}
	for _, want := range []string{"VER='v0.107.79'", "DIR='/opt/AdGuardHome'", "releases/download/$VER/AdGuardHome_linux_$A.tar.gz", "AdGuardHome.gt-backup", stageBackedUp} {
		if !strings.Contains(last, want) {
			t.Errorf("upgrade script lacks %q", want)
		}
	}

	// A failure before the backup stage is not undoable…
	srv.exit = func(string, string) (string, uint32) { return "curl: (22) 404\n", 22 }
	res, err := d.Upgrade(ctx, "v0.107.79", func(string) {})
	if err == nil || res != nil {
		t.Fatalf("early failure: res=%v err=%v", res, err)
	}
	// …one after it is, and undo runs the rollback script.
	srv.exit = func(_, in string) (string, uint32) {
		if strings.Contains(in, "VER=") {
			return stageBackedUp + "\nsystemctl: failed\n", 1
		}
		return "restored previous version\n", 0
	}
	res, err = d.Upgrade(ctx, "v0.107.79", func(string) {})
	if err == nil || res == nil || res.Undo == nil {
		t.Fatalf("late failure: res=%v err=%v", res, err)
	}
	if err := res.Undo(ctx); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	last = srv.runs[len(srv.runs)-1]
	srv.mu.Unlock()
	if !strings.Contains(last, `cp -p "$DIR/AdGuardHome.gt-backup" "$DIR/AdGuardHome"`) {
		t.Errorf("rollback script not run: %q", last)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Errorf("shellQuote = %s", got)
	}
}
