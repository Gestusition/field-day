package httpx

import (
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	requestIDHeader = "X-Request-ID"
	requestIDKey    = "request_id"
)

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// RequestIDMiddleware accepts a sane incoming X-Request-ID (e.g. from Caddy)
// or generates one, and echoes it in the response.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if !validRequestID.MatchString(id) {
			id = uuid.NewString()
		}
		c.Set(requestIDKey, id)
		c.Header(requestIDHeader, id)
		c.Next()
	}
}

// RequestID returns the id assigned by RequestIDMiddleware.
func RequestID(c *gin.Context) string { return c.GetString(requestIDKey) }

// LogMiddleware writes one structured line per request. Authorization headers
// and bodies are never logged.
func LogMiddleware(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		status := c.Writer.Status()
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		}
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		log.LogAttrs(c.Request.Context(), level, "http request",
			slog.String("method", c.Request.Method),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Duration("duration", time.Since(start)),
			slog.String("request_id", RequestID(c)),
			slog.String("remote", c.ClientIP()),
		)
	}
}

// RecoverMiddleware turns panics into a logged 500 problem response.
func RecoverMiddleware(log *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, rec any) {
		log.ErrorContext(c.Request.Context(), "panic", "panic", rec, "request_id", RequestID(c))
		WriteProblem(c, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.", nil)
	})
}

// NotFound and MethodNotAllowed keep unknown routes on the problem format.
func NotFound(c *gin.Context) {
	WriteProblem(c, http.StatusNotFound, "route_not_found", "No such endpoint.", nil)
}

func MethodNotAllowed(c *gin.Context) {
	WriteProblem(c, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed for this endpoint.", nil)
}
