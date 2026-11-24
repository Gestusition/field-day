package secret

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSealerRoundTrip(t *testing.T) {
	s, err := NewSealer(Random(32))
	require.NoError(t, err)

	sealed := s.Seal([]byte("top secret"))
	assert.NotContains(t, string(sealed), "top secret")

	plain, err := s.Open(sealed)
	require.NoError(t, err)
	assert.Equal(t, "top secret", string(plain))

	sealed[len(sealed)-1] ^= 0xFF
	_, err = s.Open(sealed)
	assert.Error(t, err, "tampered ciphertext must not open")
}

func TestSealerRejectsBadKey(t *testing.T) {
	_, err := NewSealer([]byte("short"))
	assert.Error(t, err)
}

func TestAPIKey(t *testing.T) {
	a, b := NewAPIKey("fdr_"), NewAPIKey("fdr_")
	assert.True(t, strings.HasPrefix(a, "fdr_"))
	assert.NotEqual(t, a, b)
	assert.Len(t, HashAPIKey(a), 32)
	assert.Equal(t, HashAPIKey(a), HashAPIKey(a))
}

func TestMAC(t *testing.T) {
	key := Random(32)
	mac := MAC(key, []byte("challenge"), []byte("reader"))
	assert.True(t, VerifyMAC(key, mac, []byte("challenge"), []byte("reader")))
	assert.False(t, VerifyMAC(key, mac, []byte("challenge"), []byte("other")))
	assert.False(t, VerifyMAC(Random(32), mac, []byte("challenge"), []byte("reader")))
}
