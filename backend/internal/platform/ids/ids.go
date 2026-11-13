// Package ids generates identifiers.
package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"github.com/google/uuid"
)

// New returns a UUIDv7: time-ordered, so primary-key indexes and cursors stay efficient.
func New() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// NewToken returns a random URL-safe secret token (32 bytes of entropy) and its SHA-256 hash.
// Only the hash is ever stored.
func NewToken() (token string, hash []byte) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token)
}

// HashToken returns the SHA-256 hash used to look a token up.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
