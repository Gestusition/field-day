package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// issuer is a minimal OIDC provider: discovery document + JWKS + a signer.
type issuer struct {
	srv  *httptest.Server
	sign func(claims map[string]any) string
}

func newIssuer(t *testing.T) *issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srv.URL,
			"jwks_uri":                              srv.URL + "/jwks",
			"authorization_endpoint":                srv.URL + "/authorize",
			"token_endpoint":                        srv.URL + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	require.NoError(t, err)

	return &issuer{srv: srv, sign: func(claims map[string]any) string {
		b, err := json.Marshal(claims)
		require.NoError(t, err)
		obj, err := signer.Sign(b)
		require.NoError(t, err)
		s, err := obj.CompactSerialize()
		require.NoError(t, err)
		return s
	}}
}

func (i *issuer) claims(overrides map[string]any) map[string]any {
	c := map[string]any{
		"iss":   i.srv.URL,
		"sub":   "user-1",
		"aud":   "field-day-web",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
		"email": "ada@example.com",
		"roles": []map[string]string{{"name": "admin"}},
	}
	for k, v := range overrides {
		c[k] = v
	}
	return c
}

func TestOIDCVerifier(t *testing.T) {
	iss := newIssuer(t)
	ctx := context.Background()
	auds := []string{"field-day-web", "field-day-mobile"}

	for name, v := range map[string]*OIDCVerifier{
		"discovery":     NewOIDCVerifier(iss.srv.URL, "", auds, "roles"),
		"explicit jwks": NewOIDCVerifier(iss.srv.URL, iss.srv.URL+"/jwks", auds, "roles"),
	} {
		t.Run(name, func(t *testing.T) {
			c, err := v.Verify(ctx, iss.sign(iss.claims(nil)))
			require.NoError(t, err)
			assert.Equal(t, "user-1", c.Subject)
			assert.Equal(t, []string{"admin"}, c.Roles)

			_, err = v.Verify(ctx, iss.sign(iss.claims(map[string]any{"aud": "field-day-mobile"})))
			assert.NoError(t, err, "second audience is accepted")

			_, err = v.Verify(ctx, iss.sign(iss.claims(map[string]any{"aud": "someone-else"})))
			assert.ErrorIs(t, err, errInvalidToken)

			_, err = v.Verify(ctx, iss.sign(iss.claims(map[string]any{"exp": time.Now().Add(-time.Minute).Unix()})))
			assert.ErrorIs(t, err, errInvalidToken)

			_, err = v.Verify(ctx, iss.sign(iss.claims(map[string]any{"iss": "https://evil.example"})))
			assert.ErrorIs(t, err, errInvalidToken)

			_, err = v.Verify(ctx, "not.a.jwt")
			assert.ErrorIs(t, err, errInvalidToken)
		})
	}

	t.Run("token signed by another key", func(t *testing.T) {
		other := newIssuer(t)
		v := NewOIDCVerifier(iss.srv.URL, "", auds, "roles")
		forged := other.sign(iss.claims(nil)) // right claims, wrong key
		_, err := v.Verify(ctx, forged)
		assert.ErrorIs(t, err, errInvalidToken)
	})
}
