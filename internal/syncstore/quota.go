package syncstore

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
)

// UnlimitedConfigured means no vault byte cap (disk free space may still bind).
const UnlimitedConfigured int64 = -1

// QuotaMode values for GET /sync/quota.
const (
	QuotaModeDefault   = "default"
	QuotaModePersonal  = "personal"
	QuotaModeUnlimited = "unlimited"
)

// QuotaView is the GET /sync/quota response shape (Phase 2).
type QuotaView struct {
	UsageBytes           int64  `json:"usageBytes"`
	ConfiguredLimitBytes *int64 `json:"configuredLimitBytes"` // null = unlimited
	EffectiveLimitBytes  *int64 `json:"effectiveLimitBytes"`  // null = unlimited / unknown
	Mode                 string `json:"mode"`
	DefaultLimitBytes    int64  `json:"defaultLimitBytes"` // 0 when server default is unlimited
}

// ResolveConfigured maps user.quota_bytes + server default to configured limit + mode.
// userQuota Valid=false → server default; Int64=0 → unlimited; >0 → personal.
// defaultBytes 0 → unlimited server default.
func ResolveConfigured(userQuota sql.NullInt64, defaultBytes int64) (configured int64, mode string) {
	if !userQuota.Valid {
		mode = QuotaModeDefault
		if defaultBytes <= 0 {
			return UnlimitedConfigured, mode
		}
		return defaultBytes, mode
	}
	if userQuota.Int64 == 0 {
		return UnlimitedConfigured, QuotaModeUnlimited
	}
	if userQuota.Int64 < 0 {
		return UnlimitedConfigured, QuotaModeUnlimited
	}
	return userQuota.Int64, QuotaModePersonal
}

// WalkUsage sums regular file sizes under root. Does not follow symlinks.
// Missing root returns 0, nil.
func WalkUsage(root string) (int64, error) {
	info, err := os.Lstat(root)
	if errorsIsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return 0, nil
	}
	var total int64
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		total += fi.Size()
		return nil
	})
	return total, err
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}

// EffectiveQuotaLimit is the hard stop for writes.
// configuredQuota == UnlimitedConfigured → disk-only (usage+diskFree), or UnlimitedConfigured if disk unknown.
func EffectiveQuotaLimit(usage, configuredQuota, diskFree int64) int64 {
	if configuredQuota == UnlimitedConfigured {
		if diskFree < 0 {
			return UnlimitedConfigured
		}
		return usage + diskFree
	}
	if configuredQuota <= 0 {
		// Invalid input (callers must use UnlimitedConfigured or a positive cap).
		return 0
	}
	if diskFree < 0 {
		return configuredQuota
	}
	capped := usage + diskFree
	if capped < configuredQuota {
		return capped
	}
	return configuredQuota
}

// CanAcceptGrowth reports whether usage may grow by delta bytes.
func CanAcceptGrowth(usage, delta, configuredQuota, diskFree int64) bool {
	if delta <= 0 {
		return true
	}
	if configuredQuota != UnlimitedConfigured {
		if usage+delta > configuredQuota {
			return false
		}
	}
	if diskFree >= 0 && diskFree < delta {
		return false
	}
	return true
}

// GrowthRejectIsDisk reports whether a failed CanAcceptGrowth is due to free disk (vs quota).
func GrowthRejectIsDisk(_usage, delta, _configuredQuota, diskFree int64) bool {
	if delta <= 0 {
		return false
	}
	return diskFree >= 0 && diskFree < delta
}

// BuildQuotaView builds the API quota payload.
func BuildQuotaView(usage, configured, defaultBytes int64, mode string, diskFree int64) QuotaView {
	v := QuotaView{
		UsageBytes:        usage,
		Mode:              mode,
		DefaultLimitBytes: defaultBytes,
	}
	if defaultBytes < 0 {
		v.DefaultLimitBytes = 0
	}
	if configured != UnlimitedConfigured {
		c := configured
		v.ConfiguredLimitBytes = &c
	}
	eff := EffectiveQuotaLimit(usage, configured, diskFree)
	if eff != UnlimitedConfigured {
		v.EffectiveLimitBytes = &eff
	}
	return v
}
