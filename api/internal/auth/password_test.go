package auth

import "testing"

func TestPasswordHashAndVerify(t *testing.T) {
	h, err := HashPassword("supersecret123")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword("supersecret123", h)
	if err != nil || !ok {
		t.Fatalf("expected verify true, got ok=%v err=%v", ok, err)
	}
	ok, _ = VerifyPassword("wrong", h)
	if ok {
		t.Fatal("verify should be false for wrong password")
	}
}

func TestPasswordHashIsRandom(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Fatal("same password must produce different hashes due to salt")
	}
}
