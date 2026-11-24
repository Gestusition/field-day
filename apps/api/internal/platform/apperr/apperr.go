// Package apperr defines the error type domain modules return. The HTTP layer
// turns it into an RFC 9457 problem response; any other error becomes a 500.
package apperr

import (
	"errors"
	"fmt"
)

// Kind classifies an error and decides its HTTP status.
type Kind int

const (
	KindInvalid       Kind = iota + 1 // 400
	KindUnauthorized                  // 401
	KindForbidden                     // 403
	KindNotFound                      // 404
	KindConflict                      // 409
	KindUnprocessable                 // 422
)

// FieldError points at one invalid input field.
type FieldError struct {
	Field   string
	Message string
}

// Error is a client-facing error. Code is a stable snake_case identifier that
// clients may switch on; Message is human readable.
type Error struct {
	Kind    Kind
	Code    string
	Message string
	Fields  []FieldError
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// As extracts an *Error from err.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

func newErr(k Kind, code, msg string) *Error { return &Error{Kind: k, Code: code, Message: msg} }

func Invalid(code, msg string) *Error       { return newErr(KindInvalid, code, msg) }
func Unauthorized(code, msg string) *Error  { return newErr(KindUnauthorized, code, msg) }
func Forbidden(code, msg string) *Error     { return newErr(KindForbidden, code, msg) }
func NotFound(code, msg string) *Error      { return newErr(KindNotFound, code, msg) }
func Conflict(code, msg string) *Error      { return newErr(KindConflict, code, msg) }
func Unprocessable(code, msg string) *Error { return newErr(KindUnprocessable, code, msg) }

// InvalidField is a validation error for a single field.
func InvalidField(field, msg string) *Error {
	return &Error{
		Kind:    KindInvalid,
		Code:    "validation_failed",
		Message: "The request failed validation.",
		Fields:  []FieldError{{Field: field, Message: msg}},
	}
}
