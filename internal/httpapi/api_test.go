package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aleksey-mu/travel-planner-server/internal/auth"
	"github.com/aleksey-mu/travel-planner-server/internal/config"
	"github.com/aleksey-mu/travel-planner-server/internal/httpapi"
	"github.com/aleksey-mu/travel-planner-server/internal/syncstore"
)

func testAPI(t *testing.T, allowRegister bool, cors []string, quota, maxUpload int64) http.Handler {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:           dir,
		JWTSecret:         strings.Repeat("k", 32),
		HTTPAddr:          ":0",
		CORSOrigins:       cors,
		AllowRegister:     allowRegister,
		JWTTTLSeconds:     3600,
		DefaultQuotaBytes: quota,
		MaxUploadBytes:    maxUpload,
		LogFormat:         "text",
		LogLevel:          "error",
	}
	users, err := auth.OpenUserStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	_, err = users.Create(t.Context(), "you@example.com", "secret123", "You")
	if err != nil {
		t.Fatal(err)
	}
	tokens := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTTTLSeconds)
	store := syncstore.New(dir, cfg.DefaultQuotaBytes, cfg.MaxUploadBytes)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpapi.NewAPI(cfg, users, tokens, store, log).Handler()
}

func login(t *testing.T, h http.Handler) string {
	t.Helper()
	body := `{"email":"you@example.com","password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("login %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.AccessToken == "" {
		t.Fatal(resp)
	}
	return resp.AccessToken
}

func TestHealth(t *testing.T) {
	h := testAPI(t, false, nil, 1<<30, 1<<20)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Fatal(resp)
	}
}

func TestAuthFlow(t *testing.T) {
	h := testAPI(t, false, nil, 1<<30, 1<<20)
	tok := login(t, h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 401 {
		t.Fatalf("want 401 got %d", rec2.Code)
	}

	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{"email":"a@b.c","password":"password1"}`))
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 403 {
		t.Fatalf("want 403 got %d %s", rec3.Code, rec3.Body.String())
	}
}

func TestRegisterEnabledOwnerExists(t *testing.T) {
	h := testAPI(t, true, nil, 1<<30, 1<<20)
	body := `{"email":"other@example.com","password":"password1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("want 403 OWNER_EXISTS got %d %s", rec.Code, rec.Body.String())
	}
}

func TestSyncHappyPath(t *testing.T) {
	h := testAPI(t, false, nil, 1<<30, 1<<20)
	tok := login(t, h)
	authz := "Bearer " + tok

	do := func(method, path string, body io.Reader, ct string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, body)
		req.Header.Set("Authorization", authz)
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := do(http.MethodPost, "/api/v1/sync/bootstrap", nil, ""); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if rec := do(http.MethodGet, "/api/v1/sync/manifest", nil, ""); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	man := `{"schemaVersion":1,"updatedAt":"2026-07-19T12:00:00Z","travels":[]}`
	if rec := do(http.MethodPut, "/api/v1/sync/manifest", strings.NewReader(man), "application/json"); rec.Code != 204 {
		t.Fatalf("put manifest %d %s", rec.Code, rec.Body.String())
	}
	travel := `{"id":"t1","name":"Trip","items":[]}`
	rec := do(http.MethodPut, "/api/v1/sync/travels/t1", strings.NewReader(travel), "application/json")
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var putResp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &putResp)
	if putResp["fileId"] != "t1" {
		t.Fatal(putResp)
	}

	if rec := do(http.MethodPost, "/api/v1/sync/files/t1/i1/ensure-folder", nil, ""); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	rec = do(http.MethodPut, "/api/v1/sync/files/t1/i1/photo.jpg", bytes.NewReader([]byte("img")), "application/octet-stream")
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var fileResp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &fileResp)
	if fileResp["fileId"] != "files/t1/i1/photo.jpg" {
		t.Fatal(fileResp)
	}

	// double files/ prefix (client behavior)
	rec = do(http.MethodGet, "/api/v1/sync/files/files/t1/i1/photo.jpg", nil, "")
	if rec.Code != 200 || rec.Body.String() != "img" {
		t.Fatalf("double prefix get %d %q", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/api/v1/sync/files/t1/i1/photo.jpg", nil, "")
	if rec.Code != 200 || rec.Body.String() != "img" {
		t.Fatalf("bare get %d %q", rec.Code, rec.Body.String())
	}

	if rec := do(http.MethodDelete, "/api/v1/sync/files/files/t1/i1/photo.jpg", nil, ""); rec.Code != 204 {
		t.Fatal(rec.Code)
	}
	if rec := do(http.MethodDelete, "/api/v1/sync/travels/t1", nil, ""); rec.Code != 204 {
		t.Fatal(rec.Code)
	}
	if rec := do(http.MethodDelete, "/api/v1/sync/travels/t1", nil, ""); rec.Code != 204 {
		t.Fatal("idempotent delete", rec.Code)
	}
}

func TestPathTraversalRejected(t *testing.T) {
	h := testAPI(t, false, nil, 1<<30, 1<<20)
	tok := login(t, h)
	// Encoded ".." so ServeMux does not redirect before the handler.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/files/%2e%2e/%2e%2e/etc/passwd", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("want 400 got %d %s", rec.Code, rec.Body.String())
	}
}

func TestSyncUnauthorized(t *testing.T) {
	h := testAPI(t, false, nil, 1<<30, 1<<20)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/manifest", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("want 401 got %d", rec.Code)
	}
}

func TestQuotaAndPayload413(t *testing.T) {
	h := testAPI(t, false, nil, 8, 5)
	tok := login(t, h)
	authz := "Bearer " + tok

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/bootstrap", nil)
	req.Header.Set("Authorization", authz)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodPut, "/api/v1/sync/files/t1/i1/big.bin", bytes.NewReader([]byte("123456")))
	req.Header.Set("Authorization", authz)
	req.Header.Set("Content-Type", "application/octet-stream")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 413 {
		t.Fatalf("want 413 payload got %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/sync/files/t1/i1/a.bin", bytes.NewReader([]byte("12345")))
	req.Header.Set("Authorization", authz)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("first put %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPut, "/api/v1/sync/files/t1/i1/b.bin", bytes.NewReader([]byte("12345")))
	req.Header.Set("Authorization", authz)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 413 {
		t.Fatalf("want 413 quota got %d %s", rec.Code, rec.Body.String())
	}
}

func TestCORSPreflight(t *testing.T) {
	h := testAPI(t, false, []string{"http://localhost:4200"}, 1<<30, 1<<20)
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/me", nil)
	req.Header.Set("Origin", "http://localhost:4200")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "Authorization")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("want 204 got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:4200" {
		t.Fatalf("ACAOrigin=%q", got)
	}
}

func TestUserDataAllowlist(t *testing.T) {
	h := testAPI(t, false, nil, 1<<30, 1<<20)
	tok := login(t, h)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/sync/user-data/item-types.json", strings.NewReader(`{"types":[]}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatal(rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPut, "/api/v1/sync/user-data/evil.json", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("want 400 got %d", rec.Code)
	}
}

func TestConfigPlaceholderRejected(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("JWT_SECRET", "change-me-to-a-long-random-string-at-least-32-chars")
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected placeholder reject")
	}
}

func TestOpenUserStoreCreatesDB(t *testing.T) {
	dir := t.TempDir()
	s, err := auth.OpenUserStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(filepath.Join(dir, "server.db")); err != nil {
		t.Fatal(err)
	}
}
