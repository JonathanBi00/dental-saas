// Package audit writes append-only audit entries.
//
// Record must be called with the same transaction as the change it describes, so the entry
// and the change commit or roll back together. Request metadata (request ID, IP, user agent)
// comes from the context.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"

	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/platform/ids"
	"dentalsaas/internal/platform/reqctx"
	"dentalsaas/internal/store"
)

type Event struct {
	OrgID      uuid.UUID // uuid.Nil for pre-tenant events (e.g. failed sign-in)
	ActorID    uuid.UUID // uuid.Nil when unknown
	Action     string    // "<resource>.<verb>", e.g. "location.updated"
	TargetType string
	TargetID   uuid.UUID
	Details    map[string]any
}

// Record inserts the event in tx.
func Record(ctx context.Context, tx *db.Tx, e Event) error {
	details, err := json.Marshal(Sanitize(e.Details))
	if err != nil {
		return fmt.Errorf("audit details: %w", err)
	}
	meta := reqctx.From(ctx)
	err = tx.InsertAuditEvent(ctx, store.InsertAuditEventParams{
		ID:          ids.New(),
		OrgID:       nilIfZero(e.OrgID),
		ActorUserID: nilIfZero(e.ActorID),
		Action:      e.Action,
		TargetType:  nilIfEmpty(e.TargetType),
		TargetID:    nilIfZero(e.TargetID),
		Ip:          meta.IP,
		UserAgent:   meta.UserAgent,
		RequestID:   meta.RequestID,
		Details:     details,
	})
	if err != nil {
		return fmt.Errorf("record audit event %s: %w", e.Action, err)
	}
	return nil
}

// secretMarkers: any detail key containing one of these is dropped, at any depth.
var secretMarkers = []string{"password", "token", "secret", "hash", "session"}

// Sanitize returns a copy of details with secret-looking keys removed.
func Sanitize(details map[string]any) map[string]any {
	out := make(map[string]any, len(details))
	for k, v := range details {
		if isSecretKey(k) {
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			v = Sanitize(nested)
		} else if ch, ok := v.(Changes); ok {
			v = Sanitize(map[string]any(ch))
		}
		out[k] = v
	}
	return out
}

func isSecretKey(k string) bool {
	k = strings.ToLower(k)
	for _, m := range secretMarkers {
		if strings.Contains(k, m) {
			return true
		}
	}
	return false
}

// Change is the before/after value of one field.
type Change struct {
	Old any `json:"old"`
	New any `json:"new"`
}

// Changes collects field-level differences for an update event.
type Changes map[string]any

// Diff records field if old and new differ.
func (c Changes) Diff(field string, old, new any) {
	if !reflect.DeepEqual(old, new) {
		c[field] = Change{Old: old, New: new}
	}
}

func nilIfZero(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
