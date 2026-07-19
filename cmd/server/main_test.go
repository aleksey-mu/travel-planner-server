package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/aleksey-mu/travel-planner-server/internal/auth"
	"github.com/aleksey-mu/travel-planner-server/internal/config"
)

func TestCreateUserCLIRequiresDisplayName(t *testing.T) {
	code := runCreateUser([]string{"--email", "a@example.com", "--password", "password123"})
	if code != 2 {
		t.Fatalf("exit=%d want 2", code)
	}
}

func TestCreateUserCLITwoUsersMaxAndAdmin(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:  dir,
		MaxUsers: 2,
	}
	prev := loadCLIConfig
	loadCLIConfig = func() *config.Config { return cfg }
	t.Cleanup(func() { loadCLIConfig = prev })

	if code := runCreateUser([]string{
		"--email", "a@example.com", "--password", "password123", "--display-name", "Alice",
	}); code != 0 {
		t.Fatalf("first create exit=%d", code)
	}
	if code := runCreateUser([]string{
		"--email", "b@example.com", "--password", "password123", "--display-name", "Bob",
	}); code != 0 {
		t.Fatalf("second create exit=%d", code)
	}
	if code := runCreateUser([]string{
		"--email", "c@example.com", "--password", "password123", "--display-name", "Carol",
	}); code != 1 {
		t.Fatalf("over max exit=%d want 1", code)
	}

	store, err := auth.OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, err := store.GetByEmail(context.Background(), "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !a.IsAdmin {
		t.Fatal("first user must be admin")
	}
	b, err := store.GetByEmail(context.Background(), "b@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if b.IsAdmin {
		t.Fatal("second user must not be admin without --admin")
	}

	cfg.MaxUsers = 0
	if code := runCreateUser([]string{
		"--email", "c@example.com", "--password", "password123", "--display-name", "Carol", "--admin",
	}); code != 0 {
		t.Fatalf("admin create exit=%d", code)
	}
	c, err := store.GetByEmail(context.Background(), "c@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsAdmin {
		t.Fatal("explicit --admin")
	}
}

func TestSeedAdminSkipsWhenUsersExist(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Create(context.Background(), "owner@example.com", "password123", "Owner", false, 0); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DataDir:              dir,
		SeedAdminEmail:       "seed@example.com",
		SeedAdminPassword:    "password123",
		SeedAdminDisplayName: "Seed",
		MaxUsers:             0,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := seedAdmin(context.Background(), store, cfg, log); err != nil {
		t.Fatal(err)
	}
	_, err = store.GetByEmail(context.Background(), "seed@example.com")
	if err == nil {
		t.Fatal("seed must not create extra admin when users already exist")
	}
	if !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("want ErrNotFound got %v", err)
	}
	n, err := store.Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("userCount=%d want 1", n)
	}
}

func TestSeedAdminCreatesOnEmptyDB(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.Config{
		DataDir:              dir,
		SeedAdminEmail:       "seed@example.com",
		SeedAdminPassword:    "password123",
		SeedAdminDisplayName: "Seed",
		MaxUsers:             0,
	}
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	if err := seedAdmin(context.Background(), store, cfg, log); err != nil {
		t.Fatal(err)
	}
	u, err := store.GetByEmail(context.Background(), "seed@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsAdmin {
		t.Fatal("seeded user must be admin")
	}
}

func TestCreateInviteCLI(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:       dir,
		InviteTTLDays: 7,
		PublicBaseURL: "https://sync.example.com",
	}
	prev := loadCLIConfig
	loadCLIConfig = func() *config.Config { return cfg }
	t.Cleanup(func() { loadCLIConfig = prev })

	store, err := auth.OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Create(context.Background(), "a@example.com", "password123", "Admin", true, 0); err != nil {
		t.Fatal(err)
	}

	if code := runCreateInvite([]string{"--label", "friend", "--expires-days", "3"}); code != 0 {
		t.Fatalf("create-invite exit=%d", code)
	}
	list, err := store.ListInvites(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if list[0].Label != "friend" {
		t.Fatal(list[0].Label)
	}
	if code := runRevokeInvite([]string{"--id", list[0].ID}); code != 0 {
		t.Fatalf("revoke exit=%d", code)
	}
	got, err := store.GetInviteByID(context.Background(), list[0].ID)
	if err != nil || !got.RevokedAt.Valid {
		t.Fatalf("revoked=%v err=%v", got, err)
	}
}
