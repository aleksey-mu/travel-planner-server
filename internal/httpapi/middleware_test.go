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
		{"/health", "/health"},
		{"/invites/secret-token", "/invites/[redacted]"},
		{"/invites/secret-token/accept", "/invites/[redacted]/accept"},
		{"/password-reset/secret", "/password-reset/[redacted]"},
		{"/invites/", "/invites/"},
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
	req := httptest.NewRequest(http.MethodGet, "/invites/"+secret, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("log leaked token: %s", out)
	}
	if !strings.Contains(out, "/invites/[redacted]") {
		t.Fatalf("expected redacted path in log: %s", out)
	}
}
