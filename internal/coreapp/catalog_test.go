package coreapp

import (
	"testing"

	"github.com/willunylabs/amsonia-core"
)

func TestCoreCatalogSeparatesReadFromManagePermissions(t *testing.T) {
	t.Parallel()
	catalog, controls, err := CoreCatalog()
	if err != nil {
		t.Fatal(err)
	}
	want := []amsonia.PermissionKey{
		PermissionRoleRead,
		PermissionRoleManage,
		PermissionGrantManage,
		PermissionRoleAssign,
		PermissionMemberRead,
		PermissionMemberManage,
		PermissionAuditRead,
	}
	for _, permission := range want {
		if _, found := catalog.Lookup(permission); !found {
			t.Fatalf("catalog missing %s", permission)
		}
	}
	if controls.ManageRoles != PermissionRoleManage || controls.ManageGrants != PermissionGrantManage || controls.AssignRoles != PermissionRoleAssign {
		t.Fatalf("unexpected control permissions: %+v", controls)
	}
	grants := ownerGrants("role_owner")
	if len(grants) != len(want) {
		t.Fatalf("owner grant count = %d, want %d", len(grants), len(want))
	}
	for _, grant := range grants {
		if grant.RoleID != "role_owner" || grant.Scope != amsonia.ScopeTenant {
			t.Fatalf("invalid owner grant: %+v", grant)
		}
	}
}
