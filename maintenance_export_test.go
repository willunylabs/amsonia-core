package amsonia_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/willunylabs/amsonia-core"
)

func TestMemoryMaintenanceExportIsCanonicalAndTenantScoped(t *testing.T) {
	store, manager, bootstrapper, _ := setup(t)
	bootstrapTenant(t, bootstrapper, "tenant-a", "owner", "owner-role")
	bootstrapTenant(t, bootstrapper, "tenant-b", "other-owner", "private-role")
	actor := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	ctx := t.Context()
	for _, roleID := range []amsonia.RoleID{"role-z", "role-a"} {
		if _, _, err := manager.CreateRoleWithPermissions(ctx, actor, meta(), amsonia.CreateRoleWithPermissionsInput{
			RoleID: roleID, Name: string(roleID), Permissions: []amsonia.PermissionKey{permInvoiceRead},
		}); err != nil {
			t.Fatal(err)
		}
		for i, grant := range []amsonia.GrantPermissionInput{
			{Scope: amsonia.ScopeWorkspace, WorkspaceRoles: []string{"editor"}},
			{Scope: amsonia.ScopeOwn},
			{Scope: amsonia.ScopeWorkspace, WorkspaceRoles: []string{"admin"}},
		} {
			grant.RoleID, grant.Permission, grant.ExpectedVersion = roleID, permInvoiceRead, int64(i+1)
			if _, err := manager.GrantPermission(ctx, actor, meta(), grant); err != nil {
				t.Fatal(err)
			}
		}
		for _, subject := range []amsonia.SubjectID{"zoe", "alice"} {
			if _, err := manager.AssignRole(ctx, actor, meta(), amsonia.AssignRoleInput{SubjectID: subject, RoleID: roleID, ExpectedRoleVersion: 4}); err != nil {
				t.Fatal(err)
			}
		}
	}
	maintenance, err := amsonia.NewMaintenance(store, testAuthorizer{auth: hostAuth}, nullAudit{}, fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	data, err := maintenance.ExportTenant(ctx, actor.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		again, err := maintenance.ExportTenant(ctx, actor.TenantID)
		if err != nil || !bytes.Equal(data, again) {
			t.Fatalf("unchanged tenant produced different export bytes: %v", err)
		}
	}
	var snapshot struct {
		Format   string                        `json:"format"`
		TenantID amsonia.TenantID              `json:"tenant_id"`
		Roles    []amsonia.Role                `json:"roles"`
		Grants   []amsonia.RolePermissionGrant `json:"grants"`
		Versions []amsonia.RoleVersion         `json:"versions"`
		Edges    []amsonia.GrantEdge           `json:"edges"`
		Audit    []amsonia.MutationAuditEvent  `json:"audit"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Format != "amsonia.tenant.v1" || snapshot.TenantID != actor.TenantID ||
		len(snapshot.Roles) != 3 || len(snapshot.Grants) != 12 || len(snapshot.Versions) != 9 || len(snapshot.Edges) != 4 {
		t.Fatalf("unexpected export contents: %+v", snapshot)
	}
	if snapshot.Roles[0].RoleID != "owner-role" || snapshot.Roles[1].RoleID != "role-a" || snapshot.Roles[2].RoleID != "role-z" {
		t.Errorf("exported roles are not ordered or tenant-scoped: %+v", snapshot.Roles)
	}
	if !reflect.DeepEqual(snapshot.Audit, store.DebugAuditEvents(actor.TenantID)) {
		t.Error("export changed mutation audit sequence or contents")
	}
}
