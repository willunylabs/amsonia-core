package amsonia_test

import (
	"context"
	"errors"
	"testing"

	"github.com/willunylabs/amsonia-core"
)

type bootstrapMembership struct{ role string }

func (m bootstrapMembership) LookupWorkspaceMembership(context.Context, amsonia.TenantID, amsonia.WorkspaceID, amsonia.SubjectID) (amsonia.WorkspaceMembership, error) {
	return amsonia.WorkspaceMembership{Role: m.role}, nil
}

func bootstrapValidationInput() amsonia.BootstrapInput {
	return amsonia.BootstrapInput{
		TenantID: "tenant-a", OwnerSubjectID: "owner", OwnerRoleID: "owner-role", OwnerRoleName: "Owner",
		Grants: []amsonia.RolePermissionGrant{
			{RoleID: "owner-role", Permission: permRoleManage, Scope: amsonia.ScopeTenant},
			{RoleID: "owner-role", Permission: permGrantManage, Scope: amsonia.ScopeTenant},
			{RoleID: "owner-role", Permission: permRoleAssign, Scope: amsonia.ScopeTenant},
		},
		Metadata: meta(),
	}
}

func TestBootstrapPreservesDistinctWorkspaceConstraints(t *testing.T) {
	store, _, bootstrapper, catalog := setup(t)
	input := bootstrapValidationInput()
	for _, roles := range [][]string{{"admin"}, {"editor"}, {" admin ", "admin"}} {
		input.Grants = append(input.Grants, amsonia.RolePermissionGrant{
			RoleID: input.OwnerRoleID, Permission: permInvoiceRead, Scope: amsonia.ScopeWorkspace, WorkspaceRoles: roles,
		})
	}
	if _, err := bootstrapper.BootstrapTenant(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	grants, err := store.ListEffectiveGrants(t.Context(), input.TenantID, input.OwnerSubjectID, permInvoiceRead)
	if err != nil || len(grants) != 2 {
		t.Fatalf("bootstrap dropped distinct grants or retained duplicates: %+v, %v", grants, err)
	}
	for _, role := range []string{"admin", "editor", "viewer"} {
		authorizer, err := amsonia.NewAuthorizer(catalog, store, bootstrapMembership{role: role})
		if err != nil {
			t.Fatal(err)
		}
		decision, err := authorizer.Check(t.Context(), amsonia.CheckRequest{
			Principal:  amsonia.Principal{TenantID: input.TenantID, SubjectID: input.OwnerSubjectID},
			Permission: permInvoiceRead, Mode: amsonia.ResourceExisting,
			Resource: amsonia.ResourceContext{TenantID: input.TenantID, ResourceID: "invoice-1", WorkspaceID: "workspace-1"},
		})
		if err != nil || decision.Allowed != (role != "viewer") {
			t.Errorf("workspace role %s: decision=%+v error=%v", role, decision, err)
		}
	}
}

func TestRoleMutationsRejectMalformedUTF8NamesBeforeStorage(t *testing.T) {
	for _, name := range []string{"owner\xff", "owner\xe2\x82"} {
		store, _, _, catalog := setup(t)
		tracked := &countingMutationStore{Store: store}
		manager, err := amsonia.NewManager(catalog, tracked, noMemberships{}, controls, nullAudit{}, fixedTime())
		if err != nil {
			t.Fatal(err)
		}
		bootstrapper, err := amsonia.NewBootstrapper(catalog, tracked, controls, testAuthorizer{auth: hostAuth}, fixedTime())
		if err != nil {
			t.Fatal(err)
		}
		actor := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner"}
		_, _, createErr := manager.CreateRole(t.Context(), actor, meta(), amsonia.CreateRoleInput{RoleID: "role", Name: name})
		_, _, grantErr := manager.CreateRoleWithPermissions(t.Context(), actor, meta(), amsonia.CreateRoleWithPermissionsInput{RoleID: "role", Name: name})
		_, _, updateErr := manager.UpdateRole(t.Context(), actor, meta(), amsonia.UpdateRoleInput{RoleID: "role", ExpectedVersion: 1, Name: name})
		input := bootstrapValidationInput()
		input.OwnerRoleName = name
		_, bootstrapErr := bootstrapper.BootstrapTenant(t.Context(), input)
		for operation, err := range map[string]error{"create": createErr, "create with grants": grantErr, "update": updateErr, "bootstrap": bootstrapErr} {
			if !errors.Is(err, amsonia.ErrInvalidInput) {
				t.Errorf("%s accepted malformed UTF-8: %v", operation, err)
			}
		}
		if tracked.calls != 0 {
			t.Errorf("malformed names reached %d storage transactions", tracked.calls)
		}
	}
}
