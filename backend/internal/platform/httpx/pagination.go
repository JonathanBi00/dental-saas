package httpx

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"

	"dentalsaas/internal/platform/apperr"
)

const (
	DefaultLimit = 50
	MaxLimit     = 200
	// SyncMargin is subtracted from "now" when computing sync_until, so rows written by
	// transactions that commit after their updated_at timestamp are not skipped.
	SyncMargin = 5 * time.Second
)

// Limit validates a requested page size and applies the default.
func Limit(requested *int) (int32, error) {
	if requested == nil {
		return DefaultLimit, nil
	}
	if *requested < 1 || *requested > MaxLimit {
		return 0, apperr.Validation(apperr.FieldError{
			Field: "limit", Message: "must be between 1 and " + strconv.Itoa(MaxLimit),
		})
	}
	return int32(*requested), nil
}

// Cursor is a keyset position: the sort key of the last row returned plus its id.
// Encoded as opaque base64url JSON; clients must not parse it.
type Cursor struct {
	Key string    `json:"k"`
	ID  uuid.UUID `json:"i"`
}

func EncodeCursor(key string, id uuid.UUID) *string {
	b, _ := json.Marshal(Cursor{Key: key, ID: id})
	s := base64.RawURLEncoding.EncodeToString(b)
	return &s
}

func DecodeCursor(s *string) (*Cursor, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(*s)
	var c Cursor
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	if err != nil || c.ID == uuid.Nil {
		return nil, apperr.Validation(apperr.FieldError{Field: "cursor", Message: "is invalid"})
	}
	return &c, nil
}

// TimeKey and ParseTimeKey encode timestamp sort keys losslessly.
func TimeKey(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func ParseTimeKey(c *Cursor) (*time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, c.Key)
	if err != nil {
		return nil, apperr.Validation(apperr.FieldError{Field: "cursor", Message: "is invalid"})
	}
	return &t, nil
}

// Page trims a result fetched with limit+1 rows and reports whether there is another page.
func Page[T any](rows []T, limit int32) ([]T, bool) {
	if int32(len(rows)) > limit {
		return rows[:limit], true
	}
	return rows, false
}
