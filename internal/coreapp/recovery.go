package coreapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/internal/security"
)

// RecoverAdministrator replaces an existing active administrator's password.
// It requires an operator database connection with recovery-ledger write access;
// it is not exposed through the application API.
func RecoverAdministrator(ctx context.Context, pool *pgxpool.Pool, email, password string) (string, error) {
	email, err := normalizeEmail(email)
	if err != nil || !validBootstrapPassword(password) {
		return "", errors.New("enter a valid administrator email and a password of 12–128 characters")
	}
	hash, err := security.NewPasswordHasher(64*1024, 3, 2, 32).Hash(password)
	if err != nil {
		return "", fmt.Errorf("hash replacement password: %w", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())
	var id string
	err = tx.QueryRow(ctx, `SELECT a.account_id FROM amsonia.accounts a
		JOIN amsonia.system_administrators sa USING (account_id)
		WHERE a.normalized_email = $1 AND a.status = 'active' FOR UPDATE OF a, sa`, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", amsonia.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE amsonia.accounts
		SET password_hash=$1, failed_login_count=0, locked_until=NULL, updated_at=now()
		WHERE account_id=$2`, hash, id); err != nil {
		return "", err
	}
	if err := revokeCredentialSessions(ctx, tx, id); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO amsonia.administrator_recoveries (account_id) VALUES ($1)`, id); err != nil {
		return "", fmt.Errorf("record operator recovery: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}
