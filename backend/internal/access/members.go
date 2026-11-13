package access

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"dentalsaas/internal/platform/apperr"
	"dentalsaas/internal/platform/audit"
	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/platform/httpx"
	"dentalsaas/internal/store"
)

var (
	ErrOwnerOnly = apperr.Forbidden("owner_only", "Only owners can grant, remove or change the owner role.")
	ErrLastOwner = apperr.Conflict("last_owner", "An organisation needs at least one active owner.")
)

// ValidateLocationIDs de-duplicates ids and checks they all belong to the tenant in tx.
func ValidateLocationIDs(ctx context.Context, tx *db.Tx, ids []uuid.UUID) ([]uuid.UUID, error) {
	out := []uuid.UUID{}
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return out, nil
	}
	n, err := tx.CountLocationsInOrg(ctx, out)
	if err != nil {
		return nil, err
	}
	if int(n) != len(out) {
		return nil, apperr.Validation(apperr.FieldError{Field: "location_ids", Message: "contains an unknown location"})
	}
	return out, nil
}

type Member = store.GetMemberRow

type MemberService struct {
	DB *db.Pool
}

type ListMembersResult struct {
	Items      []Member
	NextCursor *string
}

func (s *MemberService) List(ctx context.Context, limit *int, cursor *string) (ListMembersResult, error) {
	var res ListMembersResult
	p, err := Require(ctx, PermMembersManage)
	if err != nil {
		return res, err
	}
	n, err := httpx.Limit(limit)
	if err != nil {
		return res, err
	}
	cur, err := httpx.DecodeCursor(cursor)
	if err != nil {
		return res, err
	}
	q := store.ListMembersParams{OrgID: p.OrgID, RowLimit: n + 1}
	if cur != nil {
		if q.AfterCreated, err = httpx.ParseTimeKey(cur); err != nil {
			return res, err
		}
		q.AfterID = &cur.ID
	}
	err = s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		rows, err := tx.ListMembers(ctx, q)
		if err != nil {
			return err
		}
		page, more := httpx.Page(rows, n)
		res.Items = make([]Member, len(page))
		for i, r := range page {
			res.Items[i] = Member(r)
		}
		if more {
			last := page[len(page)-1]
			res.NextCursor = httpx.EncodeCursor(httpx.TimeKey(last.CreatedAt), last.ID)
		}
		return nil
	})
	return res, err
}

type MemberUpdate struct {
	Role        *Role
	LocationIDs *[]uuid.UUID // nil = unchanged; empty = unrestricted
}

func (s *MemberService) Update(ctx context.Context, id uuid.UUID, in MemberUpdate) (Member, error) {
	p, err := Require(ctx, PermMembersManage)
	if err != nil {
		return Member{}, err
	}
	if in.Role != nil && !in.Role.Valid() {
		return Member{}, apperr.Validation(apperr.FieldError{Field: "role", Message: "must be one of owner, admin, practitioner, front_desk"})
	}
	var out Member
	err = s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		m, err := getMember(ctx, tx, p.OrgID, id)
		if err != nil {
			return err
		}
		oldRole := Role(m.Role)
		newRole := oldRole
		if in.Role != nil {
			newRole = *in.Role
		}
		if err := checkOwnerRules(ctx, tx, p, m, oldRole, newRole, m.Status == "active"); err != nil {
			return err
		}

		newLocs := m.LocationIds
		if in.LocationIDs != nil {
			if newRole.Unrestricted() && len(*in.LocationIDs) > 0 {
				return apperr.Validation(apperr.FieldError{Field: "location_ids", Message: "owners and admins always have access to all locations"})
			}
			if newLocs, err = ValidateLocationIDs(ctx, tx, *in.LocationIDs); err != nil {
				return err
			}
		}
		if newRole.Unrestricted() {
			newLocs = []uuid.UUID{}
		}

		ch := audit.Changes{}
		if newRole != oldRole {
			if err := tx.UpdateMembershipRole(ctx, store.UpdateMembershipRoleParams{ID: m.ID, Role: string(newRole)}); err != nil {
				return err
			}
			ch.Diff("role", oldRole, newRole)
		}
		if !sameSet(m.LocationIds, newLocs) {
			if err := tx.DeleteMembershipLocations(ctx, m.ID); err != nil {
				return err
			}
			for _, loc := range newLocs {
				if err := tx.AddMembershipLocation(ctx, store.AddMembershipLocationParams{
					OrgID: p.OrgID, MembershipID: m.ID, LocationID: loc,
				}); err != nil {
					return err
				}
			}
			ch.Diff("location_ids", m.LocationIds, newLocs)
		}
		if len(ch) > 0 {
			if err := audit.Record(ctx, tx, audit.Event{
				OrgID: p.OrgID, ActorID: p.UserID, Action: "membership.updated",
				TargetType: "membership", TargetID: m.ID,
				Details: map[string]any{"user_id": m.UserID, "changes": ch},
			}); err != nil {
				return err
			}
		}
		out, err = getMember(ctx, tx, p.OrgID, id)
		return err
	})
	return out, err
}

func (s *MemberService) Deactivate(ctx context.Context, id uuid.UUID) (Member, error) {
	return s.setStatus(ctx, id, "deactivated", "membership.deactivated")
}

func (s *MemberService) Reactivate(ctx context.Context, id uuid.UUID) (Member, error) {
	return s.setStatus(ctx, id, "active", "membership.reactivated")
}

func (s *MemberService) setStatus(ctx context.Context, id uuid.UUID, status, action string) (Member, error) {
	p, err := Require(ctx, PermMembersManage)
	if err != nil {
		return Member{}, err
	}
	var out Member
	err = s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		m, err := getMember(ctx, tx, p.OrgID, id)
		if err != nil {
			return err
		}
		if m.Status == status {
			out = m
			return nil
		}
		role := Role(m.Role)
		if err := checkOwnerRules(ctx, tx, p, m, role, role, status == "active"); err != nil {
			return err
		}
		if err := tx.UpdateMembershipStatus(ctx, store.UpdateMembershipStatusParams{ID: m.ID, Status: status}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Event{
			OrgID: p.OrgID, ActorID: p.UserID, Action: action, TargetType: "membership", TargetID: m.ID,
			Details: map[string]any{"user_id": m.UserID, "email": m.Email},
		}); err != nil {
			return err
		}
		out, err = getMember(ctx, tx, p.OrgID, id)
		return err
	})
	return out, err
}

// checkOwnerRules enforces: only owners change owner memberships or grant the owner role,
// and the organisation always keeps at least one active owner.
func checkOwnerRules(ctx context.Context, tx *db.Tx, p Principal, m Member, oldRole, newRole Role, willBeActive bool) error {
	touchesOwner := oldRole == RoleOwner || newRole == RoleOwner
	if touchesOwner && p.Role != RoleOwner {
		return ErrOwnerOnly
	}
	losesOwner := oldRole == RoleOwner && m.Status == "active" && (newRole != RoleOwner || !willBeActive)
	if losesOwner {
		n, err := tx.CountActiveOwners(ctx, p.OrgID)
		if err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastOwner
		}
	}
	return nil
}

func getMember(ctx context.Context, tx *db.Tx, orgID, id uuid.UUID) (Member, error) {
	m, err := tx.GetMember(ctx, store.GetMemberParams{OrgID: orgID, ID: id})
	if db.IsNoRows(err) {
		return Member{}, apperr.NotFound("Member")
	}
	return m, err
}

func sameSet(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	return true
}
