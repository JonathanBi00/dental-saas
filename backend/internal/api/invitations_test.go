package api_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"dentalsaas/internal/access"
	"dentalsaas/internal/api/gen"
	"dentalsaas/internal/identity"
	"dentalsaas/internal/platform/testdb"
)

func invite(t *testing.T, c *client, email, role string, locations ...uuid.UUID) gen.Invitation {
	t.Helper()
	body := map[string]any{"email": email, "role": role}
	if len(locations) > 0 {
		body["location_ids"] = locations
	}
	var inv gen.Invitation
	expect(t, c.post("/api/v1/invitations", body), http.StatusCreated, &inv)
	return inv
}

func pendingInvitationIDs(t *testing.T, c *client) []uuid.UUID {
	t.Helper()
	var list gen.InvitationList
	expect(t, c.get("/api/v1/invitations"), http.StatusOK, &list)
	out := make([]uuid.UUID, len(list.Items))
	for i, x := range list.Items {
		out[i] = x.Id
	}
	return out
}

func listMembers(t *testing.T, c *client) []gen.Member {
	t.Helper()
	var list gen.MemberList
	expect(t, c.get("/api/v1/members?limit=200"), http.StatusOK, &list)
	return list.Items
}

func acceptPath(token string) string { return "/api/v1/auth/invitations/" + token + "/accept" }

// user-auth spec "Invite a new person" and "Accept an invitation as a new user".
func TestInviteAndAcceptAsNewUser(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Invite Org")
	parramatta := testdb.CreateLocation(t, h.env.Pool, org, "Parramatta")
	testdb.CreateLocation(t, h.env.Pool, org, "Sydney CBD")
	admin := testdb.CreateMember(t, h.env.Pool, org, access.RoleAdmin)
	c := h.signedIn(t, admin.Email)
	email := testdb.Email(t, "new")

	inv := invite(t, c, email, "front_desk", parramatta)
	if inv.Email != email || inv.Role != "front_desk" || !slices.Equal(inv.LocationIds, []uuid.UUID{parramatta}) ||
		inv.InvitedBy == nil || *inv.InvitedBy != admin.UserID || inv.AcceptedAt != nil || inv.RevokedAt != nil {
		t.Fatalf("invitation = %+v", inv)
	}
	if d := inv.ExpiresAt.Sub(h.clock.Now()); d < identity.InvitationTTL-time.Minute || d > identity.InvitationTTL+time.Minute {
		t.Errorf("expires in %v, want 7 days", d)
	}
	testdb.AssertAudited(t, h.env.Pool, org, "invitation.created", &inv.Id)
	if got := pendingInvitationIDs(t, c); !slices.Equal(got, []uuid.UUID{inv.Id}) {
		t.Errorf("pending invitations = %v, want [%s]", got, inv.Id)
	}

	// The email carries the link in both parts; the token is never returned by the API.
	token := h.tokenFromMail(t, email)
	m, _ := h.mail.Last(email)
	link := appOrigin + "/invite/" + token
	if !strings.Contains(m.Text, link) || !strings.Contains(m.HTML, link) || !strings.Contains(m.Subject, "Invite Org") {
		t.Errorf("invitation email: subject %q\ntext: %s", m.Subject, m.Text)
	}

	// Public preview by token.
	anon := h.client()
	var pv gen.InvitationPreview
	expect(t, anon.get("/api/v1/auth/invitations/"+token), http.StatusOK, &pv)
	if pv.OrganisationName != "Invite Org" || pv.Email != email || pv.Role != "front_desk" || pv.ExistingAccount {
		t.Errorf("preview = %+v", pv)
	}

	// A new account needs a name and a password that passes the policy; failures create nothing.
	expectProblem(t, anon.post(acceptPath(token), map[string]any{"password": newPassword}), http.StatusUnprocessableEntity, "validation_failed")
	expectProblem(t, anon.post(acceptPath(token), map[string]any{"name": "Nina New", "password": "short"}), http.StatusUnprocessableEntity, "validation_failed")
	if n := len(listMembers(t, c)); n != 1 {
		t.Fatalf("members after failed accepts = %d, want 1", n)
	}

	// Accept: account and membership created, and the invitee is signed in.
	r := anon.post(acceptPath(token), map[string]any{"name": "  Nina New ", "password": newPassword})
	var s gen.SessionInfo
	expect(t, r, http.StatusOK, &s)
	if !strings.Contains(r.Header.Get("Set-Cookie"), identity.CookieName+"=") {
		t.Errorf("accept should set the session cookie, got %q", r.Header.Get("Set-Cookie"))
	}
	if s.User.Email != email || s.User.Name != "Nina New" {
		t.Errorf("user = %+v", s.User)
	}
	if s.ActiveOrganisationId == nil || *s.ActiveOrganisationId != org || s.Role == nil || *s.Role != "front_desk" {
		t.Errorf("session = %+v", s)
	}
	if !slices.Equal(s.AccessibleLocationIds, []uuid.UUID{parramatta}) {
		t.Errorf("accessible locations = %v, want only Parramatta", s.AccessibleLocationIds)
	}
	var locs gen.LocationList
	expect(t, anon.get("/api/v1/locations"), http.StatusOK, &locs)
	if !slices.Equal(locationIDs(locs), []uuid.UUID{parramatta}) {
		t.Errorf("invitee lists %v, want only Parramatta", locationIDs(locs))
	}
	testdb.AssertAudited(t, h.env.Pool, org, "invitation.accepted", &inv.Id)

	// The organisation now has the member, and the invitation is no longer pending.
	members := listMembers(t, c)
	i := slices.IndexFunc(members, func(m gen.Member) bool { return m.Email == email })
	if len(members) != 2 || i < 0 {
		t.Fatalf("members = %+v, want the admin and the invitee", members)
	}
	if m := members[i]; m.Role != "front_desk" || m.Status != "active" || !slices.Equal(m.LocationIds, []uuid.UUID{parramatta}) || m.UserId != s.User.Id {
		t.Errorf("new member = %+v", m)
	}
	if got := pendingInvitationIDs(t, c); len(got) != 0 {
		t.Errorf("pending invitations after accept = %v, want none", got)
	}
	h.client().login(t, email, newPassword)

	// user-auth spec "Expired or reused invitation": the link is single-use.
	expectProblem(t, h.client().post(acceptPath(token), map[string]any{"name": "Again", "password": newPassword}), http.StatusGone, "invitation_invalid")
	expectProblem(t, h.client().get("/api/v1/auth/invitations/"+token), http.StatusGone, "invitation_invalid")
	if n := len(listMembers(t, c)); n != 2 {
		t.Errorf("members after reuse = %d, want 2", n)
	}
}

// Task 4.10: an invitation to someone who already has an account (matched case-insensitively)
// adds a membership to that account instead of creating a second one.
func TestAcceptInvitationAsExistingUser(t *testing.T) {
	h := newHarness(t)
	orgA := testdb.CreateOrg(t, h.env.Pool, "Existing A")
	orgB := testdb.CreateOrg(t, h.env.Pool, "Existing B")
	existing := testdb.CreateMember(t, h.env.Pool, orgA, access.RolePractitioner)
	ownerB := testdb.CreateMember(t, h.env.Pool, orgB, access.RoleOwner)
	c := h.signedIn(t, ownerB.Email)

	shouted := strings.ToUpper(existing.Email)
	inv := invite(t, c, shouted, "admin")
	token := h.tokenFromMail(t, shouted)

	anon := h.client()
	var pv gen.InvitationPreview
	expect(t, anon.get("/api/v1/auth/invitations/"+token), http.StatusOK, &pv)
	if !pv.ExistingAccount {
		t.Errorf("preview = %+v, want existing_account", pv)
	}

	// The account's own password is required; a wrong one leaves the invitation pending.
	expectProblem(t, anon.post(acceptPath(token), map[string]any{"password": "not-their-password"}), http.StatusUnauthorized, "unauthorized")
	if got := pendingInvitationIDs(t, c); !slices.Equal(got, []uuid.UUID{inv.Id}) {
		t.Errorf("pending after wrong password = %v, want [%s]", got, inv.Id)
	}

	var s gen.SessionInfo
	expect(t, anon.post(acceptPath(token), map[string]any{"password": testdb.Password}), http.StatusOK, &s)
	if s.User.Id != existing.UserID {
		t.Fatalf("accepted as user %s, want the existing account %s", s.User.Id, existing.UserID)
	}
	if s.User.Email != existing.Email {
		t.Errorf("account email changed to %q", s.User.Email)
	}
	if len(s.Memberships) != 2 || s.ActiveOrganisationId == nil || *s.ActiveOrganisationId != orgB || s.Role == nil || *s.Role != "admin" {
		t.Errorf("session = %+v, want two memberships with B active as admin", s)
	}

	// Exactly one account for that email: org B's member is the same user, and signing in
	// (in either case) reaches both memberships.
	members := listMembers(t, c)
	n := 0
	for _, m := range members {
		if strings.EqualFold(m.Email, existing.Email) {
			n++
			if m.UserId != existing.UserID {
				t.Errorf("org B member has user id %s, want %s", m.UserId, existing.UserID)
			}
		}
	}
	if n != 1 {
		t.Errorf("org B has %d members with that email, want 1", n)
	}
	if got := h.client().login(t, shouted, testdb.Password); got.User.Id != existing.UserID || len(got.Memberships) != 2 {
		t.Errorf("sign-in after accept = %+v", got)
	}

	// Someone who is already an active member can't be invited again.
	expectProblem(t, c.post("/api/v1/invitations", map[string]any{"email": existing.Email, "role": "front_desk"}), http.StatusConflict, "already_member")
}

// user-auth spec "Expired or reused invitation".
func TestExpiredRevokedAndUnknownInvitations(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Invalid Invites Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	c := h.signedIn(t, owner.Email)
	accept := map[string]any{"name": "Late Comer", "password": newPassword}

	expectProblem(t, h.client().get("/api/v1/auth/invitations/no-such-token"), http.StatusNotFound, "not_found")
	expectProblem(t, h.client().post(acceptPath("no-such-token"), accept), http.StatusNotFound, "not_found")

	// Revoked.
	revoked := testdb.Email(t, "revoked")
	inv := invite(t, c, revoked, "practitioner")
	revokedToken := h.tokenFromMail(t, revoked)
	var got gen.Invitation
	expect(t, c.post("/api/v1/invitations/"+inv.Id.String()+"/revoke", nil), http.StatusOK, &got)
	if got.RevokedAt == nil {
		t.Error("revoke response has no revoked_at")
	}
	testdb.AssertAudited(t, h.env.Pool, org, "invitation.revoked", &inv.Id)
	expectProblem(t, c.post("/api/v1/invitations/"+inv.Id.String()+"/revoke", nil), http.StatusConflict, "invitation_not_pending")
	expectProblem(t, c.post("/api/v1/invitations/"+uuid.NewString()+"/revoke", nil), http.StatusNotFound, "not_found")
	expectProblem(t, h.client().get("/api/v1/auth/invitations/"+revokedToken), http.StatusGone, "invitation_invalid")
	expectProblem(t, h.client().post(acceptPath(revokedToken), accept), http.StatusGone, "invitation_invalid")

	// Inviting the same address again replaces the earlier pending invitation.
	twice := testdb.Email(t, "twice")
	first := invite(t, c, twice, "practitioner")
	firstToken := h.tokenFromMail(t, twice)
	second := invite(t, c, twice, "front_desk")
	secondToken := h.tokenFromMail(t, twice)
	if firstToken == secondToken {
		t.Fatal("re-inviting reused the token")
	}
	expectProblem(t, h.client().post(acceptPath(firstToken), accept), http.StatusGone, "invitation_invalid")
	if ids := pendingInvitationIDs(t, c); slices.Contains(ids, first.Id) || !slices.Contains(ids, second.Id) {
		t.Errorf("pending = %v, want %s but not %s", ids, second.Id, first.Id)
	}

	// Expired: a minute past 7 days. (The owner's own session has expired too by then.)
	h.clock.Advance(identity.InvitationTTL + time.Minute)
	expectProblem(t, h.client().get("/api/v1/auth/invitations/"+secondToken), http.StatusGone, "invitation_invalid")
	expectProblem(t, h.client().post(acceptPath(secondToken), accept), http.StatusGone, "invitation_invalid")

	// Nothing was created by any of the rejected attempts.
	c = h.signedIn(t, owner.Email)
	if members := listMembers(t, c); len(members) != 1 {
		t.Errorf("members = %+v, want only the owner", members)
	}
	if ids := pendingInvitationIDs(t, c); len(ids) != 0 {
		t.Errorf("pending invitations after expiry = %v, want none", ids)
	}
	for _, email := range []string{revoked, twice} {
		expectProblem(t, h.client().post("/api/v1/auth/login", loginBody(email, newPassword)), http.StatusUnauthorized, "unauthorized")
	}
	if n := len(testdb.AuditEvents(t, h.env.Pool, org, "invitation.accepted", nil)); n != 0 {
		t.Errorf("invitation.accepted events = %d, want 0", n)
	}
	expectProblem(t, c.post("/api/v1/invitations/"+second.Id.String()+"/revoke", nil), http.StatusConflict, "invitation_not_pending")
}

func TestInvitationPermissionsAndValidation(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Invite Rules Org")
	other := testdb.CreateOrg(t, h.env.Pool, "Invite Rules Other Org")
	foreignLoc := testdb.CreateLocation(t, h.env.Pool, other, "Foreign")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	admin := testdb.CreateMember(t, h.env.Pool, org, access.RoleAdmin)
	staff := testdb.CreateMember(t, h.env.Pool, org, access.RoleFrontDesk)
	otherOwner := testdb.CreateMember(t, h.env.Pool, other, access.RoleOwner)
	ownerC, adminC, staffC := h.signedIn(t, owner.Email), h.signedIn(t, admin.Email), h.signedIn(t, staff.Email)
	body := func(role string) map[string]any {
		return map[string]any{"email": testdb.Email(t, "invitee"), "role": role}
	}

	// Front desk and practitioners can't manage invitations.
	expectProblem(t, staffC.post("/api/v1/invitations", body("front_desk")), http.StatusForbidden, "permission_denied")
	expectProblem(t, staffC.get("/api/v1/invitations"), http.StatusForbidden, "permission_denied")
	expectProblem(t, h.client().post("/api/v1/invitations", body("front_desk")), http.StatusUnauthorized, "unauthorized")

	// Only owners can invite owners, or revoke an owner invitation.
	expectProblem(t, adminC.post("/api/v1/invitations", body("owner")), http.StatusForbidden, "owner_only")
	ownerInvite := invite(t, ownerC, testdb.Email(t, "co-owner"), "owner")
	expectProblem(t, adminC.post("/api/v1/invitations/"+ownerInvite.Id.String()+"/revoke", nil), http.StatusForbidden, "owner_only")
	expectProblem(t, staffC.post("/api/v1/invitations/"+ownerInvite.Id.String()+"/revoke", nil), http.StatusForbidden, "permission_denied")

	// Invitations are invisible to other organisations.
	if ids := pendingInvitationIDs(t, h.signedIn(t, otherOwner.Email)); len(ids) != 0 {
		t.Errorf("another organisation sees invitations %v", ids)
	}
	expectProblem(t, h.signedIn(t, otherOwner.Email).post("/api/v1/invitations/"+ownerInvite.Id.String()+"/revoke", nil), http.StatusNotFound, "not_found")

	// Validation.
	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"malformed email":         {map[string]any{"email": "not-an-email", "role": "admin"}, "email"},
		"unknown role":            {map[string]any{"email": testdb.Email(t, "x"), "role": "janitor"}, "role"},
		"locations for an admin":  {map[string]any{"email": testdb.Email(t, "x"), "role": "admin", "location_ids": []uuid.UUID{foreignLoc}}, "location_ids"},
		"another org's location":  {map[string]any{"email": testdb.Email(t, "x"), "role": "front_desk", "location_ids": []uuid.UUID{foreignLoc}}, "location_ids"},
		"a location that is made": {map[string]any{"email": testdb.Email(t, "x"), "role": "front_desk", "location_ids": []uuid.UUID{uuid.New()}}, "location_ids"},
	} {
		r := ownerC.post("/api/v1/invitations", tc.body)
		if name == "unknown role" && r.Status == http.StatusBadRequest {
			continue // rejected by the OpenAPI enum before reaching the service
		}
		p := expectProblem(t, r, http.StatusUnprocessableEntity, "validation_failed")
		if len(p.Errors) != 1 || p.Errors[0].Field != tc.field {
			t.Errorf("%s: errors = %+v, want one on %s", name, p.Errors, tc.field)
		}
	}
	if ids := pendingInvitationIDs(t, ownerC); !slices.Equal(ids, []uuid.UUID{ownerInvite.Id}) {
		t.Errorf("pending = %v, want only the owner invitation", ids)
	}
}
