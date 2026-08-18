package coreapp

import "github.com/willunylabs/amsonia-core"

const (
	PermissionRoleRead     amsonia.PermissionKey = "iam:role:read"
	PermissionRoleManage   amsonia.PermissionKey = "iam:role:manage"
	PermissionGrantManage  amsonia.PermissionKey = "iam:grant:manage"
	PermissionRoleAssign   amsonia.PermissionKey = "iam:role:assign"
	PermissionMemberRead   amsonia.PermissionKey = "iam:member:read"
	PermissionMemberManage amsonia.PermissionKey = "iam:member:manage"
	PermissionAuditRead    amsonia.PermissionKey = "iam:audit:read"
)

// CoreCatalog is the single permission vocabulary shared by the API and
// operator workflows. Read permissions stay separate from mutations so a
// public or support role never needs a manage grant just to inspect a tenant.
func CoreCatalog() (*amsonia.Catalog, amsonia.ControlPermissions, error) {
	definitions := []amsonia.PermissionDefinition{
		{Key: PermissionRoleRead, Description: "Read tenant roles"},
		{Key: PermissionRoleManage, Description: "Create, update, and retire tenant roles"},
		{Key: PermissionGrantManage, Description: "Assign permissions to tenant roles"},
		{Key: PermissionRoleAssign, Description: "Assign tenant roles to members"},
		{Key: PermissionMemberRead, Description: "Read tenant members"},
		{Key: PermissionMemberManage, Description: "Invite and manage tenant members"},
		{Key: PermissionAuditRead, Description: "Read tenant administration audit events"},
	}
	controls := amsonia.ControlPermissions{
		ManageRoles:  PermissionRoleManage,
		ManageGrants: PermissionGrantManage,
		AssignRoles:  PermissionRoleAssign,
	}
	catalog, err := amsonia.NewCatalog(definitions)
	return catalog, controls, err
}

func ownerGrants(roleID amsonia.RoleID) []amsonia.RolePermissionGrant {
	permissions := []amsonia.PermissionKey{
		PermissionRoleRead,
		PermissionRoleManage,
		PermissionGrantManage,
		PermissionRoleAssign,
		PermissionMemberRead,
		PermissionMemberManage,
		PermissionAuditRead,
	}
	grants := make([]amsonia.RolePermissionGrant, 0, len(permissions))
	for _, permission := range permissions {
		grants = append(grants, amsonia.RolePermissionGrant{
			RoleID: roleID, Permission: permission, Scope: amsonia.ScopeTenant,
		})
	}
	return grants
}
