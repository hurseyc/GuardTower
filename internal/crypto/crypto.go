// Package crypto encrypts secrets (AdGuard Home credentials, SSH keys, API
// tokens) at rest with AES-256-GCM using a key file generated on first run.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const keyFileName = "secret.key"

// Box seals and opens secrets.
type Box struct {
	aead cipher.AEAD
}

// LoadOrCreate loads the key from dataDir/secret.key, creating it with 0600
// permissions if it does not exist. GT_SECRET_KEY (64 hex chars) overrides the
// file, which suits container secret stores.
func LoadOrCreate(dataDir string) (*Box, error) {
	if env := strings.TrimSpace(os.Getenv("GT_SECRET_KEY")); env != "" {
		key, err := hex.DecodeString(env)
		if err != nil || len(key) != 32 {
			return nil, errors.New("GT_SECRET_KEY must be 64 hex characters (32 bytes)")
		}
		return New(key)
	}
	path := filepath.Join(dataDir, keyFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
			return nil, fmt.Errorf("write key file: %w", err)
		}
		return New(key)
	}
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("key file %s is corrupt", path)
	}
	return New(key)
}

// New creates a Box from a raw 32-byte key.
func New(key []byte) (*Box, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext. An empty plaintext yields nil so "no secret" stays
// distinguishable in the database.
func (b *Box) Seal(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Open decrypts a value produced by Seal.
func (b *Box) Open(ciphertext []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", nil
	}
	ns := b.aead.NonceSize()
	if len(ciphertext) < ns {
		return "", errors.New("ciphertext too short")
	}
	pt, err := b.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
	if err != nil {
		return "", errors.New("decrypt secret: wrong key or corrupt data")
	}
	return string(pt), nil
}

// RandomToken returns n random bytes hex-encoded.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failure is unrecoverable.
	}
	return hex.EncodeToString(b)
}

// HashToken returns the SHA-256 of a token, hex-encoded, for storing session
// tokens without keeping the bearer value in the database.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
