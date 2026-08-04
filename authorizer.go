package amsonia

import (
	"context"
	"strings"
)

// Authorizer evaluates authorization requests against the catalog and the
// injected policy reader. It is safe for concurrent use.
type Authorizer struct {
	catalog     *Catalog
	policies    PolicyReader
	memberships WorkspaceMembershipReader
}

// NewAuthorizer constructs an Authorizer. All dependencies are required; a
// nil dependency returns ErrInvalidInput.
func NewAuthorizer(
	catalog *Catalog,
	policies PolicyReader,
	memberships WorkspaceMembershipReader,
) (*Authorizer, error) {
	if catalog == nil || policies == nil || memberships == nil {
		return nil, ErrInvalidInput
	}
	return &Authorizer{
		catalog:     catalog,
		policies:    policies,
		memberships: memberships,
	}, nil
}

// Check evaluates one exact permission for one principal within one tenant.
//
// The result is deterministic and independent of adapter row order:
//
//   - All loaded grants are validated first; any malformed persisted grant
//     returns reason invalid_policy.
//   - Valid candidates are evaluated in scope precedence tenant, workspace,
//     own (matching delegation coverage order).
//   - The first scope with a satisfying grant becomes EffectiveScope.
//   - When no grant allows, reason precedence is invalid_request,
//     tenant_mismatch, owner_mismatch, workspace_membership_missing,
//     workspace_role_denied, then permission_not_granted.
//   - Dependency failure returns an error with reason dependency_unavailable
//     and never enters reason precedence.
func (a *Authorizer) Check(ctx context.Context, request CheckRequest) (Decision, error) {
	if err := a.validateRequest(request); err != nil {
		return Decision{Allowed: false, Reason: ReasonInvalidRequest}, nil
	}

	// Cross-tenant resource addressing is a deterministic security denial and
	// is checked before any grant evaluation so no policy row is touched.
	if request.Resource.TenantID != "" && request.Resource.TenantID != request.Principal.TenantID {
		return Decision{Allowed: false, Reason: ReasonTenantMismatch}, nil
	}

	grants, err := a.policies.ListEffectiveGrants(
		ctx,
		request.Principal.TenantID,
		request.Principal.SubjectID,
		request.Permission,
	)
	if err != nil {
		return Decision{
			Allowed: false,
			Reason:  ReasonDependencyUnavailable,
		}, err
	}

	// Validate all grants before evaluating any of them.
	for _, g := range grants {
		if err := g.Validate(request.Permission); err != nil {
			return Decision{Allowed: false, Reason: ReasonInvalidPolicy}, nil
		}
	}

	// Deterministic scope precedence: tenant, workspace, own.
	bestDenied := deniedNone
	for _, scope := range []Scope{ScopeTenant, ScopeWorkspace, ScopeOwn} {
		var denied deniedReason
		for _, g := range grants {
			if g.Scope != scope {
				continue
			}
			allowed, reason, depErr := a.evaluate(ctx, request, g)
			if depErr != nil {
				return Decision{
					Allowed: false,
					Reason:  ReasonDependencyUnavailable,
				}, depErr
			}
			if allowed {
				return Decision{
					Allowed:        true,
					Reason:         ReasonAllowed,
					EffectiveScope: scope,
				}, nil
			}
			denied = mergeDenied(denied, reason)
		}
		bestDenied = mergeDenied(bestDenied, denied)
	}

	return Decision{
		Allowed: false,
		Reason:  bestDenied.reasonOr(ReasonPermissionNotGranted),
	}, nil
}

func (a *Authorizer) validateRequest(request CheckRequest) error {
	if err := request.Principal.TenantID.Validate(); err != nil {
		return err
	}
	if err := request.Principal.SubjectID.Validate(); err != nil {
		return err
	}
	if err := request.Permission.Validate(); err != nil {
		return err
	}
	if _, ok := a.catalog.Lookup(request.Permission); !ok {
		return ErrInvalidInput
	}
	switch request.Mode {
	case ResourceExisting, ResourceCreate, ResourceTenantAction:
	default:
		return ErrInvalidInput
	}
	return validateResourceContext(request)
}

func validateResourceContext(request CheckRequest) error {
	res := request.Resource
	switch request.Mode {
	case ResourceExisting:
		if res.ResourceID == "" {
			return ErrInvalidInput
		}
		if res.TenantID == "" {
			return ErrInvalidInput
		}
	case ResourceCreate:
		if res.TenantID == "" {
			return ErrInvalidInput
		}
	case ResourceTenantAction:
		if res.ResourceID != "" {
			return ErrInvalidInput
		}
	}
	if res.OwnerSubjectID != "" {
		if err := res.OwnerSubjectID.Validate(); err != nil {
			return ErrInvalidInput
		}
	}
	if res.WorkspaceID != "" {
		if err := res.WorkspaceID.Validate(); err != nil {
			return ErrInvalidInput
		}
	}
	return nil
}

type deniedReason int

const (
	deniedNone deniedReason = iota
	deniedTenantMismatch
	deniedOwnerMismatch
	deniedWorkspaceMissing
	deniedWorkspaceRole
	deniedOther
)

func mergeDenied(a, b deniedReason) deniedReason {
	if a == deniedOther || b == deniedOther {
		return deniedOther
	}
	if a == deniedNone {
		return b
	}
	if b == deniedNone {
		return a
	}
	// Deterministic precedence: tenant, owner, workspace membership,
	// workspace role.
	rank := func(r deniedReason) int {
		switch r {
		case deniedTenantMismatch:
			return 1
		case deniedOwnerMismatch:
			return 2
		case deniedWorkspaceMissing:
			return 3
		case deniedWorkspaceRole:
			return 4
		default:
			return 5
		}
	}
	if rank(b) < rank(a) {
		return b
	}
	return a
}

func (d deniedReason) reasonOr(fallback ReasonCode) ReasonCode {
	switch d {
	case deniedTenantMismatch:
		return ReasonTenantMismatch
	case deniedOwnerMismatch:
		return ReasonOwnerMismatch
	case deniedWorkspaceMissing:
		return ReasonWorkspaceMembershipMiss
	case deniedWorkspaceRole:
		return ReasonWorkspaceRoleDenied
	default:
		return fallback
	}
}

// evaluate decides one grant against the request. The third return value is
// a dependency error that must abort evaluation and never becomes a denial
// reason.
func (a *Authorizer) evaluate(ctx context.Context, request CheckRequest, grant EffectiveGrant) (bool, deniedReason, error) {
	switch grant.Scope {
	case ScopeTenant:
		if request.Resource.TenantID != "" && request.Resource.TenantID != request.Principal.TenantID {
			return false, deniedTenantMismatch, nil
		}
		return true, deniedNone, nil
	case ScopeOwn:
		if request.Mode == ResourceTenantAction {
			return false, deniedOther, nil
		}
		if request.Resource.OwnerSubjectID == "" {
			return false, deniedOwnerMismatch, nil
		}
		if request.Resource.OwnerSubjectID != request.Principal.SubjectID {
			return false, deniedOwnerMismatch, nil
		}
		return true, deniedNone, nil
	case ScopeWorkspace:
		if request.Mode == ResourceTenantAction {
			return false, deniedOther, nil
		}
		if request.Resource.WorkspaceID == "" {
			return false, deniedWorkspaceMissing, nil
		}
		membership, err := a.memberships.LookupWorkspaceMembership(
			ctx,
			request.Principal.TenantID,
			request.Resource.WorkspaceID,
			request.Principal.SubjectID,
		)
		if err != nil {
			if err == ErrNotFound {
				return false, deniedWorkspaceMissing, nil
			}
			return false, deniedNone, err
		}
		if len(grant.WorkspaceRoles) == 0 {
			return true, deniedNone, nil
		}
		for _, allowed := range grant.WorkspaceRoles {
			if strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(membership.Role)) {
				return true, deniedNone, nil
			}
		}
		return false, deniedWorkspaceRole, nil
	default:
		return false, deniedOther, nil
	}
}

// Validate checks a persisted grant against the catalog. It is exported for
// adapters and tests; the authorizer calls it on every loaded grant.
func (g EffectiveGrant) Validate(requested PermissionKey) error {
	if err := g.Permission.Validate(); err != nil {
		return ErrInvalidPolicy
	}
	if g.Permission != requested {
		return ErrInvalidPolicy
	}
	switch g.Scope {
	case ScopeOwn, ScopeWorkspace, ScopeTenant:
	default:
		return ErrInvalidPolicy
	}
	if g.RoleID == "" {
		return ErrInvalidPolicy
	}
	for _, role := range g.WorkspaceRoles {
		if err := validateWorkspaceRole(role); err != nil {
			return ErrInvalidPolicy
		}
	}
	return nil
}

func validateWorkspaceRole(role string) error {
	if role == "" {
		return ErrInvalidPolicy
	}
	if len(role) > maxSegmentLength {
		return ErrInvalidPolicy
	}
	for i, r := range role {
		if r > 127 || !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return ErrInvalidPolicy
		}
		if i == 0 && !(r >= 'a' && r <= 'z') {
			return ErrInvalidPolicy
		}
	}
	return nil
}
