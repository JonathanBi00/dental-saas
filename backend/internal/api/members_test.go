package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"dentalsaas/internal/access"
	"dentalsaas/internal/api/gen"
	"dentalsaas/internal/platform/testdb"
)

func memberPath(id uuid.UUID) string { return "/api/v1/members/" + id.String() }

func findMember(t *testing.T, c *client, id uuid.UUID) gen.Member {
	t.Helper()
	for _, m := range listMembers(t, c) {
		if m.Id == id {
			return m
		}
	}
	t.Fatalf("member %s not in the list", id)
	return gen.Member{}
}

// ------------------------------------------- 5.2 permissions on location endpoints

// access-control spec "Role determines permitted actions".
func TestFrontDeskAndPractitionerCannotManageLocations(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Read Only Org")
	loc := testdb.CreateLocation(t, h.env.Pool, org, "Clinic")

	for _, role := range []access.Role{access.RoleFrontDesk, access.RolePractitioner} {
		c := h.signedIn(t, testdb.CreateMember(t, h.env.Pool, org, role).Email)
		expectProblem(t, c.post("/api/v1/locations", map[string]any{"name": "New", "timezone": "Australia/Sydney"}), http.StatusForbidden, "permission_denied")
		expectProblem(t, c.patch("/api/v1/locations/"+loc.String(), map[string]any{"name": "Renamed"}), http.StatusForbidden, "permission_denied")
		expectProblem(t, c.post("/api/v1/locations/"+loc.String()+"/archive", nil), http.StatusForbidden, "permission_denied")

		// They can still read.
		var got gen.Location
		expect(t, c.get("/api/v1/locations/"+loc.String()), http.StatusOK, &got)
		if got.Name != "Clinic" || got.ArchivedAt != nil {
			t.Errorf("%s: location changed: %+v", role, got)
		}
		if s := c.session(t); !slices.Equal(s.Permissions, []string{"locations:read"}) {
			t.Errorf("%s: permissions = %v, want [locations:read]", role, s.Permissions)
		}
	}
	for _, action := range []string{"location.created", "location.updated", "location.archived"} {
		if n := len(testdb.AuditEvents(t, h.env.Pool, org, action, nil)); n != 0 {
			t.Errorf("%s events = %d, want 0", action, n)
		}
	}

	// An admin can do all three.
	c := h.signedIn(t, testdb.CreateMember(t, h.env.Pool, org, access.RoleAdmin).Email)
	expect(t, c.post("/api/v1/locations", map[string]any{"name": "New", "timezone": "Australia/Sydney"}), http.StatusCreated, nil)
	expect(t, c.patch("/api/v1/locations/"+loc.String(), map[string]any{"name": "Renamed"}), http.StatusOK, nil)
	expect(t, c.post("/api/v1/locations/"+loc.String()+"/archive", nil), http.StatusOK, nil)
}

// access-control spec "Restricted member lists locations": anything outside their locations
// is treated as not found.
func TestRestrictedPractitionerSeesOnlyTheirLocations(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Scoped Org")
	parramatta := testdb.CreateLocation(t, h.env.Pool, org, "Parramatta")
	cbd := testdb.CreateLocation(t, h.env.Pool, org, "Sydney CBD")
	restricted := testdb.CreateMember(t, h.env.Pool, org, access.RolePractitioner, parramatta)
	unrestricted := testdb.CreateMember(t, h.env.Pool, org, access.RolePractitioner)

	c := h.signedIn(t, restricted.Email)
	for _, path := range []string{"/api/v1/locations", "/api/v1/locations?include_archived=true", "/api/v1/locations?updated_since=2000-01-01T00:00:00Z"} {
		var list gen.LocationList
		expect(t, c.get(path), http.StatusOK, &list)
		if !slices.Equal(locationIDs(list), []uuid.UUID{parramatta}) {
			t.Errorf("GET %s = %v, want only Parramatta", path, locationIDs(list))
		}
	}
	expect(t, c.get("/api/v1/locations/"+parramatta.String()), http.StatusOK, nil)
	expectProblem(t, c.get("/api/v1/locations/"+cbd.String()), http.StatusNotFound, "not_found")
	if s := c.session(t); !slices.Equal(s.AccessibleLocationIds, []uuid.UUID{parramatta}) {
		t.Errorf("accessible locations = %v, want only Parramatta", s.AccessibleLocationIds)
	}

	// A practitioner without restrictions sees every location.
	var list gen.LocationList
	expect(t, h.signedIn(t, unrestricted.Email).get("/api/v1/locations"), http.StatusOK, &list)
	if !sameIDs(locationIDs(list), []uuid.UUID{parramatta, cbd}) {
		t.Errorf("unrestricted practitioner lists %v, want both", locationIDs(list))
	}
}

// ------------------------------------------------------------ 5.3 member endpoints

func TestListMembers(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Members Org")
	other := testdb.CreateOrg(t, h.env.Pool, "Members Other Org")
	loc := testdb.CreateLocation(t, h.env.Pool, org, "Clinic")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	admin := testdb.CreateMember(t, h.env.Pool, org, access.RoleAdmin)
	staff := testdb.CreateMember(t, h.env.Pool, org, access.RoleFrontDesk, loc)
	outsider := testdb.CreateMember(t, h.env.Pool, other, access.RoleOwner)

	c := h.signedIn(t, admin.Email)
	members := listMembers(t, c)
	if len(members) != 3 {
		t.Fatalf("members = %d, want 3 (none from another organisation)", len(members))
	}
	for _, want := range []struct {
		m    testdb.Member
		role gen.Role
		locs []uuid.UUID
	}{{owner, "owner", []uuid.UUID{}}, {admin, "admin", []uuid.UUID{}}, {staff, "front_desk", []uuid.UUID{loc}}} {
		m := findMember(t, c, want.m.MembershipID)
		if m.UserId != want.m.UserID || m.Email != want.m.Email || m.Role != want.role || m.Status != "active" || !slices.Equal(m.LocationIds, want.locs) {
			t.Errorf("member = %+v, want role %s with locations %v", m, want.role, want.locs)
		}
	}

	// Pagination walks every member exactly once.
	var seen []uuid.UUID
	path := "/api/v1/members?limit=2"
	for range 3 {
		var page gen.MemberList
		expect(t, c.get(path), http.StatusOK, &page)
		for _, m := range page.Items {
			seen = append(seen, m.Id)
		}
		if page.NextCursor == nil {
			break
		}
		path = "/api/v1/members?limit=2&cursor=" + *page.NextCursor
	}
	if !sameIDs(seen, []uuid.UUID{owner.MembershipID, admin.MembershipID, staff.MembershipID}) {
		t.Errorf("paged members = %v", seen)
	}

	expectProblem(t, h.signedIn(t, staff.Email).get("/api/v1/members"), http.StatusForbidden, "permission_denied")
	expectProblem(t, h.client().get("/api/v1/members"), http.StatusUnauthorized, "unauthorized")

	// Another organisation's member is not found, whatever the action.
	expectProblem(t, c.patch(memberPath(outsider.MembershipID), map[string]any{"role": "front_desk"}), http.StatusNotFound, "not_found")
	expectProblem(t, c.post(memberPath(outsider.MembershipID)+"/deactivate", nil), http.StatusNotFound, "not_found")
	expectProblem(t, c.post(memberPath(outsider.MembershipID)+"/reactivate", nil), http.StatusNotFound, "not_found")
	if s := h.signedIn(t, outsider.Email).session(t); s.Role == nil || *s.Role != "owner" {
		t.Errorf("outsider's membership changed: %+v", s)
	}
}

// access-control spec "Admin manages members".
func TestAdminChangesRoleAndLocationRestrictions(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Role Change Org")
	other := testdb.CreateOrg(t, h.env.Pool, "Role Change Other Org")
	parramatta := testdb.CreateLocation(t, h.env.Pool, org, "Parramatta")
	cbd := testdb.CreateLocation(t, h.env.Pool, org, "Sydney CBD")
	foreign := testdb.CreateLocation(t, h.env.Pool, other, "Foreign")
	admin := testdb.CreateMember(t, h.env.Pool, org, access.RoleAdmin)
	staff := testdb.CreateMember(t, h.env.Pool, org, access.RoleFrontDesk)
	c := h.signedIn(t, admin.Email)
	staffC := h.signedIn(t, staff.Email)
	path := memberPath(staff.MembershipID)

	// Role change, recorded with old and new values.
	var m gen.Member
	expect(t, c.patch(path, map[string]any{"role": "practitioner"}), http.StatusOK, &m)
	if m.Role != "practitioner" || m.Id != staff.MembershipID {
		t.Fatalf("after role change = %+v", m)
	}
	ev := testdb.AssertAudited(t, h.env.Pool, org, "membership.updated", &staff.MembershipID)
	var details struct {
		Changes map[string]struct{ Old, New any } `json:"changes"`
	}
	if err := json.Unmarshal(ev.Details, &details); err != nil {
		t.Fatal(err)
	}
	if ch := details.Changes["role"]; ch.Old != "front_desk" || ch.New != "practitioner" {
		t.Errorf("audit changes = %s, want role front_desk -> practitioner", ev.Details)
	}
	if ev.ActorUserID == nil || *ev.ActorUserID != admin.UserID {
		t.Errorf("audit actor = %v, want the admin", ev.ActorUserID)
	}
	if s := staffC.session(t); s.Role == nil || *s.Role != "practitioner" {
		t.Errorf("member's session role = %v, want practitioner on the next request", s.Role)
	}

	// Restrict to one location: applies to the member's next request.
	expect(t, c.patch(path, map[string]any{"location_ids": []uuid.UUID{parramatta, parramatta}}), http.StatusOK, &m)
	if !slices.Equal(m.LocationIds, []uuid.UUID{parramatta}) {
		t.Fatalf("location_ids = %v, want [Parramatta]", m.LocationIds)
	}
	var list gen.LocationList
	expect(t, staffC.get("/api/v1/locations"), http.StatusOK, &list)
	if !slices.Equal(locationIDs(list), []uuid.UUID{parramatta}) {
		t.Errorf("restricted member lists %v, want only Parramatta", locationIDs(list))
	}

	// Invalid restrictions are rejected and change nothing.
	for name, ids := range map[string][]uuid.UUID{"another org's location": {foreign}, "unknown location": {cbd, uuid.New()}} {
		p := expectProblem(t, c.patch(path, map[string]any{"location_ids": ids}), http.StatusUnprocessableEntity, "validation_failed")
		if len(p.Errors) != 1 || p.Errors[0].Field != "location_ids" {
			t.Errorf("%s: errors = %+v, want one on location_ids", name, p.Errors)
		}
	}
	expectProblem(t, c.patch(path, map[string]any{"role": "admin", "location_ids": []uuid.UUID{cbd}}), http.StatusUnprocessableEntity, "validation_failed")
	if got := findMember(t, c, staff.MembershipID); got.Role != "practitioner" || !slices.Equal(got.LocationIds, []uuid.UUID{parramatta}) {
		t.Errorf("after rejected updates = %+v", got)
	}

	// A no-op update writes no audit entry.
	before := len(testdb.AuditEvents(t, h.env.Pool, org, "membership.updated", &staff.MembershipID))
	expect(t, c.patch(path, map[string]any{"role": "practitioner"}), http.StatusOK, nil)
	if after := len(testdb.AuditEvents(t, h.env.Pool, org, "membership.updated", &staff.MembershipID)); after != before {
		t.Errorf("no-op update added %d audit entries", after-before)
	}

	// An empty list lifts the restriction; promoting to admin always does.
	expect(t, c.patch(path, map[string]any{"location_ids": []uuid.UUID{}}), http.StatusOK, &m)
	if len(m.LocationIds) != 0 {
		t.Errorf("after clearing: location_ids = %v", m.LocationIds)
	}
	expect(t, staffC.get("/api/v1/locations"), http.StatusOK, &list)
	if len(list.Items) != 2 {
		t.Errorf("unrestricted member lists %d locations, want 2", len(list.Items))
	}
	expect(t, c.patch(path, map[string]any{"location_ids": []uuid.UUID{cbd}}), http.StatusOK, nil)
	expect(t, c.patch(path, map[string]any{"role": "admin"}), http.StatusOK, &m)
	if m.Role != "admin" || len(m.LocationIds) != 0 {
		t.Errorf("after promotion to admin = %+v, want no location restrictions", m)
	}

	expectProblem(t, h.signedIn(t, testdb.CreateMember(t, h.env.Pool, org, access.RoleFrontDesk).Email).patch(path, map[string]any{"role": "owner"}),
		http.StatusForbidden, "permission_denied")
}

// access-control spec "Admin attempts to create an owner" and "Removing the last owner".
func TestOwnerProtection(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Owner Rules Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	admin := testdb.CreateMember(t, h.env.Pool, org, access.RoleAdmin)
	staff := testdb.CreateMember(t, h.env.Pool, org, access.RoleFrontDesk)
	ownerC, adminC := h.signedIn(t, owner.Email), h.signedIn(t, admin.Email)

	// An admin can't grant owner (to others or themselves), or touch an owner's membership.
	expectProblem(t, adminC.patch(memberPath(staff.MembershipID), map[string]any{"role": "owner"}), http.StatusForbidden, "owner_only")
	expectProblem(t, adminC.patch(memberPath(admin.MembershipID), map[string]any{"role": "owner"}), http.StatusForbidden, "owner_only")
	expectProblem(t, adminC.patch(memberPath(owner.MembershipID), map[string]any{"role": "admin"}), http.StatusForbidden, "owner_only")
	expectProblem(t, adminC.post(memberPath(owner.MembershipID)+"/deactivate", nil), http.StatusForbidden, "owner_only")

	// The only owner can't be demoted or deactivated, even by themselves.
	expectProblem(t, ownerC.patch(memberPath(owner.MembershipID), map[string]any{"role": "admin"}), http.StatusConflict, "last_owner")
	expectProblem(t, ownerC.post(memberPath(owner.MembershipID)+"/deactivate", nil), http.StatusConflict, "last_owner")

	// Nothing changed and nothing was audited.
	for id, role := range map[uuid.UUID]gen.Role{owner.MembershipID: "owner", admin.MembershipID: "admin", staff.MembershipID: "front_desk"} {
		if m := findMember(t, ownerC, id); m.Role != role || m.Status != "active" {
			t.Errorf("member %s = %+v, want active %s", id, m, role)
		}
	}
	for _, action := range []string{"membership.updated", "membership.deactivated"} {
		if n := len(testdb.AuditEvents(t, h.env.Pool, org, action, nil)); n != 0 {
			t.Errorf("%s events = %d, want 0", action, n)
		}
	}

	// An owner can grant owner. With two owners, one can step down, but then the other is
	// the last owner again.
	var m gen.Member
	expect(t, ownerC.patch(memberPath(admin.MembershipID), map[string]any{"role": "owner"}), http.StatusOK, &m)
	if m.Role != "owner" {
		t.Fatalf("after grant = %+v", m)
	}
	expect(t, ownerC.patch(memberPath(owner.MembershipID), map[string]any{"role": "admin"}), http.StatusOK, &m)
	if m.Role != "admin" {
		t.Fatalf("after stepping down = %+v", m)
	}
	// adminC is now the only owner (their role is re-read on every request).
	expectProblem(t, adminC.post(memberPath(admin.MembershipID)+"/deactivate", nil), http.StatusConflict, "last_owner")
	expectProblem(t, ownerC.post(memberPath(admin.MembershipID)+"/deactivate", nil), http.StatusForbidden, "owner_only")
}

func TestDeactivateAndReactivateMember(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Deactivate Org")
	admin := testdb.CreateMember(t, h.env.Pool, org, access.RoleAdmin)
	staff := testdb.CreateMember(t, h.env.Pool, org, access.RoleFrontDesk)
	c, staffC := h.signedIn(t, admin.Email), h.signedIn(t, staff.Email)
	path := memberPath(staff.MembershipID)

	var m gen.Member
	expect(t, c.post(path+"/deactivate", nil), http.StatusOK, &m)
	if m.Status != "deactivated" || m.Role != "front_desk" {
		t.Fatalf("after deactivate = %+v", m)
	}
	testdb.AssertAudited(t, h.env.Pool, org, "membership.deactivated", &staff.MembershipID)
	expectProblem(t, staffC.get("/api/v1/locations"), http.StatusForbidden, "membership_inactive")

	// Deactivating again is a no-op.
	expect(t, c.post(path+"/deactivate", nil), http.StatusOK, &m)
	if n := len(testdb.AuditEvents(t, h.env.Pool, org, "membership.deactivated", &staff.MembershipID)); n != 1 {
		t.Errorf("membership.deactivated events = %d, want 1", n)
	}

	// A new sign-in gets no organisation either.
	if s := h.client().login(t, staff.Email, testdb.Password); s.ActiveOrganisationId != nil {
		t.Errorf("deactivated member signed in with an active organisation: %+v", s)
	}

	expect(t, c.post(path+"/reactivate", nil), http.StatusOK, &m)
	if m.Status != "active" {
		t.Fatalf("after reactivate = %+v", m)
	}
	testdb.AssertAudited(t, h.env.Pool, org, "membership.reactivated", &staff.MembershipID)
	expect(t, staffC.get("/api/v1/locations"), http.StatusOK, nil)

	expectProblem(t, staffC.post(memberPath(admin.MembershipID)+"/deactivate", nil), http.StatusForbidden, "permission_denied")
	expectProblem(t, staffC.post(path+"/reactivate", nil), http.StatusForbidden, "permission_denied")
}
