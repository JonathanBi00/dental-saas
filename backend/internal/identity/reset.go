package identity

import (
	"context"

	"dentalsaas/internal/platform/apperr"
	"dentalsaas/internal/platform/audit"
	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/platform/ids"
	"dentalsaas/internal/store"
)

// RequestPasswordReset emails a single-use reset link if the account exists. The caller
// always gets the same response, so this cannot be used to discover accounts.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	email = normaliseEmail(email)
	if email == "" {
		return apperr.Validation(apperr.FieldError{Field: "email", Message: "is required"})
	}
	var token string
	var user *userRecord
	err := s.DB.InAuthTx(ctx, func(tx *db.Tx) error {
		var err error
		if user, err = findUserByEmail(ctx, tx, email); err != nil || user == nil {
			return err
		}
		var hash []byte
		token, hash = ids.NewToken()
		if err := tx.CreatePasswordReset(ctx, store.CreatePasswordResetParams{
			ID: ids.New(), UserID: user.ID, TokenHash: hash, ExpiresAt: s.Now().Add(PasswordResetTTL),
		}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: "auth.password_reset_requested", TargetType: "user", TargetID: user.ID,
		})
	})
	if err != nil {
		return err
	}
	if user != nil {
		s.send(ctx, resetEmail, user.Email, map[string]string{
			"Name": user.Name, "Link": s.AppOrigin + "/reset-password/" + token,
		})
	}
	return nil
}

var errResetInvalid = apperr.Gone("reset_link_invalid", "This password reset link is invalid or has expired. Request a new one.")

// CompletePasswordReset sets a new password and revokes all of the user's sessions.
func (s *Service) CompletePasswordReset(ctx context.Context, token, password string) error {
	return s.DB.InAuthTx(ctx, func(tx *db.Tx) error {
		r, err := tx.GetPasswordResetByTokenHash(ctx, ids.HashToken(token))
		if db.IsNoRows(err) {
			return errResetInvalid
		} else if err != nil {
			return err
		}
		now := s.Now()
		if r.UsedAt != nil || !now.Before(r.ExpiresAt) {
			return errResetInvalid
		}
		if err := tx.SetUser(ctx, r.UserID); err != nil {
			return err
		}
		user, err := tx.GetUser(ctx, r.UserID)
		if err != nil {
			return err
		}
		if err := ValidatePassword(password, user.Email); err != nil {
			return err
		}
		hash, err := HashPassword(password)
		if err != nil {
			return err
		}
		if n, err := tx.MarkPasswordResetUsed(ctx, store.MarkPasswordResetUsedParams{ID: r.ID, UsedAt: &now}); err != nil {
			return err
		} else if n == 0 {
			return errResetInvalid
		}
		if err := tx.UpdateUserPassword(ctx, store.UpdateUserPasswordParams{ID: user.ID, PasswordHash: hash}); err != nil {
			return err
		}
		if err := tx.RevokeUserSessions(ctx, store.RevokeUserSessionsParams{UserID: user.ID, RevokedAt: &now}); err != nil {
			return err
		}
		if err := tx.DeleteLoginAttempt(ctx, user.Email); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			ActorID: user.ID, Action: "auth.password_reset_completed", TargetType: "user", TargetID: user.ID,
		})
	})
}
