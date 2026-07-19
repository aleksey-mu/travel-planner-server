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

// Invite is a one-time join token row (raw token never stored).
type Invite struct {
	ID        string
	TokenHash string
	CreatedBy string
	Label     string
	ExpiresAt string
	MaxUses   int
	UseCount  int
	CreatedAt string
	RevokedAt sql.NullString
}

// CreatedInvite includes the plaintext token shown once at creation.
type CreatedInvite struct {
	Invite   *Invite
	RawToken string
}

// ErrInviteNotFound is returned when the token hash is unknown.
var ErrInviteNotFound = errors.New("invite not found")

// ErrInviteExpired is returned when expires_at is in the past.
var ErrInviteExpired = errors.New("invite expired")

// ErrInviteRevoked is returned when revoked_at is set.
var ErrInviteRevoked = errors.New("invite revoked")

// ErrInviteUsed is returned when use_count >= max_uses.
var ErrInviteUsed = errors.New("invite already used")

const inviteSelectCols = `id, token_hash, created_by, label, expires_at, max_uses, use_count, created_at, revoked_at`

func ensureInvitesSchema(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS invites (
  id          TEXT PRIMARY KEY NOT NULL,
  token_hash  TEXT NOT NULL UNIQUE,
  created_by  TEXT NOT NULL,
  label       TEXT,
  expires_at  TEXT NOT NULL,
  max_uses    INTEGER NOT NULL DEFAULT 1,
  use_count   INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL,
  revoked_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_invites_token_hash ON invites(token_hash);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("ensure invites schema: %w", err)
	}
	return nil
}

// HashInviteToken returns a hex SHA-256 of the raw invite token.
func HashInviteToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func generateInviteToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// CreateInvite mints a one-time invite. expiresInDays must be > 0.
func (s *UserStore) CreateInvite(ctx context.Context, createdBy, label string, expiresInDays int) (*CreatedInvite, error) {
	if createdBy == "" {
		return nil, fmt.Errorf("created_by is required")
	}
	if expiresInDays <= 0 {
		return nil, fmt.Errorf("expiresInDays must be positive")
	}
	raw, err := generateInviteToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	inv := &Invite{
		ID:        uuid.NewString(),
		TokenHash: HashInviteToken(raw),
		CreatedBy: createdBy,
		Label:     strings.TrimSpace(label),
		ExpiresAt: now.Add(time.Duration(expiresInDays) * 24 * time.Hour).Format(time.RFC3339),
		MaxUses:   1,
		UseCount:  0,
		CreatedAt: now.Format(time.RFC3339),
	}
	var labelArg any
	if inv.Label != "" {
		labelArg = inv.Label
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO invites (id, token_hash, created_by, label, expires_at, max_uses, use_count, created_at)
		 VALUES (?, ?, ?, ?, ?, 1, 0, ?)`,
		inv.ID, inv.TokenHash, inv.CreatedBy, labelArg, inv.ExpiresAt, inv.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert invite: %w", err)
	}
	return &CreatedInvite{Invite: inv, RawToken: raw}, nil
}

// GetInviteByID loads an invite by id.
func (s *UserStore) GetInviteByID(ctx context.Context, id string) (*Invite, error) {
	return s.scanInvite(ctx, `SELECT `+inviteSelectCols+` FROM invites WHERE id = ?`, id)
}

// ListInvites returns all invites newest first.
func (s *UserStore) ListInvites(ctx context.Context) ([]*Invite, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+inviteSelectCols+` FROM invites ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer rows.Close()
	var out []*Invite
	for rows.Next() {
		inv, err := scanInviteRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// RevokeInvite sets revoked_at if not already revoked.
func (s *UserStore) RevokeInvite(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE invites SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		inv, err := s.GetInviteByID(ctx, id)
		if err != nil {
			return err
		}
		if inv.RevokedAt.Valid {
			return nil // already revoked
		}
		return ErrInviteNotFound
	}
	return nil
}

// ValidateInviteToken returns the invite if usable for join step 1.
func (s *UserStore) ValidateInviteToken(ctx context.Context, rawToken string) (*Invite, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrInviteNotFound
	}
	inv, err := s.scanInvite(ctx,
		`SELECT `+inviteSelectCols+` FROM invites WHERE token_hash = ?`,
		HashInviteToken(rawToken),
	)
	if err != nil {
		return nil, err
	}
	if inv.RevokedAt.Valid {
		return nil, ErrInviteRevoked
	}
	if inv.UseCount >= inv.MaxUses {
		return nil, ErrInviteUsed
	}
	exp, err := time.Parse(time.RFC3339, inv.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("parse expires_at: %w", err)
	}
	if time.Now().UTC().After(exp) {
		return nil, ErrInviteExpired
	}
	return inv, nil
}

// ListEnabledAdminDisplayNames returns display names of enabled admins (no emails).
func (s *UserStore) ListEnabledAdminDisplayNames(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT display_name FROM users
		 WHERE is_admin = 1 AND disabled_at IS NULL
		 ORDER BY display_name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list admin names: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if names == nil {
		names = []string{}
	}
	return names, rows.Err()
}

// FirstEnabledAdminID returns the id of an enabled admin for CLI create-invite.
func (s *UserStore) FirstEnabledAdminID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM users WHERE is_admin = 1 AND disabled_at IS NULL ORDER BY created_at LIMIT 1`,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *UserStore) scanInvite(ctx context.Context, query string, arg any) (*Invite, error) {
	row := s.db.QueryRowContext(ctx, query, arg)
	inv, err := scanInviteRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInviteNotFound
	}
	return inv, err
}

type inviteScanner interface {
	Scan(dest ...any) error
}

func scanInviteRow(row inviteScanner) (*Invite, error) {
	var inv Invite
	var label sql.NullString
	if err := row.Scan(
		&inv.ID, &inv.TokenHash, &inv.CreatedBy, &label, &inv.ExpiresAt,
		&inv.MaxUses, &inv.UseCount, &inv.CreatedAt, &inv.RevokedAt,
	); err != nil {
		return nil, err
	}
	if label.Valid {
		inv.Label = label.String
	}
	return &inv, nil
}

// ExpireInviteForTest sets expires_at in the past (tests only).
func (s *UserStore) ExpireInviteForTest(ctx context.Context, id string) error {
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `UPDATE invites SET expires_at = ? WHERE id = ?`, past, id)
	return err
}

// MarkInviteUsedForTest increments use_count (tests only; S5 owns production consume).
func (s *UserStore) MarkInviteUsedForTest(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE invites SET use_count = use_count + 1 WHERE id = ?`, id)
	return err
}
