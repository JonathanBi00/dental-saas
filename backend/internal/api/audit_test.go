package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"dentalsaas/internal/access"
	"dentalsaas/internal/api/gen"
	"dentalsaas/internal/platform/audit"
	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/platform/httpx"
	"dentalsaas/internal/platform/testdb"
)

func auditEvents(t *testing.T, c *client, query url.Values) gen.AuditEventList {
	t.Helper()
	var list gen.AuditEventList
	expect(t, c.get("/api/v1/audit-events?"+query.Encode()), http.StatusOK, &list)
	return list
}

func auditActions(l gen.AuditEventList) []string {
	out := make([]string, len(l.Items))
	for i, e := range l.Items {
		out[i] = e.Action
	}
	return out
}

// ----------------------------------------------------------- 6.3 audit recording

// audit-log spec "Data change is audited" and "Entry links to the request".
func TestAuditEntryContents(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Audit Contents Org")
	loc := testdb.CreateLocation(t, h.env.Pool, org, "Old Name")
	admin := testdb.CreateMember(t, h.env.Pool, org, access.RoleAdmin)
	c := h.signedIn(t, admin.Email)

	// A client-supplied request ID ties the response to the entry.
	reqID := "audit-test-" + uuid.NewString()
	r := c.doWith(map[string]string{"Origin": appOrigin, httpx.RequestIDHeader: reqID, "User-Agent": "audit-test-agent/1.0"},
		http.MethodPatch, "/api/v1/locations/"+loc.String(), map[string]any{"name": "New Name"})
	expect(t, r, http.StatusOK, nil)
	if got := r.Header.Get(httpx.RequestIDHeader); got != reqID {
		t.Fatalf("response request id = %q, want %q", got, reqID)
	}

	list := auditEvents(t, c, url.Values{"action": {"location.updated"}, "target_id": {loc.String()}})
	if len(list.Items) != 1 {
		t.Fatalf("location.updated entries = %d, want 1", len(list.Items))
	}
	e := list.Items[0]
	if e.RequestId != reqID {
		t.Errorf("entry request id = %q, want %q", e.RequestId, reqID)
	}
	if e.Actor == nil || e.Actor.Id != admin.UserID || e.Actor.Email != admin.Email {
		t.Errorf("actor = %+v, want the admin", e.Actor)
	}
	if e.TargetType == nil || *e.TargetType != "location" || e.TargetId == nil || *e.TargetId != loc {
		t.Errorf("target = %v %v, want location %s", e.TargetType, e.TargetId, loc)
	}
	if e.Ip == "" || e.UserAgent != "audit-test-agent/1.0" {
		t.Errorf("ip = %q, user agent = %q", e.Ip, e.UserAgent)
	}
	if _, offset := e.CreatedAt.Zone(); offset != 0 || time.Since(e.CreatedAt).Abs() > time.Minute {
		t.Errorf("created_at = %v, want about now in UTC", e.CreatedAt)
	}
	changes, _ := e.Details["changes"].(map[string]any)
	name, _ := changes["name"].(map[string]any)
	if len(changes) != 1 || name["old"] != "Old Name" || name["new"] != "New Name" {
		t.Errorf("details = %v, want only name changed from Old Name to New Name", e.Details)
	}
}

// audit-log spec "Change fails": the entry is in the same transaction as the change, so a
// rollback removes both.
func TestRolledBackChangeLeavesNoAuditEntry(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Rollback Org")
	loc := testdb.CreateLocation(t, h.env.Pool, org, "Stays Put")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	ctx := context.Background()

	boom := errors.New("fails after the change and its audit entry were written")
	err := h.env.Pool.InTenantTx(ctx, db.Tenant{OrgID: org, UserID: owner.UserID}, func(tx *db.Tx) error {
		if _, err := tx.ArchiveLocation(ctx, loc); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Event{
			OrgID: org, ActorID: owner.UserID, Action: "location.archived", TargetType: "location", TargetID: loc,
		}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTenantTx error = %v, want the injected failure", err)
	}
	if n := len(testdb.AuditEvents(t, h.env.Pool, org, "location.archived", &loc)); n != 0 {
		t.Errorf("location.archived entries after rollback = %d, want 0", n)
	}

	// Over HTTP: a rejected update leaves the record and the log alone.
	c := h.signedIn(t, owner.Email)
	expectProblem(t, c.patch("/api/v1/locations/"+loc.String(), map[string]any{"name": "Moved", "timezone": "Moon/Base"}),
		http.StatusUnprocessableEntity, "validation_failed")
	var got gen.Location
	expect(t, c.get("/api/v1/locations/"+loc.String()), http.StatusOK, &got)
	if got.Name != "Stays Put" || got.ArchivedAt != nil {
		t.Errorf("location after failed changes = %+v", got)
	}
	if list := auditEvents(t, c, url.Values{"target_id": {loc.String()}}); len(list.Items) != 0 {
		t.Errorf("audit entries for the location = %v, want none", auditActions(list))
	}
}

// ------------------------------------------------------- 6.4 GET /audit-events

// audit-log spec "Filter by actor", plus the other filters, ordering and pagination.
func TestListAuditEvents(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Audit List Org")
	otherOrg := testdb.CreateOrg(t, h.env.Pool, "Audit List Other Org")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	admin := testdb.CreateMember(t, h.env.Pool, org, access.RoleAdmin)
	outsider := testdb.CreateMember(t, h.env.Pool, otherOrg, access.RoleOwner)
	ownerC, adminC := h.signedIn(t, owner.Email), h.signedIn(t, admin.Email)

	// Owner creates, admin renames, owner archives.
	var loc gen.Location
	expect(t, ownerC.post("/api/v1/locations", map[string]any{"name": "Bondi", "timezone": "Australia/Sydney"}), http.StatusCreated, &loc)
	expect(t, adminC.patch("/api/v1/locations/"+loc.Id.String(), map[string]any{"name": "Bondi Beach"}), http.StatusOK, nil)
	expect(t, ownerC.post("/api/v1/locations/"+loc.Id.String()+"/archive", nil), http.StatusOK, nil)
	var other gen.Location
	expect(t, adminC.post("/api/v1/locations", map[string]any{"name": "Manly", "timezone": "Australia/Sydney"}), http.StatusCreated, &other)

	// Target filter, newest first.
	byTarget := auditEvents(t, ownerC, url.Values{"target_id": {loc.Id.String()}})
	if want := []string{"location.archived", "location.updated", "location.created"}; !slices.Equal(auditActions(byTarget), want) {
		t.Errorf("target filter = %v, want %v", auditActions(byTarget), want)
	}
	byType := auditEvents(t, ownerC, url.Values{"target_type": {"location"}})
	if want := []string{"location.created", "location.archived", "location.updated", "location.created"}; !slices.Equal(auditActions(byType), want) {
		t.Errorf("target_type filter = %v, want %v", auditActions(byType), want)
	}

	// Action filter.
	byAction := auditEvents(t, ownerC, url.Values{"action": {"location.created"}})
	if len(byAction.Items) != 2 || *byAction.Items[0].TargetId != other.Id || *byAction.Items[1].TargetId != loc.Id {
		t.Errorf("action filter = %+v, want Manly's then Bondi's creation", byAction.Items)
	}

	// Actor filter over the last 7 days: only the admin's entries, in the owner's organisation.
	weekAgo := time.Now().Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	byActor := auditEvents(t, ownerC, url.Values{"actor_user_id": {admin.UserID.String()}, "from": {weekAgo}})
	if want := []string{"location.created", "location.updated", "auth.login_succeeded"}; !slices.Equal(auditActions(byActor), want) {
		t.Errorf("actor filter = %v, want %v", auditActions(byActor), want)
	}
	for _, e := range byActor.Items {
		if e.Actor == nil || e.Actor.Id != admin.UserID {
			t.Errorf("actor filter returned an entry by %+v", e.Actor)
		}
	}
	// Filters combine.
	combined := auditEvents(t, ownerC, url.Values{"actor_user_id": {admin.UserID.String()}, "target_id": {loc.Id.String()}})
	if !slices.Equal(auditActions(combined), []string{"location.updated"}) {
		t.Errorf("actor + target filter = %v, want [location.updated]", auditActions(combined))
	}

	// Date range: from is inclusive, to is exclusive, and both bound the result.
	hourAgo := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	inHour := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	all := auditEvents(t, ownerC, url.Values{"limit": {"200"}})
	if got := auditEvents(t, ownerC, url.Values{"from": {hourAgo}, "to": {inHour}, "limit": {"200"}}); len(got.Items) != len(all.Items) {
		t.Errorf("range around now = %d entries, want all %d", len(got.Items), len(all.Items))
	}
	if got := auditEvents(t, ownerC, url.Values{"from": {inHour}}); len(got.Items) != 0 {
		t.Errorf("from in the future = %v, want none", auditActions(got))
	}
	if got := auditEvents(t, ownerC, url.Values{"to": {hourAgo}}); len(got.Items) != 0 {
		t.Errorf("to in the past = %v, want none", auditActions(got))
	}
	expectProblem(t, ownerC.get("/api/v1/audit-events?"+url.Values{"from": {inHour}, "to": {hourAgo}}.Encode()), http.StatusUnprocessableEntity, "validation_failed")
	expectProblem(t, ownerC.get("/api/v1/audit-events?limit=201"), http.StatusUnprocessableEntity, "validation_failed")

	// Pagination: every entry exactly once, newest first. (2 sign-ins + 4 location events.)
	if len(all.Items) != 6 || all.NextCursor != nil {
		t.Fatalf("all entries = %v (next_cursor %v), want 6 on one page", auditActions(all), all.NextCursor)
	}
	var paged []uuid.UUID
	q := url.Values{"limit": {"4"}}
	for range 3 {
		page := auditEvents(t, ownerC, q)
		for _, e := range page.Items {
			paged = append(paged, e.Id)
		}
		if page.NextCursor == nil {
			break
		}
		q.Set("cursor", *page.NextCursor)
	}
	var want []uuid.UUID
	for i, e := range all.Items {
		want = append(want, e.Id)
		if i > 0 && e.CreatedAt.After(all.Items[i-1].CreatedAt) {
			t.Errorf("entry %d is newer than the one before it", i)
		}
	}
	if !slices.Equal(paged, want) {
		t.Errorf("paged ids = %v, want %v", paged, want)
	}

	// Another organisation's owner sees none of it.
	outsiderC := h.signedIn(t, outsider.Email)
	theirs := auditEvents(t, outsiderC, url.Values{"limit": {"200"}})
	if !slices.Equal(auditActions(theirs), []string{"auth.login_succeeded"}) {
		t.Errorf("other organisation sees %v, want only its own sign-in", auditActions(theirs))
	}
	for _, q := range []url.Values{{"target_id": {loc.Id.String()}}, {"actor_user_id": {admin.UserID.String()}}} {
		if got := auditEvents(t, outsiderC, q); len(got.Items) != 0 {
			t.Errorf("other organisation with %v sees %v, want none", q, auditActions(got))
		}
	}
}

// access-control spec "Practitioner views audit log".
func TestAuditEventsRequirePermission(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "Audit Permission Org")

	for _, role := range []access.Role{access.RolePractitioner, access.RoleFrontDesk} {
		c := h.signedIn(t, testdb.CreateMember(t, h.env.Pool, org, role).Email)
		expectProblem(t, c.get("/api/v1/audit-events"), http.StatusForbidden, "permission_denied")
	}
	for _, role := range []access.Role{access.RoleOwner, access.RoleAdmin} {
		c := h.signedIn(t, testdb.CreateMember(t, h.env.Pool, org, role).Email)
		expect(t, c.get("/api/v1/audit-events"), http.StatusOK, nil)
	}
	expectProblem(t, h.client().get("/api/v1/audit-events"), http.StatusUnauthorized, "unauthorized")
}
