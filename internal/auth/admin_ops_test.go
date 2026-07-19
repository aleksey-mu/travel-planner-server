package auth

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestLastEnabledAdminRefuseDisableAndDemote(t *testing.T) {
	store, err := OpenUserStore(filepath.ToSlash(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	admin, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	dis := true
	if err := store.ApplyAdminUserPatch(context.Background(), admin.ID, AdminUserPatch{Disabled: &dis}); !errors.Is(err, ErrLastEnabledAdmin) {
		t.Fatalf("disable last admin: %v", err)
	}
	adminFlag := false
	if err := store.ApplyAdminUserPatch(context.Background(), admin.ID, AdminUserPatch{IsAdmin: &adminFlag}); !errors.Is(err, ErrLastEnabledAdmin) {
		t.Fatalf("demote last admin: %v", err)
	}
}

func TestPromoteAndDisableSelfWhenTwoAdmins(t *testing.T) {
	store, err := OpenUserStore(filepath.ToSlash(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Create(context.Background(), "b@example.com", "password123", "B", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	if err := store.ApplyAdminUserPatch(context.Background(), b.ID, AdminUserPatch{IsAdmin: &yes}); err != nil {
		t.Fatal(err)
	}
	dis := true
	if err := store.ApplyAdminUserPatch(context.Background(), a.ID, AdminUserPatch{Disabled: &dis}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetByID(context.Background(), a.ID)
	if err != nil || !got.IsDisabled() {
		t.Fatalf("want disabled: %v %#v", err, got)
	}
}

func TestConcurrentDemoteOnlyOneSucceeds(t *testing.T) {
	store, err := OpenUserStore(filepath.ToSlash(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a, err := store.Create(context.Background(), "a@example.com", "password123", "A", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Create(context.Background(), "b@example.com", "password123", "B", true, 0)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []string{a.ID, b.ID} {
		wg.Add(1)
		go func(uid string) {
			defer wg.Done()
			admin := false
			errs <- store.ApplyAdminUserPatch(context.Background(), uid, AdminUserPatch{IsAdmin: &admin})
		}(id)
	}
	wg.Wait()
	close(errs)
	var ok, last int
	for e := range errs {
		if e == nil {
			ok++
		} else if errors.Is(e, ErrLastEnabledAdmin) {
			last++
		} else {
			t.Fatalf("unexpected: %v", e)
		}
	}
	if ok != 1 || last != 1 {
		t.Fatalf("ok=%d last=%d want 1/1", ok, last)
	}
	n, err := store.CountEnabledAdmins(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("enabled admins=%d err=%v", n, err)
	}
}

func TestConcurrentDeleteAdminsOnlyOneSucceeds(t *testing.T) {
	store, err := OpenUserStore(filepath.ToSlash(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a, err := store.Create(context.Background(), "a@example.com", "password123", "A", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Create(context.Background(), "b@example.com", "password123", "B", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), "m@example.com", "password123", "M", false, 0); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []string{a.ID, b.ID} {
		wg.Add(1)
		go func(uid string) {
			defer wg.Done()
			errs <- store.DeleteUserHard(context.Background(), uid)
		}(id)
	}
	wg.Wait()
	close(errs)
	var ok, last int
	for e := range errs {
		if e == nil {
			ok++
		} else if errors.Is(e, ErrLastEnabledAdmin) {
			last++
		} else {
			t.Fatalf("unexpected: %v", e)
		}
	}
	if ok != 1 || last != 1 {
		t.Fatalf("ok=%d last=%d want 1/1", ok, last)
	}
	n, err := store.CountEnabledAdmins(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("enabled admins=%d err=%v", n, err)
	}
}

func TestDeleteLastAdminWithMembersRefused(t *testing.T) {
	store, err := OpenUserStore(filepath.ToSlash(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	admin, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), "m@example.com", "password123", "M", false, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteUserHard(context.Background(), admin.ID); !errors.Is(err, ErrLastEnabledAdmin) {
		t.Fatalf("want ErrLastEnabledAdmin got %v", err)
	}
}

func TestQuotaModePatch(t *testing.T) {
	store, err := OpenUserStore(filepath.ToSlash(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	u, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	mode := "unlimited"
	if err := store.ApplyAdminUserPatch(context.Background(), u.ID, AdminUserPatch{QuotaMode: &mode}); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetByID(context.Background(), u.ID)
	if !got.QuotaBytes.Valid || got.QuotaBytes.Int64 != 0 {
		t.Fatalf("unlimited: %#v", got.QuotaBytes)
	}
	mode = "personal"
	b := int64(1024)
	if err := store.ApplyAdminUserPatch(context.Background(), u.ID, AdminUserPatch{QuotaMode: &mode, QuotaBytes: &b}); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetByID(context.Background(), u.ID)
	if !got.QuotaBytes.Valid || got.QuotaBytes.Int64 != 1024 {
		t.Fatalf("personal: %#v", got.QuotaBytes)
	}
	mode = "default"
	if err := store.ApplyAdminUserPatch(context.Background(), u.ID, AdminUserPatch{QuotaMode: &mode}); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetByID(context.Background(), u.ID)
	if got.QuotaBytes.Valid {
		t.Fatalf("default should clear override: %#v", got.QuotaBytes)
	}
	mode = "personal"
	if err := store.ApplyAdminUserPatch(context.Background(), u.ID, AdminUserPatch{QuotaMode: &mode}); !errors.Is(err, ErrInvalidQuotaMode) {
		t.Fatalf("personal without bytes: %v", err)
	}
}

func TestDeleteSelfSoleAdminWithMembers(t *testing.T) {
	store, err := OpenUserStore(filepath.ToSlash(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	admin, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), "m@example.com", "password123", "M", false, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSelf(context.Background(), admin.ID); !errors.Is(err, ErrSoleAdminWithMembers) {
		t.Fatalf("want sole admin block: %v", err)
	}
}

func TestDeleteSelfAloneOK(t *testing.T) {
	store, err := OpenUserStore(filepath.ToSlash(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	admin, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSelf(context.Background(), admin.ID); err != nil {
		t.Fatal(err)
	}
	n, err := store.Count(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("count=%d err=%v", n, err)
	}
}

func TestDeleteUserHardRemovesInvites(t *testing.T) {
	store, err := OpenUserStore(filepath.ToSlash(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	admin, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateInvite(context.Background(), admin.ID, "t", 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteUserHard(context.Background(), admin.ID); err != nil {
		t.Fatal(err)
	}
	_, err = store.GetInviteByID(context.Background(), created.Invite.ID)
	if !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("invite should be gone: %v", err)
	}
}

func TestListEnabledDirectorySkipsDisabled(t *testing.T) {
	store, err := OpenUserStore(filepath.ToSlash(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a, err := store.Create(context.Background(), "a@example.com", "password123", "A", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Create(context.Background(), "b@example.com", "password123", "B", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetDisabled(context.Background(), b.ID, true); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListEnabledDirectory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("%+v", list)
	}
}

func TestPromoteDisabledOK(t *testing.T) {
	store, err := OpenUserStore(filepath.ToSlash(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, err = store.Create(context.Background(), "a@example.com", "password123", "A", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Create(context.Background(), "b@example.com", "password123", "B", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetDisabled(context.Background(), b.ID, true); err != nil {
		t.Fatal(err)
	}
	yes := true
	if err := store.ApplyAdminUserPatch(context.Background(), b.ID, AdminUserPatch{IsAdmin: &yes}); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetByID(context.Background(), b.ID)
	if !got.IsAdmin || !got.IsDisabled() {
		t.Fatalf("%#v", got)
	}
}
