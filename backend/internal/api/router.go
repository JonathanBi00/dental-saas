// Package api adapts the generated OpenAPI server interface to the domain services and
// assembles the HTTP handler with the platform middleware.
package api

import (
	"context"
	_ "embed"
	"net/http"
	"time"

	"dentalsaas/internal/access"
	"dentalsaas/internal/api/gen"
	"dentalsaas/internal/auditlog"
	"dentalsaas/internal/identity"
	"dentalsaas/internal/platform/apperr"
	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/platform/httpx"
	"dentalsaas/internal/tenancy"
)

// openapiSpec is a copy of api/openapi.yaml, refreshed by `task gen`.
//
//go:embed openapi.yaml
var openapiSpec []byte

type Deps struct {
	DB       *db.Pool
	Identity *identity.Service
	Tenancy  *tenancy.Service
	Members  *access.MemberService
	Audit    *auditlog.Service
	// AllowedOrigins are the browser origins allowed to make state-changing requests.
	AllowedOrigins []string
}

// publicOps need no session (operationIds with `security: []` in the spec).
var publicOps = map[string]bool{
	"Login":                 true,
	"RequestPasswordReset":  true,
	"CompletePasswordReset": true,
	"GetInvitationPreview":  true,
	"AcceptInvitation":      true,
}

// sessionOps need a session but not a usable active organisation, so a user whose
// membership was deactivated can still see their session, switch away, or sign out.
var sessionOps = map[string]bool{
	"GetSession":         true,
	"SwitchOrganisation": true,
	"Logout":             true,
}

func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.DB.Ping(ctx); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "database": "unreachable"})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "database": "ok"})
	})
	mux.HandleFunc("GET /api/v1/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openapiSpec)
	})

	srv := &Server{d: d}
	strict := gen.NewStrictHandlerWithOptions(srv, []gen.StrictMiddlewareFunc{srv.authenticate}, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			httpx.WriteError(w, r, apperr.BadRequest("Malformed request body."))
		},
		ResponseErrorHandlerFunc: httpx.WriteError,
	})
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseURL:    "/api/v1",
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			httpx.WriteError(w, r, apperr.BadRequest(err.Error()))
		},
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, apperr.NotFound("Endpoint"))
	})

	return httpx.Chain(mux,
		httpx.RequestMeta,
		httpx.AccessLog,
		httpx.Recover,
		httpx.SameOrigin(d.AllowedOrigins...),
	)
}

// authenticate resolves the session cookie for every non-public operation.
func (s *Server) authenticate(f gen.StrictHandlerFunc, op string) gen.StrictHandlerFunc {
	if publicOps[op] {
		return f
	}
	needOrg := !sessionOps[op]
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		c, err := r.Cookie(identity.CookieName)
		if err != nil || c.Value == "" {
			return nil, apperr.Unauthorized("Sign in required.")
		}
		p, err := s.d.Identity.Authenticate(ctx, c.Value, needOrg)
		if err != nil {
			if identity.IsSessionInvalid(err) {
				w.Header().Add("Set-Cookie", s.d.Identity.ClearCookie())
			}
			return nil, err
		}
		return f(access.WithPrincipal(ctx, p), w, r, req)
	}
}
