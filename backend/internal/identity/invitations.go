package identity

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"dentalsaas/internal/access"
	"dentalsaas/internal/platform/apperr"
	"dentalsaas/internal/platform/audit"
	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/platform/httpx"
	"dentalsaas/internal/platform/ids"
	"dentalsaas/internal/store"
)

type InvitationInput struct {
	Email       string
	Role        access.Role
	LocationIDs []uuid.UUID
}

// CreateInvitation invites someone to the caller's organisation and emails them a link.
func (s *Service) CreateInvitation(ctx context.Context, in InvitationInput) (store.Invitation, error) {
	p, err := access.Require(ctx, access.PermMembersManage)
	if err != nil {
		return store.Invitation{}, err
	}
	if in.Role == access.RoleOwner && p.Role != access.RoleOwner {
		return store.Invitation{}, access.ErrOwnerOnly
	}
	inv, _, err := s.createInvitation(ctx, p.Tenant(), p.UserID, in)
	return inv, err
}

// CreateOwnerInvitation is the operator path (CLI) for a new organisation's first owner.
// It returns the invitation token so the operator can share the link directly.
func (s *Service) CreateOwnerInvitation(ctx context.Context, orgID uuid.UUID, email string) (store.Invitation, string, error) {
	return s.createInvitation(ctx, db.Tenant{OrgID: orgID}, uuid.Nil, InvitationInput{Email: email, Role: access.RoleOwner})
}

func (s *Service) createInvitation(ctx context.Context, t db.Tenant, invitedBy uuid.UUID, in InvitationInput) (store.Invitation, string, error) {
	email := normaliseEmail(in.Email)
	var errs apperr.Fields
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
		errs.Add("email", "is not a valid email address")
	}
	if !in.Role.Valid() {
		errs.Add("role", "must be one of owner, admin, practitioner, front_desk")
	} else if in.Role.Unrestricted() && len(in.LocationIDs) > 0 {
		errs.Add("location_ids", "owners and admins always have access to all locations")
	}
	if err := errs.Err(); err != nil {
		return store.Invitation{}, "", err
	}

	var inv store.Invitation
	var orgName, token string
	err := s.DB.InTenantTx(ctx, t, func(tx *db.Tx) error {
		org, err := tx.GetOrganisation(ctx, t.OrgID)
		if err != nil {
			return err
		}
		orgName = org.Name
		locIDs, err := access.ValidateLocationIDs(ctx, tx, in.LocationIDs)
		if err != nil {
			return err
		}
		if existing, err := findUserByEmail(ctx, tx, email); err != nil {
			return err
		} else if existing != nil {
			m, err := tx.GetMembershipByUser(ctx, store.GetMembershipByUserParams{OrgID: t.OrgID, UserID: existing.ID})
			if err == nil && m.Status == "active" {
				return apperr.Conflict("already_member", "This person is already a member of the organisation.")
			} else if err != nil && !db.IsNoRows(err) {
				return err
			}
		}

		now := s.Now()
		if err := tx.RevokePendingInvitationsForEmail(ctx, store.RevokePendingInvitationsForEmailParams{Email: email, RevokedAt: &now}); err != nil {
			return err
		}
		var hash []byte
		token, hash = ids.NewToken()
		var by *uuid.UUID
		if invitedBy != uuid.Nil {
			by = &invitedBy
		}
		inv, err = tx.CreateInvitation(ctx, store.CreateInvitationParams{
			ID: ids.New(), OrgID: t.OrgID, Email: email, Role: string(in.Role), LocationIds: locIDs,
			TokenHash: hash, InvitedBy: by, ExpiresAt: now.Add(InvitationTTL),
		})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: t.OrgID, ActorID: invitedBy, Action: "invitation.created", TargetType: "invitation", TargetID: inv.ID,
			Details: map[string]any{"email": email, "role": in.Role, "location_ids": locIDs},
		})
	})
	if err != nil {
		return store.Invitation{}, "", err
	}
	s.send(ctx, invitationEmail, email, map[string]string{
		"OrgName": orgName, "Role": roleLabel(in.Role), "Link": s.InvitationLink(token),
	})
	return inv, token, nil
}

func (s *Service) InvitationLink(token string) string { return s.AppOrigin + "/invite/" + token }

func roleLabel(r access.Role) string {
	switch r {
	case access.RoleFrontDesk:
		return "front desk"
	default:
		return string(r)
	}
}

type ListInvitationsResult struct {
	Items      []store.Invitation
	NextCursor *string
}

// ListInvitations returns pending (not accepted, revoked or expired) invitations.
func (s *Service) ListInvitations(ctx context.Context, limit *int, cursor *string) (ListInvitationsResult, error) {
	var res ListInvitationsResult
	p, err := access.Require(ctx, access.PermMembersManage)
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
	q := store.ListPendingInvitationsParams{Now: s.Now(), RowLimit: n + 1}
	if cur != nil {
		if q.AfterCreated, err = httpx.ParseTimeKey(cur); err != nil {
			return res, err
		}
		q.AfterID = &cur.ID
	}
	err = s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		rows, err := tx.ListPendingInvitations(ctx, q)
		if err != nil {
			return err
		}
		var more bool
		res.Items, more = httpx.Page(rows, n)
		if more {
			last := res.Items[len(res.Items)-1]
			res.NextCursor = httpx.EncodeCursor(httpx.TimeKey(last.CreatedAt), last.ID)
		}
		return nil
	})
	return res, err
}

func (s *Service) RevokeInvitation(ctx context.Context, id uuid.UUID) (store.Invitation, error) {
	p, err := access.Require(ctx, access.PermMembersManage)
	if err != nil {
		return store.Invitation{}, err
	}
	var inv store.Invitation
	err = s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		cur, err := tx.GetInvitation(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound("Invitation")
		} else if err != nil {
			return err
		}
		if cur.Role == string(access.RoleOwner) && p.Role != access.RoleOwner {
			return access.ErrOwnerOnly
		}
		now := s.Now()
		if cur.AcceptedAt != nil || cur.RevokedAt != nil || !now.Before(cur.ExpiresAt) {
			return apperr.Conflict("invitation_not_pending", "This invitation is no longer pending.")
		}
		if inv, err = tx.RevokeInvitation(ctx, store.RevokeInvitationParams{ID: id, RevokedAt: &now}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: p.OrgID, ActorID: p.UserID, Action: "invitation.revoked", TargetType: "invitation", TargetID: id,
			Details: map[string]any{"email": cur.Email},
		})
	})
	return inv, err
}

type InvitationPreview struct {
	OrganisationName string
	Email            string
	Role             access.Role
	ExpiresAt        time.Time
	ExistingAccount  bool
}

var (
	errInvitationNotFound = apperr.NotFound("Invitation")
	errInvitationInvalid  = apperr.Gone("invitation_invalid", "This invitation is no longer valid.")
)

func (s *Service) checkInvitation(inv *invitationRecord) error {
	if inv == nil {
		return errInvitationNotFound
	}
	if inv.AcceptedAt != nil || inv.RevokedAt != nil || !s.Now().Before(inv.ExpiresAt) || inv.OrgStatus != "active" {
		return errInvitationInvalid
	}
	return nil
}

// PreviewInvitation lets the invitee see what they are accepting (public, token-authorised).
func (s *Service) PreviewInvitation(ctx context.Context, token string) (InvitationPreview, error) {
	var pv InvitationPreview
	err := s.DB.InAuthTx(ctx, func(tx *db.Tx) error {
		inv, err := findInvitation(ctx, tx, token)
		if err != nil {
			return err
		}
		if err := s.checkInvitation(inv); err != nil {
			return err
		}
		user, err := findUserByEmail(ctx, tx, inv.Email)
		if err != nil {
			return err
		}
		pv = InvitationPreview{
			OrganisationName: inv.OrgName, Email: inv.Email, Role: inv.Role,
			ExpiresAt: inv.ExpiresAt, ExistingAccount: user != nil,
		}
		return nil
	})
	return pv, err
}

// AcceptInvitation adds the membership (creating the account if needed) and signs the
// invitee in. For an existing account, password must be that account's password.
// It returns a new session token.
func (s *Service) AcceptInvitation(ctx context.Context, token string, name *string, password string) (string, error) {
	var sessionToken string
	err := s.DB.InAuthTx(ctx, func(tx *db.Tx) error {
		inv, err := findInvitation(ctx, tx, token)
		if err != nil {
			return err
		}
		if err := s.checkInvitation(inv); err != nil {
			return err
		}
		existing, err := findUserByEmail(ctx, tx, inv.Email)
		if err != nil {
			return err
		}

		userID := ids.New()
		var newUserName, newUserHash string
		if existing != nil {
			ok, err := VerifyPassword(password, existing.PasswordHash)
			if err != nil {
				return err
			}
			if !ok {
				return apperr.Unauthorized("Incorrect password for your existing account.")
			}
			userID = existing.ID
		} else {
			if name == nil || strings.TrimSpace(*name) == "" {
				return apperr.Validation(apperr.FieldError{Field: "name", Message: "is required"})
			}
			if err := ValidatePassword(password, inv.Email); err != nil {
				return err
			}
			newUserName = strings.TrimSpace(*name)
			if newUserHash, err = HashPassword(password); err != nil {
				return err
			}
		}

		if err := tx.SetOrg(ctx, inv.OrgID); err != nil {
			return err
		}
		if err := tx.SetUser(ctx, userID); err != nil {
			return err
		}
		if existing == nil {
			if err := tx.CreateUser(ctx, store.CreateUserParams{
				ID: userID, Email: inv.Email, Name: newUserName, PasswordHash: newUserHash,
			}); err != nil {
				return err
			}
		} else {
			m, err := tx.GetMembershipByUser(ctx, store.GetMembershipByUserParams{OrgID: inv.OrgID, UserID: userID})
			if err == nil && m.Status == "active" {
				return apperr.Conflict("already_member", "You are already a member of this organisation.")
			} else if err != nil && !db.IsNoRows(err) {
				return err
			}
		}

		now := s.Now()
		if n, err := tx.MarkInvitationAccepted(ctx, store.MarkInvitationAcceptedParams{ID: inv.ID, AcceptedAt: &now}); err != nil {
			return err
		} else if n == 0 {
			return errInvitationInvalid
		}
		m, err := tx.UpsertMembership(ctx, store.UpsertMembershipParams{
			ID: ids.New(), OrgID: inv.OrgID, UserID: userID, Role: string(inv.Role),
		})
		if err != nil {
			return err
		}
		if err := tx.DeleteMembershipLocations(ctx, m.ID); err != nil {
			return err
		}
		if !inv.Role.Unrestricted() {
			for _, loc := range inv.LocationIDs {
				if err := tx.AddMembershipLocation(ctx, store.AddMembershipLocationParams{
					OrgID: inv.OrgID, MembershipID: m.ID, LocationID: loc,
				}); err != nil {
					return err
				}
			}
		}
		if sessionToken, err = s.createSession(ctx, tx, userID, &inv.OrgID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: inv.OrgID, ActorID: userID, Action: "invitation.accepted", TargetType: "invitation", TargetID: inv.ID,
			Details: map[string]any{"email": inv.Email, "role": inv.Role, "new_account": existing == nil},
		})
	})
	return sessionToken, err
}
