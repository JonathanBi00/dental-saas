// Package identity handles user accounts, sign-in, sessions, password reset and invitations.
package identity

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"dentalsaas/internal/access"
	"dentalsaas/internal/platform/apperr"
	"dentalsaas/internal/platform/audit"
	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/platform/ids"
	"dentalsaas/internal/platform/mail"
	"dentalsaas/internal/platform/reqctx"
	"dentalsaas/internal/store"
)

const (
	CookieName      = "ds_session"
	IdleTimeout     = 60 * time.Minute
	AbsoluteTimeout = 12 * time.Hour
	touchInterval   = time.Minute

	maxFailedLogins = 5
	failureWindow   = 15 * time.Minute
	lockoutDuration = 15 * time.Minute

	InvitationTTL    = 7 * 24 * time.Hour
	PasswordResetTTL = 60 * time.Minute
)

type Service struct {
	DB        *db.Pool
	Mail      mail.Sender
	AppOrigin string // base URL for links in emails
	Now       func() time.Time
}

func New(pool *db.Pool, sender mail.Sender, appOrigin string) *Service {
	return &Service{DB: pool, Mail: sender, AppOrigin: strings.TrimRight(appOrigin, "/"), Now: time.Now}
}

func normaliseEmail(e string) string { return strings.TrimSpace(e) }

// ---------------------------------------------------------------------- cookies

// Cookie returns the Set-Cookie value for a new session token.
func (s *Service) Cookie(token string) string {
	c := http.Cookie{
		Name: CookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: strings.HasPrefix(s.AppOrigin, "https://"), MaxAge: int(AbsoluteTimeout.Seconds()),
	}
	return c.String()
}

// ClearCookie returns a Set-Cookie value that deletes the session cookie.
func (s *Service) ClearCookie() string {
	c := http.Cookie{
		Name: CookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: strings.HasPrefix(s.AppOrigin, "https://"), MaxAge: -1,
	}
	return c.String()
}

// ---------------------------------------------------------- pre-tenant lookups
// Hand-written because sqlc cannot infer the columns of set-returning functions.

type userRecord struct {
	ID           uuid.UUID
	Email        string
	Name         string
	PasswordHash string
}

func findUserByEmail(ctx context.Context, tx *db.Tx, email string) (*userRecord, error) {
	var u userRecord
	err := tx.PG.QueryRow(ctx,
		"SELECT id, email::text, name, password_hash FROM auth_find_user_by_email($1::citext)", email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

type invitationRecord struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	OrgName     string
	OrgStatus   string
	Email       string
	Role        access.Role
	LocationIDs []uuid.UUID
	ExpiresAt   time.Time
	AcceptedAt  *time.Time
	RevokedAt   *time.Time
}

func findInvitation(ctx context.Context, tx *db.Tx, token string) (*invitationRecord, error) {
	var r invitationRecord
	err := tx.PG.QueryRow(ctx, `
		SELECT id, org_id, org_name, org_status, email::text, role, location_ids,
		       expires_at, accepted_at, revoked_at
		FROM auth_find_invitation($1)`, ids.HashToken(token),
	).Scan(&r.ID, &r.OrgID, &r.OrgName, &r.OrgStatus, &r.Email, &r.Role, &r.LocationIDs,
		&r.ExpiresAt, &r.AcceptedAt, &r.RevokedAt)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// --------------------------------------------------------------------- sessions

func (s *Service) createSession(ctx context.Context, tx *db.Tx, userID uuid.UUID, activeOrg *uuid.UUID) (string, error) {
	token, hash := ids.NewToken()
	now := s.Now()
	meta := reqctx.From(ctx)
	_, err := tx.CreateSession(ctx, store.CreateSessionParams{
		ID: ids.New(), TokenHash: hash, UserID: userID, ActiveOrgID: activeOrg,
		Ip: meta.IP, UserAgent: meta.UserAgent, CreatedAt: now, ExpiresAt: now.Add(AbsoluteTimeout),
	})
	return token, err
}

// Authenticate resolves a session token to a Principal. It re-checks the active
// organisation and membership on every call, so suspension and deactivation apply at once.
// When needOrg is true, a request whose organisation context is unusable is rejected with 403;
// otherwise the principal is returned without an organisation.
func (s *Service) Authenticate(ctx context.Context, token string, needOrg bool) (access.Principal, error) {
	var p access.Principal
	err := s.DB.InAuthTx(ctx, func(tx *db.Tx) error {
		sess, err := tx.GetSessionByTokenHash(ctx, ids.HashToken(token))
		if db.IsNoRows(err) {
			return errSessionInvalid
		}
		if err != nil {
			return err
		}
		now := s.Now()
		if sess.RevokedAt != nil || !now.Before(sess.ExpiresAt) || now.Sub(sess.LastSeenAt) > IdleTimeout {
			return errSessionInvalid
		}
		if now.Sub(sess.LastSeenAt) >= touchInterval {
			if err := tx.TouchSession(ctx, store.TouchSessionParams{ID: sess.ID, LastSeenAt: now}); err != nil {
				return err
			}
		}
		p, err = s.principal(ctx, tx, sess, needOrg)
		return err
	})
	return p, err
}

var errSessionInvalid = apperr.Unauthorized("Your session has expired. Please sign in again.")

// IsSessionInvalid reports whether err means the cookie should be cleared.
func IsSessionInvalid(err error) bool { return err == errSessionInvalid }

// principal builds the caller for a session inside tx (which it switches to the session's user
// and organisation).
func (s *Service) principal(ctx context.Context, tx *db.Tx, sess store.Session, needOrg bool) (access.Principal, error) {
	if err := tx.SetUser(ctx, sess.UserID); err != nil {
		return access.Principal{}, err
	}
	user, err := tx.GetUser(ctx, sess.UserID)
	if err != nil {
		return access.Principal{}, err
	}
	p := access.Principal{SessionID: sess.ID, UserID: user.ID, Email: user.Email, Name: user.Name}
	if sess.ActiveOrgID == nil {
		return p, nil
	}

	orgID := *sess.ActiveOrgID
	if err := tx.SetOrg(ctx, orgID); err != nil {
		return p, err
	}
	reject := func(e *apperr.Error) (access.Principal, error) {
		if needOrg {
			return p, e
		}
		return p, nil
	}
	org, err := tx.GetOrganisation(ctx, orgID)
	if db.IsNoRows(err) {
		return reject(errNotMember)
	} else if err != nil {
		return p, err
	}
	if org.Status != "active" {
		return reject(apperr.Forbidden("organisation_suspended", "This organisation is suspended."))
	}
	m, err := tx.GetMembershipByUser(ctx, store.GetMembershipByUserParams{OrgID: orgID, UserID: user.ID})
	if db.IsNoRows(err) {
		return reject(errNotMember)
	} else if err != nil {
		return p, err
	}
	if m.Status != "active" {
		return reject(apperr.Forbidden("membership_inactive", "Your access to this organisation has been deactivated."))
	}

	p.OrgID = orgID
	p.Role = access.Role(m.Role)
	if !p.Role.Unrestricted() {
		locs, err := tx.ListMembershipLocationIDs(ctx, m.ID)
		if err != nil {
			return p, err
		}
		if len(locs) > 0 {
			p.LocationIDs = locs
		}
	}
	if sess.ActiveLocationID != nil && p.CanAccessLocation(*sess.ActiveLocationID) {
		p.ActiveLocationID = sess.ActiveLocationID
	}
	return p, nil
}

var errNotMember = apperr.Forbidden("not_a_member", "You are not a member of this organisation.")

// ------------------------------------------------------------------- sign in/out

// Login verifies credentials and creates a session. It returns the session token.
func (s *Service) Login(ctx context.Context, email, password string) (string, error) {
	email = normaliseEmail(email)
	if email == "" || password == "" {
		return "", errBadCredentials
	}

	var token string
	var outcome error
	err := s.DB.InAuthTx(ctx, func(tx *db.Tx) error {
		now := s.Now()
		attempt, err := tx.GetLoginAttempt(ctx, email)
		hasAttempt := err == nil
		if err != nil && !db.IsNoRows(err) {
			return err
		}
		if hasAttempt && attempt.LockedUntil != nil && now.Before(*attempt.LockedUntil) {
			outcome = apperr.TooManyRequests("Too many failed sign-in attempts. Try again later.")
			return nil
		}

		user, err := findUserByEmail(ctx, tx, email)
		if err != nil {
			return err
		}
		hash := dummyHash()
		if user != nil {
			hash = user.PasswordHash
		}
		ok, err := VerifyPassword(password, hash)
		if err != nil {
			return err
		}

		if user == nil || !ok {
			// Record the failure and commit it: the error is returned after the transaction.
			outcome = errBadCredentials
			return s.recordFailedLogin(ctx, tx, email, user, attempt, hasAttempt, now)
		}

		if hasAttempt {
			if err := tx.DeleteLoginAttempt(ctx, email); err != nil {
				return err
			}
		}
		if err := tx.SetUser(ctx, user.ID); err != nil {
			return err
		}
		activeOrg, err := soleActiveOrganisation(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		ev := audit.Event{ActorID: user.ID, Action: "auth.login_succeeded", TargetType: "user", TargetID: user.ID}
		if activeOrg != nil {
			if err := tx.SetOrg(ctx, *activeOrg); err != nil {
				return err
			}
			ev.OrgID = *activeOrg
		}
		if token, err = s.createSession(ctx, tx, user.ID, activeOrg); err != nil {
			return err
		}
		return audit.Record(ctx, tx, ev)
	})
	if err != nil {
		return "", err
	}
	return token, outcome
}

var errBadCredentials = apperr.Unauthorized("Incorrect email or password.")

func (s *Service) recordFailedLogin(ctx context.Context, tx *db.Tx, email string, user *userRecord,
	attempt store.LoginAttempt, hasAttempt bool, now time.Time) error {
	failures, windowStart := int32(1), now
	if hasAttempt && now.Sub(attempt.WindowStartedAt) < failureWindow {
		failures, windowStart = attempt.Failures+1, attempt.WindowStartedAt
	}
	var lockedUntil *time.Time
	if failures >= maxFailedLogins {
		t := now.Add(lockoutDuration)
		lockedUntil = &t
	}
	if err := tx.UpsertLoginAttempt(ctx, store.UpsertLoginAttemptParams{
		Email: email, Failures: failures, WindowStartedAt: windowStart, LockedUntil: lockedUntil,
	}); err != nil {
		return err
	}

	ev := audit.Event{Action: "auth.login_failed", Details: map[string]any{"email": email, "reason": "invalid_credentials"}}
	if user != nil {
		ev.TargetType, ev.TargetID = "user", user.ID
	}
	if err := audit.Record(ctx, tx, ev); err != nil {
		return err
	}
	if lockedUntil != nil {
		ev.Action = "auth.locked_out"
		ev.Details = map[string]any{"email": email, "locked_until": lockedUntil.UTC()}
		return audit.Record(ctx, tx, ev)
	}
	return nil
}

// soleActiveOrganisation returns the user's organisation if they have exactly one usable membership.
func soleActiveOrganisation(ctx context.Context, tx *db.Tx, userID uuid.UUID) (*uuid.UUID, error) {
	ms, err := tx.ListUserMemberships(ctx, userID)
	if err != nil {
		return nil, err
	}
	var found *uuid.UUID
	for _, m := range ms {
		if m.Status == "active" && m.OrgStatus == "active" {
			if found != nil {
				return nil, nil
			}
			id := m.OrgID
			found = &id
		}
	}
	return found, nil
}

// Logout revokes the caller's session.
func (s *Service) Logout(ctx context.Context) error {
	p, err := access.Authenticated(ctx)
	if err != nil {
		return err
	}
	return s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		if err := tx.RevokeSession(ctx, store.RevokeSessionParams{ID: p.SessionID, RevokedAt: ptr(s.Now())}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: p.OrgID, ActorID: p.UserID, Action: "auth.logout", TargetType: "user", TargetID: p.UserID,
		})
	})
}

// ------------------------------------------------------------- session details

type SessionInfo struct {
	User                  store.User
	Memberships           []store.ListUserMembershipsRow
	ActiveOrgID           *uuid.UUID
	ActiveLocationID      *uuid.UUID
	Role                  *access.Role
	Permissions           []string
	AccessibleLocationIDs []uuid.UUID
}

// Info describes the session for the client (user, memberships, permissions, locations).
func (s *Service) Info(ctx context.Context, p access.Principal) (SessionInfo, error) {
	info := SessionInfo{Permissions: p.Permissions(), AccessibleLocationIDs: []uuid.UUID{}}
	err := s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		var err error
		if info.User, err = tx.GetUser(ctx, p.UserID); err != nil {
			return err
		}
		if info.Memberships, err = tx.ListUserMemberships(ctx, p.UserID); err != nil {
			return err
		}
		if !p.HasOrg() {
			return nil
		}
		info.ActiveOrgID = &p.OrgID
		info.ActiveLocationID = p.ActiveLocationID
		role := p.Role
		info.Role = &role
		ids, err := tx.ListAccessibleLocationIDs(ctx, p.LocationIDs)
		if err != nil {
			return err
		}
		if ids != nil {
			info.AccessibleLocationIDs = ids
		}
		return nil
	})
	return info, err
}

// InfoForToken authenticates a fresh token (after sign-in) and describes it.
func (s *Service) InfoForToken(ctx context.Context, token string) (SessionInfo, error) {
	p, err := s.Authenticate(ctx, token, false)
	if err != nil {
		return SessionInfo{}, err
	}
	return s.Info(ctx, p)
}

// SwitchOrganisation sets the session's active organisation. The user must be an active
// member of an active organisation; otherwise nothing changes.
func (s *Service) SwitchOrganisation(ctx context.Context, orgID uuid.UUID) (access.Principal, error) {
	p, err := access.Authenticated(ctx)
	if err != nil {
		return p, err
	}
	var np access.Principal
	err = s.DB.InTenantTx(ctx, db.Tenant{OrgID: orgID, UserID: p.UserID}, func(tx *db.Tx) error {
		sess := store.Session{ID: p.SessionID, UserID: p.UserID, ActiveOrgID: &orgID}
		var err error
		if np, err = s.principal(ctx, tx, sess, true); err != nil {
			return err
		}
		if err := tx.SetSessionOrganisation(ctx, store.SetSessionOrganisationParams{ID: p.SessionID, ActiveOrgID: &orgID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: orgID, ActorID: p.UserID, Action: "session.organisation_switched",
			TargetType: "organisation", TargetID: orgID,
		})
	})
	return np, err
}

// SwitchLocation sets (or clears, with nil) the session's active location.
func (s *Service) SwitchLocation(ctx context.Context, locationID *uuid.UUID) (access.Principal, error) {
	p, err := access.RequireOrg(ctx)
	if err != nil {
		return p, err
	}
	err = s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		if locationID != nil {
			loc, err := tx.GetLocation(ctx, *locationID)
			if db.IsNoRows(err) {
				return apperr.NotFound("Location")
			} else if err != nil {
				return err
			}
			if loc.ArchivedAt != nil || !p.CanAccessLocation(loc.ID) {
				return apperr.Forbidden("location_not_accessible", "You do not have access to this location.")
			}
		}
		return tx.SetSessionLocation(ctx, store.SetSessionLocationParams{ID: p.SessionID, ActiveLocationID: locationID})
	})
	if err != nil {
		return p, err
	}
	p.ActiveLocationID = locationID
	return p, nil
}

// ------------------------------------------------------------------------ utils

func ptr[T any](v T) *T { return &v }
