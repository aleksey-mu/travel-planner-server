package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims are HS256 access-token claims.
type Claims struct {
	Email string `json:"email"`
	Pwd   int    `json:"pwd"` // password_version — bump invalidates token
	jwt.RegisteredClaims
}

// TokenManager issues and parses HS256 JWTs.
type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenManager creates a TokenManager.
func NewTokenManager(secret string, ttlSeconds int) *TokenManager {
	return &TokenManager{
		secret: []byte(secret),
		ttl:    time.Duration(ttlSeconds) * time.Second,
	}
}

// TTL returns the configured token lifetime.
func (m *TokenManager) TTL() time.Duration {
	return m.ttl
}

// Issue creates a signed access token for the user.
func (m *TokenManager) Issue(userID, email string, passwordVersion int) (token string, expiresIn int, err error) {
	now := time.Now().UTC()
	exp := now.Add(m.ttl)
	claims := Claims{
		Email: email,
		Pwd:   passwordVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := t.SignedString(m.secret)
	if err != nil {
		return "", 0, fmt.Errorf("sign token: %w", err)
	}
	return signed, int(m.ttl.Seconds()), nil
}

// Parse validates an access token and returns claims. Rejects non-HS256 algs.
func (m *TokenManager) Parse(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("missing subject")
	}
	return claims, nil
}
