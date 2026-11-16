package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"dentalsaas/internal/access"
	"dentalsaas/internal/api"
	"dentalsaas/internal/api/gen"
	"dentalsaas/internal/auditlog"
	"dentalsaas/internal/identity"
	"dentalsaas/internal/platform/httpx"
	"dentalsaas/internal/platform/mail"
	"dentalsaas/internal/platform/testdb"
	"dentalsaas/internal/tenancy"
)

func TestMain(m *testing.M) { testdb.Main(m) }

const appOrigin = "http://app.test"

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type harness struct {
	t     *testing.T
	env   *testdb.Env
	srv   *httptest.Server
	mail  *mail.Recorder
	clock *clock
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	env := testdb.Start(t)
	h := &harness{t: t, env: env, mail: &mail.Recorder{}, clock: &clock{now: time.Now()}}

	ident := identity.New(env.Pool, h.mail, appOrigin)
	ident.Now = h.clock.Now
	ten := tenancy.New(env.Pool)
	ten.Now = h.clock.Now

	h.srv = httptest.NewServer(api.NewHandler(api.Deps{
		DB: env.Pool, Identity: ident, Tenancy: ten,
		Members: &access.MemberService{DB: env.Pool}, Audit: &auditlog.Service{DB: env.Pool},
		AllowedOrigins: []string{appOrigin},
	}))
	t.Cleanup(h.srv.Close)
	return h
}

type client struct {
	h  *harness
	hc *http.Client
}

func (h *harness) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{h: h, hc: &http.Client{Jar: jar}}
}

type resp struct {
	Status int
	Body   []byte
	Header http.Header
}

// do sends a request as the web app would (same-origin Origin header, JSON body).
func (c *client) do(method, path string, body any) resp {
	c.h.t.Helper()
	hdr := map[string]string{}
	if method != http.MethodGet {
		hdr["Origin"] = appOrigin
	}
	return c.doWith(hdr, method, path, body)
}

// doWith sends a request with exactly the given extra headers (no Origin unless supplied).
func (c *client) doWith(hdr map[string]string, method, path string, body any) resp {
	c.h.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.h.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.h.srv.URL+path, r)
	if err != nil {
		c.h.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		c.h.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{Status: res.StatusCode, Body: b, Header: res.Header}
}

func (c *client) get(path string) resp             { return c.do(http.MethodGet, path, nil) }
func (c *client) post(path string, body any) resp  { return c.do(http.MethodPost, path, body) }
func (c *client) patch(path string, body any) resp { return c.do(http.MethodPatch, path, body) }
func (c *client) sessionCookie() string {
	u, _ := url.Parse(c.h.srv.URL)
	for _, ck := range c.hc.Jar.Cookies(u) {
		if ck.Name == identity.CookieName {
			return ck.Value
		}
	}
	return ""
}

// withSessionCookie returns a new client that presents the given session cookie value.
func (h *harness) withSessionCookie(value string) *client {
	c := h.client()
	u, _ := url.Parse(h.srv.URL)
	c.hc.Jar.SetCookies(u, []*http.Cookie{{Name: identity.CookieName, Value: value, Path: "/"}})
	return c
}

// session fetches the caller's current session info.
func (c *client) session(t *testing.T) gen.SessionInfo {
	t.Helper()
	var s gen.SessionInfo
	expect(t, c.get("/api/v1/auth/session"), http.StatusOK, &s)
	return s
}

// expect fails the test unless the response has the given status, and decodes the body into v.
func expect(t *testing.T, r resp, status int, v any) {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status %d, want %d; body: %s", r.Status, status, r.Body)
	}
	if v != nil {
		if err := json.Unmarshal(r.Body, v); err != nil {
			t.Fatalf("decode %s: %v", r.Body, err)
		}
	}
}

// expectProblem asserts an error status and problem type code, returning the problem.
func expectProblem(t *testing.T, r resp, status int, code string) httpx.Problem {
	t.Helper()
	var p httpx.Problem
	expect(t, r, status, &p)
	if r.Header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("content-type %q, want application/problem+json", r.Header.Get("Content-Type"))
	}
	if code != "" && p.Type != "/problems/"+code {
		t.Fatalf("problem type %q, want /problems/%s; detail: %s", p.Type, code, p.Detail)
	}
	return p
}

func (c *client) login(t *testing.T, email, password string) gen.SessionInfo {
	t.Helper()
	var s gen.SessionInfo
	expect(t, c.post("/api/v1/auth/login", map[string]string{"email": email, "password": password}), 200, &s)
	return s
}

// signedIn returns a client signed in as email (password testdb.Password).
func (h *harness) signedIn(t *testing.T, email string) *client {
	t.Helper()
	c := h.client()
	c.login(t, email, testdb.Password)
	return c
}

var linkToken = regexp.MustCompile(`/(?:invite|reset-password)/([A-Za-z0-9_-]+)`)

// tokenFromMail returns the token in the most recent email to addr.
func (h *harness) tokenFromMail(t *testing.T, addr string) string {
	t.Helper()
	m, ok := h.mail.Last(addr)
	if !ok {
		t.Fatalf("no email sent to %s", addr)
	}
	match := linkToken.FindStringSubmatch(m.Text)
	if match == nil {
		t.Fatalf("no link in email: %s", m.Text)
	}
	if !strings.HasPrefix(m.Text, "") || !strings.Contains(m.Text, appOrigin) {
		t.Fatalf("email link should use the app origin: %s", m.Text)
	}
	return match[1]
}

// superuser runs fn on a connection that bypasses RLS. Only for assertions on rows no tenant
// can read, such as pre-tenant audit events.
func (h *harness) superuser(t *testing.T, fn func(ctx context.Context, conn *pgx.Conn) error) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, h.env.SuperuserURL())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if err := fn(ctx, conn); err != nil {
		t.Fatal(err)
	}
}

// countGlobalAudit counts audit events (including pre-tenant ones no tenant can read)
// whose details.email matches.
func (h *harness) countGlobalAudit(t *testing.T, action, email string) int {
	t.Helper()
	var n int
	h.superuser(t, func(ctx context.Context, conn *pgx.Conn) error {
		return conn.QueryRow(ctx,
			"SELECT count(*) FROM audit_events WHERE action = $1 AND details->>'email' = $2", action, email,
		).Scan(&n)
	})
	return n
}

// countTargetAudit counts audit events for an action and target in any (or no) organisation.
func (h *harness) countTargetAudit(t *testing.T, action string, target uuid.UUID) int {
	t.Helper()
	var n int
	h.superuser(t, func(ctx context.Context, conn *pgx.Conn) error {
		return conn.QueryRow(ctx,
			"SELECT count(*) FROM audit_events WHERE action = $1 AND target_id = $2", action, target,
		).Scan(&n)
	})
	return n
}
