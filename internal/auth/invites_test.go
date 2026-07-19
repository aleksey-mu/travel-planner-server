package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestInviteLifecycle(t *testing.T) {
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
	created, err := store.CreateInvite(context.Background(), admin.ID, "friend", 7)
	if err != nil {
		t.Fatal(err)
	}
	if created.RawToken == "" || created.Invite.MaxUses != 1 {
		t.Fatal(created)
	}
	if _, err := store.ValidateInviteToken(context.Background(), created.RawToken); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeInvite(context.Background(), created.Invite.ID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(storeValidate(t, store, created.RawToken), ErrInviteRevoked) {
		t.Fatal("want revoked")
	}
}

func storeValidate(t *testing.T, store *UserStore, raw string) error {
	t.Helper()
	_, err := store.ValidateInviteToken(context.Background(), raw)
	return err
}

func TestInviteExpiredAndUsed(t *testing.T) {
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
	created, err := store.CreateInvite(context.Background(), admin.ID, "", 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ExpireInviteForTest(context.Background(), created.Invite.ID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(storeValidate(t, store, created.RawToken), ErrInviteExpired) {
		t.Fatal("want expired")
	}

	created2, err := store.CreateInvite(context.Background(), admin.ID, "", 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkInviteUsedForTest(context.Background(), created2.Invite.ID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(storeValidate(t, store, created2.RawToken), ErrInviteUsed) {
		t.Fatal("want used")
	}
}

func TestListEnabledAdminDisplayNames(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, err := store.Create(context.Background(), "a@example.com", "password123", "Zed", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), "b@example.com", "password123", "Amy", true, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), "c@example.com", "password123", "Member", false, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDisabled(context.Background(), a.ID, true); err != nil {
		t.Fatal(err)
	}
	names, err := store.ListEnabledAdminDisplayNames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "Amy" {
		t.Fatalf("names=%v", names)
	}
}
