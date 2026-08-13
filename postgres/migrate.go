package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.up.sql migrations/*.down.sql
var migrationFiles embed.FS

const migrationLockID int64 = 0x416d736f6e6961

// Migration describes one embedded, versioned database change.
type Migration struct {
	Version  int64
	Name     string
	UpSQL    string
	DownSQL  string
	Checksum string
}

// MigrationState is the durable migration runner state.
type MigrationState struct {
	Version   int64
	Name      string
	Checksum  string
	Dirty     bool
	AppliedAt time.Time
}

// VerifyMigrationState fails when the schema is missing, dirty, or behind the
// embedded migration set. The runtime role only needs SELECT on the version
// table to call it.
func VerifyMigrationState(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := EmbeddedMigrations()
	if err != nil {
		return err
	}
	states, err := MigrationStates(ctx, pool)
	if err != nil {
		return err
	}
	if len(states) != len(migrations) {
		return fmt.Errorf("amsonia/postgres: schema has %d migrations; binary requires %d", len(states), len(migrations))
	}
	for index, migration := range migrations {
		state := states[index]
		if state.Version != migration.Version || state.Name != migration.Name || state.Checksum != migration.Checksum || state.Dirty {
			return fmt.Errorf("amsonia/postgres: migration %06d state is incompatible or dirty", migration.Version)
		}
	}
	return nil
}

// EmbeddedMigrations returns the validated migrations shipped with this
// module in ascending version order.
func EmbeddedMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("amsonia/postgres: read migrations: %w", err)
	}
	byVersion := make(map[int64]*Migration)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		kind := ""
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			kind = "up"
		case strings.HasSuffix(name, ".down.sql"):
			kind = "down"
		default:
			continue
		}
		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 || len(parts[0]) != 6 {
			return nil, fmt.Errorf("amsonia/postgres: invalid migration filename %q", name)
		}
		version, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("amsonia/postgres: invalid migration version in %q", name)
		}
		migration := byVersion[version]
		if migration == nil {
			migration = &Migration{Version: version, Name: strings.TrimSuffix(strings.TrimSuffix(parts[1], ".up.sql"), ".down.sql")}
			byVersion[version] = migration
		}
		data, err := migrationFiles.ReadFile(filepath.ToSlash(filepath.Join("migrations", name)))
		if err != nil {
			return nil, fmt.Errorf("amsonia/postgres: read migration %q: %w", name, err)
		}
		if kind == "up" {
			if migration.UpSQL != "" {
				return nil, fmt.Errorf("amsonia/postgres: duplicate up migration %d", version)
			}
			migration.UpSQL = string(data)
		} else {
			if migration.DownSQL != "" {
				return nil, fmt.Errorf("amsonia/postgres: duplicate down migration %d", version)
			}
			migration.DownSQL = string(data)
		}
	}
	versions := make([]int64, 0, len(byVersion))
	for version := range byVersion {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	migrations := make([]Migration, 0, len(versions))
	for index, version := range versions {
		if version != int64(index+1) {
			return nil, fmt.Errorf("amsonia/postgres: migration sequence gap before %06d", version)
		}
		migration := *byVersion[version]
		if migration.UpSQL == "" || migration.DownSQL == "" {
			return nil, fmt.Errorf("amsonia/postgres: migration %06d requires up and down files", version)
		}
		digest := sha256.Sum256([]byte(migration.UpSQL))
		migration.Checksum = hex.EncodeToString(digest[:])
		migrations = append(migrations, migration)
	}
	return migrations, nil
}

// Migrate applies every pending embedded migration using an administrative
// pool. It serializes runners and records a dirty marker outside each DDL
// transaction so interrupted migrations cannot be mistaken for success.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("amsonia/postgres: nil migration pool")
	}
	migrations, err := EmbeddedMigrations()
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("amsonia/postgres: acquire migration connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("amsonia/postgres: acquire migration lock: %w", err)
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockID)
	if _, err := conn.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS amsonia;
		CREATE TABLE IF NOT EXISTS amsonia.schema_migrations (
			version BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT,
			dirty BOOLEAN NOT NULL DEFAULT TRUE,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		ALTER TABLE amsonia.schema_migrations ADD COLUMN IF NOT EXISTS checksum TEXT
	`); err != nil {
		return fmt.Errorf("amsonia/postgres: initialize migration table: %w", err)
	}
	var dirtyVersion int64
	err = conn.QueryRow(ctx, `
		SELECT version FROM amsonia.schema_migrations
		WHERE dirty ORDER BY version DESC LIMIT 1
	`).Scan(&dirtyVersion)
	if err == nil {
		return fmt.Errorf("amsonia/postgres: migration %06d is dirty; repair it before continuing", dirtyVersion)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("amsonia/postgres: inspect migration state: %w", err)
	}
	var futureVersion int64
	err = conn.QueryRow(ctx, `
		SELECT version FROM amsonia.schema_migrations
		WHERE version > $1 ORDER BY version LIMIT 1
	`, len(migrations)).Scan(&futureVersion)
	if err == nil {
		return fmt.Errorf("amsonia/postgres: database migration %06d is newer than this binary", futureVersion)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("amsonia/postgres: inspect future migration state: %w", err)
	}
	for _, migration := range migrations {
		var appliedName string
		var appliedChecksum *string
		err := conn.QueryRow(ctx, `
			SELECT name, checksum FROM amsonia.schema_migrations WHERE version = $1
		`, migration.Version).Scan(&appliedName, &appliedChecksum)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("amsonia/postgres: inspect migration %06d: %w", migration.Version, err)
		}
		if err == nil {
			if appliedName != migration.Name {
				return fmt.Errorf("amsonia/postgres: migration %06d name changed from %q to %q", migration.Version, appliedName, migration.Name)
			}
			if appliedChecksum != nil && *appliedChecksum != migration.Checksum {
				return fmt.Errorf("amsonia/postgres: migration %06d checksum mismatch", migration.Version)
			}
			// Databases created before checksum tracking receive a one-time
			// baseline. Every subsequent source change is rejected.
			if appliedChecksum == nil {
				if _, err := conn.Exec(ctx, `UPDATE amsonia.schema_migrations SET checksum = $1 WHERE version = $2 AND checksum IS NULL`, migration.Checksum, migration.Version); err != nil {
					return fmt.Errorf("amsonia/postgres: baseline migration %06d checksum: %w", migration.Version, err)
				}
			}
			continue
		}
		if _, err := conn.Exec(ctx, `
			INSERT INTO amsonia.schema_migrations (version, name, checksum, dirty)
			VALUES ($1, $2, $3, TRUE)
		`, migration.Version, migration.Name, migration.Checksum); err != nil {
			return fmt.Errorf("amsonia/postgres: mark migration %06d dirty: %w", migration.Version, err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("amsonia/postgres: begin migration %06d: %w", migration.Version, err)
		}
		if _, err := tx.Exec(ctx, migration.UpSQL); err != nil {
			_ = tx.Rollback(context.Background())
			return fmt.Errorf("amsonia/postgres: apply migration %06d: %w", migration.Version, err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE amsonia.schema_migrations
			SET dirty = FALSE, applied_at = now()
			WHERE version = $1
		`, migration.Version); err != nil {
			_ = tx.Rollback(context.Background())
			return fmt.Errorf("amsonia/postgres: finalize migration %06d: %w", migration.Version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("amsonia/postgres: commit migration %06d: %w", migration.Version, err)
		}
	}
	if _, err := conn.Exec(ctx, `ALTER TABLE amsonia.schema_migrations ALTER COLUMN checksum SET NOT NULL`); err != nil {
		return fmt.Errorf("amsonia/postgres: require migration checksums: %w", err)
	}
	return nil
}

// MigrationStates returns the durable version table for health checks and
// operator diagnostics.
func MigrationStates(ctx context.Context, pool *pgxpool.Pool) ([]MigrationState, error) {
	if pool == nil {
		return nil, errors.New("amsonia/postgres: nil migration pool")
	}
	rows, err := pool.Query(ctx, `
		SELECT version, name, checksum, dirty, applied_at
		FROM amsonia.schema_migrations ORDER BY version
	`)
	if err != nil {
		return nil, fmt.Errorf("amsonia/postgres: list migration state: %w", err)
	}
	defer rows.Close()
	states := make([]MigrationState, 0)
	for rows.Next() {
		var state MigrationState
		if err := rows.Scan(&state.Version, &state.Name, &state.Checksum, &state.Dirty, &state.AppliedAt); err != nil {
			return nil, fmt.Errorf("amsonia/postgres: scan migration state: %w", err)
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("amsonia/postgres: list migration state: %w", err)
	}
	return states, nil
}
