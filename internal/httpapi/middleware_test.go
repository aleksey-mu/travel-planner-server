package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedactSensitivePath(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"/api/v1/health", "/api/v1/health"},
		{"/api/v1/invites/secret-token", "/api/v1/invites/[redacted]"},
		{"/api/v1/invites/secret-token/accept", "/api/v1/invites/[redacted]/accept"},
		{"/api/v1/password-reset/secret", "/api/v1/password-reset/[redacted]"},
		{"/api/v1/invites/", "/api/v1/invites/"},
	}
	for _, tc := range tests {
		if got := redactSensitivePath(tc.in); got != tc.want {
			t.Fatalf("%q → %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoggingMiddlewareRedactsInviteToken(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	h := LoggingMiddleware(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	secret := "super-secret-invite-token-value"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/invites/"+secret, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("log leaked token: %s", out)
	}
	if !strings.Contains(out, "/api/v1/invites/[redacted]") {
		t.Fatalf("expected redacted path in log: %s", out)
	}
}
