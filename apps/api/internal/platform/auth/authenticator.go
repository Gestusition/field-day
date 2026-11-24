package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/Gestusition/field-day/apps/api/internal/platform/apperr"
)

// Security scheme names as declared in api/openapi.yaml.
const (
	SchemeUser   = "userAuth"
	SchemeAdmin  = "adminAuth"
	SchemeDevice = "deviceAuth"
)

var errInvalidToken = errors.New("invalid token")

// UserProvisioner maps verified claims to a local user id, creating the user
// on first login. It returns a Forbidden error for suspended users.
type UserProvisioner interface {
	Provision(ctx context.Context, c Claims) (uuid.UUID, error)
}

// DeviceAuthenticator resolves a reader API key to a device principal.
type DeviceAuthenticator interface {
	AuthenticateDevice(ctx context.Context, apiKey string) (Device, error)
}

// Authenticator applies an OpenAPI security scheme to a request.
type Authenticator struct {
	Tokens    TokenVerifier
	Users     UserProvisioner
	Devices   DeviceAuthenticator
	AdminRole string
	Log       *slog.Logger
}

// Authenticate enforces scheme on r and returns a context carrying the
// principal. The error is always an *apperr.Error.
func (a *Authenticator) Authenticate(r *http.Request, scheme string) (context.Context, error) {
	ctx := r.Context()
	raw, ok := bearer(r)
	if !ok {
		return nil, apperr.Unauthorized("missing_token", "A bearer token is required.")
	}

	switch scheme {
	case SchemeUser, SchemeAdmin:
		claims, err := a.Tokens.Verify(ctx, raw)
		if err != nil {
			if !errors.Is(err, errInvalidToken) {
				// Identity provider unreachable etc. — log it, but the
				// client still just sees 401.
				a.Log.ErrorContext(ctx, "token verification unavailable", "err", err)
			}
			return nil, apperr.Unauthorized("invalid_token", "The access token is invalid or expired.")
		}
		u := User{
			Subject: claims.Subject,
			Email:   claims.Email,
			Roles:   claims.Roles,
			IsAdmin: slices.Contains(claims.Roles, a.AdminRole),
		}
		if scheme == SchemeAdmin && !u.IsAdmin {
			return nil, apperr.Forbidden("admin_required", "This operation requires the admin role.")
		}
		id, err := a.Users.Provision(ctx, claims)
		if err != nil {
			return nil, err
		}
		u.ID = id
		return WithUser(ctx, u), nil

	case SchemeDevice:
		d, err := a.Devices.AuthenticateDevice(ctx, raw)
		if err != nil {
			return nil, err
		}
		return WithDevice(ctx, d), nil
	}
	return nil, apperr.Unauthorized("unsupported_scheme", "Unsupported authentication scheme.")
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const p = "bearer "
	if len(h) <= len(p) || !strings.EqualFold(h[:len(p)], p) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(p):])
	return tok, tok != ""
}
