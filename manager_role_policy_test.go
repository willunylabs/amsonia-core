package amsonia_test

import (
	"context"
	"errors"
	"testing"

	"github.com/willunylabs/amsonia-core"
)

// targetPolicyStore models an adapter returning malformed target-role rows,
// while leaving the actor's effective grants and real transaction intact.
type targetPolicyStore struct {
	amsonia.Store
	roleID        amsonia.RoleID
	grants        []amsonia.RolePermissionGrant
	coverageReads int
}

func (s *targetPolicyStore) MutateTenant(ctx context.Context, tenantID amsonia.TenantID, fn func(amsonia.TenantTx) error) error {
	return s.Store.MutateTenant(ctx, tenantID, func(tx amsonia.TenantTx) error {
		return fn(targetPolicyTx{TenantTx: tx, store: s})
	})
}

type targetPolicyTx struct {
	amsonia.TenantTx
	store *targetPolicyStore
}

func (tx targetPolicyTx) ListRolePermissionGrants(ctx context.Context, roleID amsonia.RoleID) ([]amsonia.RolePermissionGrant, error) {
	if roleID == tx.store.roleID {
		return tx.store.grants, nil
	}
	return tx.TenantTx.ListRolePermissionGrants(ctx, roleID)
}

func (tx targetPolicyTx) ListEffectiveGrants(ctx context.Context, subjectID amsonia.SubjectID, permission amsonia.PermissionKey) ([]amsonia.EffectiveGrant, error) {
	if permission != permRoleAssign {
		tx.store.coverageReads++
	}
	return tx.TenantTx.ListEffectiveGrants(ctx, subjectID, permission)
}

func TestRoleBindingsValidateTargetPolicy(t *testing.T) {
	valid := amsonia.RolePermissionGrant{RoleID: "reader", Permission: permInvoiceRead, Scope: amsonia.ScopeTenant}
	uncovered := amsonia.RolePermissionGrant{RoleID: "reader", Permission: permInvoiceWrite, Scope: amsonia.ScopeTenant}
	type testCase struct {
		name   string
		grants []amsonia.RolePermissionGrant
		want   error
	}
	tests := []testCase{
		{name: "empty role"},
		{name: "valid scopes and metadata", grants: []amsonia.RolePermissionGrant{
			{RoleID: "reader", Permission: permInvoiceRead, Scope: amsonia.ScopeTenant, WorkspaceRoles: []string{"editor"}},
			{RoleID: "reader", Permission: permInvoiceRead, Scope: amsonia.ScopeWorkspace, WorkspaceRoles: []string{"admin", "editor"}},
			{RoleID: "reader", Permission: permInvoiceRead, Scope: amsonia.ScopeOwn, WorkspaceRoles: []string{"editor"}},
		}},
		{name: "valid uncovered permission", grants: []amsonia.RolePermissionGrant{uncovered}, want: amsonia.ErrForbidden},
	}
	invalid := []struct {
		name   string
		modify func(*amsonia.RolePermissionGrant)
	}{
		{name: "missing role", modify: func(g *amsonia.RolePermissionGrant) { g.RoleID = "" }},
		{name: "other role", modify: func(g *amsonia.RolePermissionGrant) { g.RoleID = "other" }},
		{name: "malformed role", modify: func(g *amsonia.RolePermissionGrant) { g.RoleID = "reader\n" }},
		{name: "unknown catalog permission", modify: func(g *amsonia.RolePermissionGrant) { g.Permission = "billing:invoice:unknown" }},
		{name: "malformed permission", modify: func(g *amsonia.RolePermissionGrant) { g.Permission = "*" }},
		{name: "unknown scope", modify: func(g *amsonia.RolePermissionGrant) { g.Scope = "unknown" }},
		{name: "invalid tenant metadata", modify: func(g *amsonia.RolePermissionGrant) { g.WorkspaceRoles = []string{"*"} }},
		{name: "invalid own metadata", modify: func(g *amsonia.RolePermissionGrant) { g.Scope = amsonia.ScopeOwn; g.WorkspaceRoles = []string{"*"} }},
		{name: "invalid workspace constraints", modify: func(g *amsonia.RolePermissionGrant) {
			g.Scope = amsonia.ScopeWorkspace
			g.WorkspaceRoles = []string{"*"}
		}},
	}
	for _, bad := range invalid {
		grant := valid
		bad.modify(&grant)
		tests = append(tests, testCase{name: bad.name, grants: []amsonia.RolePermissionGrant{valid, grant}, want: amsonia.ErrInvalidPolicy})
	}
	malformed := valid
	malformed.WorkspaceRoles = []string{"*"}
	for _, rows := range []struct {
		name   string
		grants []amsonia.RolePermissionGrant
	}{
		{name: "malformed before uncovered", grants: []amsonia.RolePermissionGrant{malformed, uncovered}},
		{name: "malformed after uncovered", grants: []amsonia.RolePermissionGrant{uncovered, malformed}},
	} {
		tests = append(tests, testCase{name: rows.name, grants: rows.grants, want: amsonia.ErrInvalidPolicy})
	}
	for _, tt := range tests {
		for _, operation := range []string{"assign", "unassign"} {
			t.Run(tt.name+"/"+operation, func(t *testing.T) {
				store, setupManager, bootstrapper, catalog := setup(t)
				bootstrapTenant(t, bootstrapper, "tenant-a", "owner", "owner-role")
				actor := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner"}
				ctx := t.Context()
				_, _, err := setupManager.CreateRoleWithPermissions(ctx, actor, meta(), amsonia.CreateRoleWithPermissionsInput{
					RoleID: "reader", Name: "Reader", Permissions: []amsonia.PermissionKey{permInvoiceRead},
				})
				if err != nil {
					t.Fatal(err)
				}
				input := amsonia.AssignRoleInput{SubjectID: "alice", RoleID: "reader", ExpectedRoleVersion: 1}
				if operation == "unassign" {
					if _, err := setupManager.AssignRole(ctx, actor, meta(), input); err != nil {
						t.Fatal(err)
					}
				}
				adapter := &targetPolicyStore{Store: store, roleID: "reader", grants: tt.grants}
				audit := &recordingSecurityAudit{}
				manager, err := amsonia.NewManager(catalog, adapter, noMemberships{}, controls, audit, fixedTime())
				if err != nil {
					t.Fatal(err)
				}
				before := len(store.DebugAuditEvents(actor.TenantID))
				run := manager.AssignRole
				if operation == "unassign" {
					run = manager.UnassignRole
				}
				result, err := run(ctx, actor, meta(), input)
				if !errors.Is(err, tt.want) {
					t.Errorf("error = %v; want %v", err, tt.want)
				}
				if tt.want != nil {
					if result != (amsonia.MutationResult{}) || len(store.DebugAuditEvents(actor.TenantID)) != before {
						t.Error("rejected policy changed result or committed mutation audit")
					}
					outcome := amsonia.AuditOutcomeFailed
					if errors.Is(tt.want, amsonia.ErrForbidden) {
						outcome = amsonia.AuditOutcomeDenied
					}
					if len(audit.events) != 1 || audit.events[0].Outcome != outcome || audit.events[0].Operation != operation+".role" {
						t.Errorf("security audit = %+v; want one %s event", audit.events, outcome)
					}
				} else if result != (amsonia.MutationResult{Changed: true, RoleVersion: 1}) || len(store.DebugAuditEvents(actor.TenantID)) != before+1 || len(audit.events) != 0 {
					t.Errorf("valid policy result = %+v, security audit = %+v; want successful binding mutation", result, audit.events)
				}
				if errors.Is(tt.want, amsonia.ErrInvalidPolicy) && adapter.coverageReads != 0 {
					t.Errorf("delegation reads = %d; malformed policy must be rejected before coverage", adapter.coverageReads)
				}
				wantBound := operation == "unassign"
				if tt.want == nil {
					wantBound = !wantBound
				}
				if err := store.ReadTenant(ctx, actor.TenantID, func(reader amsonia.TenantReader) error {
					roles, err := reader.ListSubjectRoleIDs(ctx, input.SubjectID)
					if err != nil {
						return err
					}
					if bound := len(roles) == 1 && roles[0] == input.RoleID; bound != wantBound {
						t.Errorf("subject roles = %v; want bound %t", roles, wantBound)
					}
					path, err := reader.HasGrantPath(ctx, actor.SubjectID, input.SubjectID)
					if err == nil && path != wantBound {
						t.Errorf("grant path = %t; want %t", path, wantBound)
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
