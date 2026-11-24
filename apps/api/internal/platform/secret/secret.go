// Package secret holds the small cryptographic building blocks of the API:
// random tokens, API key hashing, AES-GCM sealing and HMAC verification.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// Random returns n cryptographically random bytes.
func Random(n int) []byte {
	b := make([]byte, n)
	// crypto/rand.Read never returns an error on supported platforms.
	_, _ = rand.Read(b)
	return b
}

// NewAPIKey returns a fresh API key with the given prefix (e.g. "fdr_").
func NewAPIKey(prefix string) string {
	return prefix + base64.RawURLEncoding.EncodeToString(Random(32))
}

// HashAPIKey returns the SHA-256 digest stored instead of the key. API keys
// carry 256 bits of entropy, so a fast hash is appropriate (no KDF needed).
func HashAPIKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}

// Sealer encrypts small secrets at rest with AES-256-GCM.
type Sealer struct{ aead cipher.AEAD }

// NewSealer builds a Sealer from a 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, errors.New("secret: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext; the output is nonce || ciphertext.
func (s *Sealer) Seal(plaintext []byte) []byte {
	nonce := Random(s.aead.NonceSize())
	return s.aead.Seal(nonce, nonce, plaintext, nil)
}

// Open reverses Seal.
func (s *Sealer) Open(sealed []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(sealed) < ns {
		return nil, errors.New("secret: sealed value too short")
	}
	return s.aead.Open(nil, sealed[:ns], sealed[ns:], nil)
}

// MAC returns HMAC-SHA256(key, parts...).
func MAC(key []byte, parts ...[]byte) []byte {
	m := hmac.New(sha256.New, key)
	for _, p := range parts {
		m.Write(p)
	}
	return m.Sum(nil)
}

// VerifyMAC checks a MAC in constant time.
func VerifyMAC(key, mac []byte, parts ...[]byte) bool {
	return hmac.Equal(MAC(key, parts...), mac)
}
