package amsonia

import (
	"context"
	"strings"
)

// Bootstrapper provisions an empty tenant exactly once. Bootstrap is exposed
// through a separate type, not ordinary Manager, and requires a host
// BootstrapAuthorizer.
type Bootstrapper struct {
	catalog    *Catalog
	store      Store
	controls   ControlPermissions
	authorizer BootstrapAuthorizer
	clock      Clock
}

// NewBootstrapper constructs a Bootstrapper. All dependencies are required.
func NewBootstrapper(
	catalog *Catalog,
	store Store,
	controls ControlPermissions,
	authorizer BootstrapAuthorizer,
	clock Clock,
) (*Bootstrapper, error) {
	if catalog == nil || store == nil || authorizer == nil || clock == nil {
		return nil, ErrInvalidInput
	}
	if err := validateControls(catalog, controls); err != nil {
		return nil, err
	}
	return &Bootstrapper{
		catalog:    catalog,
		store:      store,
		controls:   controls,
		authorizer: authorizer,
		clock:      clock,
	}, nil
}

// BootstrapTenant atomically creates the initial owner role, required owner
// grants, owner assignment with bootstrap provenance, initial role snapshot,
// bootstrap marker, and audit event. It creates no normal grant edge.
//
// A retained purge tombstone makes bootstrap permanently unavailable for a
// previously purged tenant ID.
func (b *Bootstrapper) BootstrapTenant(ctx context.Context, input BootstrapInput) (Role, error) {
	if err := input.TenantID.Validate(); err != nil {
		return Role{}, ErrInvalidInput
	}
	if err := input.OwnerSubjectID.Validate(); err != nil {
		return Role{}, ErrInvalidInput
	}
	if err := input.OwnerRoleID.Validate(); err != nil {
		return Role{}, ErrInvalidInput
	}
	if err := validateRoleName(input.OwnerRoleName); err != nil {
		return Role{}, err
	}
	if input.Metadata.ReasonCode == "" || len(input.Metadata.ReasonCode) > 64 {
		return Role{}, ErrInvalidInput
	}
	if len(input.Metadata.RequestID) > 128 {
		return Role{}, ErrInvalidInput
	}
	if len(input.Grants) == 0 {
		return Role{}, ErrInvalidInput
	}

	provenance, err := b.authorizer.AuthorizeBootstrap(ctx, input.TenantID)
	if err != nil {
		return Role{}, err
	}

	// Build the sorted grant set and verify every grant references the owner
	// role, is in the catalog, and includes the three control permissions at
	// tenant scope.
	grantMap := map[string]RolePermissionGrant{}
	for _, g := range input.Grants {
		if g.RoleID != input.OwnerRoleID {
			return Role{}, ErrInvalidInput
		}
		if err := g.Permission.Validate(); err != nil {
			return Role{}, ErrInvalidInput
		}
		if _, ok := b.catalog.Lookup(g.Permission); !ok {
			return Role{}, ErrInvalidInput
		}
		switch g.Scope {
		case ScopeOwn, ScopeWorkspace, ScopeTenant:
		default:
			return Role{}, ErrInvalidInput
		}
		roles, err := normalizeWorkspaceRoles(g.WorkspaceRoles)
		if err != nil {
			return Role{}, err
		}
		key := string(g.Permission) + ":" + string(g.Scope)
		grantMap[key] = RolePermissionGrant{
			RoleID:         g.RoleID,
			Permission:     g.Permission,
			Scope:          g.Scope,
			WorkspaceRoles: roles,
		}
	}
	for _, control := range []PermissionKey{b.controls.ManageRoles, b.controls.ManageGrants, b.controls.AssignRoles} {
		found := false
		for _, g := range grantMap {
			if g.Permission == control && g.Scope == ScopeTenant {
				found = true
				break
			}
		}
		if !found {
			return Role{}, ErrInvalidInput
		}
	}

	ownerRole := Role{
		TenantID:    input.TenantID,
		RoleID:      input.OwnerRoleID,
		Name:        strings.TrimSpace(input.OwnerRoleName),
		Description: strings.TrimSpace(input.OwnerRoleDescription),
		Version:     1,
	}

	err = b.store.MutateTenant(ctx, input.TenantID, func(tx TenantTx) error {
		already, err := tx.IsBootstrapped(ctx)
		if err != nil {
			return err
		}
		if already {
			return ErrAlreadyBootstrapped
		}
		if err := tx.InsertRole(ctx, ownerRole); err != nil {
			return err
		}
		grants := make([]RolePermissionGrant, 0, len(grantMap))
		for _, g := range grantMap {
			grants = append(grants, g)
		}
		for _, g := range grants {
			if _, err := tx.InsertRolePermissionGrant(ctx, g); err != nil {
				return err
			}
		}
		grant := SubjectRoleGrant{
			TenantID:   input.TenantID,
			SubjectID:  input.OwnerSubjectID,
			RoleID:     input.OwnerRoleID,
			Provenance: GrantProvenanceBootstrap,
			GrantedAt:  b.clock.Now(),
		}
		if _, err := tx.InsertSubjectRoleGrant(ctx, grant); err != nil {
			return err
		}
		snapshot := RoleVersion{
			TenantID:           input.TenantID,
			RoleID:             input.OwnerRoleID,
			Version:            1,
			Name:               ownerRole.Name,
			Description:        ownerRole.Description,
			Grants:             sortedGrants(grants),
			CreatedAt:          b.clock.Now(),
			BootstrapInitiator: provenance.Initiator,
		}
		if err := tx.InsertRoleVersion(ctx, snapshot); err != nil {
			return err
		}
		event := MutationAuditEvent{
			TenantID:      input.TenantID,
			HostInitiator: provenance.Initiator,
			Operation:     "tenant.bootstrap",
			Phase:         AuditPhaseResult,
			TargetType:    "tenant",
			TargetID:      string(input.TenantID),
			Outcome:       AuditOutcomeSuccess,
			ReasonCode:    input.Metadata.ReasonCode,
			RequestID:     input.Metadata.RequestID,
			RoleVersion:   1,
			At:            b.clock.Now(),
		}
		if err := tx.InsertMutationAudit(ctx, event); err != nil {
			return err
		}
		return tx.MarkBootstrapped(ctx, provenance)
	})
	if err != nil {
		return Role{}, err
	}
	return ownerRole, nil
}
