package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	RecoveryCodeCount      = 10
	recoveryCodeChars      = 16 // ~80 bits with 32-symbol alphabet
	recoveryBcryptCost     = 4 // codes are ~80-bit; cost 4 keeps accept/recover interactive
	recoveryAlphabet       = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no I/O/0/1
)

// ErrAckGenerationMismatch is returned when ack generation does not match the current codes.
var ErrAckGenerationMismatch = errors.New("recovery codes generation mismatch")

// ErrAckNoCodes is returned when ack is attempted with no recovery codes stored.
var ErrAckNoCodes = errors.New("no recovery codes to acknowledge")

func ensureRecoveryCodesSchema(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS recovery_codes (
  id         TEXT PRIMARY KEY NOT NULL,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash  TEXT NOT NULL,
  used_at    TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_recovery_codes_user ON recovery_codes(user_id);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("ensure recovery_codes schema: %w", err)
	}
	return nil
}

// NeedsRecoveryCodesAck reports whether the user must complete codes onboarding.
func (u *User) NeedsRecoveryCodesAck() bool {
	return !u.RecoveryCodesAckedAt.Valid || strings.TrimSpace(u.RecoveryCodesAckedAt.String) == ""
}

// NormalizeRecoveryCode uppercases and strips non-alphanumeric characters.
func NormalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// HashRecoveryCode returns a bcrypt hash of the normalized code.
func HashRecoveryCode(code string) (string, error) {
	norm := NormalizeRecoveryCode(code)
	if norm == "" {
		return "", fmt.Errorf("empty recovery code")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(norm), recoveryBcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash recovery code: %w", err)
	}
	return string(b), nil
}

// DummyRecoveryCodeHash equalizes recover timing when the account or code is missing.
var DummyRecoveryCodeHash = mustHashRecovery("timing-equalization-dummy-recovery-code")

func mustHashRecovery(code string) string {
	h, err := HashRecoveryCode(code)
	if err != nil {
		panic(err)
	}
	return h
}

// CheckRecoveryCode compares a bcrypt hash with a plaintext recovery code.
func CheckRecoveryCode(hash, code string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(NormalizeRecoveryCode(code))) == nil
}

// padRecoverCompares always runs RecoveryCodeCount bcrypt compares for timing.
// Returns the first real matching code id (empty if none).
func padRecoverCompares(normCode string, ids []string, hashes []string) string {
	for len(hashes) < RecoveryCodeCount {
		hashes = append(hashes, DummyRecoveryCodeHash)
		ids = append(ids, "")
	}
	if len(hashes) > RecoveryCodeCount {
		hashes = hashes[:RecoveryCodeCount]
		ids = ids[:RecoveryCodeCount]
	}
	var matchedID string
	for i := 0; i < RecoveryCodeCount; i++ {
		if CheckRecoveryCode(hashes[i], normCode) && ids[i] != "" && matchedID == "" {
			matchedID = ids[i]
		}
	}
	return matchedID
}

func generateRecoveryCodes(n int) ([]string, error) {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		full := make([]byte, recoveryCodeChars)
		if _, err := rand.Read(full); err != nil {
			return nil, err
		}
		chars := make([]byte, recoveryCodeChars)
		for j := 0; j < recoveryCodeChars; j++ {
			chars[j] = recoveryAlphabet[int(full[j])%len(recoveryAlphabet)]
		}
		// XXXX-XXXX-XXXX-XXXX
		var parts []string
		for j := 0; j < recoveryCodeChars; j += 4 {
			parts = append(parts, string(chars[j:j+4]))
		}
		out = append(out, strings.Join(parts, "-"))
	}
	return out, nil
}

func insertRecoveryCodesTx(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, userID string, codes []string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, code := range codes {
		hash, err := HashRecoveryCode(code)
		if err != nil {
			return err
		}
		_, err = exec.ExecContext(ctx,
			`INSERT INTO recovery_codes (id, user_id, code_hash, created_at) VALUES (?, ?, ?, ?)`,
			uuid.NewString(), userID, hash, now,
		)
		if err != nil {
			return fmt.Errorf("insert recovery code: %w", err)
		}
	}
	return nil
}

// AcceptInvite atomically validates the invite, creates the user, mints recovery codes, and consumes the invite.
// userID must be a pre-issued UUID so the caller can mint a JWT before commit.
// Invitees are never admin (even if the users table is empty).
func (s *UserStore) AcceptInvite(ctx context.Context, rawToken, email, password, displayName string, maxUsers int, userID string) (*User, []string, int, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, nil, 0, fmt.Errorf("email is required")
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return nil, nil, 0, ErrDisplayNameRequired
	}
	if err := ValidatePasswordStrength(password); err != nil {
		return nil, nil, 0, err
	}
	if !ValidUserID(userID) {
		return nil, nil, 0, fmt.Errorf("user id is required")
	}
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, nil, 0, ErrInviteNotFound
	}
	passHash, err := HashPassword(password)
	if err != nil {
		return nil, nil, 0, err
	}
	codes, err := generateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		return nil, nil, 0, err
	}
	tokenHash := HashInviteToken(rawToken)

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("db conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return nil, nil, 0, fmt.Errorf("begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()

	inv, err := scanInviteRow(conn.QueryRowContext(ctx,
		`SELECT `+inviteSelectCols+` FROM invites WHERE token_hash = ?`, tokenHash,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, 0, ErrInviteNotFound
	}
	if err != nil {
		return nil, nil, 0, err
	}
	if inv.RevokedAt.Valid {
		return nil, nil, 0, ErrInviteRevoked
	}
	if inv.UseCount >= inv.MaxUses {
		return nil, nil, 0, ErrInviteUsed
	}
	exp, err := time.Parse(time.RFC3339, inv.ExpiresAt)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("parse expires_at: %w", err)
	}
	if time.Now().UTC().After(exp) {
		return nil, nil, 0, ErrInviteExpired
	}

	var emailTaken int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = ?`, email).Scan(&emailTaken); err != nil {
		return nil, nil, 0, fmt.Errorf("check email: %w", err)
	}
	if emailTaken > 0 {
		return nil, nil, 0, ErrConflict
	}

	var n int64
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return nil, nil, 0, fmt.Errorf("count users: %w", err)
	}
	if maxUsers > 0 && n >= int64(maxUsers) {
		return nil, nil, 0, ErrUserLimit
	}

	generation := 1
	now := time.Now().UTC().Format(time.RFC3339)
	u := &User{
		ID:                      userID,
		Email:                   email,
		PasswordHash:            passHash,
		DisplayName:             displayName,
		IsAdmin:                 false,
		PasswordVersion:         0,
		RecoveryCodesGeneration: generation,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	_, err = conn.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, password_version, recovery_codes_generation, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 0, 0, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, u.DisplayName, generation, u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, nil, 0, ErrConflict
		}
		return nil, nil, 0, fmt.Errorf("insert user: %w", err)
	}

	if err := insertRecoveryCodesTx(ctx, conn, u.ID, codes); err != nil {
		return nil, nil, 0, err
	}

	res, err := conn.ExecContext(ctx,
		`UPDATE invites SET use_count = use_count + 1
		 WHERE id = ? AND use_count < max_uses AND revoked_at IS NULL`,
		inv.ID,
	)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("consume invite: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, nil, 0, err
	}
	if affected == 0 {
		return nil, nil, 0, ErrInviteUsed
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, nil, 0, fmt.Errorf("commit: %w", err)
	}
	committed = true
	return u, codes, generation, nil
}

// RegenerateRecoveryCodes replaces all codes, clears ack, bumps generation, returns plaintext once.
func (s *UserStore) RegenerateRecoveryCodes(ctx context.Context, userID string) ([]string, int, error) {
	if userID == "" {
		return nil, 0, ErrNotFound
	}
	codes, err := generateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		return nil, 0, err
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("db conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return nil, 0, fmt.Errorf("begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()

	var gen int
	err = conn.QueryRowContext(ctx,
		`SELECT recovery_codes_generation FROM users WHERE id = ?`, userID,
	).Scan(&gen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	gen++

	if _, err := conn.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return nil, 0, fmt.Errorf("delete recovery codes: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := conn.ExecContext(ctx,
		`UPDATE users SET recovery_codes_acked_at = NULL, recovery_codes_generation = ?, updated_at = ? WHERE id = ?`,
		gen, now, userID,
	); err != nil {
		return nil, 0, fmt.Errorf("clear ack: %w", err)
	}
	if err := insertRecoveryCodesTx(ctx, conn, userID, codes); err != nil {
		return nil, 0, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, 0, fmt.Errorf("commit: %w", err)
	}
	committed = true
	return codes, gen, nil
}

// AckRecoveryCodes sets recovery_codes_acked_at when generation matches and codes exist.
func (s *UserStore) AckRecoveryCodes(ctx context.Context, userID string, generation int) error {
	if generation <= 0 {
		return ErrAckGenerationMismatch
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("db conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()

	var curGen int
	err = conn.QueryRowContext(ctx,
		`SELECT recovery_codes_generation FROM users WHERE id = ?`, userID,
	).Scan(&curGen)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if curGen != generation {
		return ErrAckGenerationMismatch
	}

	var codeCount int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM recovery_codes WHERE user_id = ?`, userID,
	).Scan(&codeCount); err != nil {
		return err
	}
	if codeCount == 0 {
		return ErrAckNoCodes
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := conn.ExecContext(ctx,
		`UPDATE users SET recovery_codes_acked_at = ?, updated_at = ?
		 WHERE id = ? AND recovery_codes_generation = ?`,
		now, now, userID, generation,
	)
	if err != nil {
		return fmt.Errorf("ack recovery codes: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAckGenerationMismatch
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}
