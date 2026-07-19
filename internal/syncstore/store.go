package syncstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrNotFound indicates a missing file.
var ErrNotFound = errors.New("not found")

// ErrQuotaExceeded indicates usage would exceed the quota.
var ErrQuotaExceeded = errors.New("quota exceeded")

// ErrDiskFull indicates the host volume does not have enough free space.
var ErrDiskFull = errors.New("disk full")

// ErrPayloadTooLarge indicates the body exceeds MaxUploadBytes.
var ErrPayloadTooLarge = errors.New("payload too large")

// Store is per-user filesystem storage under DATA_DIR/users/{userId}/.
type Store struct {
	dataDir        string
	quotaLimit     int64
	maxUploadBytes int64
	writeMu        sync.Mutex
}

// New creates a Store.
func New(dataDir string, quotaLimit, maxUploadBytes int64) *Store {
	return &Store{
		dataDir:        dataDir,
		quotaLimit:     quotaLimit,
		maxUploadBytes: maxUploadBytes,
	}
}

func (s *Store) userRoot(userID string) (string, error) {
	if err := ValidateSegmentID(userID); err != nil {
		return "", fmt.Errorf("invalid user id: %w", err)
	}
	return filepath.Join(s.dataDir, "users", userID), nil
}

// Bootstrap creates the per-user directory tree. Idempotent.
func (s *Store) Bootstrap(userID string) error {
	root, err := s.userRoot(userID)
	if err != nil {
		return err
	}
	dirs := []string{
		root,
		filepath.Join(root, "travels"),
		filepath.Join(root, "files"),
		filepath.Join(root, "user-data"),
		filepath.Join(root, "meta"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}

// EmptyManifest returns the default empty manifest JSON bytes.
func EmptyManifest() []byte {
	now := time.Now().UTC().Format(time.RFC3339)
	m := map[string]any{
		"schemaVersion": 1,
		"updatedAt":     now,
		"travels":       []any{},
	}
	b, _ := json.Marshal(m)
	return b
}

// GetManifest returns manifest JSON or empty default if missing.
func (s *Store) GetManifest(userID string) ([]byte, error) {
	root, err := s.userRoot(userID)
	if err != nil {
		return nil, err
	}
	p := filepath.Join(root, "manifest.json")
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return EmptyManifest(), nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// PutManifest writes manifest JSON after quota check.
func (s *Store) PutManifest(userID string, body []byte) error {
	if err := s.ensureJSONObject(body); err != nil {
		return err
	}
	root, err := s.userRoot(userID)
	if err != nil {
		return err
	}
	p := filepath.Join(root, "manifest.json")
	return s.writeFileQuota(userID, p, body)
}

// GetTravel returns travel JSON.
func (s *Store) GetTravel(userID, travelID string) ([]byte, error) {
	if err := ValidateSegmentID(travelID); err != nil {
		return nil, err
	}
	root, err := s.userRoot(userID)
	if err != nil {
		return nil, err
	}
	p := filepath.Join(root, "travels", travelID+".json")
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

// PutTravel writes travel JSON and returns fileId (travelId).
func (s *Store) PutTravel(userID, travelID string, body []byte) error {
	if err := ValidateSegmentID(travelID); err != nil {
		return err
	}
	if err := s.ensureJSONObject(body); err != nil {
		return err
	}
	root, err := s.userRoot(userID)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "travels")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dir, travelID+".json")
	return s.writeFileQuota(userID, p, body)
}

// DeleteTravel removes a travel document (idempotent).
func (s *Store) DeleteTravel(userID, travelID string) error {
	if err := ValidateSegmentID(travelID); err != nil {
		return err
	}
	root, err := s.userRoot(userID)
	if err != nil {
		return err
	}
	p := filepath.Join(root, "travels", travelID+".json")
	err = os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// GetUserData returns a user-data JSON file.
func (s *Store) GetUserData(userID, fileName string) ([]byte, error) {
	if !IsAllowedUserDataFile(fileName) {
		return nil, fmt.Errorf("invalid user-data file")
	}
	root, err := s.userRoot(userID)
	if err != nil {
		return nil, err
	}
	p := filepath.Join(root, "user-data", fileName)
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

// PutUserData writes a user-data JSON file.
func (s *Store) PutUserData(userID, fileName string, body []byte) error {
	if !IsAllowedUserDataFile(fileName) {
		return fmt.Errorf("invalid user-data file")
	}
	if err := s.ensureJSONObject(body); err != nil {
		return err
	}
	root, err := s.userRoot(userID)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "user-data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dir, fileName)
	return s.writeFileQuota(userID, p, body)
}

// EnsureFolder creates files/{travelId}/{itemId}/.
func (s *Store) EnsureFolder(userID, travelID, itemID string) (folderID string, err error) {
	if err := ValidateSegmentID(travelID); err != nil {
		return "", err
	}
	if err := ValidateSegmentID(itemID); err != nil {
		return "", err
	}
	root, err := s.userRoot(userID)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "files", travelID, itemID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return "files/" + travelID + "/" + itemID, nil
}

// PutFile writes an attachment blob. fileId is always files/{travel}/{item}/{name}.
func (s *Store) PutFile(userID, travelID, itemID, fileName string, body []byte) (fileID string, err error) {
	if err := ValidateSegmentID(travelID); err != nil {
		return "", err
	}
	if err := ValidateSegmentID(itemID); err != nil {
		return "", err
	}
	if err := ValidateAttachmentFileName(fileName); err != nil {
		return "", err
	}
	if int64(len(body)) > s.maxUploadBytes {
		return "", ErrPayloadTooLarge
	}
	root, err := s.userRoot(userID)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "files", travelID, itemID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, fileName)
	if err := s.writeFileQuota(userID, p, body); err != nil {
		return "", err
	}
	return "files/" + travelID + "/" + itemID + "/" + fileName, nil
}

// GetFile reads an attachment by normalized relative path under files/.
func (s *Store) GetFile(userID, relUnderFiles string) ([]byte, string, error) {
	root, err := s.userRoot(userID)
	if err != nil {
		return nil, "", err
	}
	filesRoot := filepath.Join(root, "files")
	full, err := ResolveUnderRoot(filesRoot, relUnderFiles)
	if err != nil {
		return nil, "", err
	}
	info, err := os.Lstat(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, "", ErrNotFound
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return nil, "", err
	}
	ct := "application/octet-stream"
	return b, ct, nil
}

// DeleteFile removes an attachment (idempotent).
func (s *Store) DeleteFile(userID, relUnderFiles string) error {
	root, err := s.userRoot(userID)
	if err != nil {
		return err
	}
	filesRoot := filepath.Join(root, "files")
	full, err := ResolveUnderRoot(filesRoot, relUnderFiles)
	if err != nil {
		return err
	}
	err = os.Remove(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Usage returns recursive byte usage of the user root (no symlink follow).
func (s *Store) Usage(userID string) (int64, error) {
	root, err := s.userRoot(userID)
	if err != nil {
		return 0, err
	}
	return WalkUsage(root)
}

// Quota returns usage and effective limit (min of configured quota and free disk).
func (s *Store) Quota(userID string) (usage, limit int64, err error) {
	usage, err = s.Usage(userID)
	if err != nil {
		return 0, 0, err
	}
	diskFree := s.diskFreeOrUnknown()
	return usage, EffectiveQuotaLimit(usage, s.quotaLimit, diskFree), nil
}

func (s *Store) diskFreeOrUnknown() int64 {
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return -1
	}
	free, err := FreeBytes(s.dataDir)
	if err != nil {
		return -1
	}
	return free
}

func (s *Store) writeFileQuota(userID, dest string, body []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	var existingSize int64
	if info, err := os.Lstat(dest); err == nil && info.Mode().IsRegular() {
		existingSize = info.Size()
	}
	usage, err := s.Usage(userID)
	if err != nil {
		return err
	}
	delta := int64(len(body)) - existingSize
	diskFree := s.diskFreeOrUnknown()
	if !CanAcceptGrowth(usage, delta, s.quotaLimit, diskFree) {
		if delta > 0 && usage+delta <= s.quotaLimit && diskFree >= 0 && diskFree < delta {
			return ErrDiskFull
		}
		return ErrQuotaExceeded
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return mapWriteErr(err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return mapWriteErr(err)
	}
	return nil
}

func mapWriteErr(err error) error {
	if err == nil {
		return nil
	}
	if isNoSpaceErr(err) {
		return ErrDiskFull
	}
	return err
}

func isNoSpaceErr(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == syscall.ENOSPC {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no space left") ||
		strings.Contains(msg, "not enough space") ||
		strings.Contains(msg, "disk full")
}

func (s *Store) ensureJSONObject(body []byte) error {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return fmt.Errorf("invalid json")
	}
	switch v.(type) {
	case map[string]any:
		return nil
	default:
		return fmt.Errorf("json must be an object")
	}
}

// ReadBodyLimited reads r up to max+1 bytes; returns ErrPayloadTooLarge if exceeded.
func ReadBodyLimited(r io.Reader, max int64) ([]byte, error) {
	limited := io.LimitReader(r, max+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, ErrPayloadTooLarge
	}
	return b, nil
}
