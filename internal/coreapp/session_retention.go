package coreapp

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willunylabs/amsonia-core"
)

// SessionPruneOptions bounds operator-only cleanup. Even revoked records stay
// until expiry plus retention, preserving the refresh replay detection window.
type SessionPruneOptions struct {
	Apply         bool
	RetentionDays int
	BatchSize     int
}

type SessionPruneResult struct {
	Applied            bool      `json:"applied"`
	Cutoff             time.Time `json:"cutoff"`
	BatchSize          int       `json:"batch_size"`
	AccessSessions     int64     `json:"access_sessions"`
	RefreshSessions    int64     `json:"refresh_sessions"`
	PasswordRecoveries int64     `json:"password_recoveries"`
}

func (o SessionPruneOptions) Validate() error {
	if o.RetentionDays < 1 || o.RetentionDays > 3650 || o.BatchSize < 1 || o.BatchSize > 5000 {
		return fmt.Errorf("retention-days must be 1–3650 and batch-size must be 1–5000: %w", amsonia.ErrInvalidInput)
	}
	return nil
}

// PruneSessions uses the database clock and one bounded batch per table. It
// requires an operator connection; the runtime and maintenance roles cannot
// delete global credentials. Dry runs report eligible rows capped by BatchSize.
func PruneSessions(ctx context.Context, pool *pgxpool.Pool, options SessionPruneOptions) (SessionPruneResult, error) {
	if err := options.Validate(); err != nil {
		return SessionPruneResult{}, err
	}
	result := SessionPruneResult{Applied: options.Apply, BatchSize: options.BatchSize}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	if err := tx.QueryRow(ctx, `SELECT now()-make_interval(days=>$1)`, options.RetentionDays).Scan(&result.Cutoff); err != nil {
		return result, err
	}
	for _, table := range []struct {
		name, key string
		count     *int64
	}{
		{"access_sessions", "token_hash", &result.AccessSessions},
		{"refresh_sessions", "token_hash", &result.RefreshSessions},
		{"member_password_recoveries", "recovery_id", &result.PasswordRecoveries},
	} {
		// Identifiers come exclusively from the fixed list above, never CLI input.
		selection := `SELECT ` + table.key + ` FROM amsonia.` + table.name + ` WHERE expires_at<$1 ORDER BY expires_at,` + table.key + ` LIMIT $2`
		if options.Apply {
			tag, err := tx.Exec(ctx, `WITH expired AS (`+selection+` FOR UPDATE SKIP LOCKED) DELETE FROM amsonia.`+table.name+` s USING expired e WHERE s.`+table.key+`=e.`+table.key, result.Cutoff, options.BatchSize)
			if err != nil {
				return result, err
			}
			*table.count = tag.RowsAffected()
		} else {
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM (`+selection+`) eligible`, result.Cutoff, options.BatchSize).Scan(table.count); err != nil {
				return result, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	return result, nil
}
