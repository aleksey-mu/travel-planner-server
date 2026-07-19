package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrLastEnabledAdmin is returned when disable/demote would leave zero enabled admins.
var ErrLastEnabledAdmin = errors.New("cannot disable or demote the last enabled admin")

// ErrSoleAdminWithMembers is returned when sole enabled admin tries DELETE /me while others remain.
var ErrSoleAdminWithMembers = errors.New("sole admin cannot delete while members remain")

// ErrCannotDeleteSelf is returned when an admin tries DELETE /admin/users/{own-id}.
var ErrCannotDeleteSelf = errors.New("cannot delete self via admin API")

// ErrInvalidQuotaMode is returned for unknown quotaMode values.
var ErrInvalidQuotaMode = errors.New("invalid quota mode")

// DirectoryUser is a public member-directory entry (no email).
type DirectoryUser struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// AdminUserPatch is a partial update for PATCH /admin/users/{id}.
type AdminUserPatch struct {
	IsAdmin    *bool
	Disabled   *bool
	QuotaMode  *string // default | unlimited | personal
	QuotaBytes *int64  // required when quotaMode=personal
}

// ListAllUsers returns every account (incl. disabled) for admin UI.
func (s *UserStore) ListAllUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+userSelectCols+` FROM users ORDER BY display_name COLLATE NOCASE, email COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ListEnabledDirectory returns enabled users for GET /users (no emails).
func (s *UserStore) ListEnabledDirectory(ctx context.Context) ([]DirectoryUser, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, display_name FROM users
		 WHERE disabled_at IS NULL
		 ORDER BY display_name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list directory: %w", err)
	}
	defer rows.Close()
	out := make([]DirectoryUser, 0)
	for rows.Next() {
		var d DirectoryUser
		if err := rows.Scan(&d.ID, &d.DisplayName); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CountEnabledAdmins returns enabled admins (is_admin=1, disabled_at NULL).
func (s *UserStore) CountEnabledAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND disabled_at IS NULL`,
	).Scan(&n)
	return n, err
}

// ApplyAdminUserPatch applies isAdmin/disabled/quota with last-enabled-admin checks.
func (s *UserStore) ApplyAdminUserPatch(ctx context.Context, targetID string, patch AdminUserPatch) error {
	if targetID == "" {
		return ErrNotFound
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

	u, err := scanUserRow(conn.QueryRowContext(ctx,
		`SELECT `+userSelectCols+` FROM users WHERE id = ?`, targetID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	willDisable := patch.Disabled != nil && *patch.Disabled && !u.IsDisabled()
	willDemote := patch.IsAdmin != nil && !*patch.IsAdmin && u.IsAdmin
	if (willDisable || willDemote) && u.IsAdmin && !u.IsDisabled() {
		var n int64
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND disabled_at IS NULL`,
		).Scan(&n); err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastEnabledAdmin
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if patch.IsAdmin != nil {
		flag := 0
		if *patch.IsAdmin {
			flag = 1
		}
		if _, err := conn.ExecContext(ctx,
			`UPDATE users SET is_admin = ?, updated_at = ? WHERE id = ?`,
			flag, now, targetID,
		); err != nil {
			return fmt.Errorf("set admin: %w", err)
		}
	}
	if patch.Disabled != nil {
		if *patch.Disabled {
			if _, err := conn.ExecContext(ctx,
				`UPDATE users SET disabled_at = ?, updated_at = ? WHERE id = ?`,
				now, now, targetID,
			); err != nil {
				return fmt.Errorf("disable: %w", err)
			}
		} else {
			if _, err := conn.ExecContext(ctx,
				`UPDATE users SET disabled_at = NULL, updated_at = ? WHERE id = ?`,
				now, targetID,
			); err != nil {
				return fmt.Errorf("enable: %w", err)
			}
		}
	}
	if patch.QuotaMode != nil {
		qb, err := quotaBytesFromMode(*patch.QuotaMode, patch.QuotaBytes)
		if err != nil {
			return err
		}
		if qb == nil {
			if _, err := conn.ExecContext(ctx,
				`UPDATE users SET quota_bytes = NULL, updated_at = ? WHERE id = ?`,
				now, targetID,
			); err != nil {
				return fmt.Errorf("set quota: %w", err)
			}
		} else {
			if _, err := conn.ExecContext(ctx,
				`UPDATE users SET quota_bytes = ?, updated_at = ? WHERE id = ?`,
				*qb, now, targetID,
			); err != nil {
				return fmt.Errorf("set quota: %w", err)
			}
		}
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}

func quotaBytesFromMode(mode string, personal *int64) (*int64, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "default":
		return nil, nil
	case "unlimited":
		z := int64(0)
		return &z, nil
	case "personal":
		if personal == nil || *personal <= 0 {
			return nil, fmt.Errorf("%w: personal requires quotaBytes > 0", ErrInvalidQuotaMode)
		}
		v := *personal
		return &v, nil
	default:
		return nil, ErrInvalidQuotaMode
	}
}

// DeleteUserHard removes the user row and invites they created (FK cascades codes/resets).
// Refuses deleting the last enabled admin while other users remain.
func (s *UserStore) DeleteUserHard(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrNotFound
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

	u, err := scanUserRow(conn.QueryRowContext(ctx,
		`SELECT `+userSelectCols+` FROM users WHERE id = ?`, userID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	if u.IsAdmin && !u.IsDisabled() {
		var enabledAdmins, totalUsers int64
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND disabled_at IS NULL`,
		).Scan(&enabledAdmins); err != nil {
			return err
		}
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&totalUsers); err != nil {
			return err
		}
		if enabledAdmins <= 1 && totalUsers > 1 {
			return ErrLastEnabledAdmin
		}
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM invites WHERE created_by = ?`, userID); err != nil {
		return fmt.Errorf("delete invites: %w", err)
	}
	res, err := conn.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}

// DeleteSelf enforces sole-admin-with-members rules then hard-deletes.
func (s *UserStore) DeleteSelf(ctx context.Context, userID string) error {
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

	u, err := scanUserRow(conn.QueryRowContext(ctx,
		`SELECT `+userSelectCols+` FROM users WHERE id = ?`, userID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	if u.IsAdmin && !u.IsDisabled() {
		var enabledAdmins, totalUsers int64
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND disabled_at IS NULL`,
		).Scan(&enabledAdmins); err != nil {
			return err
		}
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&totalUsers); err != nil {
			return err
		}
		if enabledAdmins <= 1 && totalUsers > 1 {
			return ErrSoleAdminWithMembers
		}
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM invites WHERE created_by = ?`, userID); err != nil {
		return fmt.Errorf("delete invites: %w", err)
	}
	res, err := conn.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}

// DisableUserCLI disables by email with last-admin check.
func (s *UserStore) DisableUserCLI(ctx context.Context, email string) error {
	u, err := s.GetByEmail(ctx, email)
	if err != nil {
		return err
	}
	dis := true
	return s.ApplyAdminUserPatch(ctx, u.ID, AdminUserPatch{Disabled: &dis})
}

// EnableUserCLI clears disabled_at.
func (s *UserStore) EnableUserCLI(ctx context.Context, email string) error {
	u, err := s.GetByEmail(ctx, email)
	if err != nil {
		return err
	}
	dis := false
	return s.ApplyAdminUserPatch(ctx, u.ID, AdminUserPatch{Disabled: &dis})
}

// PromoteAdminCLI sets is_admin (OK if disabled).
func (s *UserStore) PromoteAdminCLI(ctx context.Context, email string) error {
	u, err := s.GetByEmail(ctx, email)
	if err != nil {
		return err
	}
	admin := true
	return s.ApplyAdminUserPatch(ctx, u.ID, AdminUserPatch{IsAdmin: &admin})
}

// DemoteAdminCLI clears is_admin with last-admin check.
func (s *UserStore) DemoteAdminCLI(ctx context.Context, email string) error {
	u, err := s.GetByEmail(ctx, email)
	if err != nil {
		return err
	}
	admin := false
	return s.ApplyAdminUserPatch(ctx, u.ID, AdminUserPatch{IsAdmin: &admin})
}

// SetQuotaCLI sets quota by mode string: default|unlimited|N (MiB).
func (s *UserStore) SetQuotaCLI(ctx context.Context, email, modeOrMB string) error {
	u, err := s.GetByEmail(ctx, email)
	if err != nil {
		return err
	}
	modeOrMB = strings.TrimSpace(strings.ToLower(modeOrMB))
	patch := AdminUserPatch{}
	switch modeOrMB {
	case "default":
		m := "default"
		patch.QuotaMode = &m
	case "unlimited":
		m := "unlimited"
		patch.QuotaMode = &m
	default:
		mb, err := strconv.ParseInt(modeOrMB, 10, 64)
		if err != nil || mb <= 0 {
			return fmt.Errorf("quota must be default, unlimited, or positive MiB")
		}
		const maxMiB = (1 << 62) / (1024 * 1024)
		if mb > maxMiB {
			return fmt.Errorf("quota MiB too large")
		}
		m := "personal"
		b := mb * 1024 * 1024
		patch.QuotaMode = &m
		patch.QuotaBytes = &b
	}
	return s.ApplyAdminUserPatch(ctx, u.ID, patch)
}
