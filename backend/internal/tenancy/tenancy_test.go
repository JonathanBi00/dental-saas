package tenancy_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"dentalsaas/internal/access"
	"dentalsaas/internal/platform/apperr"
	"dentalsaas/internal/platform/httpx"
	"dentalsaas/internal/platform/testdb"
	"dentalsaas/internal/store"
	"dentalsaas/internal/tenancy"
)

func TestMain(m *testing.M) { testdb.Main(m) }

type fixture struct {
	env *testdb.Env
	svc *tenancy.Service
	org uuid.UUID
	ctx context.Context // signed in as the organisation's owner
}

func setup(t *testing.T) fixture {
	t.Helper()
	env := testdb.Start(t)
	org := testdb.CreateOrg(t, env.Pool, "Tenancy "+t.Name())
	owner := testdb.CreateMember(t, env.Pool, org, access.RoleOwner)
	ctx := access.WithPrincipal(context.Background(), access.Principal{
		UserID: owner.UserID, Email: owner.Email, OrgID: org, Role: access.RoleOwner,
	})
	return fixture{env: env, svc: tenancy.New(env.Pool), org: org, ctx: ctx}
}

func str(s string) *string { return &s }

func (f fixture) create(t *testing.T, name string) store.Location {
	t.Helper()
	loc, err := f.svc.CreateLocation(f.ctx, tenancy.LocationInput{Name: str(name), Timezone: str("Australia/Sydney")})
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return loc
}

func ids(locs []store.Location) []uuid.UUID {
	out := make([]uuid.UUID, len(locs))
	for i, l := range locs {
		out[i] = l.ID
	}
	return out
}

// wantTimezoneError asserts a 422 validation error on the timezone field.
func wantTimezoneError(t *testing.T, err error) {
	t.Helper()
	e, ok := apperr.As(err)
	if !ok || e.Kind != apperr.KindValidation {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if !slices.ContainsFunc(e.Fields, func(f apperr.FieldError) bool { return f.Field == "timezone" }) {
		t.Fatalf("fields = %+v, want one on timezone", e.Fields)
	}
}

// Task 3.2 / tenancy spec "Invalid timezone".
func TestInvalidTimezoneIsRejected(t *testing.T) {
	f := setup(t)

	for _, tz := range []string{"Mars/Olympus_Mons", "AEST", "Local", ""} {
		_, err := f.svc.CreateLocation(f.ctx, tenancy.LocationInput{Name: str("Bad TZ"), Timezone: str(tz)})
		t.Run("create "+tz, func(t *testing.T) { wantTimezoneError(t, err) })
	}
	for _, tz := range []string{"Australia/Perth", "UTC"} {
		if _, err := f.svc.CreateLocation(f.ctx, tenancy.LocationInput{Name: str("Good " + tz), Timezone: str(tz)}); err != nil {
			t.Errorf("create with %q: %v", tz, err)
		}
	}

	loc := f.create(t, "Update target")
	_, err := f.svc.UpdateLocation(f.ctx, loc.ID, tenancy.LocationInput{Timezone: str("Nowhere/Special")})
	wantTimezoneError(t, err)
	got, err := f.svc.GetLocation(f.ctx, loc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Timezone != "Australia/Sydney" {
		t.Errorf("rejected update changed timezone to %q", got.Timezone)
	}
}

// Task 3.2 / tenancy spec "Archive a location".
func TestArchivedLocationHiddenFromDefaultList(t *testing.T) {
	f := setup(t)
	kept := f.create(t, "Kept")
	gone := f.create(t, "Gone")

	archived, err := f.svc.ArchiveLocation(f.ctx, gone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("archive did not set archived_at")
	}
	testdb.AssertAudited(t, f.env.Pool, f.org, "location.archived", &gone.ID)

	res, err := f.svc.ListLocations(f.ctx, tenancy.ListLocationsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(res.Items); !slices.Equal(got, []uuid.UUID{kept.ID}) {
		t.Errorf("default list = %v, want only %s", got, kept.ID)
	}

	res, err = f.svc.ListLocations(f.ctx, tenancy.ListLocationsParams{IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(res.Items); !slices.Equal(got, []uuid.UUID{gone.ID, kept.ID}) { // ordered by name
		t.Errorf("include_archived list = %v, want %s and %s", got, gone.ID, kept.ID)
	}

	got, err := f.svc.GetLocation(f.ctx, gone.ID)
	if err != nil || got.ArchivedAt == nil {
		t.Errorf("get archived location: archived_at=%v err=%v; want it retrievable and archived", got.ArchivedAt, err)
	}

	if _, err := f.svc.ArchiveLocation(f.ctx, gone.ID); !apperr.Is(err, apperr.KindConflict) {
		t.Errorf("archiving twice: err = %v, want conflict", err)
	}
}

// Task 3.2: delta sync includes archived rows, so clients learn about archives.
func TestDeltaSyncReturnsArchivedRow(t *testing.T) {
	f := setup(t)
	untouched := f.create(t, "Untouched")
	loc := f.create(t, "Archived later")
	since := loc.UpdatedAt

	if _, err := f.svc.ArchiveLocation(f.ctx, loc.ID); err != nil {
		t.Fatal(err)
	}

	before := time.Now()
	res, err := f.svc.ListLocations(f.ctx, tenancy.ListLocationsParams{UpdatedSince: &since})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(res.Items); !slices.Equal(got, []uuid.UUID{loc.ID}) {
		t.Fatalf("delta since %s = %v, want only the archived %s (not %s)", since, got, loc.ID, untouched.ID)
	}
	if res.Items[0].ArchivedAt == nil {
		t.Error("delta row should carry archived_at")
	}
	if res.SyncUntil == nil {
		t.Fatal("delta response must include sync_until")
	}
	if lag := before.Sub(*res.SyncUntil); lag < httpx.SyncMargin-time.Second || lag > httpx.SyncMargin+5*time.Second {
		t.Errorf("sync_until is %s behind now, want about %s", lag, httpx.SyncMargin)
	}

	// A plain list never sets sync_until.
	plain, err := f.svc.ListLocations(f.ctx, tenancy.ListLocationsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.SyncUntil != nil {
		t.Error("sync_until should only be set in delta-sync mode")
	}
}

// Pagination follows next_cursor without gaps or duplicates, in both list modes.
func TestListPagination(t *testing.T) {
	f := setup(t)
	var want []uuid.UUID
	for _, name := range []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo"} {
		want = append(want, f.create(t, name).ID)
	}
	two := 2

	collect := func(params tenancy.ListLocationsParams) []uuid.UUID {
		t.Helper()
		var got []uuid.UUID
		for page := 0; ; page++ {
			if page > 10 {
				t.Fatal("pagination did not terminate")
			}
			res, err := f.svc.ListLocations(f.ctx, params)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Items) > two {
				t.Fatalf("page has %d items, limit was %d", len(res.Items), two)
			}
			got = append(got, ids(res.Items)...)
			if res.NextCursor == nil {
				return got
			}
			params.Cursor = res.NextCursor
		}
	}

	t.Run("by name", func(t *testing.T) {
		if got := collect(tenancy.ListLocationsParams{Limit: &two}); !slices.Equal(got, want) {
			t.Errorf("paged ids = %v, want %v", got, want)
		}
	})
	t.Run("delta sync", func(t *testing.T) {
		epoch := time.Unix(0, 0)
		got := collect(tenancy.ListLocationsParams{Limit: &two, UpdatedSince: &epoch})
		if !slices.Equal(got, want) { // created in this order, so updated_at order matches
			t.Errorf("paged ids = %v, want %v", got, want)
		}
	})
	t.Run("limit over 200", func(t *testing.T) {
		big := 201
		if _, err := f.svc.ListLocations(f.ctx, tenancy.ListLocationsParams{Limit: &big}); !apperr.Is(err, apperr.KindValidation) {
			t.Errorf("err = %v, want validation error", err)
		}
	})
}

// Create and update are audited, and updates record only the fields that changed.
func TestLocationChangesAreAudited(t *testing.T) {
	f := setup(t)
	loc := f.create(t, "Audited")
	testdb.AssertAudited(t, f.env.Pool, f.org, "location.created", &loc.ID)

	if _, err := f.svc.UpdateLocation(f.ctx, loc.ID, tenancy.LocationInput{Name: str("Audited"), Phone: str("02 9999 0000")}); err != nil {
		t.Fatal(err)
	}
	ev := testdb.AssertAudited(t, f.env.Pool, f.org, "location.updated", &loc.ID)
	details := string(ev.Details)
	if !strings.Contains(details, `"phone"`) || strings.Contains(details, `"name"`) {
		t.Errorf("update audit details = %s, want phone changed and name omitted", details)
	}
}
