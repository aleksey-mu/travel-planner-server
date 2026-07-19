package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

const (
	DefaultHTTPAddr        = ":8080"
	DefaultJWTTTLSeconds   = 3600
	VaultQuotaMB         = int64(10240) // 10 GiB
	DefaultMaxUploadMB     = int64(50)
	BytesPerMB             = int64(1024 * 1024)
	DefaultLogFormat       = "json"
	DefaultLogLevel        = "info"
	DefaultSeedDisplayName = "Admin"
	MinJWTSecretLen        = 32
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
	DataDir              string
	JWTSecret            string
	HTTPAddr             string
	CORSOrigins          []string
	AllowRegister        bool
	JWTTTLSeconds        int
	DefaultQuotaBytes    int64
	MaxUploadBytes       int64
	LogFormat            string
	LogLevel             string
	SeedAdminEmail       string
	SeedAdminPassword    string
	SeedAdminDisplayName string
}

// Load reads environment (optional .env) and validates required fields.
func Load() (*Config, error) {
	_ = godotenv.Load() // soft-fail if missing

	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		return nil, fmt.Errorf("DATA_DIR is required")
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

	allowRegister, err := parseBool(os.Getenv("ALLOW_REGISTER"), false)
	if err != nil {
		return nil, fmt.Errorf("ALLOW_REGISTER: %w", err)
	}

	ttl, err := parseInt(os.Getenv("JWT_TTL_SECONDS"), DefaultJWTTTLSeconds)
	if err != nil {
		return nil, fmt.Errorf("JWT_TTL_SECONDS: %w", err)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("JWT_TTL_SECONDS must be positive")
	}

	quotaMB, err := parseInt64(os.Getenv("VAULT_QUOTA_MB"), VaultQuotaMB)
	if err != nil {
		return nil, fmt.Errorf("VAULT_QUOTA_MB: %w", err)
	}
	if quotaMB <= 0 {
		return nil, fmt.Errorf("VAULT_QUOTA_MB must be positive")
	}

	maxUploadMB, err := parseInt64(os.Getenv("MAX_UPLOAD_MB"), DefaultMaxUploadMB)
	if err != nil {
		return nil, fmt.Errorf("MAX_UPLOAD_MB: %w", err)
	}
	if maxUploadMB <= 0 {
		return nil, fmt.Errorf("MAX_UPLOAD_MB must be positive")
	}

	logFormat := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_FORMAT")))
	if logFormat == "" {
		logFormat = DefaultLogFormat
	}
	if logFormat != "json" && logFormat != "text" {
		return nil, fmt.Errorf("LOG_FORMAT must be json or text")
	}

	logLevel := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL")))
	if logLevel == "" {
		logLevel = DefaultLogLevel
	}

	seedDisplay := strings.TrimSpace(os.Getenv("SEED_ADMIN_DISPLAY_NAME"))
	if seedDisplay == "" {
		seedDisplay = DefaultSeedDisplayName
	}

	return &Config{
		DataDir:              dataDir,
		JWTSecret:            secret,
		HTTPAddr:             httpAddr,
		CORSOrigins:          corsOrigins,
		AllowRegister:        allowRegister,
		JWTTTLSeconds:        ttl,
		DefaultQuotaBytes:    quotaMB * BytesPerMB,
		MaxUploadBytes:       maxUploadMB * BytesPerMB,
		LogFormat:            logFormat,
		LogLevel:             logLevel,
		SeedAdminEmail:       strings.TrimSpace(os.Getenv("SEED_ADMIN_EMAIL")),
		SeedAdminPassword:    os.Getenv("SEED_ADMIN_PASSWORD"),
		SeedAdminDisplayName: seedDisplay,
	}, nil
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
