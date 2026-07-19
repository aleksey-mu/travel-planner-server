package syncstore

import "testing"

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
