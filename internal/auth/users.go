package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// User is an account row.
type User struct {
	ID                     string
	Email                  string
	PasswordHash           string
	DisplayName            string
	IsAdmin                bool
	DisabledAt             sql.NullString
	PasswordVersion        int
	QuotaBytes             sql.NullInt64 // Valid=false → server default; Int64=0 → unlimited
	RecoveryCodesAckedAt   sql.NullString
	RecoveryCodesGeneration int
	CreatedAt              string
	UpdatedAt              string
}

// ErrNotFound is returned when a user does not exist.
var ErrNotFound = errors.New("user not found")

// ErrConflict is returned when email is already taken.
var ErrConflict = errors.New("email already registered")

// ErrUserLimit is returned when MAX_USERS is reached (all rows count).
var ErrUserLimit = errors.New("user limit reached")

// ErrDisplayNameRequired is returned when display name is empty.
var ErrDisplayNameRequired = errors.New("display name is required")

// ErrUsersExist is returned by CreateIfEmpty when the users table is not empty.
var ErrUsersExist = errors.New("users already exist")

// UserStore persists accounts in SQLite.
type UserStore struct {
	db *sql.DB
}

// OpenUserStore opens (or creates) server.db under dataDir.
func OpenUserStore(dataDir string) (*UserStore, error) {
	dbPath := filepath.ToSlash(filepath.Join(dataDir, "server.db"))
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)",
		dbPath,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &UserStore{db: db}
	if err := s.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *UserStore) ensureSchema() error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
  id                       TEXT PRIMARY KEY NOT NULL,
  email                    TEXT NOT NULL COLLATE NOCASE UNIQUE,
  password_hash            TEXT NOT NULL,
  display_name             TEXT NOT NULL DEFAULT '',
  is_admin                 INTEGER NOT NULL DEFAULT 0,
  disabled_at              TEXT,
  password_version         INTEGER NOT NULL DEFAULT 0,
  quota_bytes              INTEGER,
  recovery_codes_acked_at  TEXT,
  created_at               TEXT NOT NULL,
  updated_at               TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}
	if err := ensureRecoveryGenerationColumn(s.db); err != nil {
		return err
	}
	if err := ensureInvitesSchema(s.db); err != nil {
		return err
	}
	if err := ensureRecoveryCodesSchema(s.db); err != nil {
		return err
	}
	return nil
}

// Close closes the database.
func (s *UserStore) Close() error {
	return s.db.Close()
}

// NormalizeEmail trims and lowercases email for uniqueness.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

const userSelectCols = `id, email, password_hash, display_name, is_admin, disabled_at, password_version, quota_bytes, recovery_codes_acked_at, recovery_codes_generation, created_at, updated_at`

// Create inserts a new user. password must already meet strength rules.
// displayName is required. First user is always admin; otherwise asAdmin controls the flag.
// maxUsers: 0 = unlimited; otherwise Count (incl. disabled) must be < maxUsers.
func (s *UserStore) Create(ctx context.Context, email, password, displayName string, asAdmin bool, maxUsers int) (*User, error) {
	return s.create(ctx, email, password, displayName, asAdmin, maxUsers, false)
}

// CreateIfEmpty inserts a user only when the table is empty (same IMMEDIATE txn as the count).
// Used by boot seed so a concurrent create-user cannot race past a non-atomic empty check.
func (s *UserStore) CreateIfEmpty(ctx context.Context, email, password, displayName string, maxUsers int) (*User, error) {
	return s.create(ctx, email, password, displayName, true, maxUsers, true)
}

func (s *UserStore) create(ctx context.Context, email, password, displayName string, asAdmin bool, maxUsers int, requireEmpty bool) (*User, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return nil, ErrDisplayNameRequired
	}
	if err := ValidatePasswordStrength(password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("db conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()

	var n int64
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}
	if requireEmpty && n > 0 {
		return nil, ErrUsersExist
	}
	if maxUsers > 0 && n >= int64(maxUsers) {
		return nil, ErrUserLimit
	}

	isAdmin := asAdmin || n == 0
	adminFlag := 0
	if isAdmin {
		adminFlag = 1
	}

	now := time.Now().UTC().Format(time.RFC3339)
	u := &User{
		ID:                    uuid.NewString(),
		Email:                 email,
		PasswordHash:          hash,
		DisplayName:           displayName,
		IsAdmin:               isAdmin,
		PasswordVersion:       0,
		RecoveryCodesAckedAt:  sql.NullString{String: now, Valid: true},
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	_, err = conn.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, password_version, recovery_codes_acked_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, u.DisplayName, adminFlag, now, u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	committed = true
	return u, nil
}

// Count returns the number of users.
func (s *UserStore) Count(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// GetByEmail looks up a user by normalized email.
func (s *UserStore) GetByEmail(ctx context.Context, email string) (*User, error) {
	email = NormalizeEmail(email)
	return s.scanOne(ctx, `SELECT `+userSelectCols+` FROM users WHERE email = ?`, email)
}

// GetByID looks up a user by id.
func (s *UserStore) GetByID(ctx context.Context, id string) (*User, error) {
	return s.scanOne(ctx, `SELECT `+userSelectCols+` FROM users WHERE id = ?`, id)
}

// SetPassword updates the password hash for an existing user (admin reset).
func (s *UserStore) SetPassword(ctx context.Context, email, password string) error {
	email = NormalizeEmail(email)
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if err := ValidatePasswordStrength(password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, password_version = password_version + 1, updated_at = ? WHERE email = ?`,
		hash, now, email,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDisabled sets or clears disabled_at (RFC3339 when disabled).
func (s *UserStore) SetDisabled(ctx context.Context, userID string, disabled bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var res sql.Result
	var err error
	if disabled {
		res, err = s.db.ExecContext(ctx,
			`UPDATE users SET disabled_at = ?, updated_at = ? WHERE id = ?`,
			now, now, userID,
		)
	} else {
		res, err = s.db.ExecContext(ctx,
			`UPDATE users SET disabled_at = NULL, updated_at = ? WHERE id = ?`,
			now, userID,
		)
	}
	if err != nil {
		return fmt.Errorf("set disabled: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAdmin sets is_admin for a user.
func (s *UserStore) SetAdmin(ctx context.Context, userID string, isAdmin bool) error {
	flag := 0
	if isAdmin {
		flag = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET is_admin = ?, updated_at = ? WHERE id = ?`,
		flag, now, userID,
	)
	if err != nil {
		return fmt.Errorf("set admin: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// BumpPasswordVersion increments password_version (invalidates JWTs).
func (s *UserStore) BumpPasswordVersion(ctx context.Context, userID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_version = password_version + 1, updated_at = ? WHERE id = ?`,
		now, userID,
	)
	if err != nil {
		return fmt.Errorf("bump password_version: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetQuotaBytes sets per-user quota override. nil → server default; 0 → unlimited; >0 → personal.
func (s *UserStore) SetQuotaBytes(ctx context.Context, userID string, quotaBytes *int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var res sql.Result
	var err error
	if quotaBytes == nil {
		res, err = s.db.ExecContext(ctx,
			`UPDATE users SET quota_bytes = NULL, updated_at = ? WHERE id = ?`,
			now, userID,
		)
	} else {
		res, err = s.db.ExecContext(ctx,
			`UPDATE users SET quota_bytes = ?, updated_at = ? WHERE id = ?`,
			*quotaBytes, now, userID,
		)
	}
	if err != nil {
		return fmt.Errorf("set quota: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// IsDisabled reports whether the user has disabled_at set.
func (u *User) IsDisabled() bool {
	return u.DisabledAt.Valid && strings.TrimSpace(u.DisabledAt.String) != ""
}

func (s *UserStore) scanOne(ctx context.Context, query string, arg any) (*User, error) {
	var u User
	var isAdmin int
	err := s.db.QueryRowContext(ctx, query, arg).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName,
		&isAdmin, &u.DisabledAt, &u.PasswordVersion, &u.QuotaBytes, &u.RecoveryCodesAckedAt,
		&u.RecoveryCodesGeneration, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	u.IsAdmin = isAdmin != 0
	return &u, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint")
}
