// Package reader owns the NFC reader devices: registration, API keys,
// liveness, and the command queue used to make a reader act on a card.
package reader

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gestusition/field-day/apps/api/internal/platform/apperr"
	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
	"github.com/Gestusition/field-day/apps/api/internal/platform/database"
	"github.com/Gestusition/field-day/apps/api/internal/platform/mqtt"
	"github.com/Gestusition/field-day/apps/api/internal/platform/page"
	"github.com/Gestusition/field-day/apps/api/internal/platform/secret"
	"github.com/Gestusition/field-day/apps/api/internal/reader/store"
)

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	ModeAccess     = "access"

	apiKeyPrefix      = "fdr_"
	seenTouchInterval = 30 * time.Second
	expirySweepEvery  = 30 * time.Second
)

var errNotFound = apperr.NotFound("reader_not_found", "Reader not found.")

// Publisher pushes a message to a reader (the embedded MQTT broker).
type Publisher interface {
	Publish(topic string, payload []byte) error
}

// NopPublisher is used when MQTT is disabled; readers then poll over HTTP.
type NopPublisher struct{}

func (NopPublisher) Publish(string, []byte) error { return nil }

// Service implements the reader use cases.
type Service struct {
	q   *store.Queries
	pub Publisher
	log *slog.Logger
}

// NewService builds the service.
func NewService(pool *pgxpool.Pool, pub Publisher, log *slog.Logger) *Service {
	return &Service{q: store.New(pool), pub: pub, log: log}
}

// AuthenticateDevice implements auth.DeviceAuthenticator. It also keeps
// last_seen_at fresh so every authenticated call counts as a sign of life.
func (s *Service) AuthenticateDevice(ctx context.Context, apiKey string) (auth.Device, error) {
	if !strings.HasPrefix(apiKey, apiKeyPrefix) {
		return auth.Device{}, apperr.Unauthorized("invalid_device_key", "Invalid device key.")
	}
	r, err := s.q.GetReaderByKeyHash(ctx, secret.HashAPIKey(apiKey))
	if database.IsNotFound(err) {
		return auth.Device{}, apperr.Unauthorized("invalid_device_key", "Invalid device key.")
	}
	if err != nil {
		return auth.Device{}, err
	}
	if r.Status != StatusActive {
		return auth.Device{}, apperr.Forbidden("reader_disabled", "This reader is disabled.")
	}
	if r.LastSeenAt == nil || time.Since(*r.LastSeenAt) > seenTouchInterval {
		if err := s.q.TouchReaderSeen(ctx, store.TouchReaderSeenParams{ID: r.ID}); err != nil {
			s.log.WarnContext(ctx, "touch reader", "reader_id", r.ID, "err", err)
		}
	}
	return auth.Device{ReaderID: r.ID, Name: r.Name, ZoneID: r.ZoneID, Mode: r.Mode}, nil
}

// Get returns one reader.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (store.Reader, error) {
	r, err := s.q.GetReader(ctx, id)
	if database.IsNotFound(err) {
		return r, errNotFound
	}
	return r, err
}

// List pages through readers.
func (s *Service) List(ctx context.Context, p page.Request, zoneID *uuid.UUID, status *string) ([]store.Reader, *uuid.UUID, error) {
	rows, err := s.q.ListReaders(ctx, store.ListReadersParams{
		Cursor: p.Cursor, ZoneID: zoneID, Status: status, MaxRows: p.FetchRows(),
	})
	if err != nil {
		return nil, nil, err
	}
	rows, next := page.Trim(p, rows, func(r store.Reader) uuid.UUID { return r.ID })
	return rows, next, nil
}

// Create registers a reader and returns its one-time API key.
func (s *Service) Create(ctx context.Context, name, serial string, zoneID *uuid.UUID, mode string) (store.Reader, string, error) {
	key := secret.NewAPIKey(apiKeyPrefix)
	r, err := s.q.CreateReader(ctx, store.CreateReaderParams{
		Name: strings.TrimSpace(name), Serial: serial, ZoneID: zoneID, Mode: mode,
		KeyHash: secret.HashAPIKey(key),
	})
	if err != nil {
		return r, "", mapWriteErr(err)
	}
	return r, key, nil
}

// Update replaces the mutable fields.
func (s *Service) Update(ctx context.Context, id uuid.UUID, name string, zoneID *uuid.UUID, mode, status string) (store.Reader, error) {
	r, err := s.q.UpdateReader(ctx, store.UpdateReaderParams{
		ID: id, Name: strings.TrimSpace(name), ZoneID: zoneID, Mode: mode, Status: status,
	})
	return r, mapWriteErr(err)
}

// RotateKey issues a new API key; the old one stops working immediately.
func (s *Service) RotateKey(ctx context.Context, id uuid.UUID) (store.Reader, string, error) {
	key := secret.NewAPIKey(apiKeyPrefix)
	r, err := s.q.SetReaderKeyHash(ctx, store.SetReaderKeyHashParams{ID: id, KeyHash: secret.HashAPIKey(key)})
	if err != nil {
		return r, "", mapWriteErr(err)
	}
	return r, key, nil
}

// Heartbeat records liveness and firmware version.
func (s *Service) Heartbeat(ctx context.Context, id uuid.UUID, firmware *string) (store.Reader, error) {
	if err := s.q.TouchReaderSeen(ctx, store.TouchReaderSeenParams{ID: id, FirmwareVersion: firmware}); err != nil {
		return store.Reader{}, err
	}
	return s.Get(ctx, id)
}

// ---------------------------------------------------------------- commands

// CreateCommand queues a command and notifies the reader over MQTT. A failed
// notification is not an error: the reader also polls GET /device/commands.
func (s *Service) CreateCommand(ctx context.Context, readerID uuid.UUID, typ string, payload json.RawMessage,
	ttl time.Duration, createdBy uuid.UUID,
) (store.ReaderCommand, error) {
	cmd, err := s.q.CreateReaderCommand(ctx, store.CreateReaderCommandParams{
		ReaderID: readerID, Type: typ, Payload: payload, CreatedBy: &createdBy,
		ExpiresAt: time.Now().Add(ttl),
	})
	if database.IsForeignKeyViolation(err) {
		return cmd, errNotFound
	}
	if err != nil {
		return cmd, err
	}
	if msg, err := json.Marshal(commandToAPI(cmd)); err == nil {
		if err := s.pub.Publish(mqtt.CommandTopic(readerID), msg); err != nil {
			s.log.WarnContext(ctx, "mqtt publish failed; reader will poll", "reader_id", readerID, "err", err)
		}
	}
	return cmd, nil
}

// ListCommands pages through a reader's command history.
func (s *Service) ListCommands(ctx context.Context, readerID uuid.UUID, p page.Request) ([]store.ReaderCommand, *uuid.UUID, error) {
	if _, err := s.Get(ctx, readerID); err != nil {
		return nil, nil, err
	}
	rows, err := s.q.ListReaderCommands(ctx, store.ListReaderCommandsParams{
		ReaderID: readerID, Cursor: p.Cursor, MaxRows: p.FetchRows(),
	})
	if err != nil {
		return nil, nil, err
	}
	rows, next := page.Trim(p, rows, func(c store.ReaderCommand) uuid.UUID { return c.ID })
	return rows, next, nil
}

// OpenCommands returns pending commands for a reader and marks them delivered.
func (s *Service) OpenCommands(ctx context.Context, readerID uuid.UUID) ([]store.ReaderCommand, error) {
	rows, err := s.q.ListOpenReaderCommands(ctx, readerID)
	if err != nil || len(rows) == 0 {
		return rows, err
	}
	ids := make([]uuid.UUID, len(rows))
	for i, c := range rows {
		ids[i] = c.ID
	}
	if err := s.q.MarkReaderCommandsDelivered(ctx, ids); err != nil {
		return nil, err
	}
	return rows, nil
}

// CompleteCommand stores a reader's result.
func (s *Service) CompleteCommand(ctx context.Context, readerID, cmdID uuid.UUID, status string, result json.RawMessage) (store.ReaderCommand, error) {
	cmd, err := s.q.CompleteReaderCommand(ctx, store.CompleteReaderCommandParams{
		ID: cmdID, ReaderID: readerID, Status: status, Result: result,
	})
	if !database.IsNotFound(err) {
		return cmd, err
	}
	if _, err := s.q.GetReaderCommand(ctx, store.GetReaderCommandParams{ID: cmdID, ReaderID: readerID}); err != nil {
		if database.IsNotFound(err) {
			return cmd, apperr.NotFound("command_not_found", "Command not found.")
		}
		return cmd, err
	}
	return cmd, apperr.Conflict("command_closed", "This command is already completed or expired.")
}

// RunExpiry marks overdue commands expired until ctx is cancelled.
func (s *Service) RunExpiry(ctx context.Context) {
	t := time.NewTicker(expirySweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := s.q.ExpireReaderCommands(ctx); err != nil {
				s.log.ErrorContext(ctx, "expire reader commands", "err", err)
			} else if n > 0 {
				s.log.InfoContext(ctx, "expired reader commands", "count", n)
			}
		}
	}
}

func mapWriteErr(err error) error {
	switch {
	case err == nil:
		return nil
	case database.IsNotFound(err):
		return errNotFound
	case database.IsUniqueViolation(err, "readers_serial_key"):
		return apperr.Conflict("serial_in_use", "A reader with this serial already exists.")
	case database.IsForeignKeyViolation(err, "readers_zone_id_fkey"):
		return apperr.NotFound("zone_not_found", "Zone not found.")
	}
	return err
}
