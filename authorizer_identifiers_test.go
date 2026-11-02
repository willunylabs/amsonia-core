package amsonia

import (
	"context"
	"strings"
	"testing"
)

type policyReaderFunc func(context.Context, TenantID, SubjectID, PermissionKey) ([]EffectiveGrant, error)

func (f policyReaderFunc) ListEffectiveGrants(ctx context.Context, tenant TenantID, subject SubjectID, permission PermissionKey) ([]EffectiveGrant, error) {
	return f(ctx, tenant, subject, permission)
}

func TestInvalidResourceIdentifiersDoNotReadAdapters(t *testing.T) {
	invalid := []struct {
		name  string
		value string
	}{
		{"overlong", strings.Repeat("a", 257)},
		{"control character", "invoice\n1"},
		{"invalid UTF-8", string([]byte{0xff})},
	}
	for _, mode := range []ResourceMode{ResourceExisting, ResourceCreate, ResourceTenantAction} {
		for _, field := range []string{"tenant", "resource"} {
			for _, bad := range invalid {
				t.Run(string(mode)+"/"+field+"/"+bad.name, func(t *testing.T) {
					policyReads, membershipReads := 0, 0
					authorizer, err := NewAuthorizer(testCatalog(t), policyReaderFunc(func(context.Context, TenantID, SubjectID, PermissionKey) ([]EffectiveGrant, error) {
						policyReads++
						return []EffectiveGrant{{RoleID: "admin", Permission: testPerm, Scope: ScopeTenant}}, nil
					}), workspaceReaderFunc(func(context.Context, TenantID, WorkspaceID, SubjectID) (WorkspaceMembership, error) {
						membershipReads++
						return WorkspaceMembership{Role: "member"}, nil
					}))
					if err != nil {
						t.Fatal(err)
					}
					resource := ResourceContext{TenantID: "tenant-a", WorkspaceID: "workspace-1"}
					if mode != ResourceTenantAction {
						resource.ResourceID = "invoice-1"
					}
					if field == "tenant" {
						resource.TenantID = TenantID(bad.value)
					} else {
						resource.ResourceID = ResourceID(bad.value)
					}
					decision, err := authorizer.Check(t.Context(), tenantRequest("alice", mode, resource))
					if err != nil || decision != (Decision{Reason: ReasonInvalidRequest}) {
						t.Errorf("decision = %+v, err = %v; want invalid request", decision, err)
					}
					if policyReads != 0 || membershipReads != 0 {
						t.Errorf("adapter reads: policy %d, membership %d; want none", policyReads, membershipReads)
					}
				})
			}
		}
	}
}

func TestResourceIdentifierModeRequirements(t *testing.T) {
	tests := []struct {
		name     string
		mode     ResourceMode
		resource ResourceContext
		want     Decision
	}{
		{"existing requires resource", ResourceExisting, ResourceContext{TenantID: "tenant-a"}, Decision{Reason: ReasonInvalidRequest}},
		{"existing requires tenant", ResourceExisting, ResourceContext{ResourceID: "invoice-1"}, Decision{Reason: ReasonInvalidRequest}},
		{"create requires tenant", ResourceCreate, ResourceContext{}, Decision{Reason: ReasonInvalidRequest}},
		{"create permits omitted resource", ResourceCreate, ResourceContext{TenantID: "tenant-a"}, Decision{Allowed: true, Reason: ReasonAllowed, EffectiveScope: ScopeTenant}},
		{"create permits proposed resource", ResourceCreate, ResourceContext{TenantID: "tenant-a", ResourceID: "invoice-1"}, Decision{Allowed: true, Reason: ReasonAllowed, EffectiveScope: ScopeTenant}},
		{"tenant action permits omitted tenant", ResourceTenantAction, ResourceContext{}, Decision{Allowed: true, Reason: ReasonAllowed, EffectiveScope: ScopeTenant}},
		{"tenant action prohibits resource", ResourceTenantAction, ResourceContext{TenantID: "tenant-a", ResourceID: "invoice-1"}, Decision{Reason: ReasonInvalidRequest}},
		{"valid different tenant", ResourceExisting, ResourceContext{TenantID: "tenant-b", ResourceID: "invoice-1"}, Decision{Reason: ReasonTenantMismatch}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authorizer := mustAuthorizer(t, []EffectiveGrant{{RoleID: "admin", Permission: testPerm, Scope: ScopeTenant}}, nil)
			decision, err := authorizer.Check(t.Context(), tenantRequest("alice", tt.mode, tt.resource))
			if err != nil || decision != tt.want {
				t.Fatalf("decision = %+v, err = %v; want %+v", decision, err, tt.want)
			}
		})
	}
}

func TestAuthorizationPreservesValidUnicodeIdentifiers(t *testing.T) {
	for _, scope := range []Scope{ScopeOwn, ScopeWorkspace} {
		t.Run(string(scope), func(t *testing.T) {
			principal := Principal{TenantID: "租户-\ufffd", SubjectID: "用户-\ufffd"}
			resource := ResourceContext{
				TenantID: principal.TenantID, ResourceID: ResourceID(strings.Repeat("a", 253) + "\ufffd"),
				OwnerSubjectID: principal.SubjectID, WorkspaceID: "空间-\ufffd",
			}
			policyReads, membershipReads := 0, 0
			authorizer, err := NewAuthorizer(testCatalog(t), policyReaderFunc(func(ctx context.Context, tenant TenantID, subject SubjectID, permission PermissionKey) ([]EffectiveGrant, error) {
				policyReads++
				if tenant != principal.TenantID || subject != principal.SubjectID || permission != testPerm {
					t.Errorf("policy lookup changed opaque identifiers: %q/%q/%q", tenant, subject, permission)
				}
				return []EffectiveGrant{{RoleID: RoleID(strings.Repeat("r", 253) + "\ufffd"), Permission: permission, Scope: scope}}, nil
			}), workspaceReaderFunc(func(ctx context.Context, tenant TenantID, workspace WorkspaceID, subject SubjectID) (WorkspaceMembership, error) {
				membershipReads++
				if tenant != principal.TenantID || workspace != resource.WorkspaceID || subject != principal.SubjectID {
					t.Errorf("membership lookup changed opaque identifiers: %q/%q/%q", tenant, workspace, subject)
				}
				return WorkspaceMembership{Role: "member"}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			decision, err := authorizer.Check(t.Context(), CheckRequest{Principal: principal, Permission: testPerm, Mode: ResourceExisting, Resource: resource})
			if err != nil || decision != (Decision{Allowed: true, Reason: ReasonAllowed, EffectiveScope: scope}) {
				t.Errorf("decision = %+v, err = %v; want %s access", decision, err, scope)
			}
			wantMembershipReads := 0
			if scope == ScopeWorkspace {
				wantMembershipReads = 1
			}
			if policyReads != 1 || membershipReads != wantMembershipReads {
				t.Errorf("adapter reads: policy %d, membership %d; want 1 and %d", policyReads, membershipReads, wantMembershipReads)
			}
		})
	}
}
