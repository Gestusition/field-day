// Package auth authenticates the three kinds of callers (users, admins and
// reader devices) and carries the resulting principal through the context.
package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/Gestusition/field-day/apps/api/internal/platform/apperr"
)

// User is an authenticated person (mobile app or web).
type User struct {
	ID      uuid.UUID // local users.id
	Subject string    // Casdoor "sub"
	Email   string
	Roles   []string
	IsAdmin bool
}

// Device is an authenticated NFC reader.
type Device struct {
	ReaderID uuid.UUID
	Name     string
	ZoneID   *uuid.UUID
	Mode     string // "access" | "wallet"
}

type ctxKey int

const (
	userKey ctxKey = iota + 1
	deviceKey
)

// WithUser stores the user principal in ctx.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// WithDevice stores the device principal in ctx.
func WithDevice(ctx context.Context, d Device) context.Context {
	return context.WithValue(ctx, deviceKey, d)
}

// UserFrom returns the user principal, if any.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey).(User)
	return u, ok
}

// DeviceFrom returns the device principal, if any.
func DeviceFrom(ctx context.Context) (Device, bool) {
	d, ok := ctx.Value(deviceKey).(Device)
	return d, ok
}

// RequireUser returns the user principal or a 401 error.
func RequireUser(ctx context.Context) (User, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return User{}, apperr.Unauthorized("unauthenticated", "Authentication is required.")
	}
	return u, nil
}

// RequireDevice returns the device principal or a 401 error.
func RequireDevice(ctx context.Context) (Device, error) {
	d, ok := DeviceFrom(ctx)
	if !ok {
		return Device{}, apperr.Unauthorized("unauthenticated", "Device authentication is required.")
	}
	return d, nil
}
