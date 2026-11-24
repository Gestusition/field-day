package auth

import (
	"context"
	"fmt"
)

// DevToken is the only bearer token accepted when AUTH_MODE=dev.
const DevToken = "dev"

// DevVerifier replaces Casdoor during local development (AUTH_MODE=dev,
// started by `bun run dev`). The single token "dev" signs in as one
// developer account that has the admin role, so every endpoint can be used
// without an identity provider. Configuration refuses this mode when
// APP_ENV=production.
type DevVerifier struct {
	AdminRole string
}

// Verify implements TokenVerifier.
func (v DevVerifier) Verify(_ context.Context, raw string) (Claims, error) {
	if raw != DevToken {
		return Claims{}, fmt.Errorf("%w: in development use the token %q", errInvalidToken, DevToken)
	}
	return Claims{
		Subject:       "dev",
		Email:         "dev@dev.local",
		EmailVerified: true,
		Name:          "Developer",
		Roles:         []string{v.AdminRole},
	}, nil
}
