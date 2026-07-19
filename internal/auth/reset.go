package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrRecoveryCodeInvalid is returned when no unused matching recovery code exists.
var ErrRecoveryCodeInvalid = errors.New("invalid recovery code")

// ErrResetNotFound is returned when a password-reset token is unknown.
var ErrResetNotFound = errors.New("password reset not found")

// ErrResetExpired is returned when a password-reset token is past expires_at.
var ErrResetExpired = errors.New("password reset expired")

// ErrResetUsed is returned when a password-reset token was already consumed.
var ErrResetUsed = errors.New("password reset already used")

// ErrResetRevoked is returned when a password-reset token was revoked.
var ErrResetRevoked = errors.New("password reset revoked")

// PasswordResetToken is a one-time admin-issued reset row.
type PasswordResetToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt string
	CreatedAt string
	UsedAt    sql.NullString
	RevokedAt sql.NullString
}

// CreatedPasswordReset includes the plaintext token shown once.
type CreatedPasswordReset struct {
	Token    *PasswordResetToken
	RawToken string
}

const resetSelectCols = `id, user_id, token_hash, expires_at, created_at, used_at, revoked_at`

func ensurePasswordResetSchema(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS password_reset_tokens (
  id          TEXT PRIMARY KEY NOT NULL,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash  TEXT NOT NULL UNIQUE,
  expires_at  TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  used_at     TEXT,
  revoked_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_password_reset_user ON password_reset_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_password_reset_hash ON password_reset_tokens(token_hash);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("ensure password_reset_tokens schema: %w", err)
	}
	return nil
}

func hashResetToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func generateResetToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// RecoverWithCode sets a new password using one unused recovery code and returns the updated user.
// Timing: always runs RecoveryCodeCount recovery-hash compares (+ dummy password compare on failure paths).
func (s *UserStore) RecoverWithCode(ctx context.Context, email, recoveryCode, newPassword string) (*User, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, ErrNotFound
	}
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return nil, err
	}
	normCode := NormalizeRecoveryCode(recoveryCode)
	if normCode == "" {
		return nil, ErrRecoveryCodeInvalid
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

	failTiming := func(err error) (*User, error) {
		_ = padRecoverCompares(normCode, nil, nil)
		_ = CheckPassword(DummyPasswordHash, newPassword)
		return nil, err
	}

	u, err := scanUserRow(conn.QueryRowContext(ctx,
		`SELECT `+userSelectCols+` FROM users WHERE email = ?`, email,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return failTiming(ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	if u.IsDisabled() {
		return failTiming(ErrNotFound)
	}

	rows, err := conn.QueryContext(ctx,
		`SELECT id, code_hash FROM recovery_codes WHERE user_id = ? AND used_at IS NULL`,
		u.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("list recovery codes: %w", err)
	}
	var ids, hashes []string
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
		hashes = append(hashes, hash)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	matchedID := padRecoverCompares(normCode, ids, hashes)
	if matchedID == "" {
		_ = CheckPassword(DummyPasswordHash, newPassword)
		return nil, ErrRecoveryCodeInvalid
	}

	passHash, err := HashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := conn.ExecContext(ctx,
		`UPDATE recovery_codes SET used_at = ? WHERE id = ? AND used_at IS NULL`,
		now, matchedID,
	)
	if err != nil {
		return nil, fmt.Errorf("consume recovery code: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		_ = CheckPassword(DummyPasswordHash, newPassword)
		return nil, ErrRecoveryCodeInvalid
	}

	_, err = conn.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, password_version = password_version + 1, updated_at = ? WHERE id = ?`,
		passHash, now, u.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("set password: %w", err)
	}
	if _, err := conn.ExecContext(ctx,
		`UPDATE password_reset_tokens SET revoked_at = ? WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL`,
		now, u.ID,
	); err != nil {
		return nil, fmt.Errorf("revoke reset tokens: %w", err)
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	committed = true

	u.PasswordHash = passHash
	u.PasswordVersion++
	u.UpdatedAt = now
	return u, nil
}

// CreatePasswordResetToken mints a one-time reset token for userID.
func (s *UserStore) CreatePasswordResetToken(ctx context.Context, userID string, ttlSeconds int) (*CreatedPasswordReset, error) {
	if userID == "" {
		return nil, ErrNotFound
	}
	if ttlSeconds <= 0 {
		return nil, fmt.Errorf("ttlSeconds must be positive")
	}
	u, err := s.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.IsDisabled() {
		return nil, ErrNotFound
	}
	raw, err := generateResetToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tok := &PasswordResetToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		TokenHash: hashResetToken(raw),
		ExpiresAt: now.Add(time.Duration(ttlSeconds) * time.Second).Format(time.RFC3339),
		CreatedAt: now.Format(time.RFC3339),
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		tok.ID, tok.UserID, tok.TokenHash, tok.ExpiresAt, tok.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert password reset: %w", err)
	}
	return &CreatedPasswordReset{Token: tok, RawToken: raw}, nil
}

// GetPasswordResetToken validates a raw token for the GET trust page (not yet consumed).
func (s *UserStore) GetPasswordResetToken(ctx context.Context, rawToken string) (*PasswordResetToken, error) {
	return s.loadUsableReset(ctx, rawToken)
}

// CompletePasswordReset consumes the token and sets a new password (bumps pwd, keeps recovery codes).
func (s *UserStore) CompletePasswordReset(ctx context.Context, rawToken, newPassword string) (*User, error) {
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return nil, err
	}
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrResetNotFound
	}
	passHash, err := HashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	tokenHash := hashResetToken(rawToken)

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

	tok, err := scanResetRow(conn.QueryRowContext(ctx,
		`SELECT `+resetSelectCols+` FROM password_reset_tokens WHERE token_hash = ?`, tokenHash,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrResetNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := assertResetUsable(tok); err != nil {
		return nil, err
	}

	u, err := scanUserRow(conn.QueryRowContext(ctx,
		`SELECT `+userSelectCols+` FROM users WHERE id = ?`, tok.UserID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if u.IsDisabled() {
		return nil, ErrNotFound
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := conn.ExecContext(ctx,
		`UPDATE password_reset_tokens SET used_at = ? WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL`,
		now, tok.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("consume reset: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrResetUsed
	}

	_, err = conn.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, password_version = password_version + 1, updated_at = ? WHERE id = ?`,
		passHash, now, u.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("set password: %w", err)
	}
	// Revoke sibling outstanding resets for this user.
	if _, err := conn.ExecContext(ctx,
		`UPDATE password_reset_tokens SET revoked_at = ? WHERE user_id = ? AND id != ? AND used_at IS NULL AND revoked_at IS NULL`,
		now, u.ID, tok.ID,
	); err != nil {
		return nil, fmt.Errorf("revoke sibling resets: %w", err)
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	committed = true

	u.PasswordHash = passHash
	u.PasswordVersion++
	u.UpdatedAt = now
	return u, nil
}

// RevokePasswordResetTokensForUser marks outstanding resets revoked (admin set-password).
func (s *UserStore) RevokePasswordResetTokensForUser(ctx context.Context, userID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE password_reset_tokens SET revoked_at = ? WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL`,
		now, userID,
	)
	return err
}

func (s *UserStore) loadUsableReset(ctx context.Context, rawToken string) (*PasswordResetToken, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrResetNotFound
	}
	tok, err := scanResetRow(s.db.QueryRowContext(ctx,
		`SELECT `+resetSelectCols+` FROM password_reset_tokens WHERE token_hash = ?`,
		hashResetToken(rawToken),
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrResetNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := assertResetUsable(tok); err != nil {
		return nil, err
	}
	return tok, nil
}

func assertResetUsable(tok *PasswordResetToken) error {
	if tok.RevokedAt.Valid {
		return ErrResetRevoked
	}
	if tok.UsedAt.Valid {
		return ErrResetUsed
	}
	exp, err := time.Parse(time.RFC3339, tok.ExpiresAt)
	if err != nil {
		return fmt.Errorf("parse expires_at: %w", err)
	}
	if time.Now().UTC().After(exp) {
		return ErrResetExpired
	}
	return nil
}

type resetScanner interface {
	Scan(dest ...any) error
}

func scanResetRow(row resetScanner) (*PasswordResetToken, error) {
	var tok PasswordResetToken
	if err := row.Scan(
		&tok.ID, &tok.UserID, &tok.TokenHash, &tok.ExpiresAt, &tok.CreatedAt, &tok.UsedAt, &tok.RevokedAt,
	); err != nil {
		return nil, err
	}
	return &tok, nil
}

func scanUserRow(row inviteScanner) (*User, error) {
	var u User
	var isAdmin int
	err := row.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName,
		&isAdmin, &u.DisabledAt, &u.PasswordVersion, &u.QuotaBytes, &u.RecoveryCodesAckedAt,
		&u.RecoveryCodesGeneration, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	u.IsAdmin = isAdmin != 0
	return &u, nil
}

// ExpirePasswordResetForTest sets expires_at in the past.
func (s *UserStore) ExpirePasswordResetForTest(ctx context.Context, id string) error {
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `UPDATE password_reset_tokens SET expires_at = ? WHERE id = ?`, past, id)
	return err
}
