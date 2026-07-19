package auth

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "password123") {
		t.Fatal("expected match")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("expected mismatch")
	}
}

func TestValidatePasswordStrength(t *testing.T) {
	if err := ValidatePasswordStrength("short"); err == nil {
		t.Fatal("expected error")
	}
	if err := ValidatePasswordStrength("longenough"); err != nil {
		t.Fatal(err)
	}
}
