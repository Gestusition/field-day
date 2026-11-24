// Package httpx contains the HTTP plumbing shared by every module: RFC 9457
// problem responses, request ids, logging, recovery and the OpenAPI contract
// middleware that authenticates and validates each request.
package httpx

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Gestusition/field-day/apps/api/internal/platform/apperr"
)

const problemBase = "https://field-day.dev/problems/"

// Problem is the RFC 9457 body. It mirrors components/schemas/Problem.
type Problem struct {
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	Status    int          `json:"status"`
	Detail    string       `json:"detail,omitempty"`
	Instance  string       `json:"instance,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
	Code      string       `json:"code,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
}

// FieldError mirrors components/schemas/FieldError.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func statusOf(k apperr.Kind) int {
	switch k {
	case apperr.KindInvalid:
		return http.StatusBadRequest
	case apperr.KindUnauthorized:
		return http.StatusUnauthorized
	case apperr.KindForbidden:
		return http.StatusForbidden
	case apperr.KindNotFound:
		return http.StatusNotFound
	case apperr.KindConflict:
		return http.StatusConflict
	case apperr.KindUnprocessable:
		return http.StatusUnprocessableEntity
	}
	return http.StatusInternalServerError
}

// WriteProblem writes a problem response and aborts the gin chain.
func WriteProblem(c *gin.Context, status int, code, detail string, fields []FieldError) {
	p := Problem{
		Type:      problemBase + strings.ReplaceAll(code, "_", "-"),
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		Instance:  c.Request.URL.Path,
		RequestID: RequestID(c),
		Code:      code,
		Errors:    fields,
	}
	if status == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", `Bearer realm="field-day"`)
	}
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(status, p)
}

// WriteError renders any error. *apperr.Error values keep their status and
// message; everything else is logged and hidden behind a generic 500.
func WriteError(c *gin.Context, log *slog.Logger, err error) {
	if e, ok := apperr.As(err); ok {
		fields := make([]FieldError, len(e.Fields))
		for i, f := range e.Fields {
			fields[i] = FieldError(f)
		}
		WriteProblem(c, statusOf(e.Kind), e.Code, e.Message, fields)
		return
	}
	log.ErrorContext(c.Request.Context(), "unhandled error",
		"err", err, "method", c.Request.Method, "path", c.Request.URL.Path, "request_id", RequestID(c))
	WriteProblem(c, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.", nil)
}
