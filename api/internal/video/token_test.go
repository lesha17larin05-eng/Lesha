package video

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestVideoTokenRoundtrip(t *testing.T) {
	vid := uuid.New()
	uid := uuid.New()
	tok, err := IssueToken("s", vid, uid, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseToken("s", tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.VideoID != vid || c.UserID != uid {
		t.Fatal("claims mismatch")
	}
}

func TestVideoTokenRejectsBadSecret(t *testing.T) {
	tok, _ := IssueToken("a", uuid.New(), uuid.New(), time.Minute)
	if _, err := ParseToken("b", tok); err == nil {
		t.Fatal("expected error")
	}
}

func TestVideoTokenExpired(t *testing.T) {
	tok, _ := IssueToken("s", uuid.New(), uuid.New(), -time.Second)
	if _, err := ParseToken("s", tok); err == nil {
		t.Fatal("expected expired error")
	}
}
