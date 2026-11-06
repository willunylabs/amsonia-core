package amsonia

import (
	"context"
	"fmt"
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

func TestWorkspaceAdapterWrappedErrors(t *testing.T) {
	missing := fmt.Errorf("workspace membership lookup: %w", ErrNotFound)
	tests := []struct {
		name      string
		lookupErr error
		ownGrant  bool
		want      Decision
		wantError bool
	}{
		{
			name: "wrapped missing membership", lookupErr: missing,
			want: Decision{Reason: ReasonWorkspaceMembershipMiss},
		},
		{
			name: "nested missing membership", lookupErr: fmt.Errorf("host adapter: %w", missing),
			want: Decision{Reason: ReasonWorkspaceMembershipMiss},
		},
		{
			name: "missing membership permits own scope fallback", lookupErr: missing, ownGrant: true,
			want: Decision{Allowed: true, Reason: ReasonAllowed, EffectiveScope: ScopeOwn},
		},
		{
			name: "dependency failure prevents fallback", lookupErr: fmt.Errorf("workspace store: %w", ErrDependencyUnavailable), ownGrant: true,
			want: Decision{Reason: ReasonDependencyUnavailable}, wantError: true,
		},
		{
			name: "cancellation prevents fallback", lookupErr: fmt.Errorf("workspace lookup: %w", context.Canceled), ownGrant: true,
			want: Decision{Reason: ReasonDependencyUnavailable}, wantError: true,
		},
		{
			name: "timeout prevents fallback", lookupErr: fmt.Errorf("workspace lookup: %w", context.DeadlineExceeded), ownGrant: true,
			want: Decision{Reason: ReasonDependencyUnavailable}, wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grants := []EffectiveGrant{
				{RoleID: "workspace-reader", Permission: testPerm, Scope: ScopeWorkspace},
				{RoleID: "editor", Permission: testPerm, Scope: ScopeWorkspace, WorkspaceRoles: []string{"editor"}},
			}
			if tt.ownGrant {
				grants = append(grants, EffectiveGrant{RoleID: "owner", Permission: testPerm, Scope: ScopeOwn})
			}
			lookups := 0
			authorizer, err := NewAuthorizer(testCatalog(t), &stubPolicies{grants: grants}, workspaceReaderFunc(func(context.Context, TenantID, WorkspaceID, SubjectID) (WorkspaceMembership, error) {
				lookups++
				// Facts accompanying an error must not establish membership.
				return WorkspaceMembership{Role: "editor"}, tt.lookupErr
			}))
			if err != nil {
				t.Fatal(err)
			}
			decision, err := authorizer.Check(t.Context(), tenantRequest("alice", ResourceExisting, ResourceContext{
				TenantID: "tenant-a", ResourceID: "invoice-1", WorkspaceID: "workspace-1", OwnerSubjectID: "alice",
			}))
			if decision != tt.want {
				t.Errorf("decision = %+v; want %+v", decision, tt.want)
			}
			if tt.wantError {
				if err != tt.lookupErr {
					t.Errorf("error = %v; want original adapter error %v", err, tt.lookupErr)
				}
			} else if err != nil {
				t.Errorf("unexpected dependency error: %v", err)
			}
			if lookups != 1 {
				t.Errorf("membership lookups = %d; want 1", lookups)
			}
		})
	}
}
