package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSealOpenAndKeyFile(t *testing.T) {
	dir := t.TempDir()
	b, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, keyFileName))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file perms: %v %v", fi.Mode(), err)
	}
	ct, err := b.Seal("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := LoadOrCreate(dir) // reload same key
	if err != nil {
		t.Fatal(err)
	}
	pt, err := b2.Open(ct)
	if err != nil || pt != "hunter2" {
		t.Fatalf("open: %q %v", pt, err)
	}
	ct[len(ct)-1] ^= 1
	if _, err := b2.Open(ct); err == nil {
		t.Fatal("tampered ciphertext opened")
	}
	if ct, _ := b.Seal(""); ct != nil {
		t.Fatal("empty secret should seal to nil")
	}
}
