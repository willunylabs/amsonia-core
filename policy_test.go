package amsonia

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPolicyEnforcesTenantAndPermission(t *testing.T) {
	for _, scope := range []Scope{ScopeTenant, ScopeWorkspace, ScopeOwn} {
		t.Run(string(scope), func(t *testing.T) {
			request := tenantRequest("alice", ResourceExisting, ResourceContext{
				TenantID: "tenant-a", ResourceID: "invoice-1", OwnerSubjectID: "alice", WorkspaceID: "workspace-1",
			})
			grant := EffectiveGrant{RoleID: "reader", Permission: testPerm, Scope: scope}
			check := func(want bool) {
				t.Helper()
				allowed := allowAccess(request, grant, WorkspaceMembership{Role: "editor"}, true)
				if allowed != want {
					t.Fatalf("allowed = %t; want %t", allowed, want)
				}
			}
			check(true)
			request.Resource.TenantID = "tenant-b"
			check(false)
			request.Resource.TenantID = "tenant-a"
			grant.Permission = "billing:invoice:write"
			check(false)
		})
	}
}

func TestDelegationCoverage(t *testing.T) {
	tests := []struct {
		name        string
		actorScope  Scope
		targetScope Scope
		actorRoles  []string
		targetRoles []string
		exact       bool
		allowed     bool
	}{
		{name: "tenant covers tenant", actorScope: ScopeTenant, targetScope: ScopeTenant, allowed: true},
		{name: "tenant covers workspace", actorScope: ScopeTenant, targetScope: ScopeWorkspace, allowed: true},
		{name: "tenant covers own", actorScope: ScopeTenant, targetScope: ScopeOwn, allowed: true},
		{name: "tenant cannot cover empty scope", actorScope: ScopeTenant},
		{name: "tenant cannot cover unknown scope", actorScope: ScopeTenant, targetScope: "unknown"},
		{name: "unknown scopes cannot match", actorScope: "unknown", targetScope: "unknown", exact: true},
		{name: "workspace covers workspace", actorScope: ScopeWorkspace, targetScope: ScopeWorkspace, allowed: true},
		{name: "workspace cannot cover own", actorScope: ScopeWorkspace, targetScope: ScopeOwn},
		{name: "workspace cannot cover tenant", actorScope: ScopeWorkspace, targetScope: ScopeTenant},
		{name: "own covers own", actorScope: ScopeOwn, targetScope: ScopeOwn, allowed: true},
		{name: "own cannot cover workspace", actorScope: ScopeOwn, targetScope: ScopeWorkspace},
		{name: "own cannot cover tenant", actorScope: ScopeOwn, targetScope: ScopeTenant},
		{name: "exact scope cannot expand", actorScope: ScopeTenant, targetScope: ScopeOwn, exact: true},
		{name: "exact scope matches", actorScope: ScopeTenant, targetScope: ScopeTenant, exact: true, allowed: true},
		{name: "unconstrained covers constrained", actorScope: ScopeWorkspace, targetScope: ScopeWorkspace, targetRoles: []string{"editor"}, allowed: true},
		{name: "superset covers subset", actorScope: ScopeWorkspace, targetScope: ScopeWorkspace, actorRoles: []string{"admin", "editor"}, targetRoles: []string{"editor"}, allowed: true},
		{name: "subset cannot expand", actorScope: ScopeWorkspace, targetScope: ScopeWorkspace, actorRoles: []string{"editor"}, targetRoles: []string{"editor", "admin"}},
		{name: "constraints cannot be removed", actorScope: ScopeWorkspace, targetScope: ScopeWorkspace, actorRoles: []string{"editor"}},
		{name: "tenant metadata does not restrict management", actorScope: ScopeTenant, targetScope: ScopeTenant, actorRoles: []string{"editor"}, exact: true, allowed: true},
		{name: "tenant metadata does not restrict workspace roles", actorScope: ScopeTenant, targetScope: ScopeWorkspace, actorRoles: []string{"editor"}, targetRoles: []string{"admin"}, allowed: true},
		{name: "tenant metadata does not require workspace constraints", actorScope: ScopeTenant, targetScope: ScopeWorkspace, actorRoles: []string{"editor"}, allowed: true},
		{name: "own metadata does not constrain own coverage", actorScope: ScopeOwn, targetScope: ScopeOwn, actorRoles: []string{"editor"}, allowed: true},
		{name: "own metadata does not expand scope", actorScope: ScopeOwn, targetScope: ScopeWorkspace, actorRoles: []string{"editor"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grant := EffectiveGrant{RoleID: "delegator", Permission: testPerm, Scope: tt.actorScope, WorkspaceRoles: tt.actorRoles}
			allowed := allowDelegation(testPerm, tt.targetScope, tt.targetRoles, tt.exact, grant)
			if allowed != tt.allowed {
				t.Fatalf("allowed = %t; want %t", allowed, tt.allowed)
			}
			// Even a tenant-wide grant must never cover a different permission.
			allowed = allowDelegation("billing:invoice:write", tt.targetScope, tt.targetRoles, tt.exact, grant)
			if allowed {
				t.Fatalf("different permission allowed = %t", allowed)
			}
		})
	}
}

type grantOnlyTx struct {
	TenantTx
	grants []EffectiveGrant
}

func (tx grantOnlyTx) ListEffectiveGrants(context.Context, SubjectID, PermissionKey) ([]EffectiveGrant, error) {
	return tx.grants, nil
}

func TestInvalidGrantsCannotBeHiddenByAnAllow(t *testing.T) {
	valid := EffectiveGrant{RoleID: "reader", Permission: testPerm, Scope: ScopeTenant}
	invalid := []EffectiveGrant{
		{RoleID: "bad", Permission: "billing:invoice:write", Scope: ScopeTenant},
		{RoleID: "bad", Permission: testPerm, Scope: "unknown"},
		{Permission: testPerm, Scope: ScopeTenant},
		{RoleID: "bad", Permission: testPerm, Scope: ScopeWorkspace, WorkspaceRoles: []string{"*"}},
		{RoleID: RoleID(strings.Repeat("r", 257)), Permission: testPerm, Scope: ScopeTenant},
		{RoleID: "role\nreader", Permission: testPerm, Scope: ScopeTenant},
		{RoleID: RoleID(string([]byte{0xff})), Permission: testPerm, Scope: ScopeTenant},
	}
	request := tenantRequest("alice", ResourceExisting, ResourceContext{TenantID: "tenant-a", ResourceID: "invoice-1"})
	for _, bad := range invalid {
		for _, grants := range [][]EffectiveGrant{{valid, bad}, {bad, valid}} {
			authorizer := mustAuthorizer(t, grants, nil)
			decision, err := authorizer.Check(t.Context(), request)
			if err != nil || decision.Allowed || decision.Reason != ReasonInvalidPolicy {
				t.Fatalf("grants %+v: decision = %+v, err = %v", grants, decision, err)
			}
			manager := &Manager{}
			for _, exact := range []bool{false, true} {
				err := manager.requireGrant(t.Context(), grantOnlyTx{grants: grants}, request.Principal, testPerm, ScopeTenant, nil, exact)
				if !errors.Is(err, ErrInvalidPolicy) {
					t.Fatalf("grants %+v, exact %t: err = %v; want invalid policy", grants, exact, err)
				}
			}
		}
	}
}

func TestWorkspaceRoleNormalization(t *testing.T) {
	authorizer := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "editor", Permission: testPerm, Scope: ScopeWorkspace, WorkspaceRoles: []string{"editor"}},
	}, map[string]string{"tenant-a/workspace-1/alice": " \tEDITOR\n"})
	decision, err := authorizer.Check(t.Context(), tenantRequest("alice", ResourceCreate, ResourceContext{
		TenantID: "tenant-a", WorkspaceID: "workspace-1",
	}))
	if err != nil || !decision.Allowed || decision.EffectiveScope != ScopeWorkspace {
		t.Fatalf("decision = %+v, err = %v; want workspace access", decision, err)
	}
}

func TestTenantScopeSkipsUnavailableWorkspaceDependency(t *testing.T) {
	authorizer, err := NewAuthorizer(testCatalog(t), &stubPolicies{grants: []EffectiveGrant{
		{RoleID: "editor", Permission: testPerm, Scope: ScopeWorkspace},
		{RoleID: "admin", Permission: testPerm, Scope: ScopeTenant},
	}}, &stubMembership{err: context.Canceled})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := authorizer.Check(t.Context(), tenantRequest("alice", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "invoice-1", WorkspaceID: "workspace-1",
	}))
	if err != nil || !decision.Allowed || decision.EffectiveScope != ScopeTenant {
		t.Fatalf("decision = %+v, err = %v; want tenant access", decision, err)
	}
}
