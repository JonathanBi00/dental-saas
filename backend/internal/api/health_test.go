package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dentalsaas/internal/api"
	"dentalsaas/internal/platform/db"
)

// Task 2.7: liveness never depends on the database; readiness does.
func TestHealthAndReadiness(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	expect(t, c.get("/healthz"), http.StatusOK, nil)
	expect(t, c.get("/readyz"), http.StatusOK, nil)

	// A pool pointing at a database that isn't running (nothing listens on port 1).
	down, err := db.Open(context.Background(), "postgres://dental_app:dental_app@127.0.0.1:1/dental?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer down.Close()
	srv := httptest.NewServer(api.NewHandler(api.Deps{DB: down}))
	defer srv.Close()

	get := func(path string) int {
		t.Helper()
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := get("/healthz"); code != http.StatusOK {
		t.Errorf("/healthz with database down = %d, want 200", code)
	}
	if code := get("/readyz"); code != http.StatusServiceUnavailable {
		t.Errorf("/readyz with database down = %d, want 503", code)
	}
}

func TestServesOpenAPISpec(t *testing.T) {
	h := newHarness(t)
	r := h.client().get("/api/v1/openapi.yaml")
	expect(t, r, http.StatusOK, nil)
	if ct := r.Header.Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("content-type %q, want application/yaml", ct)
	}
	if !strings.HasPrefix(string(r.Body), "openapi:") {
		t.Errorf("body does not look like an OpenAPI document: %.60q", r.Body)
	}
}
