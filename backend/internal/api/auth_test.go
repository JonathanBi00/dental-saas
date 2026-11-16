package api_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"dentalsaas/internal/access"
	"dentalsaas/internal/api/gen"
	"dentalsaas/internal/identity"
	"dentalsaas/internal/platform/testdb"
)

func loginBody(email, password string) map[string]string {
	return map[string]string{"email": email, "password": password}
}

func sortedIDs(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return out
}

func sameIDs(a, b []uuid.UUID) bool { return slices.Equal(sortedIDs(a), sortedIDs(b)) }

// ------------------------------------------------------------------ 4.7 sign in

// user-auth spec "Successful sign in" / "User with a single organisation", and
// access-control spec "Interface reads permissions".
func TestLoginSetsCookieAndReturnsSession(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Login Org")
	loc := testdb.CreateLocation(t, h.env.Pool, org, "Main")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)

	c := h.client()
	r := c.post("/api/v1/auth/login", loginBody(owner.Email, testdb.Password))
	var s gen.SessionInfo
	expect(t, r, http.StatusOK, &s)

	cookie := r.Header.Get("Set-Cookie")
	for _, want := range []string{identity.CookieName + "=", "HttpOnly", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("Set-Cookie %q lacks %q", cookie, want)
		}
	}
	if s.User.Id != owner.UserID || s.User.Email != owner.Email {
		t.Errorf("user = %+v", s.User)
	}
	if len(s.Memberships) != 1 || s.Memberships[0].OrganisationId != org || s.Memberships[0].Role != "owner" {
		t.Errorf("memberships = %+v", s.Memberships)
	}
	if s.ActiveOrganisationId == nil || *s.ActiveOrganisationId != org {
		t.Fatalf("active organisation = %v, want the user's only organisation %s", s.ActiveOrganisationId, org)
	}
	if s.Role == nil || *s.Role != "owner" {
		t.Errorf("role = %v, want owner", s.Role)
	}
	wantPerms := []string{"locations:read", "locations:manage", "members:manage", "audit:read"}
	if !slices.Equal(s.Permissions, wantPerms) {
		t.Errorf("permissions = %v, want %v", s.Permissions, wantPerms)
	}
	if !sameIDs(s.AccessibleLocationIds, []uuid.UUID{loc}) {
		t.Errorf("accessible locations = %v, want [%s]", s.AccessibleLocationIds, loc)
	}

	// GET /auth/session returns the same thing on a later request.
	again := c.session(t)
	if again.User.Id != s.User.Id || again.ActiveOrganisationId == nil || *again.ActiveOrganisationId != org {
		t.Errorf("GET /auth/session = %+v", again)
	}
	testdb.AssertAudited(t, h.env.Pool, org, "auth.login_succeeded", &owner.UserID)

	// The email is matched case-insensitively and surrounding whitespace is ignored.
	h.client().login(t, "  "+strings.ToUpper(owner.Email)+" ", testdb.Password)
}

// user-auth spec "Wrong password or unknown email": identical 401s.
func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Login Failures")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	unknown := testdb.Email(t, "nobody")

	c := h.client()
	wrong := c.post("/api/v1/auth/login", loginBody(owner.Email, "not-the-right-password"))
	missing := c.post("/api/v1/auth/login", loginBody(unknown, "not-the-right-password"))
	pw := expectProblem(t, wrong, http.StatusUnauthorized, "unauthorized")
	pm := expectProblem(t, missing, http.StatusUnauthorized, "unauthorized")
	pw.RequestID, pm.RequestID = "", ""
	if pw.Type != pm.Type || pw.Title != pm.Title || pw.Detail != pm.Detail || pw.Status != pm.Status || len(pw.Errors)+len(pm.Errors) != 0 {
		t.Errorf("responses differ:\n wrong password: %+v\n unknown email:  %+v", pw, pm)
	}
	if wrong.Header.Get("Set-Cookie") != "" || missing.Header.Get("Set-Cookie") != "" || c.sessionCookie() != "" {
		t.Error("a failed sign-in must not set a session cookie")
	}
	expectProblem(t, c.post("/api/v1/auth/login", loginBody("", "")), http.StatusUnauthorized, "unauthorized")
	expectProblem(t, c.get("/api/v1/auth/session"), http.StatusUnauthorized, "unauthorized")

	// audit-log spec "Failed sign-in is audited": email, IP and the failure; no password data.
	for _, email := range []string{owner.Email, unknown} {
		var ip, details string
		h.superuser(t, func(ctx context.Context, conn *pgx.Conn) error {
			return conn.QueryRow(ctx,
				"SELECT ip, details::text FROM audit_events WHERE action = 'auth.login_failed' AND details->>'email' = $1", email,
			).Scan(&ip, &details)
		})
		if ip == "" {
			t.Errorf("%s: failed sign-in audit entry has no IP", email)
		}
		if strings.Contains(details, "not-the-right-password") || strings.Contains(strings.ToLower(details), "password") {
			t.Errorf("%s: audit details contain password data: %s", email, details)
		}
	}
}

// user-auth spec "Lockout after repeated failures".
func TestLoginLockoutAfterFiveFailures(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Lockout Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	c := h.client()

	for i := 1; i <= 5; i++ {
		expectProblem(t, c.post("/api/v1/auth/login", loginBody(owner.Email, "wrong-password-here")), http.StatusUnauthorized, "unauthorized")
	}
	// Sixth attempt, even with the correct password: 429 and no session.
	r := c.post("/api/v1/auth/login", loginBody(owner.Email, testdb.Password))
	expectProblem(t, r, http.StatusTooManyRequests, "too_many_requests")
	if r.Header.Get("Set-Cookie") != "" || c.sessionCookie() != "" {
		t.Error("a locked-out sign-in must not create a session")
	}
	if n := h.countGlobalAudit(t, "auth.login_failed", owner.Email); n != 5 {
		t.Errorf("auth.login_failed events = %d, want 5", n)
	}
	if n := h.countGlobalAudit(t, "auth.locked_out", owner.Email); n != 1 {
		t.Errorf("auth.locked_out events = %d, want 1", n)
	}

	// Still locked just before the 15 minutes are up; usable again afterwards.
	h.clock.Advance(14 * time.Minute)
	expectProblem(t, c.post("/api/v1/auth/login", loginBody(owner.Email, testdb.Password)), http.StatusTooManyRequests, "too_many_requests")
	h.clock.Advance(2 * time.Minute)
	c.login(t, owner.Email, testdb.Password)
}

func TestLoginFailureCountResets(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Reset Count Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	c := h.client()
	fail := func(n int) {
		t.Helper()
		for range n {
			expectProblem(t, c.post("/api/v1/auth/login", loginBody(owner.Email, "wrong-password-here")), http.StatusUnauthorized, "unauthorized")
		}
	}

	// A successful sign-in clears earlier failures.
	fail(4)
	c.login(t, owner.Email, testdb.Password)
	fail(4)
	c.login(t, owner.Email, testdb.Password)

	// Failures more than 15 minutes apart are not consecutive "within 15 minutes".
	fail(4)
	h.clock.Advance(16 * time.Minute)
	fail(1)
	c.login(t, owner.Email, testdb.Password)
}

// user-auth spec "User with a single organisation": with several (or no) organisations
// nothing is selected automatically.
func TestLoginOrganisationAutoSelection(t *testing.T) {
	h := newHarness(t)
	orgA := testdb.CreateOrg(t, h.env.Pool, "Auto A")
	orgB := testdb.CreateOrg(t, h.env.Pool, "Auto B")

	// No memberships.
	loner := testdb.Email(t, "loner")
	testdb.CreateUser(t, h.env.Pool, loner)
	c := h.client()
	s := c.login(t, loner, testdb.Password)
	if s.ActiveOrganisationId != nil || s.Role != nil || len(s.Permissions) != 0 || len(s.Memberships) != 0 {
		t.Errorf("no memberships: session = %+v", s)
	}
	expectProblem(t, c.get("/api/v1/locations"), http.StatusForbidden, "no_active_organisation")

	// Two usable memberships: the user must choose.
	multi := testdb.Email(t, "multi")
	uid := testdb.CreateUser(t, h.env.Pool, multi)
	testdb.AddMember(t, h.env.Pool, orgA, uid, access.RoleOwner)
	testdb.AddMember(t, h.env.Pool, orgB, uid, access.RoleFrontDesk)
	c = h.client()
	s = c.login(t, multi, testdb.Password)
	if s.ActiveOrganisationId != nil || len(s.Memberships) != 2 {
		t.Errorf("two memberships: active = %v, memberships = %d; want none selected, 2 listed", s.ActiveOrganisationId, len(s.Memberships))
	}
	expectProblem(t, c.get("/api/v1/locations"), http.StatusForbidden, "no_active_organisation")

	// A suspended organisation doesn't count, so the remaining one is selected.
	testdb.SetOrgStatus(t, h.env.Pool, orgB, "suspended")
	s = h.client().login(t, multi, testdb.Password)
	if s.ActiveOrganisationId == nil || *s.ActiveOrganisationId != orgA {
		t.Errorf("one usable membership: active = %v, want %s", s.ActiveOrganisationId, orgA)
	}
}

// -------------------------------------------------------- 4.3 session lifetime

// user-auth spec "Idle timeout".
func TestSessionIdleTimeout(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Idle Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	c := h.signedIn(t, owner.Email)

	h.clock.Advance(identity.IdleTimeout + time.Minute)
	r := c.get("/api/v1/auth/session")
	expectProblem(t, r, http.StatusUnauthorized, "unauthorized")
	if ck := r.Header.Get("Set-Cookie"); !strings.Contains(ck, identity.CookieName+"=;") || !strings.Contains(ck, "Max-Age=0") {
		t.Errorf("expired session should clear the cookie, got Set-Cookie %q", ck)
	}
	expectProblem(t, c.get("/api/v1/locations"), http.StatusUnauthorized, "unauthorized")
}

// user-auth spec "Activity extends the idle window", and the 12-hour absolute cap.
func TestSessionSlidingExtensionAndAbsoluteCap(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Sliding Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	c := h.signedIn(t, owner.Email)

	// A request every 50 minutes keeps the session alive well past one idle window: 14 steps
	// is 11 h 40 min after sign-in.
	for i := 1; i <= 14; i++ {
		h.clock.Advance(50 * time.Minute)
		if r := c.get("/api/v1/auth/session"); r.Status != http.StatusOK {
			t.Fatalf("request %d (%v after sign-in): status %d, want 200; body: %s", i, time.Duration(i)*50*time.Minute, r.Status, r.Body)
		}
	}
	// 12 h 10 min after sign-in the session is over, although it was used 30 minutes ago.
	h.clock.Advance(30 * time.Minute)
	expectProblem(t, c.get("/api/v1/auth/session"), http.StatusUnauthorized, "unauthorized")
}

// user-auth spec "Reusing a signed-out session".
func TestLogoutRevokesSession(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Logout Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	c := h.signedIn(t, owner.Email)
	other := h.signedIn(t, owner.Email) // a second device
	token := c.sessionCookie()
	if token == "" {
		t.Fatal("no session cookie after sign-in")
	}

	r := c.post("/api/v1/auth/logout", nil)
	expect(t, r, http.StatusNoContent, nil)
	if ck := r.Header.Get("Set-Cookie"); !strings.Contains(ck, "Max-Age=0") {
		t.Errorf("logout should clear the cookie, got Set-Cookie %q", ck)
	}
	if c.sessionCookie() != "" {
		t.Error("cookie still in the jar after logout")
	}
	testdb.AssertAudited(t, h.env.Pool, org, "auth.logout", &owner.UserID)

	// The old cookie value no longer works; the other device's session is untouched.
	stale := h.withSessionCookie(token)
	expectProblem(t, stale.get("/api/v1/auth/session"), http.StatusUnauthorized, "unauthorized")
	expectProblem(t, stale.get("/api/v1/locations"), http.StatusUnauthorized, "unauthorized")
	other.session(t)

	expectProblem(t, h.client().post("/api/v1/auth/logout", nil), http.StatusUnauthorized, "unauthorized")
}

// --------------------------------------------------------- 4.4 auth middleware

func TestUnknownSessionCookieIsRejected(t *testing.T) {
	h := newHarness(t)
	c := h.withSessionCookie("not-a-real-session-token")
	r := c.get("/api/v1/locations")
	expectProblem(t, r, http.StatusUnauthorized, "unauthorized")
	if !strings.Contains(r.Header.Get("Set-Cookie"), "Max-Age=0") {
		t.Errorf("an invalid cookie should be cleared, got Set-Cookie %q", r.Header.Get("Set-Cookie"))
	}
}

// user-auth spec "Deactivated member's next request".
func TestDeactivatedMembershipIsForbiddenOnNextRequest(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Deactivation Org")
	testdb.CreateLocation(t, h.env.Pool, org, "Secret Clinic")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	staff := testdb.CreateMember(t, h.env.Pool, org, access.RoleFrontDesk)

	c := h.signedIn(t, staff.Email)
	expect(t, c.get("/api/v1/locations"), http.StatusOK, nil)

	var m gen.Member
	expect(t, h.signedIn(t, owner.Email).post("/api/v1/members/"+staff.MembershipID.String()+"/deactivate", nil), http.StatusOK, &m)

	r := c.get("/api/v1/locations")
	expectProblem(t, r, http.StatusForbidden, "membership_inactive")
	if strings.Contains(string(r.Body), "Secret Clinic") {
		t.Errorf("403 response leaks organisation data: %s", r.Body)
	}

	// The session itself is still valid: the user can see where they stand and sign out.
	s := c.session(t)
	if s.ActiveOrganisationId != nil || s.Role != nil || len(s.Permissions) != 0 || len(s.AccessibleLocationIds) != 0 {
		t.Errorf("session after deactivation still has organisation context: %+v", s)
	}
	if len(s.Memberships) != 1 || s.Memberships[0].Status != "deactivated" {
		t.Errorf("memberships = %+v, want one deactivated", s.Memberships)
	}
	expect(t, c.post("/api/v1/auth/logout", nil), http.StatusNoContent, nil)
}

func TestSuspendedOrganisationIsForbidden(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Suspended Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	c := h.signedIn(t, owner.Email)
	expect(t, c.get("/api/v1/locations"), http.StatusOK, nil)

	testdb.SetOrgStatus(t, h.env.Pool, org, "suspended")
	expectProblem(t, c.get("/api/v1/locations"), http.StatusForbidden, "organisation_suspended")
	expectProblem(t, c.post("/api/v1/locations", map[string]any{"name": "X", "timezone": "UTC"}), http.StatusForbidden, "organisation_suspended")
	if s := c.session(t); s.ActiveOrganisationId != nil {
		t.Errorf("session reports a suspended organisation as active: %+v", s)
	}

	testdb.SetOrgStatus(t, h.env.Pool, org, "active")
	expect(t, c.get("/api/v1/locations"), http.StatusOK, nil)
}

// ------------------------------------------------------------ 4.5 origin check

// user-auth spec "Forged cross-site request".
func TestCrossOriginPostIsRejectedAndChangesNothing(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "CSRF Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	c := h.signedIn(t, owner.Email)
	body := map[string]any{"name": "Forged", "timezone": "Australia/Sydney"}

	for name, hdr := range map[string]map[string]string{
		"foreign Origin":            {"Origin": "https://evil.example"},
		"cross-site Sec-Fetch-Site": {"Sec-Fetch-Site": "cross-site"},
		"same-site Sec-Fetch-Site":  {"Sec-Fetch-Site": "same-site"},
	} {
		expectProblem(t, c.doWith(hdr, http.MethodPost, "/api/v1/locations", body), http.StatusForbidden, "cross_origin")
		var list gen.LocationList
		expect(t, c.get("/api/v1/locations?include_archived=true"), http.StatusOK, &list)
		if len(list.Items) != 0 {
			t.Fatalf("%s: forged request created a location: %+v", name, list.Items)
		}
	}
	if n := len(testdb.AuditEvents(t, h.env.Pool, org, "location.created", nil)); n != 0 {
		t.Errorf("location.created events = %d, want 0", n)
	}

	// A forged sign-out doesn't end the session either.
	expectProblem(t, c.doWith(map[string]string{"Origin": "https://evil.example"}, http.MethodPost, "/api/v1/auth/logout", nil),
		http.StatusForbidden, "cross_origin")
	c.session(t)

	// The same request from the app's own origin succeeds.
	expect(t, c.post("/api/v1/locations", body), http.StatusCreated, nil)
}

// ---------------------------------------------------------- 4.8 active switching

func TestSwitchOrganisation(t *testing.T) {
	h := newHarness(t)
	orgA := testdb.CreateOrg(t, h.env.Pool, "Switch A")
	orgB := testdb.CreateOrg(t, h.env.Pool, "Switch B")
	orgC := testdb.CreateOrg(t, h.env.Pool, "Switch C (not a member)")
	locA := testdb.CreateLocation(t, h.env.Pool, orgA, "A clinic")
	email := testdb.Email(t, "switcher")
	uid := testdb.CreateUser(t, h.env.Pool, email)
	testdb.AddMember(t, h.env.Pool, orgA, uid, access.RoleOwner)
	testdb.AddMember(t, h.env.Pool, orgB, uid, access.RoleFrontDesk)
	c := h.signedIn(t, email)

	switchTo := func(org uuid.UUID) resp {
		return c.post("/api/v1/auth/session/organisation", map[string]any{"organisation_id": org})
	}
	assertActive := func(when string, org uuid.UUID, loc *uuid.UUID) {
		t.Helper()
		s := c.session(t)
		if s.ActiveOrganisationId == nil || *s.ActiveOrganisationId != org {
			t.Fatalf("%s: active organisation = %v, want %s", when, s.ActiveOrganisationId, org)
		}
		if (loc == nil) != (s.ActiveLocationId == nil) || (loc != nil && *loc != *s.ActiveLocationId) {
			t.Fatalf("%s: active location = %v, want %v", when, s.ActiveLocationId, loc)
		}
	}

	var s gen.SessionInfo
	expect(t, switchTo(orgA), http.StatusOK, &s)
	if s.ActiveOrganisationId == nil || *s.ActiveOrganisationId != orgA || s.Role == nil || *s.Role != "owner" {
		t.Fatalf("after switching to A: %+v", s)
	}
	testdb.AssertAudited(t, h.env.Pool, orgA, "session.organisation_switched", &orgA)
	expect(t, c.post("/api/v1/auth/session/location", map[string]any{"location_id": locA}), http.StatusOK, nil)
	assertActive("after choosing a location", orgA, &locA)

	// Not a member, or no such organisation: 403 and nothing changes.
	expectProblem(t, switchTo(orgC), http.StatusForbidden, "not_a_member")
	assertActive("after switching to a non-member organisation", orgA, &locA)
	expectProblem(t, switchTo(uuid.New()), http.StatusForbidden, "not_a_member")
	assertActive("after switching to an unknown organisation", orgA, &locA)
	if n := len(testdb.AuditEvents(t, h.env.Pool, orgC, "session.organisation_switched", nil)); n != 0 {
		t.Errorf("org C has %d switch events, want 0", n)
	}

	// A suspended organisation can't be switched to.
	testdb.SetOrgStatus(t, h.env.Pool, orgB, "suspended")
	expectProblem(t, switchTo(orgB), http.StatusForbidden, "organisation_suspended")
	assertActive("after switching to a suspended organisation", orgA, &locA)
	testdb.SetOrgStatus(t, h.env.Pool, orgB, "active")

	// A valid switch changes role and permissions and clears the active location.
	expect(t, switchTo(orgB), http.StatusOK, &s)
	if s.Role == nil || *s.Role != "front_desk" || !slices.Equal(s.Permissions, []string{"locations:read"}) {
		t.Errorf("after switching to B: role = %v, permissions = %v", s.Role, s.Permissions)
	}
	assertActive("after switching to B", orgB, nil)
	testdb.AssertAudited(t, h.env.Pool, orgB, "session.organisation_switched", &orgB)

	// Requests are now scoped to B: A's location is not found.
	expectProblem(t, c.get("/api/v1/locations/"+locA.String()), http.StatusNotFound, "not_found")
}

// access-control spec "Restricted member switches active location".
func TestSwitchLocation(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Switch Location Org")
	other := testdb.CreateOrg(t, h.env.Pool, "Someone Else")
	x := testdb.CreateLocation(t, h.env.Pool, org, "X")
	y := testdb.CreateLocation(t, h.env.Pool, org, "Y")
	z := testdb.CreateLocation(t, h.env.Pool, org, "Z")
	foreign := testdb.CreateLocation(t, h.env.Pool, other, "Foreign")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	staff := testdb.CreateMember(t, h.env.Pool, org, access.RoleFrontDesk, x, y)
	c := h.signedIn(t, staff.Email)

	switchTo := func(loc any) resp {
		return c.post("/api/v1/auth/session/location", map[string]any{"location_id": loc})
	}
	assertActive := func(when string, want *uuid.UUID) {
		t.Helper()
		got := c.session(t).ActiveLocationId
		if (want == nil) != (got == nil) || (want != nil && *want != *got) {
			t.Fatalf("%s: active location = %v, want %v", when, got, want)
		}
	}

	expectProblem(t, switchTo(z), http.StatusForbidden, "location_not_accessible")
	assertActive("after a rejected switch", nil)

	var s gen.SessionInfo
	expect(t, switchTo(x), http.StatusOK, &s)
	if s.ActiveLocationId == nil || *s.ActiveLocationId != x || !sameIDs(s.AccessibleLocationIds, []uuid.UUID{x, y}) {
		t.Fatalf("after switching to X: active = %v, accessible = %v", s.ActiveLocationId, s.AccessibleLocationIds)
	}
	assertActive("after switching to X", &x)

	expectProblem(t, switchTo(z), http.StatusForbidden, "location_not_accessible")
	assertActive("after a rejected switch away from X", &x)
	expectProblem(t, switchTo(foreign), http.StatusNotFound, "not_found")
	expectProblem(t, switchTo(uuid.New()), http.StatusNotFound, "not_found")
	assertActive("after switching to another organisation's location", &x)

	// An archived location can't become the active one.
	expect(t, h.signedIn(t, owner.Email).post("/api/v1/locations/"+y.String()+"/archive", nil), http.StatusOK, nil)
	expectProblem(t, switchTo(y), http.StatusForbidden, "location_not_accessible")
	assertActive("after switching to an archived location", &x)

	// null clears the active location.
	expect(t, switchTo(nil), http.StatusOK, &s)
	if s.ActiveLocationId != nil {
		t.Errorf("after clearing: active location = %v", s.ActiveLocationId)
	}
	assertActive("after clearing", nil)

	expectProblem(t, h.client().post("/api/v1/auth/session/location", map[string]any{"location_id": x}), http.StatusUnauthorized, "unauthorized")
}
