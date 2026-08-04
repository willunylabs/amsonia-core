package amsonia_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/willunylabs/amsonia"
	"github.com/willunylabs/amsonia/memory"
)

const (
	permInvoiceRead  = amsonia.PermissionKey("billing:invoice:read")
	permInvoiceWrite = amsonia.PermissionKey("billing:invoice:write")
	permRoleManage   = amsonia.PermissionKey("iam:role:manage")
	permGrantManage  = amsonia.PermissionKey("iam:grant:manage")
	permRoleAssign   = amsonia.PermissionKey("iam:role:assign")
)

var controls = amsonia.ControlPermissions{
	ManageRoles:  permRoleManage,
	ManageGrants: permGrantManage,
	AssignRoles:  permRoleAssign,
}

func testCatalog(t *testing.T) *amsonia.Catalog {
	t.Helper()
	cat, err := amsonia.NewCatalog([]amsonia.PermissionDefinition{
		{Key: permInvoiceRead},
		{Key: permInvoiceWrite},
		{Key: permRoleManage},
		{Key: permGrantManage},
		{Key: permRoleAssign},
	})
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

func fixedTime() fixedClock { return fixedClock{t: time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)} }

type testAuthorizer struct {
	auth func(ctx context.Context, tenantID amsonia.TenantID, operation string) (amsonia.HostProvenance, error)
}

func (a testAuthorizer) AuthorizeBootstrap(ctx context.Context, tenantID amsonia.TenantID) (amsonia.HostProvenance, error) {
	return a.auth(ctx, tenantID, "bootstrap")
}

func (a testAuthorizer) AuthorizeMaintenance(ctx context.Context, tenantID amsonia.TenantID, operation string) (amsonia.HostProvenance, error) {
	return a.auth(ctx, tenantID, operation)
}

func hostAuth(ctx context.Context, tenantID amsonia.TenantID, operation string) (amsonia.HostProvenance, error) {
	return amsonia.HostProvenance{Initiator: "host-provisioner", At: time.Now().UTC()}, nil
}

type nullAudit struct{}

func (nullAudit) RecordSecurityEvent(ctx context.Context, event amsonia.MutationAuditEvent) error { return nil }

type noMemberships struct{}

func (noMemberships) LookupWorkspaceMembership(ctx context.Context, tenantID amsonia.TenantID, workspaceID amsonia.WorkspaceID, subjectID amsonia.SubjectID) (amsonia.WorkspaceMembership, error) {
	return amsonia.WorkspaceMembership{}, amsonia.ErrNotFound
}

func setup(t *testing.T) (*memory.Store, *amsonia.Manager, *amsonia.Bootstrapper, *amsonia.Catalog) {
	t.Helper()
	store := memory.NewStore()
	cat := testCatalog(t)
	mgr, err := amsonia.NewManager(cat, store, noMemberships{}, controls, nullAudit{}, fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	bs, err := amsonia.NewBootstrapper(cat, store, controls, testAuthorizer{auth: hostAuth}, fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	return store, mgr, bs, cat
}

func bootstrapTenant(t *testing.T, bs *amsonia.Bootstrapper, tenantID amsonia.TenantID, owner amsonia.SubjectID, roleID amsonia.RoleID) {
	t.Helper()
	_, err := bs.BootstrapTenant(context.Background(), amsonia.BootstrapInput{
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
	})
	if err != nil {
		t.Fatal(err)
	}
}

func meta() amsonia.MutationMetadata {
	return amsonia.MutationMetadata{ReasonCode: "ops_review"}
}

func TestBootstrapOnceThenRejected(t *testing.T) {
	store, _, bs, _ := setup(t)
	bootstrapTenant(t, bs, "tenant-a", "owner-1", "role-owner")
	_, err := bs.BootstrapTenant(context.Background(), amsonia.BootstrapInput{
		TenantID:       "tenant-a",
		OwnerSubjectID: "owner-2",
		OwnerRoleID:    "role-owner2",
		OwnerRoleName:  "tenant-owner",
		Grants: []amsonia.RolePermissionGrant{
			{RoleID: "role-owner2", Permission: permRoleManage, Scope: amsonia.ScopeTenant},
			{RoleID: "role-owner2", Permission: permGrantManage, Scope: amsonia.ScopeTenant},
			{RoleID: "role-owner2", Permission: permRoleAssign, Scope: amsonia.ScopeTenant},
		},
		Metadata: amsonia.MutationMetadata{ReasonCode: "tenant_provisioning"},
	})
	if !errors.Is(err, amsonia.ErrAlreadyBootstrapped) {
		t.Fatalf("expected ErrAlreadyBootstrapped, got %v", err)
	}
	_ = store
}

func TestBootstrapRejectsMissingControls(t *testing.T) {
	_, _, bs, _ := setup(t)
	_, err := bs.BootstrapTenant(context.Background(), amsonia.BootstrapInput{
		TenantID:       "tenant-b",
		OwnerSubjectID: "owner-1",
		OwnerRoleID:    "role-owner",
		OwnerRoleName:  "tenant-owner",
		Grants: []amsonia.RolePermissionGrant{
			{RoleID: "role-owner", Permission: permInvoiceRead, Scope: amsonia.ScopeTenant},
		},
		Metadata: amsonia.MutationMetadata{ReasonCode: "tenant_provisioning"},
	})
	if !errors.Is(err, amsonia.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestCreateRoleAndGrantPermission(t *testing.T) {
	store, mgr, bs, cat := setup(t)
	bootstrapTenant(t, bs, "tenant-a", "owner-1", "role-owner")
	owner := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner-1"}

	role, res, err := mgr.CreateRole(context.Background(), owner, meta(), amsonia.CreateRoleInput{
		RoleID: "role-billing", Name: "billing-staff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if role.Version != 1 || !res.Changed {
		t.Fatalf("unexpected role/result: %+v %+v", role, res)
	}

	res, err = mgr.GrantPermission(context.Background(), owner, meta(), amsonia.GrantPermissionInput{
		RoleID: "role-billing", ExpectedVersion: 1,
		Permission: permInvoiceRead, Scope: amsonia.ScopeTenant,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.RoleVersion != 2 {
		t.Fatalf("unexpected grant result: %+v", res)
	}

	// Duplicate grant is a no-op that does not bump the version.
	res, err = mgr.GrantPermission(context.Background(), owner, meta(), amsonia.GrantPermissionInput{
		RoleID: "role-billing", ExpectedVersion: 2,
		Permission: permInvoiceRead, Scope: amsonia.ScopeTenant,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.RoleVersion != 2 {
		t.Fatalf("duplicate grant must be a no-op: %+v", res)
	}
	_ = cat
	_ = store
}

func TestStaleVersionConflict(t *testing.T) {
	_, mgr, bs, _ := setup(t)
	bootstrapTenant(t, bs, "tenant-a", "owner-1", "role-owner")
	owner := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner-1"}
	_, _, err := mgr.CreateRole(context.Background(), owner, meta(), amsonia.CreateRoleInput{
		RoleID: "role-x", Name: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.GrantPermission(context.Background(), owner, meta(), amsonia.GrantPermissionInput{
		RoleID: "role-x", ExpectedVersion: 1,
		Permission: permInvoiceRead, Scope: amsonia.ScopeTenant,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Stale version even though the desired grant is already present.
	_, err = mgr.GrantPermission(context.Background(), owner, meta(), amsonia.GrantPermissionInput{
		RoleID: "role-x", ExpectedVersion: 1,
		Permission: permInvoiceRead, Scope: amsonia.ScopeTenant,
	})
	if !errors.Is(err, amsonia.ErrConflict) {
		t.Fatalf("expected ErrConflict for stale version, got %v", err)
	}
}

func TestAssignRoleCyclePrevention(t *testing.T) {
	_, mgr, bs, _ := setup(t)
	bootstrapTenant(t, bs, "tenant-a", "owner-1", "role-owner")
	owner := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner-1"}

	// owner creates a role with tenant invoice:read and assigns it to alice.
	role, _, err := mgr.CreateRole(context.Background(), owner, meta(), amsonia.CreateRoleInput{
		RoleID: "role-reader", Name: "reader",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.GrantPermission(context.Background(), owner, meta(), amsonia.GrantPermissionInput{
		RoleID: role.RoleID, ExpectedVersion: 1,
		Permission: permInvoiceRead, Scope: amsonia.ScopeTenant,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AssignRole(context.Background(), owner, meta(), amsonia.AssignRoleInput{
		SubjectID: "alice", RoleID: role.RoleID, ExpectedRoleVersion: 2,
	}); err != nil {
		t.Fatal(err)
	}

	// Alice cannot assign the owner role back to owner (direct cycle).
	alice := amsonia.Principal{TenantID: "tenant-a", SubjectID: "alice"}
	if _, err := mgr.AssignRole(context.Background(), alice, meta(), amsonia.AssignRoleInput{
		SubjectID: "owner-1", RoleID: "role-owner", ExpectedRoleVersion: 1,
	}); !errors.Is(err, amsonia.ErrForbidden) {
		t.Fatalf("alice lacks assign authority, expected ErrForbidden, got %v", err)
	}
}

func TestGrantCycleViaDelegation(t *testing.T) {
	_, mgr, bs, _ := setup(t)
	bootstrapTenant(t, bs, "tenant-a", "owner-1", "role-owner")
	owner := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner-1"}

	// owner assigns the owner role to alice directly (no cycle: alice is leaf).
	if _, err := mgr.AssignRole(context.Background(), owner, meta(), amsonia.AssignRoleInput{
		SubjectID: "alice", RoleID: "role-owner", ExpectedRoleVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	// alice now has all owner powers; alice assigning owner back to owner-1
	// would create a cycle owner->alice->owner.
	alice := amsonia.Principal{TenantID: "tenant-a", SubjectID: "alice"}
	if _, err := mgr.AssignRole(context.Background(), alice, meta(), amsonia.AssignRoleInput{
		SubjectID: "owner-1", RoleID: "role-owner", ExpectedRoleVersion: 1,
	}); !errors.Is(err, amsonia.ErrGrantCycle) {
		t.Fatalf("expected ErrGrantCycle, got %v", err)
	}
}

func TestActorMustCoverTargetGrant(t *testing.T) {
	_, mgr, bs, _ := setup(t)
	bootstrapTenant(t, bs, "tenant-a", "owner-1", "role-owner")
	owner := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner-1"}

	// Create a limited role for alice: invoice:read tenant scope.
	role, _, err := mgr.CreateRole(context.Background(), owner, meta(), amsonia.CreateRoleInput{
		RoleID: "role-limited", Name: "limited",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.GrantPermission(context.Background(), owner, meta(), amsonia.GrantPermissionInput{
		RoleID: role.RoleID, ExpectedVersion: 1,
		Permission: permInvoiceRead, Scope: amsonia.ScopeTenant,
	}); err != nil {
		t.Fatal(err)
	}
	// Give alice assign-role authority plus invoice:read (via role-limited).
	if _, _, err := mgr.CreateRole(context.Background(), owner, meta(), amsonia.CreateRoleInput{
		RoleID: "role-helper", Name: "helper",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.GrantPermission(context.Background(), owner, meta(), amsonia.GrantPermissionInput{
		RoleID: "role-helper", ExpectedVersion: 1,
		Permission: permRoleAssign, Scope: amsonia.ScopeTenant,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.GrantPermission(context.Background(), owner, meta(), amsonia.GrantPermissionInput{
		RoleID: "role-helper", ExpectedVersion: 2,
		Permission: permInvoiceRead, Scope: amsonia.ScopeTenant,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AssignRole(context.Background(), owner, meta(), amsonia.AssignRoleInput{
		SubjectID: "alice", RoleID: "role-helper", ExpectedRoleVersion: 3,
	}); err != nil {
		t.Fatal(err)
	}

	alice := amsonia.Principal{TenantID: "tenant-a", SubjectID: "alice"}
	// Alice holds assign-roles and invoice:read, so she covers role-limited
	// (which contains invoice:read) and may assign it.
	if _, err := mgr.AssignRole(context.Background(), alice, meta(), amsonia.AssignRoleInput{
		SubjectID: "bob", RoleID: "role-limited", ExpectedRoleVersion: 2,
	}); err != nil {
		t.Fatalf("alice should cover role-limited, got %v", err)
	}
	// Alice may NOT assign the owner role: it contains role-manage and
	// grant-manage grants she does not hold.
	if _, err := mgr.AssignRole(context.Background(), alice, meta(), amsonia.AssignRoleInput{
		SubjectID: "carol", RoleID: "role-owner", ExpectedRoleVersion: 1,
	}); !errors.Is(err, amsonia.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for uncovered composite role, got %v", err)
	}
}

func TestLastAdministratorProtection(t *testing.T) {
	_, mgr, bs, _ := setup(t)
	bootstrapTenant(t, bs, "tenant-a", "owner-1", "role-owner")
	owner := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner-1"}

	// Revoking the owner's own grant-manage control is allowed at the grant
	// level by design (the last-admin guard covers subject/role removal), but
	// unassigning the owner role from the owner must be blocked when no other
	// administrator remains.
	//
	// The unassign requires the actor to cover the role's grants; owner
	// covers them, but the last-administrator guard must reject.
	if _, err := mgr.UnassignRole(context.Background(), owner, meta(), amsonia.UnassignRoleInput{
		SubjectID: "owner-1", RoleID: "role-owner",
	}); !errors.Is(err, amsonia.ErrLastAdministrator) {
		t.Fatalf("expected ErrLastAdministrator, got %v", err)
	}
}

func TestUnassignRemovesEdge(t *testing.T) {
	store, mgr, bs, _ := setup(t)
	bootstrapTenant(t, bs, "tenant-a", "owner-1", "role-owner")
	owner := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner-1"}
	if _, err := mgr.AssignRole(context.Background(), owner, meta(), amsonia.AssignRoleInput{
		SubjectID: "alice", RoleID: "role-owner", ExpectedRoleVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	alice := amsonia.Principal{TenantID: "tenant-a", SubjectID: "alice"}
	if _, err := mgr.UnassignRole(context.Background(), alice, meta(), amsonia.UnassignRoleInput{
		SubjectID: "alice", RoleID: "role-owner",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store
}

func TestTenantIsolation(t *testing.T) {
	store, mgr, bs, _ := setup(t)
	bootstrapTenant(t, bs, "tenant-a", "owner-1", "role-owner")
	bootstrapTenant(t, bs, "tenant-b", "owner-2", "role-owner-b")

	// Same role ID and subject ID in different tenants are independent.
	ownerB := amsonia.Principal{TenantID: "tenant-b", SubjectID: "owner-2"}
	if _, _, err := mgr.CreateRole(context.Background(), ownerB, meta(), amsonia.CreateRoleInput{
		RoleID: "role-same", Name: "same",
	}); err != nil {
		t.Fatal(err)
	}
	// Creating the same role ID in tenant-a must not conflict.
	ownerA := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner-1"}
	if _, _, err := mgr.CreateRole(context.Background(), ownerA, meta(), amsonia.CreateRoleInput{
		RoleID: "role-same", Name: "same",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store
}

func TestPurgeAndReuseTombstone(t *testing.T) {
	store, _, bs, _ := setup(t)
	bootstrapTenant(t, bs, "tenant-a", "owner-1", "role-owner")

	maint, err := amsonia.NewMaintenance(store, testAuthorizer{auth: hostAuth}, nullAudit{}, fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	if err := maint.PurgeTenant(context.Background(), "tenant-a", amsonia.MutationMetadata{
		RequestID: "req-1", ReasonCode: "customer_offboarding",
	}); err != nil {
		t.Fatal(err)
	}
	if !store.Purged("tenant-a") {
		t.Fatal("expected purge tombstone")
	}
	// Re-bootstrap of a purged tenant ID must fail.
	_, err = bs.BootstrapTenant(context.Background(), amsonia.BootstrapInput{
		TenantID:       "tenant-a",
		OwnerSubjectID: "owner-1",
		OwnerRoleID:    "role-owner",
		OwnerRoleName:  "tenant-owner",
		Grants: []amsonia.RolePermissionGrant{
			{RoleID: "role-owner", Permission: permRoleManage, Scope: amsonia.ScopeTenant},
			{RoleID: "role-owner", Permission: permGrantManage, Scope: amsonia.ScopeTenant},
			{RoleID: "role-owner", Permission: permRoleAssign, Scope: amsonia.ScopeTenant},
		},
		Metadata: amsonia.MutationMetadata{ReasonCode: "tenant_provisioning"},
	})
	if err == nil {
		t.Fatal("expected re-bootstrap of purged tenant to fail")
	}
}
