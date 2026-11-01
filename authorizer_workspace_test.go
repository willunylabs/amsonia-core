package amsonia

import (
	"context"
	"testing"
)

type workspaceReaderFunc func(context.Context, TenantID, WorkspaceID, SubjectID) (WorkspaceMembership, error)

func (f workspaceReaderFunc) LookupWorkspaceMembership(ctx context.Context, tenant TenantID, workspace WorkspaceID, subject SubjectID) (WorkspaceMembership, error) {
	return f(ctx, tenant, workspace, subject)
}

func TestWorkspaceCandidatesShareOneMembershipSnapshot(t *testing.T) {
	lookups := 0
	authorizer, err := NewAuthorizer(testCatalog(t), &stubPolicies{grants: []EffectiveGrant{
		{RoleID: "editor", Permission: testPerm, Scope: ScopeWorkspace, WorkspaceRoles: []string{"editor"}},
		{RoleID: "admin", Permission: testPerm, Scope: ScopeWorkspace, WorkspaceRoles: []string{"admin"}},
	}}, workspaceReaderFunc(func(ctx context.Context, tenant TenantID, workspace WorkspaceID, subject SubjectID) (WorkspaceMembership, error) {
		if tenant != "tenant-a" || workspace != "workspace-1" || subject != "alice" {
			t.Fatalf("unexpected membership lookup: %s/%s/%s", tenant, workspace, subject)
		}
		lookups++
		switch lookups {
		case 1:
			return WorkspaceMembership{Role: "viewer"}, nil
		case 2:
			return WorkspaceMembership{Role: "admin"}, nil
		case 3:
			return WorkspaceMembership{}, ErrNotFound
		default:
			return WorkspaceMembership{Role: "editor"}, nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := tenantRequest("alice", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "invoice-1", WorkspaceID: "workspace-1",
	})
	// Changing authoritative membership between calls must be visible, while
	// every candidate within one call sees the same fact.
	wants := []Decision{
		{Reason: ReasonWorkspaceRoleDenied},
		{Allowed: true, Reason: ReasonAllowed, EffectiveScope: ScopeWorkspace},
		{Reason: ReasonWorkspaceMembershipMiss},
		{Allowed: true, Reason: ReasonAllowed, EffectiveScope: ScopeWorkspace},
	}
	for i, want := range wants {
		decision, err := authorizer.Check(t.Context(), request)
		if err != nil || decision != want {
			t.Fatalf("check %d: decision = %+v, err = %v; want %+v", i+1, decision, err, want)
		}
		if lookups != i+1 {
			t.Fatalf("check %d: %d membership lookups; want %d", i+1, lookups, i+1)
		}
	}
}

func TestMissingWorkspaceMembershipIsOneSnapshot(t *testing.T) {
	lookups := 0
	authorizer, err := NewAuthorizer(testCatalog(t), &stubPolicies{grants: []EffectiveGrant{
		{RoleID: "workspace-reader", Permission: testPerm, Scope: ScopeWorkspace},
		{RoleID: "editor", Permission: testPerm, Scope: ScopeWorkspace, WorkspaceRoles: []string{"editor"}},
	}}, workspaceReaderFunc(func(context.Context, TenantID, WorkspaceID, SubjectID) (WorkspaceMembership, error) {
		lookups++
		if lookups == 1 {
			return WorkspaceMembership{}, ErrNotFound
		}
		return WorkspaceMembership{Role: "editor"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := tenantRequest("alice", ResourceCreate, ResourceContext{
		TenantID: "tenant-a", WorkspaceID: "workspace-1",
	})
	decision, err := authorizer.Check(t.Context(), request)
	if err != nil || decision != (Decision{Reason: ReasonWorkspaceMembershipMiss}) || lookups != 1 {
		t.Fatalf("first check: decision = %+v, err = %v, lookups = %d; want missing membership with one lookup", decision, err, lookups)
	}
	decision, err = authorizer.Check(t.Context(), request)
	if err != nil || !decision.Allowed || decision.EffectiveScope != ScopeWorkspace || lookups != 2 {
		t.Fatalf("second check: decision = %+v, err = %v, lookups = %d; want fresh workspace access", decision, err, lookups)
	}
}
