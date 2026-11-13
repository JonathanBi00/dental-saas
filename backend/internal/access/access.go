// Package access decides what an authenticated member may do: roles map to named
// permissions, and memberships may be restricted to specific locations.
package access

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"dentalsaas/internal/platform/apperr"
	"dentalsaas/internal/platform/db"
)

type Role string

const (
	RoleOwner        Role = "owner"
	RoleAdmin        Role = "admin"
	RolePractitioner Role = "practitioner"
	RoleFrontDesk    Role = "front_desk"
)

func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RolePractitioner, RoleFrontDesk:
		return true
	}
	return false
}

// Unrestricted reports whether the role always has access to every location.
func (r Role) Unrestricted() bool { return r == RoleOwner || r == RoleAdmin }

type Permission string

// Handlers check permissions by name, never by role, so later domains can add permissions
// (and custom roles can become data) without touching existing checks.
const (
	PermLocationsRead   Permission = "locations:read"
	PermLocationsManage Permission = "locations:manage"
	PermMembersManage   Permission = "members:manage"
	PermAuditRead       Permission = "audit:read"
)

var RolePermissions = map[Role][]Permission{
	RoleOwner:        {PermLocationsRead, PermLocationsManage, PermMembersManage, PermAuditRead},
	RoleAdmin:        {PermLocationsRead, PermLocationsManage, PermMembersManage, PermAuditRead},
	RolePractitioner: {PermLocationsRead},
	RoleFrontDesk:    {PermLocationsRead},
}

// Principal is the authenticated caller for one request.
type Principal struct {
	SessionID uuid.UUID
	UserID    uuid.UUID
	Email     string
	Name      string

	// Set only when the session has an active organisation the user can act in.
	OrgID            uuid.UUID
	Role             Role
	LocationIDs      []uuid.UUID // nil = unrestricted
	ActiveLocationID *uuid.UUID
}

func (p Principal) HasOrg() bool { return p.OrgID != uuid.Nil }

func (p Principal) Can(perm Permission) bool {
	return p.HasOrg() && slices.Contains(RolePermissions[p.Role], perm)
}

// Permissions lists the principal's permissions as strings (for the session endpoint).
func (p Principal) Permissions() []string {
	out := []string{}
	if !p.HasOrg() {
		return out
	}
	for _, perm := range RolePermissions[p.Role] {
		out = append(out, string(perm))
	}
	return out
}

// CanAccessLocation reports whether a location is within the principal's scope.
func (p Principal) CanAccessLocation(id uuid.UUID) bool {
	return p.LocationIDs == nil || slices.Contains(p.LocationIDs, id)
}

// Tenant is the database context for this principal.
func (p Principal) Tenant() db.Tenant { return db.Tenant{OrgID: p.OrgID, UserID: p.UserID} }

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// Authenticated returns the caller or a 401.
func Authenticated(ctx context.Context) (Principal, error) {
	p, ok := FromContext(ctx)
	if !ok {
		return Principal{}, apperr.Unauthorized("Sign in required.")
	}
	return p, nil
}

// RequireOrg returns the caller, failing unless they have an active organisation.
func RequireOrg(ctx context.Context) (Principal, error) {
	p, err := Authenticated(ctx)
	if err != nil {
		return p, err
	}
	if !p.HasOrg() {
		return p, apperr.Forbidden("no_active_organisation", "Choose an organisation first.")
	}
	return p, nil
}

// Require returns the caller, failing unless they hold perm in their active organisation.
func Require(ctx context.Context, perm Permission) (Principal, error) {
	p, err := RequireOrg(ctx)
	if err != nil {
		return p, err
	}
	if !p.Can(perm) {
		return p, apperr.Forbidden("permission_denied", "You do not have permission to do this.")
	}
	return p, nil
}
