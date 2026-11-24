package config

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
)

func valid() Config {
	return Config{
		Env: "development", AuthMode: AuthModeDev, LogFormat: "json",
		CredentialEncryptionKey: base64.StdEncoding.EncodeToString(make([]byte, 32)),
		HCEAID:                  "F04649454C4444", SiteTimezone: "Europe/Istanbul", WalletCurrency: "TRY",
	}
}

func TestDevAuthNeedsNoIdentityProvider(t *testing.T) {
	c := valid()
	assert.NoError(t, c.Finalize())
}

func TestDevAuthIsRefusedInProduction(t *testing.T) {
	c := valid()
	c.Env = "production"
	assert.ErrorContains(t, c.Finalize(), "AUTH_MODE=dev is not allowed")
}

func TestOIDCRequiresIssuerAndAudience(t *testing.T) {
	c := valid()
	c.AuthMode = AuthModeOIDC
	assert.ErrorContains(t, c.Finalize(), "OIDC_ISSUER")

	c.OIDCIssuer, c.OIDCAudiences = "https://id.example", []string{"web"}
	c.Env = "production"
	assert.NoError(t, c.Finalize())
}

func TestUnknownAuthMode(t *testing.T) {
	c := valid()
	c.AuthMode = "none"
	assert.ErrorContains(t, c.Finalize(), "AUTH_MODE must be")
}

func TestBadCredentialKey(t *testing.T) {
	c := valid()
	c.CredentialEncryptionKey = base64.StdEncoding.EncodeToString(make([]byte, 34))
	assert.ErrorContains(t, c.Finalize(), "32 bytes")
}
