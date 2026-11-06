package tenantexport

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/willunylabs/amsonia-core"
)

func TestMarshalOrdersCollectionsWithoutMutatingInput(t *testing.T) {
	grants := []amsonia.RolePermissionGrant{
		{RoleID: "role-a", Permission: "billing:invoice:read", Scope: amsonia.ScopeOwn},
		{RoleID: "role-a", Permission: "billing:invoice:read", Scope: amsonia.ScopeTenant},
		{RoleID: "role-a", Permission: "billing:invoice:read", Scope: amsonia.ScopeWorkspace},
		{RoleID: "role-a", Permission: "billing:invoice:read", Scope: amsonia.ScopeWorkspace, WorkspaceRoles: []string{"admin"}},
		{RoleID: "role-a", Permission: "billing:invoice:read", Scope: amsonia.ScopeWorkspace, WorkspaceRoles: []string{"admin", "editor"}},
		{RoleID: "role-a", Permission: "billing:invoice:read", Scope: amsonia.ScopeWorkspace, WorkspaceRoles: []string{"editor"}},
		{RoleID: "role-a", Permission: "billing:invoice:write", Scope: amsonia.ScopeTenant},
		{RoleID: "role-z", Permission: "billing:invoice:read", Scope: amsonia.ScopeTenant},
	}
	want := Snapshot{
		Format: "amsonia.tenant.v1", TenantID: "tenant-a",
		Roles:  []amsonia.Role{{RoleID: "role-a"}, {RoleID: "role-z"}, {RoleID: "role-é"}, {RoleID: "role-中"}},
		Grants: grants,
		Versions: []amsonia.RoleVersion{
			{RoleID: "role-a", Version: 2, Grants: grants[:7]},
			{RoleID: "role-a", Version: 10, Grants: []amsonia.RolePermissionGrant{}},
			{RoleID: "role-z", Version: 1},
		},
		Edges: []amsonia.GrantEdge{
			{Grantor: "a", Grantee: "a", RoleID: "a"},
			{Grantor: "a", Grantee: "a", RoleID: "z"},
			{Grantor: "a", Grantee: "z", RoleID: "a"},
			{Grantor: "z", Grantee: "a", RoleID: "a"},
		},
		// Audit is a sequence, so deliberately keep it in nonlexical order.
		Audit: []amsonia.MutationAuditEvent{{Operation: "z"}, {Operation: "a"}},
	}
	input := want
	input.Roles = slices.Clone(want.Roles)
	slices.Reverse(input.Roles)
	input.Grants = slices.Clone(want.Grants)
	slices.Reverse(input.Grants)
	input.Versions = slices.Clone(want.Versions)
	input.Versions[0].Grants = slices.Clone(want.Versions[0].Grants)
	slices.Reverse(input.Versions[0].Grants)
	slices.Reverse(input.Versions)
	input.Edges = slices.Clone(want.Edges)
	slices.Reverse(input.Edges)
	before, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wantJSON) {
		t.Error("export does not use the complete canonical order or preserve audit/empty values")
	}
	after, err := json.Marshal(input)
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("serialization mutated input: %v", err)
	}
}

func TestMarshalPreservesV1EmptyShape(t *testing.T) {
	got, err := Marshal(Snapshot{Format: "amsonia.tenant.v1", TenantID: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "format": "amsonia.tenant.v1",
  "tenant_id": "tenant-a",
  "roles": null,
  "grants": null,
  "versions": null,
  "edges": null,
  "audit": null
}`
	if string(got) != want {
		t.Errorf("empty v1 shape changed: %s", got)
	}
}
