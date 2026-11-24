// Package audit records who changed what through the admin API.
package audit

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gestusition/field-day/apps/api/internal/apigen"
	"github.com/Gestusition/field-day/apps/api/internal/audit/store"
	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
	"github.com/Gestusition/field-day/apps/api/internal/platform/httpx"
	"github.com/Gestusition/field-day/apps/api/internal/platform/page"
)

// Service records and lists audit entries.
type Service struct {
	q   *store.Queries
	log *slog.Logger
}

// NewService builds the service.
func NewService(pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{q: store.New(pool), log: log}
}

// Middleware records every state-changing request under adminPrefix once the
// handler has finished, including failed attempts (the status tells which).
func (s *Service) Middleware(adminPrefix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Request.Method == http.MethodGet || !strings.HasPrefix(c.Request.URL.Path, adminPrefix) {
			return
		}
		u, ok := auth.UserFrom(c.Request.Context())
		if !ok {
			return // rejected before authentication; nothing to attribute
		}
		// Use a detached context: the request may already be cancelled.
		ctx := context.WithoutCancel(c.Request.Context())
		err := s.q.InsertAuditLog(ctx, store.InsertAuditLogParams{
			ActorUserID:  &u.ID,
			ActorSubject: u.Subject,
			Method:       c.Request.Method,
			Path:         c.Request.URL.Path,
			StatusCode:   int32(c.Writer.Status()), //nolint:gosec // HTTP status fits
			RequestID:    httpx.RequestID(c),
		})
		if err != nil {
			s.log.ErrorContext(ctx, "write audit log", "err", err)
		}
	}
}

// Handler implements the audit operations of apigen.StrictServerInterface.
type Handler struct{ svc *Service }

// NewHandler builds the handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) ListAuditLogs(ctx context.Context, r apigen.ListAuditLogsRequestObject) (apigen.ListAuditLogsResponseObject, error) {
	p := page.New(r.Params.Limit, r.Params.Cursor)
	rows, err := h.svc.q.ListAuditLogs(ctx, store.ListAuditLogsParams{
		Cursor: p.Cursor, ActorUserID: r.Params.ActorUserId, MaxRows: p.FetchRows(),
	})
	if err != nil {
		return nil, err
	}
	rows, next := page.Trim(p, rows, func(a store.AuditLog) uuid.UUID { return a.ID })
	return apigen.ListAuditLogs200JSONResponse{
		Items: page.Map(rows, func(a store.AuditLog) apigen.AuditLog {
			return apigen.AuditLog{
				Id: a.ID, ActorUserId: a.ActorUserID, ActorSubject: a.ActorSubject, Method: a.Method,
				Path: a.Path, StatusCode: int(a.StatusCode), RequestId: a.RequestID, CreatedAt: a.CreatedAt,
			}
		}),
		NextCursor: next,
	}, nil
}
