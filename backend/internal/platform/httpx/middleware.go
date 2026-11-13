package httpx

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"

	"dentalsaas/internal/platform/apperr"
	"dentalsaas/internal/platform/reqctx"
)

const RequestIDHeader = "X-Request-Id"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// RequestMeta assigns a request ID (reusing a valid client-supplied one), echoes it in the
// response, and stores request metadata in the context.
func RequestMeta(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			id = uuid.NewString()
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := reqctx.With(r.Context(), reqctx.Meta{
			RequestID: id,
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// clientIP returns the peer address. X-Forwarded-For is trusted only from loopback
// (the Vite dev proxy); there is no other proxy in front of the API yet.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			return strings.TrimSpace(first)
		}
	}
	return host
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// AccessLog logs one line per request. Bodies are never logged.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.InfoContext(r.Context(), "http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", reqctx.From(r.Context()).RequestID,
		)
	})
}

// Recover turns a panic into a logged 500 Problem response.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.ErrorContext(r.Context(), "panic", "value", v, "stack", string(debug.Stack()))
				WriteError(w, r, fmt.Errorf("panic: %v", v))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SameOrigin rejects state-changing requests that a browser marks as cross-site.
// allowed lists acceptable Origin values (the web app's origin and the API's own).
// Requests without Origin or Sec-Fetch-Site come from non-browser clients, which cannot
// carry a victim's cookies, so they are allowed.
func SameOrigin(allowed ...string) func(http.Handler) http.Handler {
	ok := map[string]bool{}
	for _, o := range allowed {
		ok[strings.TrimRight(o, "/")] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				if !ok[origin] {
					WriteError(w, r, apperr.Forbidden("cross_origin", "Cross-origin request rejected."))
					return
				}
			} else if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				WriteError(w, r, apperr.Forbidden("cross_origin", "Cross-origin request rejected."))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Chain applies middleware so the first listed is outermost.
func Chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}
