// Package crypto protects upstream API secrets and generates the Cursor gateway key.
//
// Contract: CGO-free; AES-256-GCM + master key file (0600); never logs secrets.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	gatewayKeyPrefix    = "sk-"
	gatewayKeySuffixLen = 48
	masterKeyFileName   = "master.key"
	masterKeyBytes      = 32
	gatewayCharset      = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	maskedPlaceholder   = "••••••••"
)

var (
	ErrInvalidPayload = errors.New("invalid secret payload")
	ErrDecryptFailed  = errors.New("decryption failed")
)

// GenerateGatewayKey returns an OpenAI-style API key: sk-{48 alnum}.
func GenerateGatewayKey() (string, error) {
	suffix := make([]byte, gatewayKeySuffixLen)
	charset := []byte(gatewayCharset)
	for i := range suffix {
		var b [1]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", fmt.Errorf("generate gateway key: %w", err)
		}
		suffix[i] = charset[int(b[0])%len(charset)]
	}
	return gatewayKeyPrefix + string(suffix), nil
}

// IsOpenAIStyleGatewayKey reports whether key looks like an OpenAI-compatible API key.
func IsOpenAIStyleGatewayKey(key string) bool {
	return strings.HasPrefix(key, "sk-") && len(key) >= 20
}

// MaskSecret hides the middle of a secret for CLI/status display.
func MaskSecret(secret string) string {
	if len(secret) <= 8 {
		return maskedPlaceholder
	}
	return secret[:4] + maskedPlaceholder + secret[len(secret)-4:]
}

// MasterKeyPath returns {dataRoot}/data/master.key.
func MasterKeyPath(dataRoot string) string {
	return filepath.Join(dataRoot, "data", masterKeyFileName)
}

// EnsureMasterKey loads or creates a 32-byte master key at 0600 under dataRoot.
func EnsureMasterKey(dataRoot string) ([]byte, error) {
	path := MasterKeyPath(dataRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create master key dir: %w", err)
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		if len(raw) != masterKeyBytes {
			return nil, fmt.Errorf("master key length %d, want %d", len(raw), masterKeyBytes)
		}
		return raw, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read master key: %w", err)
	}

	key := make([]byte, masterKeyBytes)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("write master key: %w", err)
	}
	// Re-assert mode in case umask interfered.
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("chmod master key: %w", err)
	}
	return key, nil
}

// Protect encrypts plaintext with AES-256-GCM using the data-root master key.
// Returns standard base64 of nonce||ciphertext||tag. Ciphertext is not plaintext base64.
func Protect(dataRoot, plaintext string) (string, error) {
	key, err := EnsureMasterKey(dataRoot)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Unprotect decrypts a Protect() payload.
func Unprotect(dataRoot, encoded string) (string, error) {
	key, err := EnsureMasterKey(dataRoot)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return "", ErrInvalidPayload
	}
	nonce, ciphertext := raw[:ns], raw[ns:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}
	return string(plain), nil
}
