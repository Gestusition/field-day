// Package credential owns everything a reader can identify: physical NFC
// cards/tags and phone (HCE) credentials, plus tap-to-enroll.
//
// Resolve is the single place that decides whether a presented credential is
// genuine and usable; the access and wallet modules build on it.
package credential

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gestusition/field-day/apps/api/internal/credential/store"
	"github.com/Gestusition/field-day/apps/api/internal/platform/apperr"
	"github.com/Gestusition/field-day/apps/api/internal/platform/database"
	"github.com/Gestusition/field-day/apps/api/internal/platform/page"
	"github.com/Gestusition/field-day/apps/api/internal/platform/secret"
)

// Credential kinds and statuses (mirror the CHECK constraints).
const (
	KindDESFire   = "desfire"
	KindMobileHCE = "mobile_hce"

	StatusActive  = "active"
	StatusLost    = "lost"
	StatusRevoked = "revoked"

	// MaxActiveMobile limits phones per user.
	MaxActiveMobile = 3
)

// Reasons returned by Resolve. They are values of the AccessReason enum.
const (
	ReasonUnknown            = "unknown_credential"
	ReasonInactive           = "credential_inactive"
	ReasonUnassigned         = "credential_unassigned"
	ReasonMobileVerification = "mobile_verification_failed"
	ReasonAlreadyRegistered  = "already_registered"
)

const (
	hceSecretLen = 32
	hceUIDLen    = 16
)

var errNotFound = apperr.NotFound("credential_not_found", "Credential not found.")

// Presentation is what a reader reports about a tapped credential.
type Presentation struct {
	UID        string
	Kind       string
	SecureAuth bool         // reader completed crypto mutual auth (DESFire)
	Mobile     *MobileProof // required for mobile_hce
}

// MobileProof is the HCE challenge/response relayed by the reader.
type MobileProof struct {
	Challenge string // hex
	Response  string // hex HMAC-SHA256(secret, challenge || reader id)
}

// Resolution is the outcome of Resolve. Reason is empty on success.
type Resolution struct {
	CredentialID *uuid.UUID
	UserID       *uuid.UUID
	Secure       bool
	Reason       string
}

// Service implements the credential use cases.
type Service struct {
	pool   *pgxpool.Pool
	q      *store.Queries
	sealer *secret.Sealer
	aid    string
}

// NewService builds the service. aid is the HCE application id phones register.
func NewService(pool *pgxpool.Pool, sealer *secret.Sealer, aid string) *Service {
	return &Service{pool: pool, q: store.New(pool), sealer: sealer, aid: aid}
}

// NormalizeUID upper-cases a hex UID.
func NormalizeUID(uid string) string { return strings.ToUpper(strings.TrimSpace(uid)) }

// Resolve identifies and verifies a presented credential.
func (s *Service) Resolve(ctx context.Context, readerID uuid.UUID, p Presentation) (Resolution, error) {
	c, err := s.q.GetLiveCredentialByUID(ctx, NormalizeUID(p.UID))
	if database.IsNotFound(err) {
		return Resolution{Reason: ReasonUnknown}, nil
	}
	if err != nil {
		return Resolution{}, err
	}
	if c.Kind != p.Kind {
		// A UID collision across technologies is treated as unknown.
		return Resolution{Reason: ReasonUnknown}, nil
	}

	res := Resolution{CredentialID: &c.ID, UserID: c.UserID}
	if c.Status != StatusActive {
		res.Reason = ReasonInactive
		return res, nil
	}

	switch c.Kind {
	case KindMobileHCE:
		ok, err := s.verifyMobile(c.SecretEnc, readerID, p.Mobile)
		if err != nil {
			return Resolution{}, err
		}
		if !ok {
			res.Reason = ReasonMobileVerification
			return res, nil
		}
		res.Secure = true
	case KindDESFire:
		res.Secure = p.SecureAuth
	}

	if c.UserID == nil {
		res.Reason = ReasonUnassigned
	}
	return res, nil
}

// verifyMobile checks response == HMAC-SHA256(secret, challenge || readerID).
// Binding the reader id stops a captured response being replayed elsewhere.
func (s *Service) verifyMobile(sealed []byte, readerID uuid.UUID, proof *MobileProof) (bool, error) {
	if proof == nil {
		return false, nil
	}
	challenge, err1 := hex.DecodeString(proof.Challenge)
	response, err2 := hex.DecodeString(proof.Response)
	if err1 != nil || err2 != nil {
		return false, nil
	}
	key, err := s.sealer.Open(sealed)
	if err != nil {
		return false, err // wrong CREDENTIAL_ENCRYPTION_KEY: operator problem
	}
	return secret.VerifyMAC(key, response, challenge, readerID[:]), nil
}

// MarkUsed records a successful use.
func (s *Service) MarkUsed(ctx context.Context, id uuid.UUID) error {
	return s.q.TouchCredentialUsed(ctx, id)
}

// Get returns one credential.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (store.Credential, error) {
	c, err := s.q.GetCredential(ctx, id)
	if database.IsNotFound(err) {
		return c, errNotFound
	}
	return c, err
}

// ListFilter narrows List.
type ListFilter struct {
	UserID    *uuid.UUID
	Status    *string
	Kind      *string
	UIDPrefix *string
}

// List pages through credentials.
func (s *Service) List(ctx context.Context, p page.Request, f ListFilter) ([]store.Credential, *uuid.UUID, error) {
	if f.UIDPrefix != nil {
		u := NormalizeUID(*f.UIDPrefix)
		f.UIDPrefix = &u
	}
	rows, err := s.q.ListCredentials(ctx, store.ListCredentialsParams{
		Cursor: p.Cursor, UserID: f.UserID, Status: f.Status, Kind: f.Kind,
		UidPrefix: f.UIDPrefix, MaxRows: p.FetchRows(),
	})
	if err != nil {
		return nil, nil, err
	}
	rows, next := page.Trim(p, rows, func(c store.Credential) uuid.UUID { return c.ID })
	return rows, next, nil
}

// ListByUser returns every credential a user owns.
func (s *Service) ListByUser(ctx context.Context, userID uuid.UUID) ([]store.Credential, error) {
	return s.q.ListCredentialsByUser(ctx, &userID)
}

// CreatePhysical registers a card by UID.
func (s *Service) CreatePhysical(ctx context.Context, kind, uid string, userID *uuid.UUID, label string) (store.Credential, error) {
	c, err := s.q.CreateCredential(ctx, store.CreateCredentialParams{
		Kind: kind, Uid: NormalizeUID(uid), UserID: userID, Label: strings.TrimSpace(label),
	})
	return c, mapWriteErr(err)
}

// Update replaces owner, label and status. Revoked is terminal.
func (s *Service) Update(ctx context.Context, id uuid.UUID, userID *uuid.UUID, label, status string) (store.Credential, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return cur, err
	}
	if cur.Status == StatusRevoked {
		return cur, apperr.Conflict("credential_revoked", "A revoked credential cannot be changed.")
	}
	c, err := s.q.UpdateCredential(ctx, store.UpdateCredentialParams{
		ID: id, UserID: userID, Label: strings.TrimSpace(label), Status: status,
	})
	return c, mapWriteErr(err)
}

// ReportLost lets an owner block their own credential.
func (s *Service) ReportLost(ctx context.Context, ownerID, id uuid.UUID) (store.Credential, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return cur, err
	}
	if cur.UserID == nil || *cur.UserID != ownerID {
		return cur, errNotFound // do not reveal other users' credentials
	}
	if cur.Status == StatusRevoked {
		return cur, apperr.Conflict("credential_revoked", "This credential is already revoked.")
	}
	return s.q.UpdateCredential(ctx, store.UpdateCredentialParams{
		ID: id, UserID: cur.UserID, Label: cur.Label, Status: StatusLost,
	})
}

// IssuedMobile is a freshly issued HCE credential with its one-time secret.
type IssuedMobile struct {
	Credential store.Credential
	Secret     []byte
	AID        string
}

// IssueMobile creates a phone credential for userID.
func (s *Service) IssueMobile(ctx context.Context, userID uuid.UUID, label string) (IssuedMobile, error) {
	n, err := s.q.CountActiveMobileCredentials(ctx, &userID)
	if err != nil {
		return IssuedMobile{}, err
	}
	if n >= MaxActiveMobile {
		return IssuedMobile{}, apperr.Conflict("mobile_credential_limit",
			"You already have the maximum number of active phone credentials. Report one as lost first.")
	}
	key := secret.Random(hceSecretLen)
	c, err := s.q.CreateCredential(ctx, store.CreateCredentialParams{
		Kind:      KindMobileHCE,
		Uid:       strings.ToUpper(hex.EncodeToString(secret.Random(hceUIDLen))),
		UserID:    &userID,
		Label:     strings.TrimSpace(label),
		SecretEnc: s.sealer.Seal(key),
	})
	if err != nil {
		return IssuedMobile{}, mapWriteErr(err)
	}
	return IssuedMobile{Credential: c, Secret: key, AID: s.aid}, nil
}

// StartEnrollment arms a reader so the next tapped card is registered.
func (s *Service) StartEnrollment(ctx context.Context, readerID uuid.UUID, userID *uuid.UUID, label string,
	ttl time.Duration, createdBy uuid.UUID,
) (store.ReaderEnrollment, error) {
	e, err := s.q.UpsertEnrollment(ctx, store.UpsertEnrollmentParams{
		ReaderID: readerID, UserID: userID, Label: strings.TrimSpace(label),
		CreatedBy: &createdBy, ExpiresAt: time.Now().Add(ttl),
	})
	switch {
	case database.IsForeignKeyViolation(err, "reader_enrollments_reader_id_fkey"):
		return e, apperr.NotFound("reader_not_found", "Reader not found.")
	case database.IsForeignKeyViolation(err, "reader_enrollments_user_id_fkey"):
		return e, apperr.NotFound("user_not_found", "User not found.")
	}
	return e, err
}

// CancelEnrollment disarms a reader.
func (s *Service) CancelEnrollment(ctx context.Context, readerID uuid.UUID) error {
	n, err := s.q.DeleteEnrollment(ctx, readerID)
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("enrollment_not_found", "No pending enrollment on this reader.")
	}
	return nil
}

// Enrollment is the outcome of TryEnroll when the reader was armed.
type Enrollment struct {
	CredentialID *uuid.UUID
	UserID       *uuid.UUID
	Reason       string // "enrolled" or ReasonAlreadyRegistered
}

// TryEnroll consumes a pending enrollment on readerID, registering the
// presented card. It returns nil when the reader is not armed. If the card is
// already registered the enrollment stays armed for another card.
func (s *Service) TryEnroll(ctx context.Context, readerID uuid.UUID, p Presentation) (*Enrollment, error) {
	if p.Kind == KindMobileHCE {
		return nil, nil // phones are issued from the app, never tap-enrolled
	}
	var out *Enrollment
	err := database.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		e, err := q.TakeEnrollment(ctx, readerID)
		if database.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		c, err := q.CreateCredential(ctx, store.CreateCredentialParams{
			Kind: p.Kind, Uid: NormalizeUID(p.UID), UserID: e.UserID, Label: e.Label,
		})
		if database.IsUniqueViolation(err, "credentials_live_uid_key") {
			out = &Enrollment{Reason: ReasonAlreadyRegistered}
			return errRollback
		}
		if err != nil {
			return err
		}
		out = &Enrollment{CredentialID: &c.ID, UserID: c.UserID, Reason: "enrolled"}
		return nil
	})
	if err != nil && err != errRollback { //nolint:errorlint // sentinel identity
		return nil, err
	}
	return out, nil
}

var errRollback = apperr.Conflict("rollback", "rollback")

func mapWriteErr(err error) error {
	switch {
	case err == nil:
		return nil
	case database.IsUniqueViolation(err, "credentials_live_uid_key"):
		return apperr.Conflict("uid_in_use", "A live credential with this UID already exists.")
	case database.IsForeignKeyViolation(err, "credentials_user_id_fkey"):
		return apperr.NotFound("user_not_found", "User not found.")
	case database.IsNotFound(err):
		return errNotFound
	}
	return err
}
