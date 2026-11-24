// Package user owns people: just-in-time provisioning from Casdoor, admin
// pre-registration and profile/status management.
package user

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gestusition/field-day/apps/api/internal/platform/apperr"
	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
	"github.com/Gestusition/field-day/apps/api/internal/platform/database"
	"github.com/Gestusition/field-day/apps/api/internal/platform/page"
	"github.com/Gestusition/field-day/apps/api/internal/user/store"
)

const (
	StatusActive    = "active"
	StatusSuspended = "suspended"

	// loginTouchInterval throttles last_login_at writes to one per window.
	loginTouchInterval = 5 * time.Minute
)

// Summary is what other modules need to know about a user.
type Summary struct {
	ID          uuid.UUID
	DisplayName string
	Active      bool
}

// Service implements the user use cases.
type Service struct {
	q *store.Queries
}

// NewService builds the service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{q: store.New(pool)}
}

var errNotFound = apperr.NotFound("user_not_found", "User not found.")

// Provision implements auth.UserProvisioner.
func (s *Service) Provision(ctx context.Context, c auth.Claims) (uuid.UUID, error) {
	u, err := s.q.GetUserBySubject(ctx, &c.Subject)
	switch {
	case err == nil:
		if u.LastLoginAt == nil || time.Since(*u.LastLoginAt) > loginTouchInterval {
			if u, err = s.q.TouchUserLogin(ctx, store.TouchUserLoginParams{ID: u.ID, Email: nonEmpty(c.Email)}); err != nil {
				return uuid.Nil, err
			}
		}
	case database.IsNotFound(err):
		if u, err = s.firstLogin(ctx, c); err != nil {
			return uuid.Nil, err
		}
	default:
		return uuid.Nil, err
	}
	if u.Status != StatusActive {
		return uuid.Nil, apperr.Forbidden("user_suspended", "This account is suspended.")
	}
	return u.ID, nil
}

// firstLogin links a pre-registered user (matched by verified email) or
// creates a new one.
func (s *Service) firstLogin(ctx context.Context, c auth.Claims) (store.User, error) {
	if c.Email != "" && c.EmailVerified {
		candidates, err := s.q.FindUnlinkedUsersByEmail(ctx, c.Email)
		if err != nil {
			return store.User{}, err
		}
		if len(candidates) == 1 {
			u, err := s.q.LinkUserSubject(ctx, store.LinkUserSubjectParams{
				ID: candidates[0].ID, Subject: &c.Subject, DisplayName: c.Name,
			})
			if err == nil {
				return u, nil
			}
			if !database.IsNotFound(err) { // NotFound: linked concurrently
				return store.User{}, err
			}
		}
	}
	return s.q.CreateUserFromLogin(ctx, store.CreateUserFromLoginParams{
		Subject: &c.Subject, Email: nonEmpty(c.Email), DisplayName: c.Name,
	})
}

// Get returns one user.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (store.User, error) {
	u, err := s.q.GetUser(ctx, id)
	if database.IsNotFound(err) {
		return u, errNotFound
	}
	return u, err
}

// Summary returns the cross-module view of a user.
func (s *Service) Summary(ctx context.Context, id uuid.UUID) (Summary, error) {
	u, err := s.Get(ctx, id)
	if err != nil {
		return Summary{}, err
	}
	return Summary{ID: u.ID, DisplayName: u.DisplayName, Active: u.Status == StatusActive}, nil
}

// GetMany returns the users with the given ids (missing ids are skipped).
func (s *Service) GetMany(ctx context.Context, ids []uuid.UUID) ([]store.User, error) {
	return s.q.ListUsersByIDs(ctx, ids)
}

// List pages through users.
func (s *Service) List(ctx context.Context, p page.Request, status, search *string) ([]store.User, *uuid.UUID, error) {
	if search != nil {
		esc := escapeLike(*search)
		search = &esc
	}
	rows, err := s.q.ListUsers(ctx, store.ListUsersParams{
		Cursor: p.Cursor, Status: status, Search: search, MaxRows: p.FetchRows(),
	})
	if err != nil {
		return nil, nil, err
	}
	rows, next := page.Trim(p, rows, func(u store.User) uuid.UUID { return u.ID })
	return rows, next, nil
}

// Create pre-registers a user by email.
func (s *Service) Create(ctx context.Context, email, displayName string) (store.User, error) {
	email = strings.TrimSpace(email)
	if err := s.ensureEmailFree(ctx, email, nil); err != nil {
		return store.User{}, err
	}
	return s.q.CreateUser(ctx, store.CreateUserParams{Email: &email, DisplayName: strings.TrimSpace(displayName)})
}

// Update replaces the mutable fields of a user.
func (s *Service) Update(ctx context.Context, id uuid.UUID, email *string, displayName, status string) (store.User, error) {
	if email != nil {
		e := strings.TrimSpace(*email)
		email = &e
		if err := s.ensureEmailFree(ctx, e, &id); err != nil {
			return store.User{}, err
		}
	}
	u, err := s.q.UpdateUser(ctx, store.UpdateUserParams{
		ID: id, Email: email, DisplayName: strings.TrimSpace(displayName), Status: status,
	})
	if database.IsNotFound(err) {
		return u, errNotFound
	}
	return u, err
}

func (s *Service) ensureEmailFree(ctx context.Context, email string, exclude *uuid.UUID) error {
	n, err := s.q.CountUsersWithEmail(ctx, store.CountUsersWithEmailParams{Email: email, ExcludeID: exclude})
	if err != nil {
		return err
	}
	if n > 0 {
		return apperr.Conflict("email_in_use", "Another user already has this email.")
	}
	return nil
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(s string) string { return likeEscaper.Replace(s) }
