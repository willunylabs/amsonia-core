package amsonia

import "context"

// PolicyReader loads effective grants for one subject and permission within a
// tenant. Implementations must scope all reads to the tenant passed in.
type PolicyReader interface {
	ListEffectiveGrants(
		ctx context.Context,
		tenantID TenantID,
		subjectID SubjectID,
		permission PermissionKey,
	) ([]EffectiveGrant, error)
}

// WorkspaceMembershipReader confirms workspace membership from authoritative
// host data. The workspace must belong to the tenant; a workspace that does
// not belong to the tenant or a subject that is not a member must return
// ErrNotFound and must not distinguish those cases.
type WorkspaceMembershipReader interface {
	LookupWorkspaceMembership(
		ctx context.Context,
		tenantID TenantID,
		workspaceID WorkspaceID,
		subjectID SubjectID,
	) (WorkspaceMembership, error)
}

// Store is the persistence boundary. ReadTenant and MutateTenant scope a
// callback to one tenant.
//
// MutateTenant must provide serializable behavior and exclusive mutation
// ordering for one tenant for the duration of the callback. A custom adapter
// that cannot provide that guarantee is not conformant.
type Store interface {
	ReadTenant(ctx context.Context, tenantID TenantID, fn func(TenantReader) error) error
	MutateTenant(ctx context.Context, tenantID TenantID, fn func(TenantTx) error) error
}

// TenantReader provides read-only access inside one tenant.
type TenantReader interface {
	ListEffectiveGrants(ctx context.Context, subjectID SubjectID, permission PermissionKey) ([]EffectiveGrant, error)
	GetRole(ctx context.Context, roleID RoleID) (Role, error)
	ListRolePermissionGrants(ctx context.Context, roleID RoleID) ([]RolePermissionGrant, error)
	ListSubjectRoleIDs(ctx context.Context, subjectID SubjectID) ([]RoleID, error)
	ListRoleSubjectIDs(ctx context.Context, roleID RoleID) ([]SubjectID, error)
	HasGrantPath(ctx context.Context, from SubjectID, to SubjectID) (bool, error)
	HasAdministrator(ctx context.Context, controls ControlPermissions) (bool, error)
	IsBootstrapped(ctx context.Context) (bool, error)
}

// TenantTx extends TenantReader with mutating operations. All operations run
// in one serialized tenant transaction; any returned error rolls back the
// transaction.
type TenantTx interface {
	TenantReader
	InsertRole(ctx context.Context, role Role) error
	UpdateRole(ctx context.Context, role Role, expectedVersion int64) error
	TombstoneRole(ctx context.Context, roleID RoleID, expectedVersion int64) error
	InsertRolePermissionGrant(ctx context.Context, grant RolePermissionGrant) (bool, error)
	DeleteRolePermissionGrant(ctx context.Context, grant RolePermissionGrant) (bool, error)
	InsertSubjectRoleGrant(ctx context.Context, grant SubjectRoleGrant) (bool, error)
	DeleteSubjectRoleGrant(ctx context.Context, subjectID SubjectID, roleID RoleID) (SubjectRoleGrant, bool, error)
	InsertGrantEdge(ctx context.Context, edge GrantEdge) error
	DeleteGrantEdge(ctx context.Context, edge GrantEdge) error
	InsertRoleVersion(ctx context.Context, snapshot RoleVersion) error
	InsertMutationAudit(ctx context.Context, event MutationAuditEvent) error
	MarkBootstrapped(ctx context.Context, provenance HostProvenance) error
}

// SecurityAuditSink durably records events that cannot live in a rolled-back
// mutation transaction: denied, conflict, failed, and maintenance lifecycle
// events. Implementations must be durable and deduplicate events by tenant,
// request ID, operation, and phase.
type SecurityAuditSink interface {
	RecordSecurityEvent(ctx context.Context, event MutationAuditEvent) error
}

// BootstrapAuthorizer authenticates the host tenant-provisioning workflow and
// returns bounded non-subject initiator provenance.
type BootstrapAuthorizer interface {
	AuthorizeBootstrap(ctx context.Context, tenantID TenantID) (HostProvenance, error)
}

// MaintenanceAuthorizer authenticates offline maintenance operations.
type MaintenanceAuthorizer interface {
	AuthorizeMaintenance(ctx context.Context, tenantID TenantID, operation string) (HostProvenance, error)
}

// MaintenanceTx scopes export and purge to one serialized tenant boundary.
type MaintenanceTx interface {
	ExportTenant(ctx context.Context) ([]byte, error)
	PurgeTenant(ctx context.Context, event MutationAuditEvent) (PurgeResult, error)
}

// MaintenanceStore serializes maintenance against normal mutations through
// the same per-tenant lock and serializable database boundary.
type MaintenanceStore interface {
	MaintainTenant(ctx context.Context, tenantID TenantID, fn func(MaintenanceTx) error) error
}

// PurgeResult reports the outcome of a purge attempt.
type PurgeResult struct {
	Changed          bool
	AlreadyCommitted bool
	CanonicalEvent   MutationAuditEvent
}
