// Package httpx holds the cross-cutting HTTP conventions: Problem Details errors,
// request IDs, logging, panic recovery, origin checks and cursor pagination.
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"dentalsaas/internal/platform/apperr"
	"dentalsaas/internal/platform/reqctx"
)

// Problem is an RFC 9457 Problem Details body.
type Problem struct {
	Type      string              `json:"type"`
	Title     string              `json:"title"`
	Status    int                 `json:"status"`
	Detail    string              `json:"detail,omitempty"`
	RequestID string              `json:"request_id"`
	Errors    []apperr.FieldError `json:"errors,omitempty"`
}

var kindStatus = map[apperr.Kind]int{
	apperr.KindBadRequest:      http.StatusBadRequest,
	apperr.KindUnauthorized:    http.StatusUnauthorized,
	apperr.KindForbidden:       http.StatusForbidden,
	apperr.KindNotFound:        http.StatusNotFound,
	apperr.KindConflict:        http.StatusConflict,
	apperr.KindGone:            http.StatusGone,
	apperr.KindValidation:      http.StatusUnprocessableEntity,
	apperr.KindTooManyRequests: http.StatusTooManyRequests,
}

// WriteError maps any error to a Problem response. Unknown errors become a generic 500
// whose details are logged, never returned.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	meta := reqctx.From(r.Context())
	p := Problem{RequestID: meta.RequestID}

	if e, ok := apperr.As(err); ok {
		p.Status = kindStatus[e.Kind]
		p.Title = http.StatusText(p.Status)
		p.Detail = e.Detail
		p.Type = "about:blank"
		if e.Code != "" {
			p.Type = "/problems/" + e.Code
		}
		p.Errors = e.Fields
	} else {
		slog.ErrorContext(r.Context(), "unhandled error",
			"request_id", meta.RequestID, "method", r.Method, "path", r.URL.Path, "error", err)
		p.Status = http.StatusInternalServerError
		p.Title = http.StatusText(p.Status)
		p.Type = "about:blank"
		p.Detail = "An unexpected error occurred."
	}
	writeProblem(w, p)
}

func writeProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// WriteJSON writes v as a JSON response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
