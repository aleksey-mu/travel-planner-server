package syncstore

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Allowed user-data file names (exact match).
var allowedUserDataFiles = map[string]struct{}{
	"user-prefs.json":          {},
	"item-types.json":          {},
	"checklist-templates.json": {},
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidateSegmentID checks travelId / itemId.
func ValidateSegmentID(id string) error {
	if id == "" || id == "." || id == ".." {
		return fmt.Errorf("invalid id")
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "\x00") {
		return fmt.Errorf("invalid id")
	}
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid id")
	}
	return nil
}

// ValidateAttachmentFileName checks attachment fileName segment.
func ValidateAttachmentFileName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid file name")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "\x00") {
		return fmt.Errorf("invalid file name")
	}
	if len(name) > 255 {
		return fmt.Errorf("file name too long")
	}
	return nil
}

// IsAllowedUserDataFile reports whether name is on the allowlist.
func IsAllowedUserDataFile(name string) bool {
	_, ok := allowedUserDataFiles[name]
	return ok
}

// cleanSlashPath rejects traversal and returns a cleaned slash-separated relative path.
// Does not use path.Clean on paths that start with ".." (Clean would collapse them away).
func cleanSlashPath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	rel = strings.ReplaceAll(rel, `\`, "/")
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.Contains(rel, "\x00") {
		return "", fmt.Errorf("invalid path")
	}
	if strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path not allowed")
	}
	parts := strings.Split(rel, "/")
	out := make([]string, 0, len(parts))
	for _, seg := range parts {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			return "", fmt.Errorf("path traversal not allowed")
		}
		out = append(out, seg)
	}
	if len(out) == 0 {
		return "", fmt.Errorf("invalid path")
	}
	return strings.Join(out, "/"), nil
}

// NormalizeFileID accepts "files/a/b/c" or "a/b/c" and returns a clean
// relative path under the user's files/ directory (slash-separated).
func NormalizeFileID(fileID string) (string, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return "", fmt.Errorf("empty file id")
	}
	fileID = strings.ReplaceAll(fileID, `\`, "/")
	if strings.Contains(fileID, "\x00") {
		return "", fmt.Errorf("invalid file id")
	}
	if strings.HasPrefix(fileID, "/") || filepath.IsAbs(fileID) {
		return "", fmt.Errorf("absolute path not allowed")
	}
	if strings.HasPrefix(fileID, "files/") {
		fileID = strings.TrimPrefix(fileID, "files/")
	}
	return cleanSlashPath(fileID)
}

// ResolveUnderRoot joins root with slash-separated relative parts and ensures
// the result stays under root (no traversal).
func ResolveUnderRoot(root string, relSlash string) (string, error) {
	cleaned, err := cleanSlashPath(relSlash)
	if err != nil {
		return "", err
	}
	parts := strings.Split(cleaned, "/")
	full := filepath.Join(append([]string{root}, parts...)...)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, absFull)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal not allowed")
	}
	return absFull, nil
}
