package coreapp

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/willunylabs/amsonia-core"
)

// Profile is the authenticated account's editable, non-credential metadata.
type Profile struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

// AccountSession describes one device family, without exposing token hashes.
type AccountSession struct {
	ID            string    `json:"id"`
	CreatedAt     time.Time `json:"created_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	RemoteAddress string    `json:"remote_address"`
	UserAgent     string    `json:"user_agent"`
	Current       bool      `json:"current"`
}

func (s *Service) Profile(ctx context.Context, accountID string) (Profile, error) {
	var profile Profile
	err := s.pool.QueryRow(ctx, `SELECT account_id, email, display_name, created_at FROM amsonia.accounts WHERE account_id = $1 AND status = 'active'`, accountID).
		Scan(&profile.ID, &profile.Email, &profile.DisplayName, &profile.CreatedAt)
	return profile, err
}

func (s *Service) UpdateProfile(ctx context.Context, accountID, name string) (Profile, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 || strings.ContainsFunc(name, func(r rune) bool { return r < 32 || r == 127 }) {
		return Profile{}, amsonia.ErrInvalidInput
	}
	var profile Profile
	err := s.pool.QueryRow(ctx, `UPDATE amsonia.accounts SET display_name = $1, updated_at = $2 WHERE account_id = $3 AND status = 'active' RETURNING account_id, email, display_name, created_at`, name, s.now(), accountID).
		Scan(&profile.ID, &profile.Email, &profile.DisplayName, &profile.CreatedAt)
	return profile, err
}

// ChangePassword verifies the current password and revokes all device families
// atomically. The caller signs in again with the new password after success.
func (s *Service) ChangePassword(ctx context.Context, accountID, current, next string) error {
	if !validBootstrapPassword(next) || current == next {
		return amsonia.ErrInvalidInput
	}
	hash, err := s.passwords.Hash(next)
	if err != nil {
		return amsonia.ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var previous string
	var locked *time.Time
	if err := tx.QueryRow(ctx, `SELECT password_hash, locked_until FROM amsonia.accounts WHERE account_id = $1 AND status = 'active' FOR UPDATE`, accountID).Scan(&previous, &locked); err != nil {
		return err
	}
	if locked != nil && locked.After(s.now()) {
		return ErrAccountLocked
	}
	if !s.passwords.Verify(current, previous) {
		if err := s.failedPassword(ctx, tx, accountID); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return ErrInvalidCredentials
	}
	if _, err := tx.Exec(ctx, `UPDATE amsonia.accounts SET password_hash = $1, failed_login_count = 0, locked_until = NULL, updated_at = $2 WHERE account_id = $3`, hash, s.now(), accountID); err != nil {
		return err
	}
	if err := revokeCredentialSessions(ctx, tx, accountID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Credential changes end the old refresh lineage as well as revoking sessions.
// A pre-change rotated token must not revoke sessions using the new password.
func revokeCredentialSessions(ctx context.Context, tx pgx.Tx, accountID string) error {
	if _, err := tx.Exec(ctx, `UPDATE amsonia.access_sessions
		SET revoked_at=now() WHERE account_id=$1 AND revoked_at IS NULL`, accountID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE amsonia.refresh_sessions
		SET revoked_at=COALESCE(revoked_at, now()), rotated_to_hash=NULL
		WHERE account_id=$1 AND (revoked_at IS NULL OR rotated_to_hash IS NOT NULL)`, accountID)
	return err
}

func (s *Service) failedPassword(ctx context.Context, tx pgx.Tx, accountID string) error {
	_, err := tx.Exec(ctx, `UPDATE amsonia.accounts SET
	    failed_login_count = CASE WHEN failed_login_count + 1 >= 5 THEN 0 ELSE failed_login_count + 1 END,
	    locked_until = CASE WHEN failed_login_count + 1 >= 5 THEN $1 ELSE locked_until END,
	    updated_at = $2 WHERE account_id = $3`, s.now().Add(15*time.Minute), s.now(), accountID)
	return err
}

func (s *Service) ListSessions(ctx context.Context, accountID, accessToken string) ([]AccountSession, error) {
	rows, err := s.pool.Query(ctx, `
	    SELECT session_id, min(created_at), max(last_seen_at), max(expires_at), max(remote_address), max(user_agent), bool_or(current_session)
	    FROM (
	      SELECT session_id, created_at, last_seen_at, expires_at, remote_address, user_agent, token_hash = $2 AS current_session
	      FROM amsonia.access_sessions WHERE account_id = $1 AND revoked_at IS NULL AND expires_at > $3
	      UNION ALL
	      SELECT session_id, created_at, created_at, expires_at, remote_address, user_agent, false
	      FROM amsonia.refresh_sessions WHERE account_id = $1 AND revoked_at IS NULL AND expires_at > $3
	    ) active GROUP BY session_id ORDER BY min(created_at) DESC, session_id`, accountID, tokenHash(accessToken), s.now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AccountSession, 0)
	for rows.Next() {
		var item AccountSession
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.LastSeenAt, &item.ExpiresAt, &item.RemoteAddress, &item.UserAgent, &item.Current); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) RevokeSession(ctx context.Context, accountID, sessionID string) error {
	if amsonia.SubjectID(sessionID).Validate() != nil || len(sessionID) > 128 {
		return amsonia.ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var lockedID string
	if err := tx.QueryRow(ctx, `SELECT account_id FROM amsonia.accounts WHERE account_id = $1 FOR UPDATE`, accountID).Scan(&lockedID); err != nil {
		return accountNotFound(err)
	}
	var found bool
	for _, table := range []string{"access_sessions", "refresh_sessions"} {
		tag, err := tx.Exec(ctx, `UPDATE amsonia.`+table+` SET revoked_at = COALESCE(revoked_at, $1) WHERE account_id = $2 AND session_id = $3`, s.now(), accountID, sessionID)
		if err != nil {
			return err
		}
		found = found || tag.RowsAffected() > 0
	}
	if !found {
		return amsonia.ErrNotFound
	}
	return tx.Commit(ctx)
}

// The account lock serializes with login, rotation, password changes and device
// revocation. Recheck the bearer after acquiring it so a concurrently revoked
// caller cannot retain a different family or act using stale authentication.
func (s *Service) revokeOtherSessions(ctx context.Context, accountID, accessToken string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var lockedID string
	if err := tx.QueryRow(ctx, `SELECT account_id FROM amsonia.accounts WHERE account_id=$1 AND status='active' FOR UPDATE`, accountID).Scan(&lockedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSessionInvalid
		}
		return err
	}
	now := s.now()
	var currentID string
	if err := tx.QueryRow(ctx, `SELECT session_id FROM amsonia.access_sessions WHERE account_id=$1 AND token_hash=$2 AND revoked_at IS NULL AND expires_at>$3`, accountID, tokenHash(accessToken), now).Scan(&currentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSessionInvalid
		}
		return err
	}
	for _, table := range []string{"access_sessions", "refresh_sessions"} {
		if _, err := tx.Exec(ctx, `UPDATE amsonia.`+table+` SET revoked_at=$1 WHERE account_id=$2 AND session_id<>$3 AND revoked_at IS NULL`, now, accountID, currentID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func accountNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return amsonia.ErrNotFound
	}
	return err
}
