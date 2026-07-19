package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestRecoverWithCodeOnce(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	admin, err := store.Create(context.Background(), "admin@example.com", "password123", "Admin", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := store.CreateInvite(context.Background(), admin.ID, "", 7)
	if err != nil {
		t.Fatal(err)
	}
	u, codes, _, err := store.AcceptInvite(context.Background(), inv.RawToken, "f@example.com", "password123", "F", 0, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AckRecoveryCodes(context.Background(), u.ID, 1); err != nil {
		t.Fatal(err)
	}

	got, err := store.RecoverWithCode(context.Background(), "f@example.com", codes[0], "password456")
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordVersion != 1 {
		t.Fatalf("pwd=%d", got.PasswordVersion)
	}
	_, err = store.RecoverWithCode(context.Background(), "f@example.com", codes[0], "password789")
	if !errors.Is(err, ErrRecoveryCodeInvalid) {
		t.Fatalf("reuse: %v", err)
	}
	// other code still works
	if _, err := store.RecoverWithCode(context.Background(), "f@example.com", codes[1], "password999"); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordResetLifecycle(t *testing.T) {
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
	created, err := store.CreatePasswordResetToken(context.Background(), u.ID, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetPasswordResetToken(context.Background(), created.RawToken); err != nil {
		t.Fatal(err)
	}
	got, err := store.CompletePasswordReset(context.Background(), created.RawToken, "password456")
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordVersion != 1 {
		t.Fatalf("pwd=%d", got.PasswordVersion)
	}
	_, err = store.CompletePasswordReset(context.Background(), created.RawToken, "password789")
	if !errors.Is(err, ErrResetUsed) && !errors.Is(err, ErrResetNotFound) {
		// after used, Get may still find and return used
		t.Fatalf("reuse: %v", err)
	}

	created2, err := store.CreatePasswordResetToken(context.Background(), u.ID, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ExpirePasswordResetForTest(context.Background(), created2.Token.ID); err != nil {
		t.Fatal(err)
	}
	_, err = store.GetPasswordResetToken(context.Background(), created2.RawToken)
	if !errors.Is(err, ErrResetExpired) {
		t.Fatalf("expired: %v", err)
	}

	created3, err := store.CreatePasswordResetToken(context.Background(), u.ID, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPassword(context.Background(), "a@example.com", "password000"); err != nil {
		t.Fatal(err)
	}
	_, err = store.GetPasswordResetToken(context.Background(), created3.RawToken)
	if !errors.Is(err, ErrResetRevoked) {
		t.Fatalf("revoked by set-password: %v", err)
	}
}
