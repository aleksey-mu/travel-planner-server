package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	DefaultDataDir                 = "data"
	DefaultHTTPAddr                = ":8080"
	DefaultJWTTTLSeconds           = 3600
	VaultQuotaMB                   = int64(10240) // 10 GiB
	DefaultMaxUploadMB             = int64(50)
	BytesPerMB                     = int64(1024 * 1024)
	DefaultSeedDisplayName         = "Admin"
	MinJWTSecretLen                = 32
	DefaultMaxUsers                = 100
	DefaultInviteTTLDays           = 7
	DefaultPasswordResetTTLSeconds = 3600
	DefaultAuthRateLimitMax        = 20
	DefaultAuthRateLimitWindowSec  = 900 // 15 minutes
)

// Known weak / documented placeholders that must be rejected even if long enough.
var jwtSecretPlaceholders = []string{
	"change-me",
	"changeme",
	"change-me-to-a-long-random-string-at-least-32-chars",
	"your-secret-here",
	"replace-me",
	"secret",
	"jwt-secret",
}

// Config holds validated runtime settings.
type Config struct {
	DataDir                 string
	JWTSecret               string
	HTTPAddr                string
	CORSOrigins             []string
	JWTTTLSeconds           int
	DefaultQuotaBytes       int64
	MaxUploadBytes          int64
	SeedAdminEmail          string
	SeedAdminPassword       string
	SeedAdminDisplayName    string
	ServerName              string
	PublicBaseURL           string // optional; must be https:// when set
	MaxUsers                int    // 0 = unlimited user seats
	InviteTTLDays           int
	PasswordResetTTLSeconds int
	AuthRateLimitMax        int           // attempts per window; 0 = disabled
	AuthRateLimitWindow     time.Duration // window length
	TrustProxy              bool          // honor X-Real-IP / X-Forwarded-For (trusted proxy only)
}

// Load reads environment (optional .env) and validates required fields.
func Load() (*Config, error) {
	_ = godotenv.Load() // soft-fail if missing

	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = DefaultDataDir
	}

	secret := os.Getenv("JWT_SECRET")
	if err := validateJWTSecret(secret); err != nil {
		return nil, err
	}

	httpAddr := strings.TrimSpace(os.Getenv("HTTP_ADDR"))
	if httpAddr == "" {
		httpAddr = DefaultHTTPAddr
	}

	corsOrigins := parseCSV(os.Getenv("CORS_ORIGINS"))

	quotaMB, err := parseInt64(os.Getenv("VAULT_QUOTA_MB"), VaultQuotaMB)
	if err != nil {
		return nil, fmt.Errorf("VAULT_QUOTA_MB: %w", err)
	}
	if quotaMB < 0 {
		return nil, fmt.Errorf("VAULT_QUOTA_MB must be >= 0 (0 = unlimited)")
	}

	maxUploadMB, err := parseInt64(os.Getenv("MAX_UPLOAD_MB"), DefaultMaxUploadMB)
	if err != nil {
		return nil, fmt.Errorf("MAX_UPLOAD_MB: %w", err)
	}
	if maxUploadMB <= 0 {
		return nil, fmt.Errorf("MAX_UPLOAD_MB must be positive")
	}

	maxUsers, err := parseInt(os.Getenv("MAX_USERS"), DefaultMaxUsers)
	if err != nil {
		return nil, fmt.Errorf("MAX_USERS: %w", err)
	}
	if maxUsers < 0 {
		return nil, fmt.Errorf("MAX_USERS must be >= 0 (0 = unlimited)")
	}

	trustProxy, err := parseBool(os.Getenv("TRUST_PROXY"), false)
	if err != nil {
		return nil, fmt.Errorf("TRUST_PROXY: %w", err)
	}

	publicBase, err := parsePublicBaseURL(os.Getenv("PUBLIC_BASE_URL"))
	if err != nil {
		return nil, err
	}

	return &Config{
		DataDir:                 dataDir,
		JWTSecret:               secret,
		HTTPAddr:                httpAddr,
		CORSOrigins:             corsOrigins,
		JWTTTLSeconds:           DefaultJWTTTLSeconds,
		DefaultQuotaBytes:       quotaMB * BytesPerMB,
		MaxUploadBytes:          maxUploadMB * BytesPerMB,
		SeedAdminEmail:          strings.TrimSpace(os.Getenv("SEED_ADMIN_EMAIL")),
		SeedAdminPassword:       os.Getenv("SEED_ADMIN_PASSWORD"),
		SeedAdminDisplayName:    DefaultSeedDisplayName,
		ServerName:              strings.TrimSpace(os.Getenv("SERVER_NAME")),
		PublicBaseURL:           publicBase,
		MaxUsers:                maxUsers,
		InviteTTLDays:           DefaultInviteTTLDays,
		PasswordResetTTLSeconds: DefaultPasswordResetTTLSeconds,
		AuthRateLimitMax:        DefaultAuthRateLimitMax,
		AuthRateLimitWindow:     DefaultAuthRateLimitWindowSec * time.Second,
		TrustProxy:              trustProxy,
	}, nil
}

// EffectiveServerName returns SERVER_NAME, or hostname from PUBLIC_BASE_URL, or "".
func (c *Config) EffectiveServerName() string {
	if name := strings.TrimSpace(c.ServerName); name != "" {
		return name
	}
	if c.PublicBaseURL == "" {
		return ""
	}
	u, err := url.Parse(c.PublicBaseURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func parsePublicBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("PUBLIC_BASE_URL: %w", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("PUBLIC_BASE_URL must be https:// when set")
	}
	if u.Host == "" {
		return "", fmt.Errorf("PUBLIC_BASE_URL must include a host")
	}
	if u.User != nil {
		return "", fmt.Errorf("PUBLIC_BASE_URL must not include credentials")
	}
	// Normalize: no trailing slash
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func validateJWTSecret(secret string) error {
	if secret == "" {
		return fmt.Errorf("JWT_SECRET is required")
	}
	if len(secret) < MinJWTSecretLen {
		return fmt.Errorf("JWT_SECRET must be at least %d characters", MinJWTSecretLen)
	}
	lower := strings.ToLower(secret)
	for _, p := range jwtSecretPlaceholders {
		if lower == strings.ToLower(p) || strings.Contains(lower, "at-least-32-chars") || strings.Contains(lower, "change-me") {
			return fmt.Errorf("JWT_SECRET looks like a documented placeholder; generate a real secret (e.g. openssl rand -base64 48)")
		}
	}
	return nil
}

func parseCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseBool(s string, def bool) (bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false, err
	}
	return v, nil
}

func parseInt(s string, def int) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	return strconv.Atoi(s)
}

func parseInt64(s string, def int64) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	return strconv.ParseInt(s, 10, 64)
}
