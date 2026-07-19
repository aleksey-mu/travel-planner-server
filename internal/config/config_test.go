package config

import (
	"path/filepath"
	"testing"
)

func TestLoadQuotaAndUploadMB(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", filepath.ToSlash(dir))
	t.Setenv("JWT_SECRET", "abcdefghijklmnopqrstuvwxyz0123456789")
	t.Setenv("VAULT_QUOTA_MB", "100")
	t.Setenv("MAX_UPLOAD_MB", "10")
	t.Setenv("ALLOW_REGISTER", "")
	t.Setenv("CORS_ORIGINS", "")
	t.Setenv("SEED_ADMIN_EMAIL", "")
	t.Setenv("SEED_ADMIN_PASSWORD", "")
	t.Setenv("SERVER_NAME", "")
	t.Setenv("MAX_USERS", "")
	t.Setenv("INVITE_TTL_DAYS", "")
	t.Setenv("PASSWORD_RESET_TTL_SECONDS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultQuotaBytes != 100*BytesPerMB {
		t.Fatalf("quota bytes=%d", cfg.DefaultQuotaBytes)
	}
	if cfg.MaxUploadBytes != 10*BytesPerMB {
		t.Fatalf("upload bytes=%d", cfg.MaxUploadBytes)
	}
	if cfg.MaxUsers != DefaultMaxUsers {
		t.Fatalf("MaxUsers=%d", cfg.MaxUsers)
	}
	if cfg.InviteTTLDays != DefaultInviteTTLDays {
		t.Fatalf("InviteTTLDays=%d", cfg.InviteTTLDays)
	}
	if cfg.PasswordResetTTLSeconds != DefaultPasswordResetTTLSeconds {
		t.Fatalf("PasswordResetTTLSeconds=%d", cfg.PasswordResetTTLSeconds)
	}
	if cfg.ServerName != "" {
		t.Fatalf("ServerName=%q", cfg.ServerName)
	}
}

func TestLoadMultiUserEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", filepath.ToSlash(dir))
	t.Setenv("JWT_SECRET", "abcdefghijklmnopqrstuvwxyz0123456789")
	t.Setenv("VAULT_QUOTA_MB", "")
	t.Setenv("MAX_UPLOAD_MB", "")
	t.Setenv("ALLOW_REGISTER", "")
	t.Setenv("CORS_ORIGINS", "")
	t.Setenv("SEED_ADMIN_EMAIL", "")
	t.Setenv("SEED_ADMIN_PASSWORD", "")
	t.Setenv("SERVER_NAME", "Alex's NAS")
	t.Setenv("MAX_USERS", "50")
	t.Setenv("INVITE_TTL_DAYS", "14")
	t.Setenv("PASSWORD_RESET_TTL_SECONDS", "7200")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "Alex's NAS" {
		t.Fatalf("ServerName=%q", cfg.ServerName)
	}
	if cfg.MaxUsers != 50 {
		t.Fatalf("MaxUsers=%d", cfg.MaxUsers)
	}
	if cfg.InviteTTLDays != 14 {
		t.Fatalf("InviteTTLDays=%d", cfg.InviteTTLDays)
	}
	if cfg.PasswordResetTTLSeconds != 7200 {
		t.Fatalf("PasswordResetTTLSeconds=%d", cfg.PasswordResetTTLSeconds)
	}
}

func TestLoadMaxUsersUnlimited(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", filepath.ToSlash(dir))
	t.Setenv("JWT_SECRET", "abcdefghijklmnopqrstuvwxyz0123456789")
	t.Setenv("VAULT_QUOTA_MB", "")
	t.Setenv("MAX_UPLOAD_MB", "")
	t.Setenv("ALLOW_REGISTER", "")
	t.Setenv("CORS_ORIGINS", "")
	t.Setenv("SEED_ADMIN_EMAIL", "")
	t.Setenv("SEED_ADMIN_PASSWORD", "")
	t.Setenv("SERVER_NAME", "")
	t.Setenv("MAX_USERS", "0")
	t.Setenv("INVITE_TTL_DAYS", "")
	t.Setenv("PASSWORD_RESET_TTL_SECONDS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxUsers != 0 {
		t.Fatalf("MaxUsers=%d want 0 (unlimited)", cfg.MaxUsers)
	}
}

func TestLoadRejectsInvalidMultiUserEnv(t *testing.T) {
	base := func(t *testing.T) {
		t.Helper()
		dir := t.TempDir()
		t.Setenv("DATA_DIR", filepath.ToSlash(dir))
		t.Setenv("JWT_SECRET", "abcdefghijklmnopqrstuvwxyz0123456789")
		t.Setenv("VAULT_QUOTA_MB", "")
		t.Setenv("MAX_UPLOAD_MB", "")
		t.Setenv("ALLOW_REGISTER", "")
		t.Setenv("CORS_ORIGINS", "")
		t.Setenv("SEED_ADMIN_EMAIL", "")
		t.Setenv("SEED_ADMIN_PASSWORD", "")
		t.Setenv("SERVER_NAME", "")
		t.Setenv("MAX_USERS", "")
		t.Setenv("INVITE_TTL_DAYS", "")
		t.Setenv("PASSWORD_RESET_TTL_SECONDS", "")
	}
	t.Run("max users negative", func(t *testing.T) {
		base(t)
		t.Setenv("MAX_USERS", "-1")
		if _, err := Load(); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invite ttl zero", func(t *testing.T) {
		base(t)
		t.Setenv("INVITE_TTL_DAYS", "0")
		if _, err := Load(); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("reset ttl zero", func(t *testing.T) {
		base(t)
		t.Setenv("PASSWORD_RESET_TTL_SECONDS", "0")
		if _, err := Load(); err == nil {
			t.Fatal("expected error")
		}
	})
}
