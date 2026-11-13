package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"dentalsaas/internal/access"
	"dentalsaas/internal/api/gen"
	"dentalsaas/internal/auditlog"
	"dentalsaas/internal/identity"
	"dentalsaas/internal/store"
	"dentalsaas/internal/tenancy"
)

// Server implements gen.StrictServerInterface. Handlers only translate between API types and
// service calls; permission checks and business rules live in the services.
type Server struct {
	d Deps
}

var _ gen.StrictServerInterface = (*Server)(nil)

// ------------------------------------------------------------------------- auth

func (s *Server) Login(ctx context.Context, req gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	token, err := s.d.Identity.Login(ctx, req.Body.Email, req.Body.Password)
	if err != nil {
		return nil, err
	}
	info, err := s.d.Identity.InfoForToken(ctx, token)
	if err != nil {
		return nil, err
	}
	cookie := s.d.Identity.Cookie(token)
	return gen.Login200JSONResponse{Body: toSessionInfo(info), Headers: gen.Login200ResponseHeaders{SetCookie: &cookie}}, nil
}

func (s *Server) Logout(ctx context.Context, _ gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	if err := s.d.Identity.Logout(ctx); err != nil {
		return nil, err
	}
	cookie := s.d.Identity.ClearCookie()
	return gen.Logout204Response{Headers: gen.Logout204ResponseHeaders{SetCookie: &cookie}}, nil
}

func (s *Server) GetSession(ctx context.Context, _ gen.GetSessionRequestObject) (gen.GetSessionResponseObject, error) {
	p, err := access.Authenticated(ctx)
	if err != nil {
		return nil, err
	}
	info, err := s.d.Identity.Info(ctx, p)
	if err != nil {
		return nil, err
	}
	return gen.GetSession200JSONResponse(toSessionInfo(info)), nil
}

func (s *Server) SwitchOrganisation(ctx context.Context, req gen.SwitchOrganisationRequestObject) (gen.SwitchOrganisationResponseObject, error) {
	p, err := s.d.Identity.SwitchOrganisation(ctx, req.Body.OrganisationId)
	if err != nil {
		return nil, err
	}
	info, err := s.d.Identity.Info(ctx, p)
	if err != nil {
		return nil, err
	}
	return gen.SwitchOrganisation200JSONResponse(toSessionInfo(info)), nil
}

func (s *Server) SwitchLocation(ctx context.Context, req gen.SwitchLocationRequestObject) (gen.SwitchLocationResponseObject, error) {
	p, err := s.d.Identity.SwitchLocation(ctx, req.Body.LocationId)
	if err != nil {
		return nil, err
	}
	info, err := s.d.Identity.Info(ctx, p)
	if err != nil {
		return nil, err
	}
	return gen.SwitchLocation200JSONResponse(toSessionInfo(info)), nil
}

func (s *Server) RequestPasswordReset(ctx context.Context, req gen.RequestPasswordResetRequestObject) (gen.RequestPasswordResetResponseObject, error) {
	if err := s.d.Identity.RequestPasswordReset(ctx, req.Body.Email); err != nil {
		return nil, err
	}
	return gen.RequestPasswordReset202Response{}, nil
}

func (s *Server) CompletePasswordReset(ctx context.Context, req gen.CompletePasswordResetRequestObject) (gen.CompletePasswordResetResponseObject, error) {
	if err := s.d.Identity.CompletePasswordReset(ctx, req.Body.Token, req.Body.Password); err != nil {
		return nil, err
	}
	return gen.CompletePasswordReset204Response{}, nil
}

func (s *Server) GetInvitationPreview(ctx context.Context, req gen.GetInvitationPreviewRequestObject) (gen.GetInvitationPreviewResponseObject, error) {
	pv, err := s.d.Identity.PreviewInvitation(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	return gen.GetInvitationPreview200JSONResponse{
		OrganisationName: pv.OrganisationName, Email: pv.Email, Role: gen.Role(pv.Role),
		ExpiresAt: pv.ExpiresAt.UTC(), ExistingAccount: pv.ExistingAccount,
	}, nil
}

func (s *Server) AcceptInvitation(ctx context.Context, req gen.AcceptInvitationRequestObject) (gen.AcceptInvitationResponseObject, error) {
	token, err := s.d.Identity.AcceptInvitation(ctx, req.Token, req.Body.Name, req.Body.Password)
	if err != nil {
		return nil, err
	}
	info, err := s.d.Identity.InfoForToken(ctx, token)
	if err != nil {
		return nil, err
	}
	cookie := s.d.Identity.Cookie(token)
	return gen.AcceptInvitation200JSONResponse{Body: toSessionInfo(info), Headers: gen.AcceptInvitation200ResponseHeaders{SetCookie: &cookie}}, nil
}

// -------------------------------------------------------------------- locations

func (s *Server) ListLocations(ctx context.Context, req gen.ListLocationsRequestObject) (gen.ListLocationsResponseObject, error) {
	res, err := s.d.Tenancy.ListLocations(ctx, tenancy.ListLocationsParams{
		Limit: req.Params.Limit, Cursor: req.Params.Cursor, UpdatedSince: req.Params.UpdatedSince,
		IncludeArchived: req.Params.IncludeArchived != nil && *req.Params.IncludeArchived,
	})
	if err != nil {
		return nil, err
	}
	out := gen.ListLocations200JSONResponse{Items: make([]gen.Location, len(res.Items)), NextCursor: res.NextCursor, SyncUntil: res.SyncUntil}
	for i, l := range res.Items {
		out.Items[i] = toLocation(l)
	}
	return out, nil
}

func (s *Server) CreateLocation(ctx context.Context, req gen.CreateLocationRequestObject) (gen.CreateLocationResponseObject, error) {
	b := req.Body
	loc, err := s.d.Tenancy.CreateLocation(ctx, tenancy.LocationInput{
		Name: &b.Name, AddressLine1: b.AddressLine1, AddressLine2: b.AddressLine2, Suburb: b.Suburb,
		State: b.State, Postcode: b.Postcode, Phone: b.Phone, Email: b.Email, Timezone: &b.Timezone,
	})
	if err != nil {
		return nil, err
	}
	return gen.CreateLocation201JSONResponse(toLocation(loc)), nil
}

func (s *Server) GetLocation(ctx context.Context, req gen.GetLocationRequestObject) (gen.GetLocationResponseObject, error) {
	loc, err := s.d.Tenancy.GetLocation(ctx, req.LocationId)
	if err != nil {
		return nil, err
	}
	return gen.GetLocation200JSONResponse(toLocation(loc)), nil
}

func (s *Server) UpdateLocation(ctx context.Context, req gen.UpdateLocationRequestObject) (gen.UpdateLocationResponseObject, error) {
	b := req.Body
	loc, err := s.d.Tenancy.UpdateLocation(ctx, req.LocationId, tenancy.LocationInput{
		Name: b.Name, AddressLine1: b.AddressLine1, AddressLine2: b.AddressLine2, Suburb: b.Suburb,
		State: b.State, Postcode: b.Postcode, Phone: b.Phone, Email: b.Email, Timezone: b.Timezone,
	})
	if err != nil {
		return nil, err
	}
	return gen.UpdateLocation200JSONResponse(toLocation(loc)), nil
}

func (s *Server) ArchiveLocation(ctx context.Context, req gen.ArchiveLocationRequestObject) (gen.ArchiveLocationResponseObject, error) {
	loc, err := s.d.Tenancy.ArchiveLocation(ctx, req.LocationId)
	if err != nil {
		return nil, err
	}
	return gen.ArchiveLocation200JSONResponse(toLocation(loc)), nil
}

// ---------------------------------------------------------------------- members

func (s *Server) ListMembers(ctx context.Context, req gen.ListMembersRequestObject) (gen.ListMembersResponseObject, error) {
	res, err := s.d.Members.List(ctx, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := gen.ListMembers200JSONResponse{Items: make([]gen.Member, len(res.Items)), NextCursor: res.NextCursor}
	for i, m := range res.Items {
		out.Items[i] = toMember(m)
	}
	return out, nil
}

func (s *Server) UpdateMember(ctx context.Context, req gen.UpdateMemberRequestObject) (gen.UpdateMemberResponseObject, error) {
	in := access.MemberUpdate{LocationIDs: req.Body.LocationIds}
	if req.Body.Role != nil {
		r := access.Role(*req.Body.Role)
		in.Role = &r
	}
	m, err := s.d.Members.Update(ctx, req.MemberId, in)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMember200JSONResponse(toMember(m)), nil
}

func (s *Server) DeactivateMember(ctx context.Context, req gen.DeactivateMemberRequestObject) (gen.DeactivateMemberResponseObject, error) {
	m, err := s.d.Members.Deactivate(ctx, req.MemberId)
	if err != nil {
		return nil, err
	}
	return gen.DeactivateMember200JSONResponse(toMember(m)), nil
}

func (s *Server) ReactivateMember(ctx context.Context, req gen.ReactivateMemberRequestObject) (gen.ReactivateMemberResponseObject, error) {
	m, err := s.d.Members.Reactivate(ctx, req.MemberId)
	if err != nil {
		return nil, err
	}
	return gen.ReactivateMember200JSONResponse(toMember(m)), nil
}

// ------------------------------------------------------------------ invitations

func (s *Server) ListInvitations(ctx context.Context, req gen.ListInvitationsRequestObject) (gen.ListInvitationsResponseObject, error) {
	res, err := s.d.Identity.ListInvitations(ctx, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := gen.ListInvitations200JSONResponse{Items: make([]gen.Invitation, len(res.Items)), NextCursor: res.NextCursor}
	for i, inv := range res.Items {
		out.Items[i] = toInvitation(inv)
	}
	return out, nil
}

func (s *Server) CreateInvitation(ctx context.Context, req gen.CreateInvitationRequestObject) (gen.CreateInvitationResponseObject, error) {
	in := identity.InvitationInput{Email: req.Body.Email, Role: access.Role(req.Body.Role)}
	if req.Body.LocationIds != nil {
		in.LocationIDs = *req.Body.LocationIds
	}
	inv, err := s.d.Identity.CreateInvitation(ctx, in)
	if err != nil {
		return nil, err
	}
	return gen.CreateInvitation201JSONResponse(toInvitation(inv)), nil
}

func (s *Server) RevokeInvitation(ctx context.Context, req gen.RevokeInvitationRequestObject) (gen.RevokeInvitationResponseObject, error) {
	inv, err := s.d.Identity.RevokeInvitation(ctx, req.InvitationId)
	if err != nil {
		return nil, err
	}
	return gen.RevokeInvitation200JSONResponse(toInvitation(inv)), nil
}

// ------------------------------------------------------------------------ audit

func (s *Server) ListAuditEvents(ctx context.Context, req gen.ListAuditEventsRequestObject) (gen.ListAuditEventsResponseObject, error) {
	p := req.Params
	res, err := s.d.Audit.List(ctx, auditlog.ListParams{
		Limit: p.Limit, Cursor: p.Cursor, From: p.From, To: p.To, ActorUserID: p.ActorUserId,
		Action: p.Action, TargetType: p.TargetType, TargetID: p.TargetId,
	})
	if err != nil {
		return nil, err
	}
	out := gen.ListAuditEvents200JSONResponse{Items: make([]gen.AuditEvent, len(res.Items)), NextCursor: res.NextCursor}
	for i, e := range res.Items {
		out.Items[i] = toAuditEvent(e)
	}
	return out, nil
}

// ------------------------------------------------------------------- converters

func toSessionInfo(i identity.SessionInfo) gen.SessionInfo {
	out := gen.SessionInfo{
		User:                  gen.UserSummary{Id: i.User.ID, Email: i.User.Email, Name: i.User.Name},
		Memberships:           make([]gen.MembershipSummary, len(i.Memberships)),
		ActiveOrganisationId:  i.ActiveOrgID,
		ActiveLocationId:      i.ActiveLocationID,
		Permissions:           i.Permissions,
		AccessibleLocationIds: i.AccessibleLocationIDs,
	}
	for k, m := range i.Memberships {
		out.Memberships[k] = gen.MembershipSummary{
			OrganisationId: m.OrgID, OrganisationName: m.OrgName,
			OrganisationStatus: gen.MembershipSummaryOrganisationStatus(m.OrgStatus),
			Role:               gen.Role(m.Role), Status: gen.MembershipSummaryStatus(m.Status),
		}
	}
	if i.Role != nil {
		r := gen.SessionInfoRole(*i.Role)
		out.Role = &r
	}
	return out
}

func toLocation(l store.Location) gen.Location {
	return gen.Location{
		Id: l.ID, Name: l.Name, AddressLine1: l.AddressLine1, AddressLine2: l.AddressLine2,
		Suburb: l.Suburb, State: l.State, Postcode: l.Postcode, Phone: l.Phone, Email: l.Email,
		Timezone: l.Timezone, ArchivedAt: utcPtr(l.ArchivedAt), CreatedAt: l.CreatedAt.UTC(), UpdatedAt: l.UpdatedAt.UTC(),
	}
}

func toMember(m access.Member) gen.Member {
	locs := m.LocationIds
	if locs == nil {
		locs = []uuid.UUID{}
	}
	return gen.Member{
		Id: m.ID, UserId: m.UserID, Email: m.Email, Name: m.Name, Role: gen.Role(m.Role),
		Status: gen.MemberStatus(m.Status), LocationIds: locs,
		CreatedAt: m.CreatedAt.UTC(), UpdatedAt: m.UpdatedAt.UTC(),
	}
}

func toInvitation(i store.Invitation) gen.Invitation {
	locs := i.LocationIds
	if locs == nil {
		locs = []uuid.UUID{}
	}
	return gen.Invitation{
		Id: i.ID, Email: i.Email, Role: gen.Role(i.Role), LocationIds: locs, InvitedBy: i.InvitedBy,
		ExpiresAt: i.ExpiresAt.UTC(), CreatedAt: i.CreatedAt.UTC(),
		AcceptedAt: utcPtr(i.AcceptedAt), RevokedAt: utcPtr(i.RevokedAt),
	}
}

func toAuditEvent(e store.ListAuditEventsRow) gen.AuditEvent {
	out := gen.AuditEvent{
		Id: e.ID, Action: e.Action, TargetType: e.TargetType, TargetId: e.TargetID,
		Ip: e.Ip, UserAgent: e.UserAgent, RequestId: e.RequestID, CreatedAt: e.CreatedAt.UTC(),
		Details: map[string]any{},
	}
	_ = json.Unmarshal(e.Details, &out.Details)
	if e.ActorUserID != nil {
		out.Actor = &gen.AuditActor{Id: *e.ActorUserID, Email: e.ActorEmail, Name: e.ActorName}
	}
	return out
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
