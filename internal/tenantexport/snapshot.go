// Package tenantexport serializes the shared tenant v1 maintenance payload.
package tenantexport

import (
	"cmp"
	"encoding/json"
	"slices"

	"github.com/willunylabs/amsonia-core"
)

// Snapshot is the existing v1 payload shared by persistence adapters.
type Snapshot struct {
	Format   string                        `json:"format"`
	TenantID amsonia.TenantID              `json:"tenant_id"`
	Roles    []amsonia.Role                `json:"roles"`
	Grants   []amsonia.RolePermissionGrant `json:"grants"`
	Versions []amsonia.RoleVersion         `json:"versions"`
	Edges    []amsonia.GrantEdge           `json:"edges"`
	Audit    []amsonia.MutationAuditEvent  `json:"audit"`
}

// Marshal returns canonical JSON without modifying adapter-owned slices.
func Marshal(snapshot Snapshot) ([]byte, error) {
	snapshot.Roles = slices.Clone(snapshot.Roles)
	slices.SortFunc(snapshot.Roles, func(a, b amsonia.Role) int {
		return cmp.Compare(a.RoleID, b.RoleID)
	})
	snapshot.Grants = sortedGrants(snapshot.Grants)
	snapshot.Versions = slices.Clone(snapshot.Versions)
	slices.SortFunc(snapshot.Versions, func(a, b amsonia.RoleVersion) int {
		return cmp.Or(cmp.Compare(a.RoleID, b.RoleID), cmp.Compare(a.Version, b.Version))
	})
	for i := range snapshot.Versions {
		snapshot.Versions[i].Grants = sortedGrants(snapshot.Versions[i].Grants)
	}
	snapshot.Edges = slices.Clone(snapshot.Edges)
	slices.SortFunc(snapshot.Edges, func(a, b amsonia.GrantEdge) int {
		return cmp.Or(cmp.Compare(a.Grantor, b.Grantor), cmp.Compare(a.Grantee, b.Grantee), cmp.Compare(a.RoleID, b.RoleID))
	})
	// Audit is an append-only sequence, not an unordered policy collection.
	return json.MarshalIndent(snapshot, "", "  ")
}

func sortedGrants(grants []amsonia.RolePermissionGrant) []amsonia.RolePermissionGrant {
	grants = slices.Clone(grants)
	slices.SortFunc(grants, func(a, b amsonia.RolePermissionGrant) int {
		return cmp.Or(
			cmp.Compare(a.RoleID, b.RoleID),
			cmp.Compare(a.Permission, b.Permission),
			cmp.Compare(a.Scope, b.Scope),
			slices.Compare(a.WorkspaceRoles, b.WorkspaceRoles),
		)
	})
	return grants
}
