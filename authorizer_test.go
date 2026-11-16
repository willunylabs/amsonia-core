package amsonia

import (
	"context"
	"testing"
)

// stubPolicies returns canned effective grants.
type stubPolicies struct {
	grants []EffectiveGrant
	err    error
}

func (s *stubPolicies) ListEffectiveGrants(ctx context.Context, tenantID TenantID, subjectID SubjectID, permission PermissionKey) ([]EffectiveGrant, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.grants, nil
}

// stubMembership returns membership from a fixed map or ErrNotFound.
type stubMembership struct {
	roles map[string]string // "tenantID/workspaceID/subjectID" -> role
	err   error
}

func (s *stubMembership) LookupWorkspaceMembership(ctx context.Context, tenantID TenantID, workspaceID WorkspaceID, subjectID SubjectID) (WorkspaceMembership, error) {
	if s.err != nil {
		return WorkspaceMembership{}, s.err
	}
	key := string(tenantID) + "/" + string(workspaceID) + "/" + string(subjectID)
	role, ok := s.roles[key]
	if !ok {
		return WorkspaceMembership{}, ErrNotFound
	}
	return WorkspaceMembership{Role: role}, nil
}

const testPerm = PermissionKey("billing:invoice:read")

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	cat, err := NewCatalog([]PermissionDefinition{
		{Key: testPerm},
		{Key: "iam:role:manage"},
		{Key: "iam:grant:manage"},
		{Key: "iam:role:assign"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func mustAuthorizer(t *testing.T, grants []EffectiveGrant, roles map[string]string) *Authorizer {
	t.Helper()
	a, err := NewAuthorizer(
		testCatalog(t),
		&stubPolicies{grants: grants},
		&stubMembership{roles: roles},
	)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func tenantRequest(subject SubjectID, mode ResourceMode, resource ResourceContext) CheckRequest {
	return CheckRequest{
		Principal:  Principal{TenantID: "tenant-a", SubjectID: subject},
		Permission: testPerm,
		Mode:       mode,
		Resource:   resource,
	}
}

func TestTenantScopeAllowed(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-admin", Permission: testPerm, Scope: ScopeTenant},
	}, nil)
	d, err := a.Check(context.Background(), tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "inv-1", OwnerSubjectID: "someone-else",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allowed, got %+v", d)
	}
	if d.EffectiveScope != ScopeTenant {
		t.Fatalf("effective scope = %q, want tenant", d.EffectiveScope)
	}
	if d.Reason != ReasonAllowed {
		t.Fatalf("reason = %q, want allowed", d.Reason)
	}
}

func TestTenantScopeTenantMismatch(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-admin", Permission: testPerm, Scope: ScopeTenant},
	}, nil)
	d, err := a.Check(context.Background(), tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-b", ResourceID: "inv-1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatal("expected denied on tenant mismatch")
	}
	if d.Reason != ReasonTenantMismatch {
		t.Fatalf("reason = %q, want tenant_mismatch", d.Reason)
	}
}

func TestOwnScopeOwnerMatch(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-owner", Permission: testPerm, Scope: ScopeOwn},
	}, nil)
	req := tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "inv-1", OwnerSubjectID: "u1",
	})
	d, err := a.Check(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allowed, got %+v", d)
	}
	if d.EffectiveScope != ScopeOwn {
		t.Fatalf("effective scope = %q, want own", d.EffectiveScope)
	}
}

func TestOwnScopeOwnerMismatch(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-owner", Permission: testPerm, Scope: ScopeOwn},
	}, nil)
	req := tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "inv-1", OwnerSubjectID: "u2",
	})
	d, err := a.Check(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatal("expected denied on owner mismatch")
	}
	if d.Reason != ReasonOwnerMismatch {
		t.Fatalf("reason = %q, want owner_mismatch", d.Reason)
	}
}

func TestOwnScopeMissingOwner(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-owner", Permission: testPerm, Scope: ScopeOwn},
	}, nil)
	req := tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "inv-1",
	})
	d, err := a.Check(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.Reason != ReasonOwnerMismatch {
		t.Fatalf("expected owner_mismatch, got %+v", d)
	}
}

func TestWorkspaceScopeMember(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-ws", Permission: testPerm, Scope: ScopeWorkspace},
	}, map[string]string{"tenant-a/ws-1/u1": "member"})
	req := tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "inv-1", WorkspaceID: "ws-1",
	})
	d, err := a.Check(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allowed, got %+v", d)
	}
	if d.EffectiveScope != ScopeWorkspace {
		t.Fatalf("effective scope = %q, want workspace", d.EffectiveScope)
	}
}

func TestWorkspaceScopeNonMember(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-ws", Permission: testPerm, Scope: ScopeWorkspace},
	}, nil) // u1 is not in ws-1
	req := tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "inv-1", WorkspaceID: "ws-1",
	})
	d, err := a.Check(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.Reason != ReasonWorkspaceMembershipMiss {
		t.Fatalf("expected workspace_membership_missing, got %+v", d)
	}
}

func TestWorkspaceScopeMissingWorkspaceID(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-ws", Permission: testPerm, Scope: ScopeWorkspace},
	}, nil)
	req := tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "inv-1",
	})
	d, err := a.Check(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.Reason != ReasonWorkspaceMembershipMiss {
		t.Fatalf("expected workspace_membership_missing, got %+v", d)
	}
}

func TestWorkspaceScopeRoleConstraint(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-ws", Permission: testPerm, Scope: ScopeWorkspace, WorkspaceRoles: []string{"editor", "admin"}},
	}, map[string]string{"tenant-a/ws-1/u1": "editor"})
	req := tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "inv-1", WorkspaceID: "ws-1",
	})
	d, err := a.Check(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allowed for editor, got %+v", d)
	}
}

func TestWorkspaceScopeRoleDenied(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-ws", Permission: testPerm, Scope: ScopeWorkspace, WorkspaceRoles: []string{"editor"}},
	}, map[string]string{"tenant-a/ws-1/u1": "viewer"})
	req := tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "inv-1", WorkspaceID: "ws-1",
	})
	d, err := a.Check(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.Reason != ReasonWorkspaceRoleDenied {
		t.Fatalf("expected workspace_role_denied, got %+v", d)
	}
}

func TestWorkspaceDependencyFailureReturnsError(t *testing.T) {
	a, err := NewAuthorizer(
		testCatalog(t),
		&stubPolicies{grants: []EffectiveGrant{
			{RoleID: "role-ws", Permission: testPerm, Scope: ScopeWorkspace},
		}},
		&stubMembership{err: context.Canceled},
	)
	if err != nil {
		t.Fatal(err)
	}
	req := tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "inv-1", WorkspaceID: "ws-1",
	})
	d, err := a.Check(context.Background(), req)
	if err == nil {
		t.Fatal("expected dependency error")
	}
	if d.Allowed || d.Reason != ReasonDependencyUnavailable {
		t.Fatalf("expected dependency_unavailable decision, got %+v", d)
	}
}

func TestScopePrecedenceTenantWins(t *testing.T) {
	// Subject has own-scope grant that fails (owner mismatch) and a tenant
	// grant that succeeds. Tenant must win deterministically.
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-own", Permission: testPerm, Scope: ScopeOwn},
		{RoleID: "role-tenant", Permission: testPerm, Scope: ScopeTenant},
	}, nil)
	req := tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "inv-1", OwnerSubjectID: "u2",
	})
	d, err := a.Check(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allowed via tenant scope, got %+v", d)
	}
	if d.EffectiveScope != ScopeTenant {
		t.Fatalf("effective scope = %q, want tenant", d.EffectiveScope)
	}
}

func TestDeniedReasonPrecedence(t *testing.T) {
	// Owner mismatch takes precedence even when workspace membership is missing.
	own := EffectiveGrant{RoleID: "r1", Permission: testPerm, Scope: ScopeOwn}
	workspace := EffectiveGrant{RoleID: "r3", Permission: testPerm, Scope: ScopeWorkspace}
	for _, grants := range [][]EffectiveGrant{{own, workspace}, {workspace, own}} {
		a := mustAuthorizer(t, grants, nil)
		d, err := a.Check(t.Context(), tenantRequest("u1", ResourceExisting, ResourceContext{
			TenantID: "tenant-a", ResourceID: "inv-1", OwnerSubjectID: "u2", WorkspaceID: "ws-1",
		}))
		if err != nil || d != (Decision{Reason: ReasonOwnerMismatch}) {
			t.Fatalf("grants %+v: decision = %+v, err = %v; want owner_mismatch", grants, d, err)
		}
	}
}

func TestCreateModeRequiresTenantAndOwner(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-owner", Permission: testPerm, Scope: ScopeOwn},
	}, nil)
	// Missing proposed tenant on create -> invalid_request.
	d, err := a.Check(context.Background(), CheckRequest{
		Principal:  Principal{TenantID: "tenant-a", SubjectID: "u1"},
		Permission: testPerm,
		Mode:       ResourceCreate,
		Resource:   ResourceContext{OwnerSubjectID: "u1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.Reason != ReasonInvalidRequest {
		t.Fatalf("expected invalid_request, got %+v", d)
	}
	// Valid create for own scope.
	d, err = a.Check(context.Background(), CheckRequest{
		Principal:  Principal{TenantID: "tenant-a", SubjectID: "u1"},
		Permission: testPerm,
		Mode:       ResourceCreate,
		Resource:   ResourceContext{TenantID: "tenant-a", OwnerSubjectID: "u1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allowed create, got %+v", d)
	}
}

func TestTenantActionMode(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "role-tenant", Permission: testPerm, Scope: ScopeTenant},
	}, nil)
	req := CheckRequest{
		Principal:  Principal{TenantID: "tenant-a", SubjectID: "u1"},
		Permission: testPerm,
		Mode:       ResourceTenantAction,
		Resource:   ResourceContext{TenantID: "tenant-a"},
	}
	d, err := a.Check(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allowed tenant action, got %+v", d)
	}
	// own-scope grant must not allow a tenant action.
	a2 := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "r", Permission: testPerm, Scope: ScopeOwn},
	}, nil)
	d, err = a2.Check(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatal("own scope must not allow tenant_action")
	}
}

func TestUnknownPermissionDenied(t *testing.T) {
	const unknown = PermissionKey("billing:invoice:destroy")
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "r", Permission: unknown, Scope: ScopeTenant},
	}, nil)
	d, err := a.Check(t.Context(), CheckRequest{
		Principal:  Principal{TenantID: "tenant-a", SubjectID: "u1"},
		Permission: unknown,
		Mode:       ResourceExisting,
		Resource:   ResourceContext{TenantID: "tenant-a", ResourceID: "x"},
	})
	if err != nil || d != (Decision{Reason: ReasonInvalidRequest}) {
		t.Fatalf("decision = %+v, err = %v; want invalid_request for unknown permission", d, err)
	}
}

func TestMalformedPersistedGrantInvalidPolicy(t *testing.T) {
	a := mustAuthorizer(t, []EffectiveGrant{
		{RoleID: "r", Permission: "bad", Scope: ScopeTenant},
	}, nil)
	d, err := a.Check(context.Background(), tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "x",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.Reason != ReasonInvalidPolicy {
		t.Fatalf("expected invalid_policy, got %+v", d)
	}
}

func TestEmptyGrantsPermissionNotGranted(t *testing.T) {
	a := mustAuthorizer(t, nil, nil)
	d, err := a.Check(context.Background(), tenantRequest("u1", ResourceExisting, ResourceContext{
		TenantID: "tenant-a", ResourceID: "x",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.Reason != ReasonPermissionNotGranted {
		t.Fatalf("expected permission_not_granted, got %+v", d)
	}
}

func TestMissingTenantOrSubjectInvalid(t *testing.T) {
	for _, principal := range []Principal{
		{SubjectID: "u1"},
		{TenantID: "tenant-a"},
		{},
	} {
		a := mustAuthorizer(t, []EffectiveGrant{
			{RoleID: "r", Permission: testPerm, Scope: ScopeTenant},
		}, nil)
		d, err := a.Check(t.Context(), CheckRequest{
			Principal:  principal,
			Permission: testPerm,
			Mode:       ResourceExisting,
			Resource:   ResourceContext{TenantID: "tenant-a", ResourceID: "x"},
		})
		if err != nil || d != (Decision{Reason: ReasonInvalidRequest}) {
			t.Fatalf("principal %+v: decision = %+v, err = %v; want invalid_request", principal, d, err)
		}
	}
}
