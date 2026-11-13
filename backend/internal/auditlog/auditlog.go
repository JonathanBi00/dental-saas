// Package auditlog lets authorised members read their organisation's audit trail.
// (Writing lives in platform/audit, used by every domain.)
package auditlog

import (
	"context"
	"time"

	"github.com/google/uuid"

	"dentalsaas/internal/access"
	"dentalsaas/internal/platform/apperr"
	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/platform/httpx"
	"dentalsaas/internal/store"
)

type Service struct {
	DB *db.Pool
}

type ListParams struct {
	Limit       *int
	Cursor      *string
	From, To    *time.Time
	ActorUserID *uuid.UUID
	Action      *string
	TargetType  *string
	TargetID    *uuid.UUID
}

type ListResult struct {
	Items      []store.ListAuditEventsRow
	NextCursor *string
}

// List returns events newest first. RLS limits rows to the caller's organisation.
func (s *Service) List(ctx context.Context, in ListParams) (ListResult, error) {
	var res ListResult
	p, err := access.Require(ctx, access.PermAuditRead)
	if err != nil {
		return res, err
	}
	n, err := httpx.Limit(in.Limit)
	if err != nil {
		return res, err
	}
	cur, err := httpx.DecodeCursor(in.Cursor)
	if err != nil {
		return res, err
	}
	if in.From != nil && in.To != nil && !in.From.Before(*in.To) {
		return res, apperr.Validation(apperr.FieldError{Field: "to", Message: "must be after from"})
	}
	q := store.ListAuditEventsParams{
		FromTime: in.From, ToTime: in.To, ActorUserID: in.ActorUserID, Action: emptyNil(in.Action),
		TargetType: emptyNil(in.TargetType), TargetID: in.TargetID, RowLimit: n + 1,
	}
	if cur != nil {
		q.BeforeID = &cur.ID
	}
	err = s.DB.InTenantTx(ctx, p.Tenant(), func(tx *db.Tx) error {
		rows, err := tx.ListAuditEvents(ctx, q)
		if err != nil {
			return err
		}
		var more bool
		res.Items, more = httpx.Page(rows, n)
		if more {
			res.NextCursor = httpx.EncodeCursor("", res.Items[len(res.Items)-1].ID)
		}
		return nil
	})
	return res, err
}

func emptyNil(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}
