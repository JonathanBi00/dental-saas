package httpx_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"dentalsaas/internal/platform/apperr"
	"dentalsaas/internal/platform/httpx"
)

func serve(h http.Handler, req *http.Request) (*httptest.ResponseRecorder, httpx.Problem) {
	rec := httptest.NewRecorder()
	httpx.Chain(h, httpx.RequestMeta, httpx.Recover).ServeHTTP(rec, req)
	var p httpx.Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return rec, p
}

func TestValidationProblemListsFields(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var f apperr.Fields
		f.Add("name", "is required")
		httpx.WriteError(w, r, f.Err())
	})
	rec, p := serve(h, httptest.NewRequest("POST", "/x", nil))
	if rec.Code != 422 || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status %d content-type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if len(p.Errors) != 1 || p.Errors[0].Field != "name" {
		t.Fatalf("errors = %+v, want the name field", p.Errors)
	}
	if p.RequestID == "" || p.RequestID != rec.Header().Get(httpx.RequestIDHeader) {
		t.Fatalf("problem request_id %q does not match header %q", p.RequestID, rec.Header().Get(httpx.RequestIDHeader))
	}
}

func TestUnexpectedErrorIsGeneric500(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, errors.New("pq: connection to 10.0.0.5 refused"))
	})
	rec, p := serve(h, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != 500 {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	if p.Detail != "An unexpected error occurred." {
		t.Fatalf("detail %q leaks internals", p.Detail)
	}
}

func TestPanicBecomes500(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") })
	rec, p := serve(h, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != 500 || p.Status != 500 {
		t.Fatalf("status %d / %d, want 500", rec.Code, p.Status)
	}
}

func TestClientRequestIDIsReused(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set(httpx.RequestIDHeader, "abc-123")
	rec, _ := serve(h, req)
	if got := rec.Header().Get(httpx.RequestIDHeader); got != "abc-123" {
		t.Fatalf("X-Request-Id = %q, want abc-123", got)
	}

	req = httptest.NewRequest("GET", "/x", nil)
	req.Header.Set(httpx.RequestIDHeader, "bad id with spaces\n")
	rec, _ = serve(h, req)
	if got := rec.Header().Get(httpx.RequestIDHeader); got == "" || got == "bad id with spaces\n" {
		t.Fatalf("invalid client id should be replaced, got %q", got)
	}
}

func TestLimit(t *testing.T) {
	n := func(v int) *int { return &v }
	if l, err := httpx.Limit(nil); err != nil || l != 50 {
		t.Fatalf("default = %d, %v", l, err)
	}
	if l, err := httpx.Limit(n(200)); err != nil || l != 200 {
		t.Fatalf("200 = %d, %v", l, err)
	}
	for _, bad := range []int{0, 201, 1000} {
		if _, err := httpx.Limit(n(bad)); !apperr.Is(err, apperr.KindValidation) {
			t.Errorf("limit %d: err = %v, want validation error", bad, err)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	id := uuid.New()
	c, err := httpx.DecodeCursor(httpx.EncodeCursor("Parramatta", id))
	if err != nil || c.Key != "Parramatta" || c.ID != id {
		t.Fatalf("round trip = %+v, %v", c, err)
	}
	bad := "not-a-cursor"
	if _, err := httpx.DecodeCursor(&bad); !apperr.Is(err, apperr.KindValidation) {
		t.Fatalf("garbage cursor: err = %v, want validation error", err)
	}
}

func TestSameOrigin(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	h := httpx.Chain(ok, httpx.RequestMeta, httpx.SameOrigin("http://localhost:5173"))
	cases := []struct {
		method, origin, site string
		want                 int
	}{
		{"POST", "http://localhost:5173", "", 204},
		{"POST", "https://evil.example", "", 403},
		{"POST", "", "cross-site", 403},
		{"POST", "", "same-origin", 204},
		{"POST", "", "", 204}, // non-browser client
		{"GET", "https://evil.example", "", 204},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, "/x", nil)
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		if c.site != "" {
			req.Header.Set("Sec-Fetch-Site", c.site)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s origin=%q site=%q: status %d, want %d", c.method, c.origin, c.site, rec.Code, c.want)
		}
	}
}
