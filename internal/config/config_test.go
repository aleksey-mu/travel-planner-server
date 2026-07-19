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
}
