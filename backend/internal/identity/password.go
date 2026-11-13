package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"

	"dentalsaas/internal/platform/apperr"
)

const (
	MinPasswordLength = 12
	maxPasswordLength = 256 // bounds hashing cost
)

// argon2id parameters (OWASP baseline). Stored in each hash, so they can be raised later.
const (
	argonMemoryKiB = 19 * 1024
	argonTime      = 2
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// ValidatePassword enforces the password policy.
func ValidatePassword(password, email string) error {
	switch {
	case len([]rune(password)) < MinPasswordLength:
		return apperr.Validation(apperr.FieldError{Field: "password", Message: fmt.Sprintf("must be at least %d characters", MinPasswordLength)})
	case len(password) > maxPasswordLength:
		return apperr.Validation(apperr.FieldError{Field: "password", Message: fmt.Sprintf("must be at most %d characters", maxPasswordLength)})
	case strings.EqualFold(strings.TrimSpace(password), strings.TrimSpace(email)):
		return apperr.Validation(apperr.FieldError{Field: "password", Message: "must not be your email address"})
	}
	return nil
}

// HashPassword returns an argon2id hash in PHC string format.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

var errBadHash = errors.New("invalid password hash format")

// VerifyPassword checks password against a PHC-format argon2id hash in constant time.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var mem, t uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &threads); err != nil {
		return false, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(password), salt, t, mem, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is verified against when an email has no account, so sign-in takes the same
// time whether or not the account exists.
var dummyHash = sync.OnceValue(func() string {
	h, err := HashPassword("timing-equaliser-not-a-real-password")
	if err != nil {
		panic(err)
	}
	return h
})
