package access_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"dentalsaas/internal/access"
	"dentalsaas/internal/platform/apperr"
)

var allPermissions = []access.Permission{
	access.PermLocationsRead, access.PermLocationsManage, access.PermMembersManage, access.PermAuditRead,
}

// Task 5.1 / access-control spec "Foundation permission matrix".
func TestPermissionMatrix(t *testing.T) {
	matrix := map[access.Role][]access.Permission{
		access.RoleOwner:        allPermissions,
		access.RoleAdmin:        allPermissions,
		access.RolePractitioner: {access.PermLocationsRead},
		access.RoleFrontDesk:    {access.PermLocationsRead},
	}
	if len(access.RolePermissions) != len(matrix) {
		t.Errorf("RolePermissions has %d roles, want %d", len(access.RolePermissions), len(matrix))
	}
	for role, granted := range matrix {
		if !role.Valid() {
			t.Errorf("%s: not a valid role", role)
		}
		p := access.Principal{UserID: uuid.New(), OrgID: uuid.New(), Role: role}
		for _, perm := range allPermissions {
			want := slices.Contains(granted, perm)
			if got := p.Can(perm); got != want {
				t.Errorf("%s can %s = %v, want %v", role, perm, got, want)
			}
			_, err := access.Require(access.WithPrincipal(context.Background(), p), perm)
			if want && err != nil {
				t.Errorf("%s: Require(%s) = %v, want nil", role, perm, err)
			}
			if e, _ := apperr.As(err); !want && (e == nil || e.Kind != apperr.KindForbidden || e.Code != "permission_denied") {
				t.Errorf("%s: Require(%s) = %v, want 403 permission_denied", role, perm, err)
			}
		}
		var wantNames []string
		for _, perm := range granted {
			wantNames = append(wantNames, string(perm))
		}
		if got := p.Permissions(); !slices.Equal(got, wantNames) {
			t.Errorf("%s: Permissions() = %v, want %v", role, got, wantNames)
		}
	}
}

func TestUnknownRoleHasNoPermissions(t *testing.T) {
	for _, role := range []access.Role{"", "janitor", "Owner"} {
		if role.Valid() {
			t.Errorf("%q should not be a valid role", role)
		}
		p := access.Principal{UserID: uuid.New(), OrgID: uuid.New(), Role: role}
		for _, perm := range allPermissions {
			if p.Can(perm) {
				t.Errorf("role %q can %s", role, perm)
			}
		}
	}
}

func TestOnlyOwnersAndAdminsAreUnrestricted(t *testing.T) {
	want := map[access.Role]bool{
		access.RoleOwner: true, access.RoleAdmin: true, access.RolePractitioner: false, access.RoleFrontDesk: false,
	}
	for role, unrestricted := range want {
		if got := role.Unrestricted(); got != unrestricted {
			t.Errorf("%s.Unrestricted() = %v, want %v", role, got, unrestricted)
		}
	}
}

// Without an active organisation a principal holds no permissions, whatever its role says.
func TestPermissionsNeedAnActiveOrganisation(t *testing.T) {
	p := access.Principal{UserID: uuid.New(), Role: access.RoleOwner}
	if p.HasOrg() || p.Can(access.PermLocationsRead) || len(p.Permissions()) != 0 {
		t.Errorf("principal without an organisation: HasOrg=%v Can=%v Permissions=%v", p.HasOrg(), p.Can(access.PermLocationsRead), p.Permissions())
	}
	if p.Permissions() == nil {
		t.Error("Permissions() should be an empty list, not nil, so it encodes as []")
	}
}

func TestRequireErrors(t *testing.T) {
	code := func(err error) (apperr.Kind, string) {
		e, ok := apperr.As(err)
		if !ok {
			return 0, ""
		}
		return e.Kind, e.Code
	}

	// Nobody signed in: 401 from every guard.
	ctx := context.Background()
	if _, err := access.Authenticated(ctx); !apperr.Is(err, apperr.KindUnauthorized) {
		t.Errorf("Authenticated without a principal = %v, want 401", err)
	}
	if _, err := access.RequireOrg(ctx); !apperr.Is(err, apperr.KindUnauthorized) {
		t.Errorf("RequireOrg without a principal = %v, want 401", err)
	}
	if _, err := access.Require(ctx, access.PermLocationsRead); !apperr.Is(err, apperr.KindUnauthorized) {
		t.Errorf("Require without a principal = %v, want 401", err)
	}

	// Signed in, but no active organisation.
	user := access.Principal{UserID: uuid.New(), Role: access.RoleOwner}
	ctx = access.WithPrincipal(context.Background(), user)
	if p, err := access.Authenticated(ctx); err != nil || p.UserID != user.UserID {
		t.Errorf("Authenticated = %+v, %v", p, err)
	}
	if kind, c := code(func() error { _, err := access.RequireOrg(ctx); return err }()); kind != apperr.KindForbidden || c != "no_active_organisation" {
		t.Errorf("RequireOrg without an organisation = %v %q, want 403 no_active_organisation", kind, c)
	}
	if kind, c := code(func() error { _, err := access.Require(ctx, access.PermLocationsRead); return err }()); kind != apperr.KindForbidden || c != "no_active_organisation" {
		t.Errorf("Require without an organisation = %v %q, want 403 no_active_organisation", kind, c)
	}

	// With an organisation, Require returns the principal for a held permission.
	member := access.Principal{UserID: uuid.New(), OrgID: uuid.New(), Role: access.RoleFrontDesk}
	ctx = access.WithPrincipal(context.Background(), member)
	if p, err := access.Require(ctx, access.PermLocationsRead); err != nil || p.OrgID != member.OrgID {
		t.Errorf("Require(locations:read) = %+v, %v", p, err)
	}
	if tn := member.Tenant(); tn.OrgID != member.OrgID || tn.UserID != member.UserID {
		t.Errorf("Tenant() = %+v", tn)
	}
}

// Accessible-location resolution: nil means every location; otherwise only those listed.
func TestCanAccessLocation(t *testing.T) {
	x, y, z := uuid.New(), uuid.New(), uuid.New()

	unrestricted := access.Principal{OrgID: uuid.New(), Role: access.RolePractitioner}
	if !unrestricted.CanAccessLocation(x) || !unrestricted.CanAccessLocation(z) {
		t.Error("an unrestricted principal should reach every location")
	}
	restricted := access.Principal{OrgID: uuid.New(), Role: access.RoleFrontDesk, LocationIDs: []uuid.UUID{x, y}}
	if !restricted.CanAccessLocation(x) || !restricted.CanAccessLocation(y) {
		t.Error("a restricted principal should reach its own locations")
	}
	if restricted.CanAccessLocation(z) {
		t.Error("a restricted principal reached a location outside its list")
	}
}
