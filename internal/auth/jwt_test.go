package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestIssueAndParse(t *testing.T) {
	m := NewTokenManager(strings.Repeat("s", 32), 3600)
	tok, expiresIn, err := m.Issue("user-1", "a@example.com", 3)
	if err != nil {
		t.Fatal(err)
	}
	if expiresIn != 3600 {
		t.Fatalf("expiresIn=%d", expiresIn)
	}
	claims, err := m.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-1" || claims.Email != "a@example.com" || claims.Pwd != 3 {
		t.Fatalf("claims=%+v", claims)
	}
}

func TestRejectNoneAlg(t *testing.T) {
	m := NewTokenManager(strings.Repeat("s", 32), 60)
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{
		Email: "x@y.z",
		Pwd:   0,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "u1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Parse(signed); err == nil {
		t.Fatal("expected reject none alg")
	}
}

func TestRejectWrongSecret(t *testing.T) {
	a := NewTokenManager(strings.Repeat("a", 32), 60)
	b := NewTokenManager(strings.Repeat("b", 32), 60)
	tok, _, err := a.Issue("u", "e@e.e", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Parse(tok); err == nil {
		t.Fatal("expected reject wrong secret")
	}
}
