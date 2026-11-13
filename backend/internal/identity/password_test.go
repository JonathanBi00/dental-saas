package identity

import (
	"strings"
	"testing"

	"dentalsaas/internal/platform/apperr"
)

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("a perfectly fine passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected PHC format: %s", h)
	}
	if ok, err := VerifyPassword("a perfectly fine passphrase", h); !ok || err != nil {
		t.Fatalf("correct password: ok=%v err=%v", ok, err)
	}
	if ok, _ := VerifyPassword("a perfectly fine passphrasE", h); ok {
		t.Fatal("wrong password verified")
	}
	h2, _ := HashPassword("a perfectly fine passphrase")
	if h == h2 {
		t.Fatal("hashes must be salted")
	}
	if _, err := VerifyPassword("x", "$bcrypt$nope"); err == nil {
		t.Fatal("malformed hash should error")
	}
}

func TestPasswordPolicy(t *testing.T) {
	cases := []struct {
		pw, email string
		ok        bool
	}{
		{"short1234", "a@b.co", false},                      // 9 chars
		{"exactly12chr", "a@b.co", true},                    // 12 chars
		{"Jane@Clinic.com.au", "jane@clinic.com.au", false}, // equals email
		{strings.Repeat("x", 300), "a@b.co", false},
	}
	for _, c := range cases {
		err := ValidatePassword(c.pw, c.email)
		if c.ok && err != nil {
			t.Errorf("%q: unexpected error %v", c.pw, err)
		}
		if !c.ok && !apperr.Is(err, apperr.KindValidation) {
			t.Errorf("%q: err = %v, want validation error", c.pw, err)
		}
	}
}
