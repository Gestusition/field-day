// Package config loads the API configuration from environment variables.
package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config is the complete runtime configuration. Every field maps to one
// environment variable; see .env.example for documentation of each.
type Config struct {
	Env       string     `env:"APP_ENV" envDefault:"development"`
	HTTPAddr  string     `env:"HTTP_ADDR" envDefault:":8080"`
	LogLevel  slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string     `env:"LOG_FORMAT" envDefault:"json"`

	DatabaseURL      string `env:"DATABASE_URL,required"`
	DatabaseMaxConns int32  `env:"DATABASE_MAX_CONNS" envDefault:"10"`
	MigrateOnStart   bool   `env:"MIGRATE_ON_START" envDefault:"false"`

	OIDCIssuer string `env:"OIDC_ISSUER,required"`
	// OIDCJWKSURL skips discovery and fetches signing keys from this URL.
	// Needed when the API reaches the IdP under a different host than the
	// issuer clients see (e.g. inside Docker Compose).
	OIDCJWKSURL    string   `env:"OIDC_JWKS_URL"`
	OIDCAudiences  []string `env:"OIDC_AUDIENCES,required" envSeparator:","`
	OIDCRolesClaim string   `env:"OIDC_ROLES_CLAIM" envDefault:"roles"`
	OIDCAdminRole  string   `env:"OIDC_ADMIN_ROLE" envDefault:"admin"`

	CORSAllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS" envSeparator:","`

	// CredentialEncryptionKey seals mobile (HCE) credential secrets at rest.
	// 32 random bytes, base64 encoded. Rotating it invalidates issued phones.
	CredentialEncryptionKey string `env:"CREDENTIAL_ENCRYPTION_KEY,required"`
	HCEAID                  string `env:"HCE_AID" envDefault:"F04649454C4444"`

	SiteTimezone   string        `env:"SITE_TIMEZONE" envDefault:"Europe/Istanbul"`
	UnlockDuration time.Duration `env:"UNLOCK_DURATION" envDefault:"3s"`
	WalletCurrency string        `env:"WALLET_CURRENCY" envDefault:"TRY"`

	MQTTEnabled     bool   `env:"MQTT_ENABLED" envDefault:"true"`
	MQTTAddr        string `env:"MQTT_ADDR" envDefault:":1883"`
	MQTTTLSCertFile string `env:"MQTT_TLS_CERT_FILE"`
	MQTTTLSKeyFile  string `env:"MQTT_TLS_KEY_FILE"`

	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"15s"`

	// Derived values, filled by Load.
	credentialKey []byte
	location      *time.Location
}

// Load reads and validates the configuration from the environment.
func Load() (*Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := c.Finalize(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Finalize validates the config and computes derived values. Load calls it;
// tests that build a Config by hand must call it too.
func (c *Config) Finalize() error {
	var errs []error

	key, err := base64.StdEncoding.DecodeString(c.CredentialEncryptionKey)
	if err != nil || len(key) != 32 {
		errs = append(errs, errors.New("CREDENTIAL_ENCRYPTION_KEY must be 32 bytes, base64 encoded"))
	}
	c.credentialKey = key

	aid, err := hex.DecodeString(c.HCEAID)
	if err != nil || len(aid) < 5 || len(aid) > 16 {
		errs = append(errs, errors.New("HCE_AID must be 5-16 bytes of hex"))
	}
	c.HCEAID = strings.ToUpper(c.HCEAID)

	loc, err := time.LoadLocation(c.SiteTimezone)
	if err != nil {
		errs = append(errs, fmt.Errorf("SITE_TIMEZONE: %w", err))
	}
	c.location = loc

	if c.LogFormat != "json" && c.LogFormat != "text" {
		errs = append(errs, errors.New("LOG_FORMAT must be json or text"))
	}
	if (c.MQTTTLSCertFile == "") != (c.MQTTTLSKeyFile == "") {
		errs = append(errs, errors.New("MQTT_TLS_CERT_FILE and MQTT_TLS_KEY_FILE must be set together"))
	}
	if len(c.WalletCurrency) != 3 {
		errs = append(errs, errors.New("WALLET_CURRENCY must be an ISO 4217 code"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return nil
}

// CredentialKey returns the decoded credential encryption key.
func (c *Config) CredentialKey() []byte { return c.credentialKey }

// Location returns the site time zone used for access schedules.
func (c *Config) Location() *time.Location { return c.location }

// IsProduction reports whether the API runs in production mode.
func (c *Config) IsProduction() bool { return c.Env == "production" }
