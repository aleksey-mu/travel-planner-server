package syncstore

import (
	"io/fs"
	"os"
	"path/filepath"
)

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

// EffectiveQuotaLimit is min(configuredQuota, usage+diskFree).
// If diskFree < 0 (unknown), returns configuredQuota.
func EffectiveQuotaLimit(usage, configuredQuota, diskFree int64) int64 {
	if configuredQuota <= 0 {
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
	if usage+delta > configuredQuota {
		return false
	}
	if diskFree >= 0 && diskFree < delta {
		return false
	}
	return true
}
