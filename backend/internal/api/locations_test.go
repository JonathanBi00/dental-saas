package api_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"dentalsaas/internal/access"
	"dentalsaas/internal/api/gen"
	"dentalsaas/internal/platform/testdb"
)

func locationIDs(l gen.LocationList) []uuid.UUID {
	out := make([]uuid.UUID, len(l.Items))
	for i, x := range l.Items {
		out[i] = x.Id
	}
	return out
}

// Task 3.3: the full location lifecycle over HTTP as an owner.
func TestLocationEndpoints(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "HTTP Locations")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	c := h.signedIn(t, owner.Email)

	// Create.
	var loc gen.Location
	expect(t, c.post("/api/v1/locations", map[string]any{
		"name": "  Bondi  ", "timezone": "Australia/Sydney", "suburb": "Bondi", "email": "bondi@clinic.test",
	}), http.StatusCreated, &loc)
	if loc.Name != "Bondi" || loc.Timezone != "Australia/Sydney" || loc.ArchivedAt != nil {
		t.Fatalf("created = %+v", loc)
	}
	testdb.AssertAudited(t, h.env.Pool, org, "location.created", &loc.Id)

	// Get and list.
	var got gen.Location
	expect(t, c.get("/api/v1/locations/"+loc.Id.String()), http.StatusOK, &got)
	if got.Id != loc.Id || got.Suburb != "Bondi" {
		t.Errorf("get = %+v", got)
	}
	var list gen.LocationList
	expect(t, c.get("/api/v1/locations"), http.StatusOK, &list)
	if !slices.Equal(locationIDs(list), []uuid.UUID{loc.Id}) {
		t.Errorf("list = %v, want only %s", locationIDs(list), loc.Id)
	}

	// Partial update leaves other fields alone.
	expect(t, c.patch("/api/v1/locations/"+loc.Id.String(), map[string]any{"phone": "02 9300 0000"}), http.StatusOK, &got)
	if got.Phone != "02 9300 0000" || got.Suburb != "Bondi" || got.Name != "Bondi" {
		t.Errorf("after patch = %+v", got)
	}
	testdb.AssertAudited(t, h.env.Pool, org, "location.updated", &loc.Id)

	// Archive: hidden by default, visible with include_archived, still gettable.
	expect(t, c.post("/api/v1/locations/"+loc.Id.String()+"/archive", nil), http.StatusOK, &got)
	if got.ArchivedAt == nil {
		t.Fatal("archive response has no archived_at")
	}
	testdb.AssertAudited(t, h.env.Pool, org, "location.archived", &loc.Id)
	expect(t, c.get("/api/v1/locations"), http.StatusOK, &list)
	if len(list.Items) != 0 {
		t.Errorf("default list after archive = %v, want empty", locationIDs(list))
	}
	expect(t, c.get("/api/v1/locations?include_archived=true"), http.StatusOK, &list)
	if !slices.Equal(locationIDs(list), []uuid.UUID{loc.Id}) {
		t.Errorf("include_archived list = %v, want %s", locationIDs(list), loc.Id)
	}
	expect(t, c.get("/api/v1/locations/"+loc.Id.String()), http.StatusOK, &got)
	expectProblem(t, c.post("/api/v1/locations/"+loc.Id.String()+"/archive", nil), http.StatusConflict, "already_archived")

	// Delta sync returns sync_until.
	expect(t, c.get("/api/v1/locations?updated_since=2000-01-01T00:00:00Z"), http.StatusOK, &list)
	if list.SyncUntil == nil || len(list.Items) != 1 {
		t.Errorf("delta sync: items=%d sync_until=%v", len(list.Items), list.SyncUntil)
	}
}

func TestLocationValidationErrors(t *testing.T) {
	h := newHarness(t)
	org := testdb.CreateOrg(t, h.env.Pool, "HTTP Validation")
	owner := testdb.CreateMember(t, h.env.Pool, org, access.RoleOwner)
	c := h.signedIn(t, owner.Email)

	p := expectProblem(t, c.post("/api/v1/locations", map[string]any{"name": "X", "timezone": "Moon/Base"}),
		http.StatusUnprocessableEntity, "validation_failed")
	if len(p.Errors) != 1 || p.Errors[0].Field != "timezone" {
		t.Errorf("errors = %+v, want one on timezone", p.Errors)
	}

	id := testdb.CreateLocation(t, h.env.Pool, org, "Valid")
	p = expectProblem(t, c.patch("/api/v1/locations/"+id.String(), map[string]any{"timezone": "Moon/Base"}),
		http.StatusUnprocessableEntity, "validation_failed")
	if len(p.Errors) != 1 || p.Errors[0].Field != "timezone" {
		t.Errorf("patch errors = %+v, want one on timezone", p.Errors)
	}

	expectProblem(t, c.get("/api/v1/locations?limit=201"), http.StatusUnprocessableEntity, "validation_failed")
	expectProblem(t, c.get("/api/v1/locations/not-a-uuid"), http.StatusBadRequest, "")
	expectProblem(t, c.get("/api/v1/locations/"+uuid.NewString()), http.StatusNotFound, "not_found")
}

// Task 3.3 / tenancy spec "Read another organisation's record by identifier": 404, never 403,
// and nothing changes.
func TestAnotherOrganisationsLocationIsNotFound(t *testing.T) {
	h := newHarness(t)
	orgA := testdb.CreateOrg(t, h.env.Pool, "Org A")
	orgB := testdb.CreateOrg(t, h.env.Pool, "Org B")
	locA := testdb.CreateLocation(t, h.env.Pool, orgA, "A's clinic")
	locB := testdb.CreateLocation(t, h.env.Pool, orgB, "B's clinic")
	ownerB := testdb.CreateMember(t, h.env.Pool, orgB, access.RoleOwner)
	c := h.signedIn(t, ownerB.Email)

	path := "/api/v1/locations/" + locA.String()
	expectProblem(t, c.get(path), http.StatusNotFound, "not_found")
	expectProblem(t, c.patch(path, map[string]any{"name": "Hijacked"}), http.StatusNotFound, "not_found")
	expectProblem(t, c.post(path+"/archive", nil), http.StatusNotFound, "not_found")

	// List endpoints only return the active organisation's records.
	var list gen.LocationList
	expect(t, c.get("/api/v1/locations?include_archived=true"), http.StatusOK, &list)
	if !slices.Equal(locationIDs(list), []uuid.UUID{locB}) {
		t.Errorf("org B list = %v, want only %s", locationIDs(list), locB)
	}

	// Org A's location is untouched.
	ownerA := testdb.CreateMember(t, h.env.Pool, orgA, access.RoleOwner)
	var got gen.Location
	expect(t, h.signedIn(t, ownerA.Email).get(path), http.StatusOK, &got)
	if got.Name != "A's clinic" || got.ArchivedAt != nil {
		t.Errorf("org A's location changed: %+v", got)
	}
	if n := len(testdb.AuditEvents(t, h.env.Pool, orgA, "location.updated", &locA)); n != 0 {
		t.Errorf("org A has %d location.updated events, want 0", n)
	}
}

func TestLocationEndpointsRequireSignIn(t *testing.T) {
	h := newHarness(t)
	c := h.client()
	expectProblem(t, c.get("/api/v1/locations"), http.StatusUnauthorized, "unauthorized")
	expectProblem(t, c.post("/api/v1/locations", map[string]any{"name": "X", "timezone": "UTC"}), http.StatusUnauthorized, "unauthorized")
}
