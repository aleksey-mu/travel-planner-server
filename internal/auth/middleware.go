package auth

import (
	"context"
	"net/http"
	"regexp"
	"strings"
)

type ctxKey int

const userIDKey ctxKey = 1

var userIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// WithUserID stores the authenticated user id in context.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserIDFromContext returns the authenticated user id, or empty.
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(userIDKey).(string)
	return v
}

// ValidUserID reports whether id is safe to use as a path segment under users/.
func ValidUserID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "\x00") {
		return false
	}
	return userIDPattern.MatchString(id)
}

// Middleware validates Bearer JWT, subject shape, and that the user still exists.
func Middleware(tokens *TokenManager, users *UserStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := r.Header.Get("Authorization")
			if !strings.HasPrefix(authz, "Bearer ") {
				writeUnauthorized(w, "missing bearer token")
				return
			}
			raw := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
			if raw == "" {
				writeUnauthorized(w, "missing bearer token")
				return
			}
			claims, err := tokens.Parse(raw)
			if err != nil {
				writeUnauthorized(w, "invalid or expired token")
				return
			}
			if !ValidUserID(claims.Subject) {
				writeUnauthorized(w, "invalid or expired token")
				return
			}
			if _, err := users.GetByID(r.Context(), claims.Subject); err != nil {
				writeUnauthorized(w, "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), claims.Subject)))
		})
	}
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"` + msg + `"}}`))
}
