package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

// Task 6.2: secrets never reach an audit entry, however they are nested.
func TestSanitizeDropsSecrets(t *testing.T) {
	ch := Changes{}
	ch.Diff("name", "Old", "New")
	ch.Diff("password_hash", "$argon2id$old", "$argon2id$new")
	in := map[string]any{
		"email":         "a@b.co",
		"password":      "hunter2hunter2",
		"PasswordHash":  "$argon2id$...",
		"reset_token":   "tok",
		"session_id":    "sid",
		"client_secret": "s",
		"changes":       ch,
		"nested":        map[string]any{"token_hash": "x", "kept": 1},
	}
	b, err := json.Marshal(Sanitize(in))
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	for _, leaked := range []string{"hunter2", "argon2id", "tok", "sid", `"s"`, "token_hash"} {
		if strings.Contains(out, leaked) {
			t.Errorf("sanitised details still contain %q: %s", leaked, out)
		}
	}
	for _, kept := range []string{`"email":"a@b.co"`, `"kept":1`, `"name":{"old":"Old","new":"New"}`} {
		if !strings.Contains(out, kept) {
			t.Errorf("sanitised details lost %s: %s", kept, out)
		}
	}
}

func TestChangesOnlyRecordsDifferences(t *testing.T) {
	ch := Changes{}
	ch.Diff("name", "Same", "Same")
	ch.Diff("timezone", "Australia/Sydney", "Australia/Perth")
	if len(ch) != 1 || ch["timezone"] == nil {
		t.Fatalf("changes = %v, want only timezone", ch)
	}
}
