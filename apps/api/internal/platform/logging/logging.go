// Package logging builds the structured slog logger used across the API.
package logging

import (
	"io"
	"log/slog"
)

// New returns a JSON (production) or text (local development) logger.
func New(w io.Writer, format string, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	if format == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}
