package amsonia

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// MutationResult reports whether a mutation changed state and the resulting
// role version.
type MutationResult struct {
	Changed     bool
	RoleVersion int64
}

// CreateRoleInput creates an empty role at version 1.
type CreateRoleInput struct {
	RoleID      RoleID
	Name        string
	Description string
}

// UpdateRoleInput renames or re-describes a role.
type UpdateRoleInput struct {
	RoleID          RoleID
	ExpectedVersion int64
	Name            string
	Description     string
}

// DeleteRoleInput tombstones a role with no permissions or subjects.
type DeleteRoleInput struct {
	RoleID          RoleID
	ExpectedVersion int64
}

// GrantPermissionInput adds one scoped grant to a role.
type GrantPermissionInput struct {
	RoleID          RoleID
	ExpectedVersion int64
	Permission      PermissionKey
	Scope           Scope
	WorkspaceRoles  []string
}

// RevokePermissionInput removes one scoped grant from a role.
type RevokePermissionInput = GrantPermissionInput

// AssignRoleInput binds a subject to a role with grant-cycle protection.
type AssignRoleInput struct {
	SubjectID           SubjectID
	RoleID              RoleID
	ExpectedRoleVersion int64
}

// UnassignRoleInput removes a subject-role binding.
type UnassignRoleInput = AssignRoleInput

// BootstrapInput is the tenant-provisioning payload. Grants must all
// reference OwnerRoleID and must include the three configured control
// permissions at tenant scope.
type BootstrapInput struct {
	TenantID             TenantID
	OwnerSubjectID       SubjectID
	OwnerRoleID          RoleID
	OwnerRoleName        string
	OwnerRoleDescription string
	Grants               []RolePermissionGrant
	Metadata             MutationMetadata
}

// Manager executes tenant policy mutations through one serialized tenant
// transaction per call. Applications must not write roles or bindings
// directly.
type Manager struct {
	catalog       *Catalog
	store         Store
	memberships   WorkspaceMembershipReader
	controls      ControlPermissions
	securityAudit SecurityAuditSink
	clock         Clock
}

// NewManager constructs a Manager. All dependencies are required.
func NewManager(
	catalog *Catalog,
	store Store,
	memberships WorkspaceMembershipReader,
	controls ControlPermissions,
	securityAudit SecurityAuditSink,
	clock Clock,
) (*Manager, error) {
	if catalog == nil || store == nil || memberships == nil || securityAudit == nil || clock == nil {
		return nil, ErrInvalidInput
	}
	if err := validateControls(catalog, controls); err != nil {
		return nil, err
	}
	return &Manager{
		catalog:       catalog,
		store:         store,
		memberships:   memberships,
		controls:      controls,
		securityAudit: securityAudit,
		clock:         clock,
	}, nil
}

func validateControls(catalog *Catalog, controls ControlPermissions) error {
	for _, key := range []PermissionKey{controls.ManageRoles, controls.ManageGrants, controls.AssignRoles} {
		if _, ok := catalog.Lookup(key); !ok {
			return fmt.Errorf("%w: control permission %q not in catalog", ErrInvalidInput, key)
		}
	}
	return nil
}

func (m *Manager) validateMeta(meta MutationMetadata) error {
	if strings.TrimSpace(meta.ReasonCode) == "" {
		return ErrInvalidInput
	}
	if len(meta.ReasonCode) > 64 {
		return ErrInvalidInput
	}
	for _, r := range meta.ReasonCode {
		if r > 127 || !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return ErrInvalidInput
		}
	}
	if len(meta.RequestID) > 128 {
		return ErrInvalidInput
	}
	return nil
}

func (m *Manager) auditEvent(tenantID TenantID, actor Principal, meta MutationMetadata, operation, targetType, targetID string, outcome AuditOutcome, roleVersion int64) MutationAuditEvent {
	return MutationAuditEvent{
		TenantID:       tenantID,
		ActorSubjectID: actor.SubjectID,
		Operation:      operation,
		Phase:          AuditPhaseResult,
		TargetType:     targetType,
		TargetID:       targetID,
		Outcome:        outcome,
		ReasonCode:     meta.ReasonCode,
		RequestID:      meta.RequestID,
		RoleVersion:    roleVersion,
		At:             m.clock.Now(),
	}
}

func (m *Manager) recordDenied(ctx context.Context, actor Principal, meta MutationMetadata, operation, targetType, targetID string, roleVersion int64, cause error) {
	outcome := AuditOutcomeDenied
	if cause == ErrConflict {
		outcome = AuditOutcomeConflict
	} else if cause != ErrForbidden {
		outcome = AuditOutcomeFailed
	}
	event := m.auditEvent(actor.TenantID, actor, meta, operation, targetType, targetID, outcome, roleVersion)
	_ = m.securityAudit.RecordSecurityEvent(ctx, event) // best-effort; host must also log sanitized
}

// CreateRole creates an empty role.
func (m *Manager) CreateRole(ctx context.Context, actor Principal, meta MutationMetadata, input CreateRoleInput) (Role, MutationResult, error) {
	if err := m.validateMeta(meta); err != nil {
		return Role{}, MutationResult{}, err
	}
	if err := actor.TenantID.Validate(); err != nil || actor.SubjectID.Validate() != nil {
		return Role{}, MutationResult{}, ErrInvalidInput
	}
	if err := input.RoleID.Validate(); err != nil {
		return Role{}, MutationResult{}, ErrInvalidInput
	}
	if err := validateRoleName(input.Name); err != nil {
		return Role{}, MutationResult{}, err
	}
	role := Role{
		TenantID:    actor.TenantID,
		RoleID:      input.RoleID,
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		Version:     1,
	}
	err := m.store.MutateTenant(ctx, actor.TenantID, func(tx TenantTx) error {
		if err := m.requireActor(ctx, tx, actor, m.controls.ManageRoles, ScopeTenant, nil); err != nil {
			return err
		}
		if err := tx.InsertRole(ctx, role); err != nil {
			return err
		}
		snapshot := RoleVersion{
			TenantID:           role.TenantID,
			RoleID:             role.RoleID,
			Version:            1,
			Name:               role.Name,
			Description:        role.Description,
			Grants:             []RolePermissionGrant{},
			CreatedAt:          m.clock.Now(),
			CreatedBySubjectID: actor.SubjectID,
		}
		if err := tx.InsertRoleVersion(ctx, snapshot); err != nil {
			return err
		}
		event := m.auditEvent(actor.TenantID, actor, meta, "role.create", "role", string(input.RoleID), AuditOutcomeSuccess, 1)
		if err := tx.InsertMutationAudit(ctx, event); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		m.recordDenied(ctx, actor, meta, "role.create", "role", string(input.RoleID), 0, err)
		return Role{}, MutationResult{}, err
	}
	return role, MutationResult{Changed: true, RoleVersion: 1}, nil
}

// UpdateRole renames or re-describes a role with optimistic concurrency.
func (m *Manager) UpdateRole(ctx context.Context, actor Principal, meta MutationMetadata, input UpdateRoleInput) (Role, MutationResult, error) {
	if err := m.validateMeta(meta); err != nil {
		return Role{}, MutationResult{}, err
	}
	if err := actor.TenantID.Validate(); err != nil || actor.SubjectID.Validate() != nil {
		return Role{}, MutationResult{}, ErrInvalidInput
	}
	if err := input.RoleID.Validate(); err != nil {
		return Role{}, MutationResult{}, ErrInvalidInput
	}
	if err := validateRoleName(input.Name); err != nil {
		return Role{}, MutationResult{}, err
	}
	var updated Role
	result := MutationResult{}
	err := m.store.MutateTenant(ctx, actor.TenantID, func(tx TenantTx) error {
		if err := m.requireActor(ctx, tx, actor, m.controls.ManageRoles, ScopeTenant, nil); err != nil {
			return err
		}
		role, err := tx.GetRole(ctx, input.RoleID)
		if err != nil {
			return err
		}
		if role.Deleted {
			return ErrNotFound
		}
		if role.Version != input.ExpectedVersion {
			return ErrConflict
		}
		role.Name = strings.TrimSpace(input.Name)
		role.Description = strings.TrimSpace(input.Description)
		if err := tx.UpdateRole(ctx, role, input.ExpectedVersion); err != nil {
			return err
		}
		next := role.Version + 1
		grants, err := tx.ListRolePermissionGrants(ctx, role.RoleID)
		if err != nil {
			return err
		}
		snapshot := RoleVersion{
			TenantID:           role.TenantID,
			RoleID:             role.RoleID,
			Version:            next,
			Name:               role.Name,
			Description:        role.Description,
			Grants:             sortedGrants(grants),
			CreatedAt:          m.clock.Now(),
			CreatedBySubjectID: actor.SubjectID,
		}
		if err := tx.InsertRoleVersion(ctx, snapshot); err != nil {
			return err
		}
		event := m.auditEvent(actor.TenantID, actor, meta, "role.update", "role", string(input.RoleID), AuditOutcomeSuccess, next)
		if err := tx.InsertMutationAudit(ctx, event); err != nil {
			return err
		}
		updated = role
		updated.Version = next
		result = MutationResult{Changed: true, RoleVersion: next}
		return nil
	})
	if err != nil {
		m.recordDenied(ctx, actor, meta, "role.update", "role", string(input.RoleID), input.ExpectedVersion, err)
		return Role{}, MutationResult{}, err
	}
	return updated, result, nil
}

// DeleteRole tombstones an empty role. The role must have no permissions or
// subjects; the role ID can never be reused.
func (m *Manager) DeleteRole(ctx context.Context, actor Principal, meta MutationMetadata, input DeleteRoleInput) (MutationResult, error) {
	if err := m.validateMeta(meta); err != nil {
		return MutationResult{}, err
	}
	if err := actor.TenantID.Validate(); err != nil || actor.SubjectID.Validate() != nil {
		return MutationResult{}, ErrInvalidInput
	}
	if err := input.RoleID.Validate(); err != nil {
		return MutationResult{}, ErrInvalidInput
	}
	result := MutationResult{}
	err := m.store.MutateTenant(ctx, actor.TenantID, func(tx TenantTx) error {
		if err := m.requireActor(ctx, tx, actor, m.controls.ManageRoles, ScopeTenant, nil); err != nil {
			return err
		}
		role, err := tx.GetRole(ctx, input.RoleID)
		if err != nil {
			return err
		}
		if role.Deleted {
			return ErrNotFound
		}
		if role.Version != input.ExpectedVersion {
			return ErrConflict
		}
		grants, err := tx.ListRolePermissionGrants(ctx, role.RoleID)
		if err != nil {
			return err
		}
		if len(grants) > 0 {
			return ErrConflict // role must be empty first
		}
		subjects, err := tx.ListRoleSubjectIDs(ctx, role.RoleID)
		if err != nil {
			return err
		}
		if len(subjects) > 0 {
			return ErrConflict
		}
		if err := tx.TombstoneRole(ctx, role.RoleID, input.ExpectedVersion); err != nil {
			return err
		}
		next := role.Version + 1
		snapshot := RoleVersion{
			TenantID:           role.TenantID,
			RoleID:             role.RoleID,
			Version:            next,
			Name:               role.Name,
			Description:        role.Description,
			Grants:             []RolePermissionGrant{},
			Deleted:            true,
			CreatedAt:          m.clock.Now(),
			CreatedBySubjectID: actor.SubjectID,
		}
		if err := tx.InsertRoleVersion(ctx, snapshot); err != nil {
			return err
		}
		event := m.auditEvent(actor.TenantID, actor, meta, "role.delete", "role", string(input.RoleID), AuditOutcomeSuccess, next)
		if err := tx.InsertMutationAudit(ctx, event); err != nil {
			return err
		}
		result = MutationResult{Changed: true, RoleVersion: next}
		return nil
	})
	if err != nil {
		m.recordDenied(ctx, actor, meta, "role.delete", "role", string(input.RoleID), input.ExpectedVersion, err)
		return MutationResult{}, err
	}
	return result, nil
}

// GrantPermission adds a scoped grant. The actor must already hold the same
// exact permission with coverage over the target scope and constraints.
func (m *Manager) GrantPermission(ctx context.Context, actor Principal, meta MutationMetadata, input GrantPermissionInput) (MutationResult, error) {
	if err := m.validateMeta(meta); err != nil {
		return MutationResult{}, err
	}
	if err := actor.TenantID.Validate(); err != nil || actor.SubjectID.Validate() != nil {
		return MutationResult{}, ErrInvalidInput
	}
	if err := input.RoleID.Validate(); err != nil {
		return MutationResult{}, ErrInvalidInput
	}
	if err := input.Permission.Validate(); err != nil {
		return MutationResult{}, ErrInvalidInput
	}
	if _, ok := m.catalog.Lookup(input.Permission); !ok {
		return MutationResult{}, ErrInvalidInput
	}
	switch input.Scope {
	case ScopeOwn, ScopeWorkspace, ScopeTenant:
	default:
		return MutationResult{}, ErrInvalidInput
	}
	roles, err := normalizeWorkspaceRoles(input.WorkspaceRoles)
	if err != nil {
		return MutationResult{}, err
	}
	grant := RolePermissionGrant{
		RoleID:         input.RoleID,
		Permission:     input.Permission,
		Scope:          input.Scope,
		WorkspaceRoles: roles,
	}
	result := MutationResult{}
	err = m.store.MutateTenant(ctx, actor.TenantID, func(tx TenantTx) error {
		if err := m.requireActorForGrant(ctx, tx, actor, input.Permission, input.Scope, roles); err != nil {
			return err
		}
		role, err := tx.GetRole(ctx, input.RoleID)
		if err != nil {
			return err
		}
		if role.Deleted {
			return ErrNotFound
		}
		if role.Version != input.ExpectedVersion {
			return ErrConflict
		}
		changed, err := tx.InsertRolePermissionGrant(ctx, grant)
		if err != nil {
			return err
		}
		if !changed {
			event := m.auditEvent(actor.TenantID, actor, meta, "grant.permission", "role_permission", string(input.RoleID), AuditOutcomeNoop, role.Version)
			return tx.InsertMutationAudit(ctx, event)
		}
		next := role.Version + 1
		if err := m.bumpRoleVersion(ctx, tx, role, actor, next); err != nil {
			return err
		}
		event := m.auditEvent(actor.TenantID, actor, meta, "grant.permission", "role_permission", string(input.RoleID), AuditOutcomeSuccess, next)
		if err := tx.InsertMutationAudit(ctx, event); err != nil {
			return err
		}
		result = MutationResult{Changed: true, RoleVersion: next}
		return nil
	})
	if err != nil {
		m.recordDenied(ctx, actor, meta, "grant.permission", "role_permission", string(input.RoleID), input.ExpectedVersion, err)
		return MutationResult{}, err
	}
	return result, nil
}

// RevokePermission removes a scoped grant. The actor must cover the exact
// existing grant.
func (m *Manager) RevokePermission(ctx context.Context, actor Principal, meta MutationMetadata, input RevokePermissionInput) (MutationResult, error) {
	if err := m.validateMeta(meta); err != nil {
		return MutationResult{}, err
	}
	if err := actor.TenantID.Validate(); err != nil || actor.SubjectID.Validate() != nil {
		return MutationResult{}, ErrInvalidInput
	}
	if err := input.RoleID.Validate(); err != nil {
		return MutationResult{}, ErrInvalidInput
	}
	if err := input.Permission.Validate(); err != nil {
		return MutationResult{}, ErrInvalidInput
	}
	roles, err := normalizeWorkspaceRoles(input.WorkspaceRoles)
	if err != nil {
		return MutationResult{}, err
	}
	grant := RolePermissionGrant{
		RoleID:         input.RoleID,
		Permission:     input.Permission,
		Scope:          input.Scope,
		WorkspaceRoles: roles,
	}
	result := MutationResult{}
	err = m.store.MutateTenant(ctx, actor.TenantID, func(tx TenantTx) error {
		if err := m.requireActorForGrant(ctx, tx, actor, input.Permission, input.Scope, roles); err != nil {
			return err
		}
		role, err := tx.GetRole(ctx, input.RoleID)
		if err != nil {
			return err
		}
		if role.Deleted {
			return ErrNotFound
		}
		if role.Version != input.ExpectedVersion {
			return ErrConflict
		}
		changed, err := tx.DeleteRolePermissionGrant(ctx, grant)
		if err != nil {
			return err
		}
		if !changed {
			event := m.auditEvent(actor.TenantID, actor, meta, "revoke.permission", "role_permission", string(input.RoleID), AuditOutcomeNoop, role.Version)
			return tx.InsertMutationAudit(ctx, event)
		}
		next := role.Version + 1
		if err := m.bumpRoleVersion(ctx, tx, role, actor, next); err != nil {
			return err
		}
		event := m.auditEvent(actor.TenantID, actor, meta, "revoke.permission", "role_permission", string(input.RoleID), AuditOutcomeSuccess, next)
		if err := tx.InsertMutationAudit(ctx, event); err != nil {
			return err
		}
		result = MutationResult{Changed: true, RoleVersion: next}
		return nil
	})
	if err != nil {
		m.recordDenied(ctx, actor, meta, "revoke.permission", "role_permission", string(input.RoleID), input.ExpectedVersion, err)
		return MutationResult{}, err
	}
	return result, nil
}

// AssignRole binds a subject to a role. The actor must cover every grant in
// the caller-specified target role version and must not create a grant cycle.
func (m *Manager) AssignRole(ctx context.Context, actor Principal, meta MutationMetadata, input AssignRoleInput) (MutationResult, error) {
	if err := m.validateMeta(meta); err != nil {
		return MutationResult{}, err
	}
	if err := actor.TenantID.Validate(); err != nil || actor.SubjectID.Validate() != nil {
		return MutationResult{}, ErrInvalidInput
	}
	if err := input.SubjectID.Validate(); err != nil {
		return MutationResult{}, ErrInvalidInput
	}
	if err := input.RoleID.Validate(); err != nil {
		return MutationResult{}, ErrInvalidInput
	}
	result := MutationResult{}
	err := m.store.MutateTenant(ctx, actor.TenantID, func(tx TenantTx) error {
		if err := m.requireActor(ctx, tx, actor, m.controls.AssignRoles, ScopeTenant, nil); err != nil {
			return err
		}
		role, err := tx.GetRole(ctx, input.RoleID)
		if err != nil {
			return err
		}
		if role.Deleted {
			return ErrNotFound
		}
		if role.Version != input.ExpectedRoleVersion {
			return ErrConflict
		}
		grants, err := tx.ListRolePermissionGrants(ctx, role.RoleID)
		if err != nil {
			return err
		}
		for _, g := range grants {
			if err := m.requireActorForGrant(ctx, tx, actor, g.Permission, g.Scope, g.WorkspaceRoles); err != nil {
				return err
			}
		}
		if actor.SubjectID != input.SubjectID {
			hasPath, err := tx.HasGrantPath(ctx, input.SubjectID, actor.SubjectID)
			if err != nil {
				return err
			}
			if hasPath {
				return ErrGrantCycle
			}
		}
		grant := SubjectRoleGrant{
			TenantID:         actor.TenantID,
			SubjectID:        input.SubjectID,
			RoleID:           input.RoleID,
			GrantorSubjectID: actor.SubjectID,
			Provenance:       GrantProvenanceDelegated,
			GrantedAt:        m.clock.Now(),
		}
		changed, err := tx.InsertSubjectRoleGrant(ctx, grant)
		if err != nil {
			return err
		}
		if !changed {
			event := m.auditEvent(actor.TenantID, actor, meta, "assign.role", "subject_role", string(input.SubjectID), AuditOutcomeNoop, role.Version)
			return tx.InsertMutationAudit(ctx, event)
		}
		edge := GrantEdge{
			TenantID:  actor.TenantID,
			Grantor:   actor.SubjectID,
			Grantee:   input.SubjectID,
			RoleID:    input.RoleID,
			CreatedAt: m.clock.Now(),
		}
		if err := tx.InsertGrantEdge(ctx, edge); err != nil {
			return err
		}
		event := m.auditEvent(actor.TenantID, actor, meta, "assign.role", "subject_role", string(input.SubjectID), AuditOutcomeSuccess, role.Version)
		if err := tx.InsertMutationAudit(ctx, event); err != nil {
			return err
		}
		result = MutationResult{Changed: true, RoleVersion: role.Version}
		return nil
	})
	if err != nil {
		m.recordDenied(ctx, actor, meta, "assign.role", "subject_role", string(input.SubjectID), input.ExpectedRoleVersion, err)
		return MutationResult{}, err
	}
	return result, nil
}

// UnassignRole removes a subject-role binding and its grant edge.
func (m *Manager) UnassignRole(ctx context.Context, actor Principal, meta MutationMetadata, input UnassignRoleInput) (MutationResult, error) {
	if err := m.validateMeta(meta); err != nil {
		return MutationResult{}, err
	}
	if err := actor.TenantID.Validate(); err != nil || actor.SubjectID.Validate() != nil {
		return MutationResult{}, ErrInvalidInput
	}
	if err := input.SubjectID.Validate(); err != nil {
		return MutationResult{}, ErrInvalidInput
	}
	if err := input.RoleID.Validate(); err != nil {
		return MutationResult{}, ErrInvalidInput
	}
	result := MutationResult{}
	err := m.store.MutateTenant(ctx, actor.TenantID, func(tx TenantTx) error {
		if err := m.requireActor(ctx, tx, actor, m.controls.AssignRoles, ScopeTenant, nil); err != nil {
			return err
		}
		role, err := tx.GetRole(ctx, input.RoleID)
		if err != nil {
			return err
		}
		if role.Deleted {
			return ErrNotFound
		}
		grants, err := tx.ListRolePermissionGrants(ctx, role.RoleID)
		if err != nil {
			return err
		}
		for _, g := range grants {
			if err := m.requireActorForGrant(ctx, tx, actor, g.Permission, g.Scope, g.WorkspaceRoles); err != nil {
				return err
			}
		}
		removed, changed, err := tx.DeleteSubjectRoleGrant(ctx, input.SubjectID, input.RoleID)
		if err != nil {
			return err
		}
		if !changed {
			event := m.auditEvent(actor.TenantID, actor, meta, "unassign.role", "subject_role", string(input.SubjectID), AuditOutcomeNoop, role.Version)
			return tx.InsertMutationAudit(ctx, event)
		}
		if removed.Provenance == GrantProvenanceDelegated && removed.GrantorSubjectID != "" {
			edge := GrantEdge{
				TenantID: actor.TenantID,
				Grantor:  removed.GrantorSubjectID,
				Grantee:  input.SubjectID,
				RoleID:   input.RoleID,
			}
			if err := tx.DeleteGrantEdge(ctx, edge); err != nil {
				return err
			}
		}
		event := m.auditEvent(actor.TenantID, actor, meta, "unassign.role", "subject_role", string(input.SubjectID), AuditOutcomeSuccess, role.Version)
		if err := tx.InsertMutationAudit(ctx, event); err != nil {
			return err
		}
		result = MutationResult{Changed: true, RoleVersion: role.Version}
		return nil
	})
	if err != nil {
		m.recordDenied(ctx, actor, meta, "unassign.role", "subject_role", string(input.SubjectID), 0, err)
		return MutationResult{}, err
	}
	return result, nil
}

// bumpRoleVersion writes the next immutable snapshot and updates the current
// role version. Callers hold the tenant mutation lock.
func (m *Manager) bumpRoleVersion(ctx context.Context, tx TenantTx, role Role, actor Principal, next int64) error {
	role.Version = next
	if err := tx.UpdateRole(ctx, role, next-1); err != nil {
		return err
	}
	grants, err := tx.ListRolePermissionGrants(ctx, role.RoleID)
	if err != nil {
		return err
	}
	snapshot := RoleVersion{
		TenantID:           role.TenantID,
		RoleID:             role.RoleID,
		Version:            next,
		Name:               role.Name,
		Description:        role.Description,
		Grants:             sortedGrants(grants),
		CreatedAt:          m.clock.Now(),
		CreatedBySubjectID: actor.SubjectID,
	}
	return tx.InsertRoleVersion(ctx, snapshot)
}

func sortedGrants(grants []RolePermissionGrant) []RolePermissionGrant {
	out := append([]RolePermissionGrant(nil), grants...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Permission != out[j].Permission {
			return out[i].Permission < out[j].Permission
		}
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return fmt.Sprint(out[i].WorkspaceRoles) < fmt.Sprint(out[j].WorkspaceRoles)
	})
	return out
}

func normalizeWorkspaceRoles(roles []string) ([]string, error) {
	if len(roles) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		trimmed := strings.TrimSpace(r)
		if err := validateWorkspaceRole(trimmed); err != nil {
			return nil, ErrInvalidInput
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out, nil
}

// requireActor checks the actor holds an exact permission at tenant scope.
func (m *Manager) requireActor(ctx context.Context, tx TenantTx, actor Principal, permission PermissionKey, scope Scope, workspaceRoles []string) error {
	grants, err := tx.ListEffectiveGrants(ctx, actor.SubjectID, permission)
	if err != nil {
		return err
	}
	for _, g := range grants {
		if g.Scope != scope {
			continue
		}
		if coversWorkspaceRoles(g.WorkspaceRoles, workspaceRoles) {
			return nil
		}
	}
	return ErrForbidden
}

// requireActorForGrant checks the actor holds the exact permission with
// coverage over the target scope and constraints. Tenant covers tenant,
// workspace, and own; workspace covers only workspace; own covers only own.
func (m *Manager) requireActorForGrant(ctx context.Context, tx TenantTx, actor Principal, permission PermissionKey, target Scope, targetRoles []string) error {
	grants, err := tx.ListEffectiveGrants(ctx, actor.SubjectID, permission)
	if err != nil {
		return err
	}
	for _, g := range grants {
		if coversScope(g.Scope, target) && coversWorkspaceRoles(g.WorkspaceRoles, targetRoles) {
			return nil
		}
	}
	return ErrForbidden
}

// coversScope reports whether an actor grant scope covers a target scope.
// tenant > {tenant, workspace, own}; workspace > {workspace}; own > {own}.
func coversScope(actor, target Scope) bool {
	switch actor {
	case ScopeTenant:
		return true
	case ScopeWorkspace:
		return target == ScopeWorkspace
	case ScopeOwn:
		return target == ScopeOwn
	default:
		return false
	}
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
	for _, r := range actorRoles {
		actorSet[r] = struct{}{}
	}
	for _, t := range targetRoles {
		if _, ok := actorSet[t]; !ok {
			return false
		}
	}
	return true
}
