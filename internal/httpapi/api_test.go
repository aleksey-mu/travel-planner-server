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
	h, _ := testAPIWithUsers(t, allowRegister, cors, quota, maxUpload)
	return h
}

func testAPIWithUsers(t *testing.T, allowRegister bool, cors []string, quota, maxUpload int64) (http.Handler, *auth.UserStore) {
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
		MaxUsers:          100,
		InviteTTLDays:     7,
	}
	users, err := auth.OpenUserStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	_, err = users.Create(t.Context(), "you@example.com", "secret123", "You", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	tokens := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTTTLSeconds)
	store := syncstore.New(dir, cfg.DefaultQuotaBytes, cfg.MaxUploadBytes)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpapi.NewAPI(cfg, users, tokens, store, log).Handler(), users
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

func TestPasswordBumpInvalidatesToken(t *testing.T) {
	h, users := testAPIWithUsers(t, false, nil, 1<<30, 1<<20)
	tok := login(t, h)
	u, err := users.GetByEmail(t.Context(), "you@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.BumpPasswordVersion(t.Context(), u.ID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("want 401 after pwd bump got %d", rec.Code)
	}
	tok2 := login(t, h)
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req2.Header.Set("Authorization", "Bearer "+tok2)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("new login should work: %d %s", rec2.Code, rec2.Body.String())
	}
}

func TestDisabledUserRejectedOnSyncAndLogin(t *testing.T) {
	h, users := testAPIWithUsers(t, false, nil, 1<<30, 1<<20)
	tok := login(t, h)
	u, err := users.GetByEmail(t.Context(), "you@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.SetDisabled(t.Context(), u.ID, true); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/quota", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("disabled sync want 401 got %d", rec.Code)
	}
	body := `{"email":"you@example.com","password":"secret123"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 401 {
		t.Fatalf("disabled login want 401 got %d", rec2.Code)
	}
}

func TestSyncQuotaShape(t *testing.T) {
	h, users := testAPIWithUsers(t, false, nil, 1000, 1<<20)
	tok := login(t, h)
	u, err := users.GetByEmail(t.Context(), "you@example.com")
	if err != nil {
		t.Fatal(err)
	}

	getQuota := func() map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/quota", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("quota %d %s", rec.Code, rec.Body.String())
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	m := getQuota()
	if m["mode"] != "default" {
		t.Fatalf("mode=%v", m["mode"])
	}
	if m["configuredLimitBytes"].(float64) != 1000 {
		t.Fatalf("configured=%v", m["configuredLimitBytes"])
	}
	if m["defaultLimitBytes"].(float64) != 1000 {
		t.Fatalf("default=%v", m["defaultLimitBytes"])
	}

	zero := int64(0)
	if err := users.SetQuotaBytes(t.Context(), u.ID, &zero); err != nil {
		t.Fatal(err)
	}
	m = getQuota()
	if m["mode"] != "unlimited" {
		t.Fatalf("mode=%v", m["mode"])
	}
	if m["configuredLimitBytes"] != nil {
		t.Fatalf("configured should be null: %v", m["configuredLimitBytes"])
	}

	personal := int64(500)
	if err := users.SetQuotaBytes(t.Context(), u.ID, &personal); err != nil {
		t.Fatal(err)
	}
	m = getQuota()
	if m["mode"] != "personal" || m["configuredLimitBytes"].(float64) != 500 {
		t.Fatalf("personal: %v", m)
	}
}

func TestRegisterAlwaysDisabled(t *testing.T) {
	h := testAPI(t, true, nil, 1<<30, 1<<20)
	body := `{"email":"other@example.com","password":"password1","displayName":"Other"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("want 403 REGISTER_DISABLED got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "REGISTER_DISABLED") {
		t.Fatalf("body=%s", rec.Body.String())
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

func TestServerInfoAndInvites(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:           dir,
		JWTSecret:         strings.Repeat("k", 32),
		HTTPAddr:          ":0",
		JWTTTLSeconds:     3600,
		DefaultQuotaBytes: 1 << 30,
		MaxUploadBytes:    1 << 20,
		LogFormat:         "text",
		LogLevel:          "error",
		PublicBaseURL:     "https://sync.example.com",
		ServerName:        "",
		MaxUsers:          10,
		InviteTTLDays:     7,
	}
	users, err := auth.OpenUserStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	admin, err := users.Create(t.Context(), "admin@example.com", "secret123", "AdminNick", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.Create(t.Context(), "member@example.com", "secret123", "Member", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.Create(t.Context(), "other-admin@example.com", "secret123", "OtherAdmin", true, 0); err != nil {
		t.Fatal(err)
	}
	if err := users.SetDisabled(t.Context(), admin.ID, true); err != nil {
		t.Fatal(err)
	}
	tokens := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTTTLSeconds)
	store := syncstore.New(dir, cfg.DefaultQuotaBytes, cfg.MaxUploadBytes)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := httpapi.NewAPI(cfg, users, tokens, store, log).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/server/info", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("server info %d %s", rec.Code, rec.Body.String())
	}
	var info map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info["publicBaseUrl"] != "https://sync.example.com" {
		t.Fatal(info)
	}
	if info["serverName"] != "sync.example.com" {
		t.Fatalf("serverName fallback=%v", info["serverName"])
	}
	names, _ := info["adminDisplayNames"].([]any)
	if len(names) != 1 || names[0] != "OtherAdmin" {
		t.Fatalf("admins=%v", names)
	}
	if int(info["userCount"].(float64)) != 3 {
		t.Fatalf("userCount=%v", info["userCount"])
	}

	// member cannot create invite
	memTok, _, err := tokens.Issue(member.ID, member.Email, member.PasswordVersion)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites", strings.NewReader(`{"label":"x"}`))
	req.Header.Set("Authorization", "Bearer "+memTok)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("member create want 403 got %d", rec.Code)
	}

	adminTok, _, err := tokens.Issue(admin.ID, admin.Email, 0)
	if err != nil {
		t.Fatal(err)
	}
	// disabled admin rejected
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+adminTok)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("disabled admin want 401 got %d %s", rec.Code, rec.Body.String())
	}

	liveAdmin, err := users.GetByEmail(t.Context(), "other-admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	liveTok, _, err := tokens.Issue(liveAdmin.ID, liveAdmin.Email, liveAdmin.PasswordVersion)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites", strings.NewReader(`{"label":"friend"}`))
	req.Header.Set("Authorization", "Bearer "+liveTok)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create invite %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID        string `json:"id"`
		Token     string `json:"token"`
		ExpiresAt string `json:"expiresAt"`
		InviteURL string `json:"inviteUrl"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Token == "" {
		t.Fatal(created)
	}
	if !strings.Contains(created.InviteURL, "token=") {
		t.Fatal(created.InviteURL)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/invites/"+created.Token, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("get invite %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control=%q", rec.Header().Get("Cache-Control"))
	}
	var inv map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inv)
	if inv["expiresAt"] != created.ExpiresAt {
		t.Fatalf("expiresAt mismatch %v vs %s", inv["expiresAt"], created.ExpiresAt)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/invites/not-a-real-token", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("invalid want 404 got %d", rec.Code)
	}

	if err := users.ExpireInviteForTest(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/invites/"+created.Token, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 410 {
		t.Fatalf("expired want 410 got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+liveTok)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var created2 struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created2)

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/invites/"+created2.ID, nil)
	req.Header.Set("Authorization", "Bearer "+liveTok)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("revoke %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/invites/"+created2.Token, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 410 {
		t.Fatalf("revoked want 410 got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+liveTok)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var created3 struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created3)
	if err := users.MarkInviteUsedForTest(t.Context(), created3.ID); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/invites/"+created3.Token, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 410 {
		t.Fatalf("used want 410 got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/invites", nil)
	req.Header.Set("Authorization", "Bearer "+liveTok)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("list %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/invites", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("unauth admin want 401 got %d", rec.Code)
	}
}
