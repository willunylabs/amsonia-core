// Package amsonia provides an opinionated, tenant-safe delegated
// authorization kernel for Go SaaS applications. It is embedded directly into
// a host application and requires no additional authorization service.
//
// The host application owns authentication, tenant binding, resource loading,
// and HTTP adaptation. Amsonia owns tenant-isolated RBAC policy evaluation and
// delegated administration.
//
// # Stability
//
// The exported API is the implementation contract for v1. Field additions
// require a design revision; the package follows Go's compatibility promise
// within each major version.
package amsonia

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Opaque identifier types. Values are preserved exactly as provided by the
// host, must be valid UTF-8, contain no ASCII control characters, and are
// limited to 256 bytes when persisted.
type (
	// TenantID identifies one tenant's authorization domain.
	TenantID string
	// SubjectID identifies an authenticated principal within a tenant.
	SubjectID string
	// RoleID identifies a role within a tenant.
	RoleID string
	// WorkspaceID identifies a workspace within a tenant.
	WorkspaceID string
	// ResourceID identifies a business resource addressed by a check.
	ResourceID string
	// PermissionKey is an exact three-segment permission identifier.
	PermissionKey string
	// ReasonCode is a stable machine-readable decision or failure reason.
	ReasonCode string
	// ResourceMode distinguishes existing-resource, create, and
	// tenant-level checks.
	ResourceMode string
)

// Access scopes carried by role-permission grants.
type Scope string

const (
	ScopeOwn       Scope = "own"
	ScopeWorkspace Scope = "workspace"
	ScopeTenant    Scope = "tenant"
)

// Resource modes.
const (
	// ResourceExisting addresses an already-persisted resource.
	ResourceExisting ResourceMode = "existing"
	// ResourceCreate addresses a resource being created.
	ResourceCreate ResourceMode = "create"
	// ResourceTenantAction addresses a tenant-level action with no resource.
	ResourceTenantAction ResourceMode = "tenant_action"
)

// Stable public reason codes returned in decisions.
const (
	ReasonAllowed                 ReasonCode = "allowed"
	ReasonInvalidRequest          ReasonCode = "invalid_request"
	ReasonPermissionNotGranted    ReasonCode = "permission_not_granted"
	ReasonTenantMismatch          ReasonCode = "tenant_mismatch"
	ReasonOwnerMismatch           ReasonCode = "owner_mismatch"
	ReasonWorkspaceMembershipMiss ReasonCode = "workspace_membership_missing"
	ReasonWorkspaceRoleDenied     ReasonCode = "workspace_role_denied"
	ReasonInvalidPolicy           ReasonCode = "invalid_policy"
	ReasonDependencyUnavailable   ReasonCode = "dependency_unavailable"
)

// Public sentinel errors. Adapters wrap underlying errors without changing
// these classifications.
var (
	ErrInvalidInput         = errors.New("amsonia: invalid input")
	ErrForbidden            = errors.New("amsonia: forbidden")
	ErrConflict             = errors.New("amsonia: version conflict")
	ErrNotFound             = errors.New("amsonia: not found")
	ErrGrantCycle           = errors.New("amsonia: grant cycle would be created")
	ErrLastAdministrator    = errors.New("amsonia: tenant would lose its last administrator")
	ErrAlreadyBootstrapped  = errors.New("amsonia: tenant already bootstrapped")
	ErrDependencyUnavailable = errors.New("amsonia: dependency unavailable")
	ErrAuditUnavailable     = errors.New("amsonia: audit unavailable")
	ErrInvalidPolicy        = errors.New("amsonia: invalid policy data")
)

// PermissionDefinition is an immutable application catalog entry.
type PermissionDefinition struct {
	Key         PermissionKey
	Description string
}

// Principal is an authenticated subject within one tenant.
type Principal struct {
	TenantID  TenantID
	SubjectID SubjectID
}

// ResourceContext carries authoritative resource facts loaded by the host
// business service. Field requirements depend on the check mode and scope.
type ResourceContext struct {
	TenantID       TenantID
	ResourceID     ResourceID
	OwnerSubjectID SubjectID
	WorkspaceID    WorkspaceID
}

// CheckRequest is a complete authorization request.
type CheckRequest struct {
	Principal  Principal
	Permission PermissionKey
	Mode       ResourceMode
	Resource   ResourceContext
}

// Decision is the public authorization outcome.
type Decision struct {
	Allowed        bool
	Reason         ReasonCode
	EffectiveScope Scope
}

// EffectiveGrant is the read-only view of one scoped grant held by a subject.
type EffectiveGrant struct {
	RoleID         RoleID
	Permission     PermissionKey
	Scope          Scope
	WorkspaceRoles []string
}

// WorkspaceMembership is the host-provided membership fact for one workspace.
type WorkspaceMembership struct {
	Role string
}

// GrantProvenance distinguishes delegated from bootstrap provenance.
type GrantProvenance string

const (
	GrantProvenanceDelegated GrantProvenance = "delegated"
	GrantProvenanceBootstrap GrantProvenance = "bootstrap"
)

// SubjectRoleGrant is a persisted subject-to-role binding.
type SubjectRoleGrant struct {
	TenantID         TenantID
	SubjectID        SubjectID
	RoleID           RoleID
	GrantorSubjectID SubjectID
	Provenance       GrantProvenance
	GrantedAt        time.Time
}

// GrantEdge records the grantor→grantee relationship used for cycle
// prevention. One edge exists per active normal assignment.
type GrantEdge struct {
	TenantID  TenantID
	Grantor   SubjectID
	Grantee   SubjectID
	RoleID    RoleID
	CreatedAt time.Time
}

// Role is a tenant-scoped role with an optimistic-concurrency version.
type Role struct {
	TenantID    TenantID
	RoleID      RoleID
	Name        string
	Description string
	Version     int64
	Deleted     bool
}

// RolePermissionGrant is one scoped permission grant on a role.
type RolePermissionGrant struct {
	RoleID         RoleID
	Permission     PermissionKey
	Scope          Scope
	WorkspaceRoles []string
}

// RoleVersion is an immutable snapshot of a role's complete grants.
type RoleVersion struct {
	TenantID           TenantID
	RoleID             RoleID
	Version            int64
	Name               string
	Description        string
	Grants             []RolePermissionGrant
	Deleted            bool
	CreatedAt          time.Time
	CreatedBySubjectID SubjectID
	BootstrapInitiator string
}

// AuditOutcome classifies an audit event.
type AuditOutcome string

const (
	AuditOutcomeSuccess  AuditOutcome = "success"
	AuditOutcomeNoop     AuditOutcome = "noop"
	AuditOutcomeDenied   AuditOutcome = "denied"
	AuditOutcomeConflict AuditOutcome = "conflict"
	AuditOutcomeFailed   AuditOutcome = "failed"
)

// AuditPhase distinguishes result, intent, and completion events.
type AuditPhase string

const (
	AuditPhaseResult    AuditPhase = "result"
	AuditPhaseIntent    AuditPhase = "intent"
	AuditPhaseCompleted AuditPhase = "completed"
)

// MutationAuditEvent is the append-only record of one administrative action.
type MutationAuditEvent struct {
	TenantID       TenantID
	ActorSubjectID SubjectID
	HostInitiator  string
	Operation      string
	Phase          AuditPhase
	TargetType     string
	TargetID       string
	Outcome        AuditOutcome
	ReasonCode     string
	RequestID      string
	RoleVersion    int64
	At             time.Time
}

// HostProvenance identifies a host-initiated operation such as bootstrap,
// export, or purge. It is not a subject identity.
type HostProvenance struct {
	Initiator string
	At        time.Time
}

// ControlPermissions are the three application-defined management permission
// keys that gate delegated administration.
type ControlPermissions struct {
	ManageRoles  PermissionKey
	ManageGrants PermissionKey
	AssignRoles  PermissionKey
}

// MutationMetadata carries bounded audit context for administrative actions.
type MutationMetadata struct {
	RequestID  string
	ReasonCode string
}

// Role is validated with a trimmed name between 1 and 64 code points.
func validateRoleName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ErrInvalidInput
	}
	if len([]rune(trimmed)) > 64 {
		return ErrInvalidInput
	}
	for _, r := range trimmed {
		if r < 0x20 || r == 0x7f {
			return ErrInvalidInput
		}
	}
	return nil
}

// Clock is the injectable time source.
type Clock interface {
	Now() time.Time
}

// RealClock returns the current time.
type RealClock struct{}

// Now implements Clock.
func (RealClock) Now() time.Time { return time.Now().UTC() }

// contextKey is a private context key type.
type contextKey struct{ name string }

var ctxTenant = contextKey{name: "tenant"}

// WithTenant returns a context carrying an explicit tenant for store calls.
func WithTenant(ctx context.Context, tenantID TenantID) context.Context {
	return context.WithValue(ctx, ctxTenant, tenantID)
}

// TenantFromContext returns the tenant carried by the context.
func TenantFromContext(ctx context.Context) (TenantID, bool) {
	v, ok := ctx.Value(ctxTenant).(TenantID)
	return v, ok
}

// Validate checks that opaque IDs are usable and bounded.
func (t TenantID) Validate() error { return validateOpaqueID(string(t), "tenant") }

func (s SubjectID) Validate() error { return validateOpaqueID(string(s), "subject") }

func (r RoleID) Validate() error { return validateOpaqueID(string(r), "role") }

func (w WorkspaceID) Validate() error { return validateOpaqueID(string(w), "workspace") }

func (r ResourceID) Validate() error { return validateOpaqueID(string(r), "resource") }

func validateOpaqueID(v, kind string) error {
	if v == "" {
		return ErrInvalidInput
	}
	if len(v) > 256 {
		return ErrInvalidInput
	}
	if !isValidUTF8NoControl(v) {
		return ErrInvalidInput
	}
	return nil
}

func isValidUTF8NoControl(v string) bool {
	for _, r := range v {
		if r < 0x20 || r == 0x7f || r == 0xfffd {
			return false
		}
	}
	return true
}
