// Package postgres provides the production PostgreSQL adapter for Amsonia.
//
// Every tenant-owned query carries an explicit tenant predicate and runs
// inside a transaction that sets the transaction-local tenant context used by
// the reference RLS policies. The application database role must not be a
// superuser, the table owner, or a BYPASSRLS role.
//
// See migrations/000001_amsonia_base.up.sql for the self-contained schema.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willunylabs/amsonia"
)

// Store is a PostgreSQL-backed Store. Create with NewStore.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore wraps a pgx pool.
func NewStore(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, amsonia.ErrInvalidInput
	}
	return &Store{pool: pool}, nil
}

// Close releases the underlying pool.
func (s *Store) Close() { s.pool.Close() }

// ListEffectiveGrants implements amsonia.PolicyReader. Each call runs inside
// a fresh tenant-scoped transaction.
func (s *Store) ListEffectiveGrants(ctx context.Context, tenantID amsonia.TenantID, subjectID amsonia.SubjectID, permission amsonia.PermissionKey) ([]amsonia.EffectiveGrant, error) {
	var grants []amsonia.EffectiveGrant
	err := s.ReadTenant(ctx, tenantID, func(r amsonia.TenantReader) error {
		var err error
		grants, err = r.ListEffectiveGrants(ctx, subjectID, permission)
		return err
	})
	if err != nil {
		return nil, err
	}
	return grants, nil
}

func setTenantSQL() string { return "SELECT amsonia.set_tenant($1)" }

func wrapPgxErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return amsonia.ErrNotFound
	}
	if strings.Contains(err.Error(), "duplicate key") {
		return amsonia.ErrConflict
	}
	return err
}

func fmtErr(operation string, err error) error {
	return fmt.Errorf("amsonia/postgres: %s: %w", operation, err)
}

// ReadTenant runs fn under a read-only transaction scoped to one tenant.
func (s *Store) ReadTenant(ctx context.Context, tenantID amsonia.TenantID, fn func(amsonia.TenantReader) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmtErr("begin", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, setTenantSQL(), string(tenantID)); err != nil {
		return fmtErr("set_tenant", err)
	}
	reader := &tenantReader{tx: tx, tenantID: tenantID}
	if err := fn(reader); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// isPurged reports whether a tenant has a permanent purge tombstone.
func (s *Store) isPurged(ctx context.Context, tx pgx.Tx, tenantID amsonia.TenantID) (bool, error) {
	var purged bool
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(purged, FALSE) FROM amsonia.tenant_state WHERE tenant_id = $1
	`, string(tenantID)).Scan(&purged)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmtErr("tenant_state", err)
	}
	return purged, nil
}

// MutateTenant runs fn inside one serializable transaction. The PostgreSQL
// adapter uses serializable isolation plus a transaction-scoped per-tenant
// advisory lock, satisfying the Store mutation contract.
func (s *Store) MutateTenant(ctx context.Context, tenantID amsonia.TenantID, fn func(amsonia.TenantTx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmtErr("begin", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE"); err != nil {
		return fmtErr("isolation", err)
	}
	if _, err := tx.Exec(ctx, setTenantSQL(), string(tenantID)); err != nil {
		return fmtErr("set_tenant", err)
	}
	purged, err := s.isPurged(ctx, tx, tenantID)
	if err != nil {
		return err
	}
	if purged {
		return amsonia.ErrNotFound
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", string(tenantID)); err != nil {
		return fmtErr("advisory_lock", err)
	}
	txImpl := &tenantTx{tx: tx, tenantID: tenantID}
	if err := fn(txImpl); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmtErr("commit", err)
	}
	return nil
}

// MaintainTenant runs maintenance against the same serialized per-tenant
// boundary as normal mutations.
func (s *Store) MaintainTenant(ctx context.Context, tenantID amsonia.TenantID, fn func(amsonia.MaintenanceTx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmtErr("begin", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE"); err != nil {
		return fmtErr("isolation", err)
	}
	if _, err := tx.Exec(ctx, setTenantSQL(), string(tenantID)); err != nil {
		return fmtErr("set_tenant", err)
	}
	purged, err := s.isPurged(ctx, tx, tenantID)
	if err != nil {
		return err
	}
	if purged {
		return amsonia.ErrNotFound
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", string(tenantID)); err != nil {
		return fmtErr("advisory_lock", err)
	}
	if err := fn(&maintenanceTx{tx: tx, tenantID: tenantID}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmtErr("commit", err)
	}
	return nil
}
