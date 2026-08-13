// Package postgres provides the production PostgreSQL adapter for Amsonia.
//
// Every tenant-owned query carries an explicit tenant predicate and runs
// inside a transaction that establishes a signed, transaction-local tenant
// binding used by the reference RLS policies. The application database role
// must not be a superuser, the table owner, or a BYPASSRLS role.
//
// See migrations/000001_amsonia_base.up.sql for the self-contained schema.
package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willunylabs/amsonia-core"
)

// Store is a PostgreSQL-backed Store. Create with NewStore.
type Store struct {
	pool                     *pgxpool.Pool
	bindingSecret            []byte
	maintenancePool          *pgxpool.Pool
	maintenanceBindingSecret []byte
}

// RunTenant executes one callback inside the signed tenant boundary. It is
// intended for Core-owned persistence that shares the same tenant RLS model.
func (s *Store) RunTenant(ctx context.Context, tenantID amsonia.TenantID, readOnly bool, fn func(pgx.Tx) error) error {
	if tenantID.Validate() != nil || fn == nil {
		return amsonia.ErrInvalidInput
	}
	options := pgx.TxOptions{IsoLevel: pgx.Serializable}
	if readOnly {
		options.AccessMode = pgx.ReadOnly
	}
	tx, err := s.pool.BeginTx(ctx, options)
	if err != nil {
		return fmtErr("begin", err)
	}
	defer tx.Rollback(context.Background())
	if err := bindTenant(ctx, tx, tenantID, s.bindingSecret); err != nil {
		return err
	}
	if !readOnly {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", string(tenantID)); err != nil {
			return fmtErr("advisory_lock", err)
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmtErr("commit", err)
	}
	return nil
}

// NewStore wraps a pgx pool. bindingSecret must be the same high-entropy value
// installed for the runtime database role in amsonia.runtime_secrets.
func NewStore(pool *pgxpool.Pool, bindingSecret []byte) (*Store, error) {
	if pool == nil || len(bindingSecret) < 32 {
		return nil, amsonia.ErrInvalidInput
	}
	secret := append([]byte(nil), bindingSecret...)
	return &Store{pool: pool, bindingSecret: secret}, nil
}

// NewStoreWithMaintenance constructs a Store whose destructive maintenance
// operations use a separately credentialed least-privileged database role.
func NewStoreWithMaintenance(
	pool *pgxpool.Pool,
	bindingSecret []byte,
	maintenancePool *pgxpool.Pool,
	maintenanceBindingSecret []byte,
) (*Store, error) {
	store, err := NewStore(pool, bindingSecret)
	if err != nil || maintenancePool == nil || len(maintenanceBindingSecret) < 32 {
		return nil, amsonia.ErrInvalidInput
	}
	store.maintenancePool = maintenancePool
	store.maintenanceBindingSecret = append([]byte(nil), maintenanceBindingSecret...)
	return store, nil
}

// Close releases the underlying pool.
func (s *Store) Close() {
	s.pool.Close()
	if s.maintenancePool != nil && s.maintenancePool != s.pool {
		s.maintenancePool.Close()
	}
}

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

func bindTenantSQL() string { return "SELECT amsonia.bind_tenant($1, $2, $3, $4)" }

func bindTenant(ctx context.Context, tx pgx.Tx, tenantID amsonia.TenantID, bindingSecret []byte) error {
	return bindContext(ctx, tx, "tenant", string(tenantID), bindingSecret)
}

func bindActor(ctx context.Context, tx pgx.Tx, accountID string, bindingSecret []byte) error {
	return bindContext(ctx, tx, "actor", accountID, bindingSecret)
}

func bindContext(ctx context.Context, tx pgx.Tx, purpose, value string, bindingSecret []byte) error {
	var txID int64
	if err := tx.QueryRow(ctx, "SELECT txid_current()").Scan(&txID); err != nil {
		return fmtErr("transaction_id", err)
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return fmtErr("tenant_nonce", err)
	}
	nonce := hex.EncodeToString(nonceBytes)
	var sessionUser string
	if err := tx.QueryRow(ctx, "SELECT session_user").Scan(&sessionUser); err != nil {
		return fmtErr("session_user", err)
	}
	payloadParts := []string{value, sessionUser, strconv.FormatInt(txID, 10), nonce}
	bindSQL := bindTenantSQL()
	if purpose == "actor" {
		payloadParts = append([]string{"actor"}, payloadParts...)
		bindSQL = "SELECT amsonia.bind_actor($1, $2, $3, $4)"
	}
	payload := strings.Join(payloadParts, "\n")
	mac := hmac.New(sha256.New, bindingSecret)
	_, _ = mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))
	if _, err := tx.Exec(ctx, bindSQL, value, txID, nonce, signature); err != nil {
		return fmtErr("bind_"+purpose, err)
	}
	return nil
}

// RunActor executes one read-only callback under a signed account binding.
// Discovery functions use this context rather than a caller-controlled ID.
func (s *Store) RunActor(ctx context.Context, accountID string, fn func(pgx.Tx) error) error {
	if accountID == "" || len(accountID) > 128 || strings.ContainsAny(accountID, "\r\n") || fn == nil {
		return amsonia.ErrInvalidInput
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmtErr("begin", err)
	}
	defer tx.Rollback(context.Background())
	if err := bindActor(ctx, tx, accountID, s.bindingSecret); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmtErr("commit", err)
	}
	return nil
}

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
	if tenantID.Validate() != nil || fn == nil {
		return amsonia.ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmtErr("begin", err)
	}
	defer tx.Rollback(context.Background())
	if err := bindTenant(ctx, tx, tenantID, s.bindingSecret); err != nil {
		return err
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
	if tenantID.Validate() != nil || fn == nil {
		return amsonia.ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmtErr("begin", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE"); err != nil {
		return fmtErr("isolation", err)
	}
	if err := bindTenant(ctx, tx, tenantID, s.bindingSecret); err != nil {
		return err
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
	if tenantID.Validate() != nil || fn == nil {
		return amsonia.ErrInvalidInput
	}
	pool := s.maintenancePool
	secret := s.maintenanceBindingSecret
	if pool == nil {
		return amsonia.ErrForbidden
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmtErr("begin", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE"); err != nil {
		return fmtErr("isolation", err)
	}
	if err := bindTenant(ctx, tx, tenantID, secret); err != nil {
		return err
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
