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
	ID           string
	Email        string
	PasswordHash string
	DisplayName  string
	CreatedAt    string
	UpdatedAt    string
}

// ErrNotFound is returned when a user does not exist.
var ErrNotFound = errors.New("user not found")

// ErrConflict is returned when email is already taken.
var ErrConflict = errors.New("email already registered")

// ErrOwnerExists is returned when Phase 1 single-owner vault already has a user.
var ErrOwnerExists = errors.New("vault already has an owner")

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
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *UserStore) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
  id            TEXT PRIMARY KEY NOT NULL,
  email         TEXT NOT NULL COLLATE NOCASE UNIQUE,
  password_hash TEXT NOT NULL,
  display_name  TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
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

// Create inserts a new user. password must already meet strength rules.
// Phase 1: at most one user (single-owner vault).
func (s *UserStore) Create(ctx context.Context, email, password, displayName string) (*User, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, fmt.Errorf("email is required")
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
	if n >= 1 {
		return nil, ErrOwnerExists
	}

	now := time.Now().UTC().Format(time.RFC3339)
	u := &User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: hash,
		DisplayName:  strings.TrimSpace(displayName),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err = conn.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, u.DisplayName, u.CreatedAt, u.UpdatedAt,
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
	return s.scanOne(ctx, `SELECT id, email, password_hash, display_name, created_at, updated_at FROM users WHERE email = ?`, email)
}

// GetByID looks up a user by id.
func (s *UserStore) GetByID(ctx context.Context, id string) (*User, error) {
	return s.scanOne(ctx, `SELECT id, email, password_hash, display_name, created_at, updated_at FROM users WHERE id = ?`, id)
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
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE email = ?`,
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

func (s *UserStore) scanOne(ctx context.Context, query string, arg any) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, query, arg).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	return &u, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint")
}
