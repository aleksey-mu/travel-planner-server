package syncstore

import (
	"database/sql"
	"testing"
)

func TestEffectiveQuotaLimit(t *testing.T) {
	const quota = 10 * 1024 * 1024 * 1024
	if got := EffectiveQuotaLimit(100, quota, -1); got != quota {
		t.Fatalf("unknown disk: got %d", got)
	}
	if got := EffectiveQuotaLimit(100, quota, 50); got != 150 {
		t.Fatalf("disk caps: got %d", got)
	}
	if got := EffectiveQuotaLimit(100, quota, quota); got != quota {
		t.Fatalf("quota wins: got %d", got)
	}
	if got := EffectiveQuotaLimit(100, UnlimitedConfigured, -1); got != UnlimitedConfigured {
		t.Fatalf("unlimited unknown disk: got %d", got)
	}
	if got := EffectiveQuotaLimit(100, UnlimitedConfigured, 50); got != 150 {
		t.Fatalf("unlimited disk floor: got %d", got)
	}
}

func TestCanAcceptGrowth(t *testing.T) {
	const quota = 1000
	if !CanAcceptGrowth(100, 0, quota, 0) {
		t.Fatal("zero delta")
	}
	if CanAcceptGrowth(900, 200, quota, 10_000) {
		t.Fatal("over quota")
	}
	if CanAcceptGrowth(100, 50, quota, 10) {
		t.Fatal("disk full")
	}
	if !CanAcceptGrowth(100, 50, quota, -1) {
		t.Fatal("unknown disk should allow when under quota")
	}
	if !CanAcceptGrowth(100, 50, quota, 100) {
		t.Fatal("ok growth")
	}
	if !CanAcceptGrowth(100, 50, UnlimitedConfigured, 100) {
		t.Fatal("unlimited under disk")
	}
	if CanAcceptGrowth(100, 50, UnlimitedConfigured, 10) {
		t.Fatal("unlimited disk full")
	}
	if !GrowthRejectIsDisk(100, 50, UnlimitedConfigured, 10) {
		t.Fatal("expect disk reject")
	}
	if GrowthRejectIsDisk(900, 200, quota, 10_000) {
		t.Fatal("expect quota reject not disk")
	}
}

func TestResolveConfigured(t *testing.T) {
	cfg, mode := ResolveConfigured(sql.NullInt64{}, 1000)
	if cfg != 1000 || mode != QuotaModeDefault {
		t.Fatalf("default %d %s", cfg, mode)
	}
	cfg, mode = ResolveConfigured(sql.NullInt64{}, 0)
	if cfg != UnlimitedConfigured || mode != QuotaModeDefault {
		t.Fatalf("default unlimited %d %s", cfg, mode)
	}
	cfg, mode = ResolveConfigured(sql.NullInt64{Valid: true, Int64: 0}, 1000)
	if cfg != UnlimitedConfigured || mode != QuotaModeUnlimited {
		t.Fatalf("personal unlimited %d %s", cfg, mode)
	}
	cfg, mode = ResolveConfigured(sql.NullInt64{Valid: true, Int64: 500}, 1000)
	if cfg != 500 || mode != QuotaModePersonal {
		t.Fatalf("personal %d %s", cfg, mode)
	}
}

func TestBuildQuotaView(t *testing.T) {
	v := BuildQuotaView(10, UnlimitedConfigured, 0, QuotaModeUnlimited, -1)
	if v.ConfiguredLimitBytes != nil || v.EffectiveLimitBytes != nil {
		t.Fatalf("want null limits: %+v", v)
	}
	v = BuildQuotaView(10, 1000, 1000, QuotaModeDefault, 50)
	if v.ConfiguredLimitBytes == nil || *v.ConfiguredLimitBytes != 1000 {
		t.Fatal("configured")
	}
	if v.EffectiveLimitBytes == nil || *v.EffectiveLimitBytes != 60 {
		t.Fatalf("effective=%v", v.EffectiveLimitBytes)
	}
}

func TestFreeBytesDataDir(t *testing.T) {
	dir := t.TempDir()
	n, err := FreeBytes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n <= 0 {
		t.Fatalf("free=%d", n)
	}
}
