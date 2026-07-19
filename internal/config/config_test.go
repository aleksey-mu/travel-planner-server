package config

import (
	"path/filepath"
	"testing"
	"time"
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
	t.Setenv("PUBLIC_BASE_URL", "")
	t.Setenv("MAX_USERS", "")
	t.Setenv("INVITE_TTL_DAYS", "")
	t.Setenv("PASSWORD_RESET_TTL_SECONDS", "")
	t.Setenv("AUTH_RATE_LIMIT_MAX", "")
	t.Setenv("AUTH_RATE_LIMIT_WINDOW_SECONDS", "")
	t.Setenv("TRUST_PROXY", "")

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
	if cfg.AuthRateLimitMax != DefaultAuthRateLimitMax {
		t.Fatalf("AuthRateLimitMax=%d", cfg.AuthRateLimitMax)
	}
	if cfg.AuthRateLimitWindow != time.Duration(DefaultAuthRateLimitWindowSec)*time.Second {
		t.Fatalf("AuthRateLimitWindow=%v", cfg.AuthRateLimitWindow)
	}
	if cfg.TrustProxy {
		t.Fatal("TrustProxy should default false")
	}
	if cfg.ServerName != "" {
		t.Fatalf("ServerName=%q", cfg.ServerName)
	}
	if cfg.PublicBaseURL != "" {
		t.Fatalf("PublicBaseURL=%q", cfg.PublicBaseURL)
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
	t.Setenv("PUBLIC_BASE_URL", "https://sync.example.com/")
	t.Setenv("MAX_USERS", "50")
	t.Setenv("INVITE_TTL_DAYS", "14")
	t.Setenv("PASSWORD_RESET_TTL_SECONDS", "7200")
	t.Setenv("AUTH_RATE_LIMIT_MAX", "5")
	t.Setenv("AUTH_RATE_LIMIT_WINDOW_SECONDS", "60")
	t.Setenv("TRUST_PROXY", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "Alex's NAS" {
		t.Fatalf("ServerName=%q", cfg.ServerName)
	}
	if cfg.PublicBaseURL != "https://sync.example.com" {
		t.Fatalf("PublicBaseURL=%q", cfg.PublicBaseURL)
	}
	if cfg.EffectiveServerName() != "Alex's NAS" {
		t.Fatalf("EffectiveServerName=%q", cfg.EffectiveServerName())
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
	if cfg.AuthRateLimitMax != 5 {
		t.Fatalf("AuthRateLimitMax=%d", cfg.AuthRateLimitMax)
	}
	if cfg.AuthRateLimitWindow != time.Minute {
		t.Fatalf("AuthRateLimitWindow=%v", cfg.AuthRateLimitWindow)
	}
	if !cfg.TrustProxy {
		t.Fatal("TrustProxy")
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
	t.Setenv("PUBLIC_BASE_URL", "")
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

func TestPublicBaseURLAndServerNameFallback(t *testing.T) {
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
	t.Setenv("PUBLIC_BASE_URL", "https://nas.home.example")
	t.Setenv("MAX_USERS", "")
	t.Setenv("INVITE_TTL_DAYS", "")
	t.Setenv("PASSWORD_RESET_TTL_SECONDS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveServerName() != "nas.home.example" {
		t.Fatalf("EffectiveServerName=%q", cfg.EffectiveServerName())
	}

	t.Setenv("PUBLIC_BASE_URL", "http://insecure.example")
	if _, err := Load(); err == nil {
		t.Fatal("expected http PUBLIC_BASE_URL reject")
	}
	t.Setenv("PUBLIC_BASE_URL", "https://user:pass@nas.home.example")
	if _, err := Load(); err == nil {
		t.Fatal("expected credentials in PUBLIC_BASE_URL reject")
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
		t.Setenv("PUBLIC_BASE_URL", "")
		t.Setenv("MAX_USERS", "")
		t.Setenv("INVITE_TTL_DAYS", "")
		t.Setenv("PASSWORD_RESET_TTL_SECONDS", "")
		t.Setenv("AUTH_RATE_LIMIT_MAX", "")
		t.Setenv("AUTH_RATE_LIMIT_WINDOW_SECONDS", "")
		t.Setenv("TRUST_PROXY", "")
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
	t.Run("rate window zero", func(t *testing.T) {
		base(t)
		t.Setenv("AUTH_RATE_LIMIT_WINDOW_SECONDS", "0")
		if _, err := Load(); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("rate max negative", func(t *testing.T) {
		base(t)
		t.Setenv("AUTH_RATE_LIMIT_MAX", "-1")
		if _, err := Load(); err == nil {
			t.Fatal("expected error")
		}
	})
}
