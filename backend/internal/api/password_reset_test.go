package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"dentalsaas/internal/access"
	"dentalsaas/internal/identity"
	"dentalsaas/internal/platform/testdb"
)

const newPassword = "a-brand-new-passphrase"

func resetBody(token, password string) map[string]string {
	return map[string]string{"token": token, "password": password}
}

// user-auth spec "Request a reset": the same response whether or not the account exists.
func TestPasswordResetRequestDoesNotRevealAccounts(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Reset Request Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	unknown := testdb.Email(t, "nobody")
	c := h.client()

	known := c.post("/api/v1/auth/password-reset/request", map[string]string{"email": owner.Email})
	missing := c.post("/api/v1/auth/password-reset/request", map[string]string{"email": unknown})
	expect(t, known, http.StatusAccepted, nil)
	expect(t, missing, http.StatusAccepted, nil)
	if string(known.Body) != string(missing.Body) || known.Header.Get("Content-Type") != missing.Header.Get("Content-Type") {
		t.Errorf("responses differ: known %q (%s), unknown %q (%s)",
			known.Body, known.Header.Get("Content-Type"), missing.Body, missing.Header.Get("Content-Type"))
	}

	// Mail goes only to the real account, with a link on the app origin in both parts.
	if _, ok := h.mail.Last(unknown); ok {
		t.Error("a reset email was sent to an address with no account")
	}
	m, ok := h.mail.Last(owner.Email)
	if !ok {
		t.Fatal("no reset email sent to the account holder")
	}
	link := appOrigin + "/reset-password/" + h.tokenFromMail(t, owner.Email)
	if !strings.Contains(m.Text, link) || !strings.Contains(m.HTML, link) || m.Subject == "" {
		t.Errorf("reset email lacks the link %s: subject %q\ntext: %s\nhtml: %s", link, m.Subject, m.Text, m.HTML)
	}
	if n := h.countTargetAudit(t, "auth.password_reset_requested", owner.UserID); n != 1 {
		t.Errorf("auth.password_reset_requested events = %d, want 1", n)
	}

	expectProblem(t, c.post("/api/v1/auth/password-reset/request", map[string]string{"email": "  "}), http.StatusUnprocessableEntity, "validation_failed")
}

// user-auth spec "Complete a reset": password changed, all sessions revoked, link single-use.
func TestPasswordResetCompleteRevokesSessionsAndIsSingleUse(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Reset Complete Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	bystander := testdb.CreateMember(t, h.env.Pool, org, access.RoleAdmin)
	laptop := h.signedIn(t, owner.Email)
	phone := h.signedIn(t, owner.Email)
	bystanderSession := h.signedIn(t, bystander.Email)

	anon := h.client()
	expect(t, anon.post("/api/v1/auth/password-reset/request", map[string]string{"email": owner.Email}), http.StatusAccepted, nil)
	token := h.tokenFromMail(t, owner.Email)

	// A password that fails the policy is rejected and doesn't use up the link.
	p := expectProblem(t, anon.post("/api/v1/auth/password-reset/complete", resetBody(token, "short")), http.StatusUnprocessableEntity, "validation_failed")
	if len(p.Errors) != 1 || p.Errors[0].Field != "password" {
		t.Errorf("errors = %+v, want one on password", p.Errors)
	}
	expectProblem(t, anon.post("/api/v1/auth/password-reset/complete", resetBody(token, owner.Email)), http.StatusUnprocessableEntity, "validation_failed")
	laptop.session(t)

	expect(t, anon.post("/api/v1/auth/password-reset/complete", resetBody(token, newPassword)), http.StatusNoContent, nil)
	if n := h.countTargetAudit(t, "auth.password_reset_completed", owner.UserID); n != 1 {
		t.Errorf("auth.password_reset_completed events = %d, want 1", n)
	}

	// Every existing session of that user is revoked; nobody else's is.
	expectProblem(t, laptop.get("/api/v1/auth/session"), http.StatusUnauthorized, "unauthorized")
	expectProblem(t, phone.get("/api/v1/locations"), http.StatusUnauthorized, "unauthorized")
	bystanderSession.session(t)

	// Only the new password works.
	expectProblem(t, anon.post("/api/v1/auth/login", loginBody(owner.Email, testdb.Password)), http.StatusUnauthorized, "unauthorized")
	h.client().login(t, owner.Email, newPassword)

	// The link can't be used again.
	expectProblem(t, anon.post("/api/v1/auth/password-reset/complete", resetBody(token, "yet-another-passphrase")), http.StatusGone, "reset_link_invalid")
	h.client().login(t, owner.Email, newPassword)
}

func TestPasswordResetRejectsExpiredAndUnknownTokens(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Reset Expiry Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	c := h.client()

	expectProblem(t, c.post("/api/v1/auth/password-reset/complete", resetBody("no-such-token", newPassword)), http.StatusGone, "reset_link_invalid")

	expect(t, c.post("/api/v1/auth/password-reset/request", map[string]string{"email": owner.Email}), http.StatusAccepted, nil)
	token := h.tokenFromMail(t, owner.Email)
	h.clock.Advance(identity.PasswordResetTTL + time.Minute)
	expectProblem(t, c.post("/api/v1/auth/password-reset/complete", resetBody(token, newPassword)), http.StatusGone, "reset_link_invalid")

	// Nothing changed: the original password still works.
	h.client().login(t, owner.Email, testdb.Password)
	if n := h.countTargetAudit(t, "auth.password_reset_completed", owner.UserID); n != 0 {
		t.Errorf("auth.password_reset_completed events = %d, want 0", n)
	}
}

// Completing a reset lifts a sign-in lockout, so a locked-out user has a way back in.
func TestPasswordResetClearsLockout(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Reset Lockout Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	c := h.client()
	for range 5 {
		expectProblem(t, c.post("/api/v1/auth/login", loginBody(owner.Email, "wrong-password-here")), http.StatusUnauthorized, "unauthorized")
	}
	expectProblem(t, c.post("/api/v1/auth/login", loginBody(owner.Email, testdb.Password)), http.StatusTooManyRequests, "too_many_requests")

	expect(t, c.post("/api/v1/auth/password-reset/request", map[string]string{"email": owner.Email}), http.StatusAccepted, nil)
	expect(t, c.post("/api/v1/auth/password-reset/complete", resetBody(h.tokenFromMail(t, owner.Email), newPassword)), http.StatusNoContent, nil)
	c.login(t, owner.Email, newPassword)
}
