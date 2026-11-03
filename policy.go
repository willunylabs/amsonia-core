package amsonia

import "strings"

// allowAccess evaluates the fixed scope contract using validated, call-local
// facts. Policy storage and tenant transaction boundaries remain in adapters.
func allowAccess(request CheckRequest, grant EffectiveGrant, membership WorkspaceMembership, member bool) bool {
	if request.Permission != grant.Permission || (request.Resource.TenantID != "" && request.Resource.TenantID != request.Principal.TenantID) {
		return false
	}
	if grant.Scope == ScopeTenant {
		return true
	}
	if request.Mode == ResourceTenantAction {
		return false
	}
	switch grant.Scope {
	case ScopeOwn:
		return request.Resource.OwnerSubjectID != "" && request.Resource.OwnerSubjectID == request.Principal.SubjectID
	case ScopeWorkspace:
		return request.Resource.WorkspaceID != "" && member && workspaceRoleMatch(membership.Role, grant.WorkspaceRoles)
	default:
		return false
	}
}

func allowDelegation(permission PermissionKey, target Scope, roles []string, exact bool, grant EffectiveGrant) bool {
	switch target {
	case ScopeOwn, ScopeWorkspace, ScopeTenant:
	default:
		return false
	}
	if permission != grant.Permission || (grant.Scope != target && (exact || grant.Scope != ScopeTenant)) {
		return false
	}
	return grant.Scope != ScopeWorkspace || coversWorkspaceRoles(grant.WorkspaceRoles, roles)
}

func workspaceRoleMatch(role string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(role)) {
			return true
		}
	}
	return false
}

// coversWorkspaceRoles reports whether the actor's constraint list covers the
// target list. An unconstrained list covers any target; otherwise the actor
// must hold a superset of the target roles.
func coversWorkspaceRoles(actorRoles, targetRoles []string) bool {
	if len(actorRoles) == 0 {
		return true
	}
	if len(targetRoles) == 0 {
		return false
	}
	actorSet := make(map[string]struct{}, len(actorRoles))
	for _, role := range actorRoles {
		actorSet[role] = struct{}{}
	}
	for _, role := range targetRoles {
		if _, ok := actorSet[role]; !ok {
			return false
		}
	}
	return true
}
