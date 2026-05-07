package auth

import (
	"testing"

	"github.com/google/uuid"
)

func TestIssueAndParseAccessToken(t *testing.T) {
	id := uuid.New()
	tok, err := IssueAccessToken("secret", id, "user")
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseAccessToken("secret", tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.UserID != id || c.Role != "user" {
		t.Fatalf("claims mismatch: %+v", c)
	}
}

func TestParseAccessTokenWrongSecret(t *testing.T) {
	tok, _ := IssueAccessToken("secret", uuid.New(), "user")
	if _, err := ParseAccessToken("other", tok); err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestRandomTokenAndHash(t *testing.T) {
	raw, h, err := RandomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	if HashToken(raw) != h {
		t.Fatal("hash mismatch")
	}
}
