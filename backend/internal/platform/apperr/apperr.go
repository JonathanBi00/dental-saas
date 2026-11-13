// Package apperr defines domain errors that services return and the HTTP layer maps to
// Problem Details responses in one place.
package apperr

import (
	"errors"
	"fmt"
)

type Kind int

const (
	KindBadRequest Kind = iota + 1
	KindUnauthorized
	KindForbidden
	KindNotFound
	KindConflict
	KindGone
	KindValidation
	KindTooManyRequests
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type Error struct {
	Kind   Kind
	Code   string // stable machine-readable code, e.g. "organisation_suspended"
	Detail string // human-readable, safe to show to clients
	Fields []FieldError
}

func (e *Error) Error() string {
	if len(e.Fields) > 0 {
		return fmt.Sprintf("%s: %v", e.Detail, e.Fields)
	}
	return e.Detail
}

func New(kind Kind, code, detail string) *Error {
	return &Error{Kind: kind, Code: code, Detail: detail}
}

func BadRequest(detail string) *Error { return New(KindBadRequest, "bad_request", detail) }
func Unauthorized(detail string) *Error {
	return New(KindUnauthorized, "unauthorized", detail)
}
func Forbidden(code, detail string) *Error { return New(KindForbidden, code, detail) }
func NotFound(what string) *Error          { return New(KindNotFound, "not_found", what+" not found") }
func Conflict(code, detail string) *Error  { return New(KindConflict, code, detail) }
func Gone(code, detail string) *Error      { return New(KindGone, code, detail) }
func TooManyRequests(detail string) *Error {
	return New(KindTooManyRequests, "too_many_requests", detail)
}

// Validation builds a 422 error. Use Fields to collect several problems at once.
func Validation(fields ...FieldError) *Error {
	return &Error{Kind: KindValidation, Code: "validation_failed", Detail: "One or more fields are invalid.", Fields: fields}
}

// Fields accumulates field errors; Err returns nil when there are none.
type Fields []FieldError

func (f *Fields) Add(field, msg string) { *f = append(*f, FieldError{Field: field, Message: msg}) }
func (f Fields) Err() error {
	if len(f) == 0 {
		return nil
	}
	return Validation(f...)
}

// As extracts an *Error from err.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// Is reports whether err is an *Error of the given kind.
func Is(err error, kind Kind) bool {
	e, ok := As(err)
	return ok && e.Kind == kind
}
