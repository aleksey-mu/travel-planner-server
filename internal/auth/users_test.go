package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestEnsureSchemaAndCreateAdmin(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	assertColumns(t, store.db)

	u, err := store.Create(context.Background(), "admin@example.com", "password123", "Admin", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsAdmin {
		t.Fatal("create should set is_admin on first user")
	}
	if u.PasswordVersion != 0 {
		t.Fatalf("password_version=%d", u.PasswordVersion)
	}
	if u.QuotaBytes.Valid {
		t.Fatal("quota_bytes should be NULL (server default)")
	}
	if !u.RecoveryCodesAckedAt.Valid {
		t.Fatal("CLI create should set recovery_codes_acked_at")
	}
	if u.DisabledAt.Valid {
		t.Fatal("disabled_at should be NULL")
	}

	got, err := store.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsAdmin {
		t.Fatal("GetByID is_admin")
	}

	_ = store.Close()
	store2, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	got2, err := store2.GetByEmail(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !got2.IsAdmin {
		t.Fatal("reopen lost is_admin")
	}
}

func TestCreateIfEmptyRejectsWhenUsersExist(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 0); err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateIfEmpty(context.Background(), "seed@example.com", "password123", "Seed", 0)
	if !errors.Is(err, ErrUsersExist) {
		t.Fatalf("want ErrUsersExist got %v", err)
	}
	n, err := store.Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("userCount=%d want 1", n)
	}
}

func TestCreateTwoUsersAndMaxUsers(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !a.IsAdmin {
		t.Fatal("first user must be admin")
	}
	b, err := store.Create(context.Background(), "b@example.com", "password123", "B", false, 2)
	if err != nil {
		t.Fatal(err)
	}
	if b.IsAdmin {
		t.Fatal("second user should not be admin without --admin")
	}
	_, err = store.Create(context.Background(), "c@example.com", "password123", "C", false, 2)
	if !errors.Is(err, ErrUserLimit) {
		t.Fatalf("want ErrUserLimit got %v", err)
	}
	_, err = store.Create(context.Background(), "c@example.com", "password123", "", false, 0)
	if !errors.Is(err, ErrDisplayNameRequired) {
		t.Fatalf("want ErrDisplayNameRequired got %v", err)
	}
	c, err := store.Create(context.Background(), "c@example.com", "password123", "C", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsAdmin {
		t.Fatal("explicit admin")
	}
}

func TestSetPasswordBumpsVersion(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPassword(context.Background(), "a@example.com", "password456"); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordVersion != 1 {
		t.Fatalf("password_version=%d want 1", got.PasswordVersion)
	}
}

func TestMiddlewareRejectsPwdMismatchAndDisabled(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	tokens := NewTokenManager(strings.Repeat("k", 32), 3600)
	tok, _, err := tokens.Issue(u.ID, u.Email, u.PasswordVersion)
	if err != nil {
		t.Fatal(err)
	}

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := Middleware(tokens, store)(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("valid token: %d %s", rec.Code, rec.Body.String())
	}

	if err := store.BumpPasswordVersion(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("after bump: %d", rec.Code)
	}

	tok2, _, err := tokens.Issue(u.ID, u.Email, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetDisabled(context.Background(), u.ID, true); err != nil {
		t.Fatal(err)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	req2.Header.Set("Authorization", "Bearer "+tok2)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req2)
	if rec.Code != 401 {
		t.Fatalf("disabled: %d", rec.Code)
	}
}

func TestAdminMiddlewareLiveIsAdmin(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	tokens := NewTokenManager(strings.Repeat("k", 32), 3600)
	tok, _, err := tokens.Issue(u.ID, u.Email, 0)
	if err != nil {
		t.Fatal(err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := Middleware(tokens, store)(AdminMiddleware(store)(inner))

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("admin: %d", rec.Code)
	}

	if err := store.SetAdmin(context.Background(), u.ID, false); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("demoted: %d", rec.Code)
	}
}

func assertColumns(t *testing.T, db *sql.DB) {
	t.Helper()
	want := map[string]bool{
		"is_admin": true, "disabled_at": true, "password_version": true,
		"quota_bytes": true, "recovery_codes_acked_at": true,
	}
	rows, err := db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		found[name] = true
	}
	for col := range want {
		if !found[col] {
			t.Fatalf("missing column %s", col)
		}
	}
}
