package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminStaticServesWithCSP(t *testing.T) {
	h := testAPI(t, false, nil, 1<<30, 1<<20)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET /admin/ %d %s", rec.Code, rec.Body.String())
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("CSP=%q", csp)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "TravelPlan Admin") || !strings.Contains(body, "/admin/app.js") {
		t.Fatalf("html missing markers: %s", body[:min(200, len(body))])
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/app.js", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("app.js %d", rec.Code)
	}
	js := rec.Body.String()
	if !strings.Contains(js, "/api/v1/admin/invites") || !strings.Contains(js, "quotaMode") {
		t.Fatal("app.js missing invite/quota wiring")
	}
	if !strings.Contains(js, "clearPanelSecrets") || !strings.Contains(js, "MiB (personal)") {
		t.Fatal("app.js missing logout clear / MiB quota UX")
	}

	req = httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/" {
		t.Fatalf("redirect /admin -> /admin/: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAdminPanelLoginListCreateInviteAndUnlimitedQuota(t *testing.T) {
	h, users := testAPIWithUsers(t, false, nil, 1<<30, 1<<20)
	admin, err := users.Create(t.Context(), "admin@example.com", "secret123", "Admin", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.Create(t.Context(), "member@example.com", "secret123", "Member", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	tok := loginEmail(t, h, "admin@example.com", "secret123")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("list users %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create invite %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"token"`) {
		t.Fatal(rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/users/"+member.ID,
		strings.NewReader(`{"quotaMode":"unlimited"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("quota unlimited %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"mode":"unlimited"`) {
		t.Fatal(rec.Body.String())
	}

	memberTok := loginEmail(t, h, "member@example.com", "secret123")
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	req.Header.Set("Authorization", "Bearer "+memberTok)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("member admin list want 403 got %d", rec.Code)
	}
	_ = admin
}
