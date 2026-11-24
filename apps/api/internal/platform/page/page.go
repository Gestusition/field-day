// Package page implements keyset (cursor) pagination over UUIDv7 ids.
//
// Queries order by id DESC and fetch Limit+1 rows; the extra row tells us a
// next page exists without a COUNT query.
package page

import "github.com/google/uuid"

const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// Request is a normalised page request.
type Request struct {
	Limit  int32
	Cursor *uuid.UUID
}

// New normalises the optional limit and cursor query parameters.
func New(limit *int, cursor *uuid.UUID) Request {
	l := DefaultLimit
	if limit != nil {
		l = *limit
	}
	l = max(1, min(l, MaxLimit))
	return Request{Limit: int32(l), Cursor: cursor} //nolint:gosec // bounded above
}

// FetchRows is the LIMIT to pass to the query.
func (r Request) FetchRows() int32 { return r.Limit + 1 }

// Trim cuts the look-ahead row and returns the cursor for the next page.
func Trim[T any](r Request, rows []T, id func(T) uuid.UUID) ([]T, *uuid.UUID) {
	if len(rows) <= int(r.Limit) {
		return rows, nil
	}
	rows = rows[:r.Limit]
	next := id(rows[len(rows)-1])
	return rows, &next
}

// Map converts a slice, always returning a non-nil slice (JSON [] not null).
func Map[T, U any](in []T, fn func(T) U) []U {
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = fn(v)
	}
	return out
}
