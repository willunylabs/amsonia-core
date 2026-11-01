package amsonia

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
)

//go:embed casbin_model.conf
var casbinModel string

// policyEngine is immutable after construction. Only the compiled model is
// shared; effective grants and membership facts belong to the current call.
type policyEngine struct {
	enforcer *casbin.SyncedEnforcer
}

func newPolicyEngine() (*policyEngine, error) {
	m, err := model.NewModelFromString(casbinModel)
	if err != nil {
		return nil, fmt.Errorf("amsonia: load Casbin model: %w", err)
	}
	enforcer, err := casbin.NewSyncedEnforcer(m)
	if err != nil {
		return nil, fmt.Errorf("amsonia: initialize Casbin: %w", err)
	}
	enforcer.AddFunction("workspaceRoleMatch", casbinWorkspaceRoleMatch)
	enforcer.AddFunction("workspaceRolesCover", casbinWorkspaceRolesCover)
	return &policyEngine{enforcer: enforcer}, nil
}

// Casbin reads exported struct fields through reflection. Use plain strings
// here so the expression language does not need to compare Go's named types.
type accessFacts struct {
	TenantID         string
	SubjectID        string
	Permission       string
	Mode             string
	ResourceTenantID string
	OwnerSubjectID   string
	WorkspaceID      string
	WorkspaceMember  bool
	WorkspaceRole    string
}

type delegationFacts struct {
	Permission     string
	Scope          string
	WorkspaceRoles []string
	ExactScope     bool
}

type grantFacts struct {
	Permission     string
	Scope          string
	WorkspaceRoles []string
}

func factsForGrant(grant EffectiveGrant) grantFacts {
	return grantFacts{
		Permission:     string(grant.Permission),
		Scope:          string(grant.Scope),
		WorkspaceRoles: grant.WorkspaceRoles,
	}
}

func (e *policyEngine) allowAccess(request CheckRequest, grant EffectiveGrant, membership WorkspaceMembership, member bool) (bool, error) {
	return e.enforce(accessFacts{
		TenantID:         string(request.Principal.TenantID),
		SubjectID:        string(request.Principal.SubjectID),
		Permission:       string(request.Permission),
		Mode:             string(request.Mode),
		ResourceTenantID: string(request.Resource.TenantID),
		OwnerSubjectID:   string(request.Resource.OwnerSubjectID),
		WorkspaceID:      string(request.Resource.WorkspaceID),
		WorkspaceMember:  member,
		WorkspaceRole:    membership.Role,
	}, factsForGrant(grant))
}

func (e *policyEngine) allowDelegation(permission PermissionKey, scope Scope, roles []string, exactScope bool, grant EffectiveGrant) (bool, error) {
	// Both ABAC matchers use the same policy effect and no stored policy.
	enforceContext := casbin.EnforceContext{RType: "r2", PType: "p", EType: "e", MType: "m2"}
	return e.enforce(enforceContext, delegationFacts{
		Permission:     string(permission),
		Scope:          string(scope),
		WorkspaceRoles: roles,
		ExactScope:     exactScope,
	}, factsForGrant(grant))
}

func (e *policyEngine) enforce(arguments ...any) (bool, error) {
	allowed, err := e.enforcer.Enforce(arguments...)
	if err != nil {
		return false, fmt.Errorf("%w: Casbin enforcement: %w", ErrDependencyUnavailable, err)
	}
	return allowed, nil
}

func casbinWorkspaceRoleMatch(arguments ...any) (any, error) {
	if len(arguments) != 2 {
		return false, fmt.Errorf("workspaceRoleMatch: expected role and allowed roles")
	}
	role, roleOK := arguments[0].(string)
	allowedRoles, rolesOK := arguments[1].([]string)
	if !roleOK || !rolesOK {
		return false, fmt.Errorf("workspaceRoleMatch: expected string and []string")
	}
	if len(allowedRoles) == 0 {
		return true, nil
	}
	for _, allowed := range allowedRoles {
		if strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(role)) {
			return true, nil
		}
	}
	return false, nil
}

func casbinWorkspaceRolesCover(arguments ...any) (any, error) {
	if len(arguments) != 2 {
		return false, fmt.Errorf("workspaceRolesCover: expected actor and target roles")
	}
	actorRoles, actorOK := arguments[0].([]string)
	targetRoles, targetOK := arguments[1].([]string)
	if !actorOK || !targetOK {
		return false, fmt.Errorf("workspaceRolesCover: expected two []string values")
	}
	return coversWorkspaceRoles(actorRoles, targetRoles), nil
}

// coversWorkspaceRoles reports whether an actor's constraint list covers the
// target list. An unconstrained list (nil) covers any target; a constrained
// list must be a superset of the target.
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
