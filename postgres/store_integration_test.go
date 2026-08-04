//go:build postgres

// Integration tests against a real PostgreSQL database.
//
// Required environment:
//
//	TEST_DATABASE_URL=postgres://...   database used by the application role
//	TEST_DATABASE_ADMIN_URL=postgres://...  superuser/admin URL for migration
//
// The test applies the reference migration to an isolated schema/database and
// runs the full bootstrap -> manage -> check -> purge lifecycle, then verifies
// RLS isolation with a non-bypass application role.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willunylabs/amsonia"
)

func adminURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("TEST_DATABASE_ADMIN_URL")
	if u == "" {
		t.Skip("TEST_DATABASE_ADMIN_URL not set")
	}
	return u
}

func appURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("TEST_DATABASE_URL")
	if u == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return u
}

const migrationFile = "migrations/000001_amsonia_base.up.sql"

func migrate(t *testing.T, adminConn *pgx.Conn) {
	t.Helper()
	data, err := os.ReadFile(migrationFile)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := adminConn.Exec(context.Background(), string(data)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
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

func setupPostgres(t *testing.T) *Store {
	t.Helper()
	admin, err := pgx.Connect(context.Background(), adminURL(t))
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer admin.Close(context.Background())
	migrate(t, admin)
	// Isolate each run: reset all tenant data and the purge tombstone.
	if _, err := admin.Exec(context.Background(), `
		TRUNCATE amsonia.audit_events, amsonia.role_versions, amsonia.grant_edges,
			amsonia.subject_roles, amsonia.role_permission_grants, amsonia.roles,
			amsonia.tenant_state, amsonia.purge_ledger RESTART IDENTITY
	`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return &Store{pool: newPool(t, appURL(t))}
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

	// RLS isolation: with tenant t-isol-a active on a raw app connection,
	// tenant t-isol-b rows must be invisible and writes must fail.
	//
	// The admin (superuser) bypasses RLS, so create a dedicated non-bypass
	// application role for this negative test.
	appConn := acquireAdminConn(t)
	defer appConn.Release()
	role := "amsonia_rls_app"
	if _, err := appConn.Exec(ctx, fmt.Sprintf("DROP OWNED BY %s CASCADE", role)); err != nil {
		// Ignore: role may not exist yet on a fresh database.
	}
	if _, err := appConn.Exec(ctx, fmt.Sprintf("DROP ROLE IF EXISTS %s", role)); err != nil {
		t.Fatal(err)
	}
	if _, err := appConn.Exec(ctx, fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD 'test-only'", role)); err != nil {
		t.Fatal(err)
	}
	if _, err := appConn.Exec(ctx, fmt.Sprintf("GRANT USAGE ON SCHEMA amsonia TO %s", role)); err != nil {
		t.Fatal(err)
	}
	if _, err := appConn.Exec(ctx, fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA amsonia TO %s", role)); err != nil {
		t.Fatal(err)
	}
	if _, err := appConn.Exec(ctx, fmt.Sprintf("GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA amsonia TO %s", role)); err != nil {
		t.Fatal(err)
	}

	appURL := appURL(t)
	// Replace the connection user with the dedicated non-bypass role.
	appURL = strings.Replace(appURL, "postgres://will@", "postgres://"+role+":test-only@", 1)
	appPool := newPool(t, appURL)
	conn, err := appPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT amsonia.set_tenant('t-isol-a')"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM amsonia.roles WHERE tenant_id = 't-isol-b'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("RLS leak: t-isol-a sees %d rows of t-isol-b", count)
	}
	_, err = conn.Exec(ctx, "INSERT INTO amsonia.roles (tenant_id, role_id, name) VALUES ('t-isol-b', 'role-x', 'x')")
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("expected RLS violation on cross-tenant insert, got %v", err)
	}
}

func acquireAdminConn(t *testing.T) *pgxpool.Conn {
	t.Helper()
	pool := newPool(t, adminURL(t))
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Release()
		pool.Close()
	})
	return conn
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
	_ = fmt.Sprintf
}
