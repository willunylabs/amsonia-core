// Command saas demonstrates the complete Amsonia lifecycle for a Go SaaS
// application: bootstrap a tenant, delegate roles, authorize requests, and
// audit changes.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// hostProvisioner authenticates the tenant-provisioning workflow. In a real
// product this verifies a signed operator credential or an admin API key.
type hostProvisioner struct{}

func (hostProvisioner) AuthorizeBootstrap(ctx context.Context, tenantID amsonia.TenantID) (amsonia.HostProvenance, error) {
	return amsonia.HostProvenance{Initiator: "host-provisioner", At: time.Now().UTC()}, nil
}

func (hostProvisioner) AuthorizeMaintenance(ctx context.Context, tenantID amsonia.TenantID, operation string) (amsonia.HostProvenance, error) {
	return amsonia.HostProvenance{Initiator: "host-maintainer", At: time.Now().UTC()}, nil
}

// noMemberships is the host workspace-membership reader; this example uses no
// workspaces.
type noMemberships struct{}

func (noMemberships) LookupWorkspaceMembership(ctx context.Context, tenantID amsonia.TenantID, workspaceID amsonia.WorkspaceID, subjectID amsonia.SubjectID) (amsonia.WorkspaceMembership, error) {
	return amsonia.WorkspaceMembership{}, amsonia.ErrNotFound
}

// consoleAudit writes security events to stdout for the example.
type consoleAudit struct{}

func (consoleAudit) RecordSecurityEvent(ctx context.Context, event amsonia.MutationAuditEvent) error {
	fmt.Printf("audit %-8s %-20s tenant=%s subject=%s reason=%s\n",
		event.Outcome, event.Operation, event.TenantID, event.ActorSubjectID, event.ReasonCode)
	return nil
}

func meta() amsonia.MutationMetadata {
	return amsonia.MutationMetadata{ReasonCode: "ops_review", RequestID: "req-1"}
}

func main() {
	ctx := context.Background()

	catalog, err := amsonia.NewCatalog([]amsonia.PermissionDefinition{
		{Key: permInvoiceRead, Description: "Read invoices"},
		{Key: permInvoiceWrite, Description: "Write invoices"},
		{Key: permRoleManage, Description: "Manage roles"},
		{Key: permGrantManage, Description: "Manage grants"},
		{Key: permRoleAssign, Description: "Assign roles"},
	})
	if err != nil {
		fatal(err)
	}

	store := memory.NewStore()
	bootstrapper, err := amsonia.NewBootstrapper(catalog, store, controls, hostProvisioner{}, amsonia.RealClock{})
	if err != nil {
		fatal(err)
	}
	manager, err := amsonia.NewManager(catalog, store, noMemberships{}, controls, consoleAudit{}, amsonia.RealClock{})
	if err != nil {
		fatal(err)
	}
	authorizer, err := amsonia.NewAuthorizer(catalog, store, noMemberships{})
	if err != nil {
		fatal(err)
	}

	// 1. Provision the tenant.
	owner := amsonia.Principal{TenantID: "tenant-1", SubjectID: "owner-1"}
	if _, err := bootstrapper.BootstrapTenant(ctx, amsonia.BootstrapInput{
		TenantID:       owner.TenantID,
		OwnerSubjectID: owner.SubjectID,
		OwnerRoleID:    "role-owner",
		OwnerRoleName:  "tenant-owner",
		Grants: []amsonia.RolePermissionGrant{
			{RoleID: "role-owner", Permission: permRoleManage, Scope: amsonia.ScopeTenant},
			{RoleID: "role-owner", Permission: permGrantManage, Scope: amsonia.ScopeTenant},
			{RoleID: "role-owner", Permission: permRoleAssign, Scope: amsonia.ScopeTenant},
			{RoleID: "role-owner", Permission: permInvoiceRead, Scope: amsonia.ScopeTenant},
			{RoleID: "role-owner", Permission: permInvoiceWrite, Scope: amsonia.ScopeOwn},
		},
		Metadata: meta(),
	}); err != nil {
		fatal(err)
	}
	fmt.Println("tenant-1 bootstrapped")

	// 2. Delegate: create a read-only billing role and assign it to alice.
	role, _, err := manager.CreateRole(ctx, owner, meta(), amsonia.CreateRoleInput{
		RoleID: "role-billing-reader", Name: "billing-reader",
	})
	if err != nil {
		fatal(err)
	}
	if _, err := manager.GrantPermission(ctx, owner, meta(), amsonia.GrantPermissionInput{
		RoleID: role.RoleID, ExpectedVersion: 1,
		Permission: permInvoiceRead, Scope: amsonia.ScopeTenant,
	}); err != nil {
		fatal(err)
	}
	if _, err := manager.AssignRole(ctx, owner, meta(), amsonia.AssignRoleInput{
		SubjectID: "alice", RoleID: role.RoleID, ExpectedRoleVersion: 2,
	}); err != nil {
		fatal(err)
	}
	fmt.Println("assigned billing-reader to alice")

	// 3. Authorize requests.
	check := func(subject amsonia.SubjectID, permission amsonia.PermissionKey, res amsonia.ResourceContext) {
		dec, err := authorizer.Check(ctx, amsonia.CheckRequest{
			Principal:  amsonia.Principal{TenantID: owner.TenantID, SubjectID: subject},
			Permission: permission,
			Mode:       amsonia.ResourceExisting,
			Resource:   res,
		})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%-8s %-28s %-30s allowed=%v reason=%s scope=%s\n",
			subject, permission, res.ResourceID, dec.Allowed, dec.Reason, dec.EffectiveScope)
	}

	res := amsonia.ResourceContext{TenantID: owner.TenantID, ResourceID: "inv-1", OwnerSubjectID: "owner-1"}
	check(owner.SubjectID, permInvoiceRead, res)  // allowed (tenant scope)
	check(owner.SubjectID, permInvoiceWrite, res) // allowed (own scope)
	check("alice", permInvoiceRead, res)          // allowed (delegated tenant scope)
	check("alice", permInvoiceWrite, res)         // denied (not granted)
	check("mallory", permInvoiceRead, res)        // denied (no grants)

	// 4. Grant cycles are rejected by construction.
	_, err = manager.AssignRole(ctx, amsonia.Principal{TenantID: owner.TenantID, SubjectID: "alice"}, meta(),
		amsonia.AssignRoleInput{SubjectID: "owner-1", RoleID: "role-owner", ExpectedRoleVersion: 1})
	if !errors.Is(err, amsonia.ErrForbidden) {
		fatal(errors.New("alice must not be able to assign the owner role"))
	}
	fmt.Println("cycle/privilege escalation rejected")

	fmt.Println("example complete")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
