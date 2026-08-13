//go:build postgres

// Integration tests against a real PostgreSQL database.
//
// Required environment:
//
//	TEST_DATABASE_ADMIN_URL=postgres://...  superuser/admin URL for migration
//
// The test applies the reference migration to an isolated schema/database and
// runs the full bootstrap -> manage -> check -> purge lifecycle, then verifies
// RLS isolation with a non-bypass application role.
package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willunylabs/amsonia-core"
)

func adminURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("TEST_DATABASE_ADMIN_URL")
	if u == "" {
		t.Skip("TEST_DATABASE_ADMIN_URL not set")
	}
	return u
}

func newPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

const (
	testRuntimeRole     = "amsonia_test_runtime"
	testMaintenanceRole = "amsonia_test_maintenance"
	testRolePassword    = "test-only"
)

var (
	testRuntimeSecret     = []byte("runtime-test-binding-secret-material-0001")
	testMaintenanceSecret = []byte("maintenance-test-binding-secret-material-1")
)

func rolePool(t *testing.T, role string) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(adminURL(t))
	if err != nil {
		t.Fatalf("parse role pool: %v", err)
	}
	config.ConnConfig.User = role
	config.ConnConfig.Password = testRolePassword
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("role pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func configureTestRoles(t *testing.T, admin *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for _, statement := range []string{
		"DROP OWNED BY " + testRuntimeRole + " CASCADE",
		"DROP OWNED BY " + testMaintenanceRole + " CASCADE",
		"DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '" + testRuntimeRole + "') THEN CREATE ROLE " + testRuntimeRole + " LOGIN; END IF; END $$",
		"DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '" + testMaintenanceRole + "') THEN CREATE ROLE " + testMaintenanceRole + " LOGIN; END IF; END $$",
		"ALTER ROLE " + testRuntimeRole + " WITH LOGIN PASSWORD '" + testRolePassword + "' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT",
		"ALTER ROLE " + testMaintenanceRole + " WITH LOGIN PASSWORD '" + testRolePassword + "' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT",
	} {
		if _, err := admin.Exec(ctx, statement); err != nil && !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("configure roles %q: %v", statement, err)
		}
	}
	if _, err := admin.Exec(ctx, `
		GRANT USAGE ON SCHEMA amsonia TO amsonia_test_runtime, amsonia_test_maintenance;
		GRANT SELECT, INSERT, UPDATE, DELETE ON
			amsonia.roles, amsonia.role_permission_grants, amsonia.subject_roles,
			amsonia.grant_edges, amsonia.tenant_memberships, amsonia.member_invitations
		TO amsonia_test_runtime;
		GRANT SELECT, INSERT ON amsonia.role_versions, amsonia.audit_events TO amsonia_test_runtime;
		GRANT SELECT, INSERT, UPDATE ON
			amsonia.tenant_state, amsonia.accounts, amsonia.system_administrators,
			amsonia.access_sessions, amsonia.refresh_sessions
		TO amsonia_test_runtime;
		GRANT INSERT ON amsonia.tenants TO amsonia_test_runtime;
		GRANT SELECT ON amsonia.schema_migrations TO amsonia_test_runtime;
		GRANT USAGE, SELECT ON SEQUENCE amsonia.audit_events_id_seq TO amsonia_test_runtime;
		GRANT EXECUTE ON FUNCTION amsonia.tenant_id() TO amsonia_test_runtime;
		GRANT EXECUTE ON FUNCTION amsonia.bind_tenant(TEXT, BIGINT, TEXT, TEXT) TO amsonia_test_runtime;
		GRANT EXECUTE ON FUNCTION amsonia.tenant_visible(TEXT) TO amsonia_test_runtime;
		GRANT EXECUTE ON FUNCTION amsonia.actor_id() TO amsonia_test_runtime;
		GRANT EXECUTE ON FUNCTION amsonia.bind_actor(TEXT, BIGINT, TEXT, TEXT) TO amsonia_test_runtime;
		GRANT EXECUTE ON FUNCTION amsonia.list_account_tenants() TO amsonia_test_runtime;
		GRANT EXECUTE ON FUNCTION amsonia.resolve_invitation(BYTEA) TO amsonia_test_runtime;
		GRANT EXECUTE ON FUNCTION amsonia.activate_tenant() TO amsonia_test_runtime;
		GRANT EXECUTE ON FUNCTION amsonia.fail_created_tenant(TEXT) TO amsonia_test_runtime;
		GRANT SELECT, DELETE ON
			amsonia.roles, amsonia.role_permission_grants, amsonia.subject_roles,
			amsonia.grant_edges, amsonia.role_versions, amsonia.audit_events
		TO amsonia_test_maintenance;
		GRANT SELECT, INSERT, UPDATE ON amsonia.tenant_state TO amsonia_test_maintenance;
		GRANT SELECT, INSERT ON amsonia.purge_ledger TO amsonia_test_maintenance;
		GRANT EXECUTE ON FUNCTION amsonia.tenant_id() TO amsonia_test_maintenance;
		GRANT EXECUTE ON FUNCTION amsonia.bind_tenant(TEXT, BIGINT, TEXT, TEXT) TO amsonia_test_maintenance;
	`); err != nil {
		t.Fatalf("grant test roles: %v", err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO amsonia.runtime_secrets (role_name, secret, rotated_at)
		VALUES ($1, decode($2, 'hex'), now()), ($3, decode($4, 'hex'), now())
		ON CONFLICT (role_name) DO UPDATE SET secret = EXCLUDED.secret, rotated_at = EXCLUDED.rotated_at
	`, testRuntimeRole, hex.EncodeToString(testRuntimeSecret), testMaintenanceRole, hex.EncodeToString(testMaintenanceSecret)); err != nil {
		t.Fatalf("install role secrets: %v", err)
	}
}

func setupPostgres(t *testing.T) *Store {
	t.Helper()
	admin := newPool(t, adminURL(t))
	if err := Migrate(context.Background(), admin); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Isolate each run: reset all tenant data and the purge tombstone.
	if _, err := admin.Exec(context.Background(), `
		TRUNCATE amsonia.audit_events, amsonia.role_versions, amsonia.grant_edges,
			amsonia.subject_roles, amsonia.role_permission_grants, amsonia.roles,
			amsonia.tenant_state, amsonia.purge_ledger, amsonia.member_invitations,
			amsonia.tenant_memberships, amsonia.refresh_sessions, amsonia.access_sessions,
			amsonia.system_administrators, amsonia.tenants, amsonia.accounts
		RESTART IDENTITY
	`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	configureTestRoles(t, admin)
	store, err := NewStoreWithMaintenance(
		rolePool(t, testRuntimeRole), testRuntimeSecret,
		rolePool(t, testMaintenanceRole), testMaintenanceSecret,
	)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return store
}

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

func fixedTime() fixedClock { return fixedClock{t: time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)} }

type hostAuthorizer struct{}

func (hostAuthorizer) AuthorizeBootstrap(ctx context.Context, tenantID amsonia.TenantID) (amsonia.HostProvenance, error) {
	return amsonia.HostProvenance{Initiator: "host-provisioner", At: time.Now().UTC()}, nil
}

func (hostAuthorizer) AuthorizeMaintenance(ctx context.Context, tenantID amsonia.TenantID, operation string) (amsonia.HostProvenance, error) {
	return amsonia.HostProvenance{Initiator: "host-maintainer", At: time.Now().UTC()}, nil
}

type nullAudit struct{}

func (nullAudit) RecordSecurityEvent(ctx context.Context, event amsonia.MutationAuditEvent) error {
	return nil
}

type noMemberships struct{}

func (noMemberships) LookupWorkspaceMembership(ctx context.Context, tenantID amsonia.TenantID, workspaceID amsonia.WorkspaceID, subjectID amsonia.SubjectID) (amsonia.WorkspaceMembership, error) {
	return amsonia.WorkspaceMembership{}, amsonia.ErrNotFound
}

const (
	permInvoiceRead = amsonia.PermissionKey("billing:invoice:read")
	permRoleManage  = amsonia.PermissionKey("iam:role:manage")
	permGrantManage = amsonia.PermissionKey("iam:grant:manage")
	permRoleAssign  = amsonia.PermissionKey("iam:role:assign")
)

var testControls = amsonia.ControlPermissions{
	ManageRoles:  permRoleManage,
	ManageGrants: permGrantManage,
	AssignRoles:  permRoleAssign,
}

func testCatalog(t *testing.T) *amsonia.Catalog {
	t.Helper()
	cat, err := amsonia.NewCatalog([]amsonia.PermissionDefinition{
		{Key: permInvoiceRead},
		{Key: permRoleManage},
		{Key: permGrantManage},
		{Key: permRoleAssign},
	})
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func bootstrapInput(tenantID amsonia.TenantID, owner amsonia.SubjectID, roleID amsonia.RoleID) amsonia.BootstrapInput {
	return amsonia.BootstrapInput{
		TenantID:       tenantID,
		OwnerSubjectID: owner,
		OwnerRoleID:    roleID,
		OwnerRoleName:  "tenant-owner",
		Grants: []amsonia.RolePermissionGrant{
			{RoleID: roleID, Permission: permRoleManage, Scope: amsonia.ScopeTenant},
			{RoleID: roleID, Permission: permGrantManage, Scope: amsonia.ScopeTenant},
			{RoleID: roleID, Permission: permRoleAssign, Scope: amsonia.ScopeTenant},
			{RoleID: roleID, Permission: permInvoiceRead, Scope: amsonia.ScopeTenant},
		},
		Metadata: amsonia.MutationMetadata{ReasonCode: "tenant_provisioning"},
	}
}

func TestPostgresFullLifecycle(t *testing.T) {
	store := setupPostgres(t)
	cat := testCatalog(t)
	clock := fixedTime()

	mgr, err := amsonia.NewManager(cat, store, noMemberships{}, testControls, nullAudit{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	bs, err := amsonia.NewBootstrapper(cat, store, testControls, hostAuthorizer{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := amsonia.NewAuthorizer(cat, store, noMemberships{})
	if err != nil {
		t.Fatal(err)
	}
	maint, err := amsonia.NewMaintenance(store, hostAuthorizer{}, nullAudit{}, clock)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	tenantID := amsonia.TenantID("pg-tenant-1")
	owner := amsonia.Principal{TenantID: tenantID, SubjectID: "owner-1"}

	// Bootstrap.
	if _, err := bs.BootstrapTenant(ctx, bootstrapInput(tenantID, owner.SubjectID, "role-owner")); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// Owner can read invoices (tenant scope).
	dec, err := auth.Check(ctx, amsonia.CheckRequest{
		Principal:  owner,
		Permission: permInvoiceRead,
		Mode:       amsonia.ResourceExisting,
		Resource:   amsonia.ResourceContext{TenantID: tenantID, ResourceID: "inv-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatalf("owner should be allowed: %+v", dec)
	}

	// Create a role and grant it.
	role, _, err := mgr.CreateRole(ctx, owner, amsonia.MutationMetadata{ReasonCode: "setup"}, amsonia.CreateRoleInput{
		RoleID: "role-reader", Name: "reader",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.GrantPermission(ctx, owner, amsonia.MutationMetadata{ReasonCode: "setup"}, amsonia.GrantPermissionInput{
		RoleID: role.RoleID, ExpectedVersion: 1,
		Permission: permInvoiceRead, Scope: amsonia.ScopeTenant,
	}); err != nil {
		t.Fatal(err)
	}
	// Assign to alice.
	if _, err := mgr.AssignRole(ctx, owner, amsonia.MutationMetadata{ReasonCode: "setup"}, amsonia.AssignRoleInput{
		SubjectID: "alice", RoleID: role.RoleID, ExpectedRoleVersion: 2,
	}); err != nil {
		t.Fatal(err)
	}

	// Alice can read invoices.
	alice := amsonia.Principal{TenantID: tenantID, SubjectID: "alice"}
	dec, err = auth.Check(ctx, amsonia.CheckRequest{
		Principal:  alice,
		Permission: permInvoiceRead,
		Mode:       amsonia.ResourceExisting,
		Resource:   amsonia.ResourceContext{TenantID: tenantID, ResourceID: "inv-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatalf("alice should be allowed: %+v", dec)
	}

	// Alice cannot create roles (no role-manage grant).
	if _, _, err := mgr.CreateRole(ctx, alice, amsonia.MutationMetadata{ReasonCode: "setup"}, amsonia.CreateRoleInput{
		RoleID: "role-x", Name: "x",
	}); !errors.Is(err, amsonia.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}

	// Export.
	data, err := maint.ExportTenant(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"format": "amsonia.tenant.v1"`) {
		t.Fatalf("export missing format marker")
	}

	// Purge.
	if err := maint.PurgeTenant(ctx, tenantID, amsonia.MutationMetadata{
		RequestID: "purge-1", ReasonCode: "offboarding",
	}); err != nil {
		t.Fatal(err)
	}

	// Re-bootstrap of purged tenant fails (permanent tombstone).
	if _, err := bs.BootstrapTenant(ctx, bootstrapInput(tenantID, owner.SubjectID, "role-owner")); err == nil {
		t.Fatal("expected re-bootstrap after purge to fail")
	}

	// A different tenant can bootstrap normally.
	if _, err := bs.BootstrapTenant(ctx, bootstrapInput("pg-tenant-2", "owner-2", "role-owner")); err != nil {
		t.Fatalf("bootstrap tenant 2: %v", err)
	}

	// Tenant 1 state is gone: alice check now denies.
	dec, err = auth.Check(ctx, amsonia.CheckRequest{
		Principal:  alice,
		Permission: permInvoiceRead,
		Mode:       amsonia.ResourceExisting,
		Resource:   amsonia.ResourceContext{TenantID: tenantID, ResourceID: "inv-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatal("purged tenant must not allow access")
	}
}

func TestPostgresTenantIsolation(t *testing.T) {
	store := setupPostgres(t)
	cat := testCatalog(t)
	clock := fixedTime()

	bs, err := amsonia.NewBootstrapper(cat, store, testControls, hostAuthorizer{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := bs.BootstrapTenant(ctx, bootstrapInput("t-isol-a", "u-a", "role-owner")); err != nil {
		t.Fatal(err)
	}
	if _, err := bs.BootstrapTenant(ctx, bootstrapInput("t-isol-b", "u-b", "role-owner")); err != nil {
		t.Fatal(err)
	}

	// Same role IDs must coexist.
	// Insert an identical role in both tenants through the adapter.
	// (The bootstrap already used role-owner in both tenants.)
	if err := store.ReadTenant(ctx, "t-isol-a", func(r amsonia.TenantReader) error {
		_, err := r.GetRole(ctx, "role-owner")
		return err
	}); err != nil {
		t.Fatalf("tenant-a role lookup: %v", err)
	}
	if err := store.ReadTenant(ctx, "t-isol-b", func(r amsonia.TenantReader) error {
		_, err := r.GetRole(ctx, "role-owner")
		return err
	}); err != nil {
		t.Fatalf("tenant-b role lookup: %v", err)
	}

	// Raw SQL cannot manufacture a valid transaction binding. Setting the
	// GUCs directly therefore exposes no rows and permits no cross-tenant DML.
	conn, err := store.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT amsonia.set_tenant('t-isol-a')"); err == nil {
		t.Fatal("legacy unsigned tenant binder must not be executable")
	}
	if _, err := conn.Exec(ctx, `
		SELECT set_config('amsonia.tenant_id', 't-isol-a', false),
		       set_config('amsonia.tenant_txid', txid_current()::text, false),
		       set_config('amsonia.tenant_nonce', '00000000000000000000000000000000', false),
		       set_config('amsonia.tenant_signature', repeat('0', 64), false)
	`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM amsonia.roles").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("forged tenant binding exposed %d rows", count)
	}
	_, err = conn.Exec(ctx, "INSERT INTO amsonia.roles (tenant_id, role_id, name) VALUES ('t-isol-b', 'role-x', 'x')")
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("expected RLS violation on cross-tenant insert, got %v", err)
	}

	// Forging actor GUCs cannot turn the tenant discovery function into an
	// arbitrary account enumeration primitive.
	if _, err := conn.Exec(ctx, `
		SELECT set_config('amsonia.actor_id', 'victim-account', false),
		       set_config('amsonia.actor_txid', txid_current()::text, false),
		       set_config('amsonia.actor_nonce', '00000000000000000000000000000000', false),
		       set_config('amsonia.actor_signature', repeat('0', 64), false)
	`); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM amsonia.list_account_tenants()").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("forged actor binding exposed %d tenants", count)
	}
}

func TestPostgresConcurrentReciprocalGrantCycle(t *testing.T) {
	store := setupPostgres(t)
	cat := testCatalog(t)
	clock := fixedTime()

	mgr, err := amsonia.NewManager(cat, store, noMemberships{}, testControls, nullAudit{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	bs, err := amsonia.NewBootstrapper(cat, store, testControls, hostAuthorizer{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := bs.BootstrapTenant(ctx, bootstrapInput("t-cycle", "owner-1", "role-owner")); err != nil {
		t.Fatal(err)
	}
	owner := amsonia.Principal{TenantID: "t-cycle", SubjectID: "owner-1"}

	// Owner -> alice (alice gets full owner role).
	if _, err := mgr.AssignRole(ctx, owner, amsonia.MutationMetadata{ReasonCode: "setup"}, amsonia.AssignRoleInput{
		SubjectID: "alice", RoleID: "role-owner", ExpectedRoleVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	alice := amsonia.Principal{TenantID: "t-cycle", SubjectID: "alice"}

	// Alice assigning owner back to owner-1 must be rejected as a cycle.
	if _, err := mgr.AssignRole(ctx, alice, amsonia.MutationMetadata{ReasonCode: "setup"}, amsonia.AssignRoleInput{
		SubjectID: "owner-1", RoleID: "role-owner", ExpectedRoleVersion: 1,
	}); !errors.Is(err, amsonia.ErrGrantCycle) {
		t.Fatalf("expected ErrGrantCycle, got %v", err)
	}
}
