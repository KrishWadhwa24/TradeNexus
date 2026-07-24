package auth

import "testing"

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("s3cret!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !CheckPassword(hash, "s3cret!") {
		t.Fatal("correct password should verify")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("wrong password must not verify")
	}
}

func TestJWTRoundTrip(t *testing.T) {
	secret := "test-secret"
	tok, err := Issue(secret, "user-123", "a@b.com", true)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := Parse(secret, tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.UserID != "user-123" || claims.Email != "a@b.com" {
		t.Fatalf("claims mismatch: %+v", claims)
	}
	if !claims.IsAdmin {
		t.Fatalf("expected IsAdmin=true in round-tripped claims, got %+v", claims)
	}
	if _, err := Parse("wrong-secret", tok); err == nil {
		t.Fatal("parse with wrong secret must fail")
	}
}
