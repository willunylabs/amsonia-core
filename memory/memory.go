// Package memory provides a deterministic in-memory adapter for tests and
// local development. It is not a production runtime store.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/willunylabs/amsonia"
)

// Store is a thread-safe, tenant-isolated in-memory Store.
type Store struct {
	mu      sync.RWMutex
	tenants map[amsonia.TenantID]*tenantState
}

type tenantState struct {
	roles               map[amsonia.RoleID]amsonia.Role
	rolePerms           map[amsonia.RoleID]map[grantKey]amsonia.RolePermissionGrant
	subjectRoles        map[amsonia.SubjectID]map[amsonia.RoleID]amsonia.SubjectRoleGrant
	grantEdges          map[edgeKey]amsonia.GrantEdge
	roleVersions        map[amsonia.RoleID]map[int64]amsonia.RoleVersion
	audit               []amsonia.MutationAuditEvent
	bootstrapped        bool
	bootstrapProvenance amsonia.HostProvenance
	purged              bool
	purgeLedger         map[string]amsonia.MutationAuditEvent
}

type grantKey struct {
	permission amsonia.PermissionKey
	scope      amsonia.Scope
	roles      string
}

type edgeKey struct {
	grantor amsonia.SubjectID
	grantee amsonia.SubjectID
	roleID  amsonia.RoleID
}

// NewStore returns an empty in-memory store.
func NewStore() *Store {
	return &Store{tenants: map[amsonia.TenantID]*tenantState{}}
}

func (s *Store) tenant(tenantID amsonia.TenantID) *tenantState {
	t, ok := s.tenants[tenantID]
	if !ok {
		t = &tenantState{
			roles:        map[amsonia.RoleID]amsonia.Role{},
			rolePerms:    map[amsonia.RoleID]map[grantKey]amsonia.RolePermissionGrant{},
			subjectRoles: map[amsonia.SubjectID]map[amsonia.RoleID]amsonia.SubjectRoleGrant{},
			grantEdges:   map[edgeKey]amsonia.GrantEdge{},
			roleVersions: map[amsonia.RoleID]map[int64]amsonia.RoleVersion{},
			purgeLedger:  map[string]amsonia.MutationAuditEvent{},
		}
		s.tenants[tenantID] = t
	}
	return t
}

// ReadTenant runs fn under a read lock scoped to one tenant.
func (s *Store) ReadTenant(ctx context.Context, tenantID amsonia.TenantID, fn func(amsonia.TenantReader) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fn(&tenantReader{tenant: s.tenant(tenantID), tenantID: tenantID})
}

// ListEffectiveGrants implements amsonia.PolicyReader.
func (s *Store) ListEffectiveGrants(ctx context.Context, tenantID amsonia.TenantID, subjectID amsonia.SubjectID, permission amsonia.PermissionKey) ([]amsonia.EffectiveGrant, error) {
	var grants []amsonia.EffectiveGrant
	err := s.ReadTenant(ctx, tenantID, func(r amsonia.TenantReader) error {
		var err error
		grants, err = r.ListEffectiveGrants(ctx, subjectID, permission)
		return err
	})
	if err != nil {
		return nil, err
	}
	return grants, nil
}

// MutateTenant runs fn under a global write lock. This provides exclusive
// ordering across tenants, which is stronger than the per-tenant guarantee
// required by the Store contract.
func (s *Store) MutateTenant(ctx context.Context, tenantID amsonia.TenantID, fn func(amsonia.TenantTx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tenant(tenantID)
	if t.purged {
		return amsonia.ErrNotFound
	}
	tx := &tenantTx{tenant: t, tenantID: tenantID}
	if err := fn(tx); err != nil {
		return err
	}
	return nil
}

type tenantReader struct {
	tenant   *tenantState
	tenantID amsonia.TenantID
}

func (r *tenantReader) ListEffectiveGrants(ctx context.Context, subjectID amsonia.SubjectID, permission amsonia.PermissionKey) ([]amsonia.EffectiveGrant, error) {
	roles, ok := r.tenant.subjectRoles[subjectID]
	if !ok {
		return nil, nil
	}
	var out []amsonia.EffectiveGrant
	for roleID := range roles {
		for key, grant := range r.tenant.rolePerms[roleID] {
			if grant.Permission != permission {
				continue
			}
			out = append(out, amsonia.EffectiveGrant{
				RoleID:         roleID,
				Permission:     grant.Permission,
				Scope:          grant.Scope,
				WorkspaceRoles: grant.WorkspaceRoles,
			})
			_ = key
		}
	}
	return out, nil
}

func (r *tenantReader) GetRole(ctx context.Context, roleID amsonia.RoleID) (amsonia.Role, error) {
	role, ok := r.tenant.roles[roleID]
	if !ok {
		return amsonia.Role{}, amsonia.ErrNotFound
	}
	return role, nil
}

func (r *tenantReader) ListRolePermissionGrants(ctx context.Context, roleID amsonia.RoleID) ([]amsonia.RolePermissionGrant, error) {
	if _, ok := r.tenant.roles[roleID]; !ok {
		return nil, amsonia.ErrNotFound
	}
	var out []amsonia.RolePermissionGrant
	for _, grant := range r.tenant.rolePerms[roleID] {
		out = append(out, grant)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Permission != out[j].Permission {
			return out[i].Permission < out[j].Permission
		}
		return out[i].Scope < out[j].Scope
	})
	return out, nil
}

func (r *tenantReader) ListSubjectRoleIDs(ctx context.Context, subjectID amsonia.SubjectID) ([]amsonia.RoleID, error) {
	var out []amsonia.RoleID
	for roleID := range r.tenant.subjectRoles[subjectID] {
		out = append(out, roleID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (r *tenantReader) ListRoleSubjectIDs(ctx context.Context, roleID amsonia.RoleID) ([]amsonia.SubjectID, error) {
	var out []amsonia.SubjectID
	for subjectID, roles := range r.tenant.subjectRoles {
		if _, ok := roles[roleID]; ok {
			out = append(out, subjectID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (r *tenantReader) HasGrantPath(ctx context.Context, from amsonia.SubjectID, to amsonia.SubjectID) (bool, error) {
	visited := map[amsonia.SubjectID]bool{}
	stack := []amsonia.SubjectID{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		if cur == to {
			return true, nil
		}
		for key := range r.tenant.grantEdges {
			if key.grantor == cur {
				stack = append(stack, key.grantee)
			}
		}
	}
	return false, nil
}

func (r *tenantReader) HasAdministrator(ctx context.Context, controls amsonia.ControlPermissions) (bool, error) {
	for subjectID, roles := range r.tenant.subjectRoles {
		for roleID := range roles {
			hasAll := true
			for _, control := range []amsonia.PermissionKey{controls.ManageRoles, controls.ManageGrants, controls.AssignRoles} {
				found := false
				for _, grant := range r.tenant.rolePerms[roleID] {
					if grant.Permission == control && grant.Scope == amsonia.ScopeTenant {
						found = true
						break
					}
				}
				if !found {
					hasAll = false
					break
				}
			}
			if hasAll {
				return true, nil
			}
			_ = subjectID
		}
	}
	return false, nil
}

func (r *tenantReader) IsBootstrapped(ctx context.Context) (bool, error) {
	return r.tenant.bootstrapped, nil
}

type tenantTx struct {
	tenant   *tenantState
	tenantID amsonia.TenantID
}

func (tx *tenantTx) ListEffectiveGrants(ctx context.Context, subjectID amsonia.SubjectID, permission amsonia.PermissionKey) ([]amsonia.EffectiveGrant, error) {
	return (&tenantReader{tenant: tx.tenant, tenantID: tx.tenantID}).ListEffectiveGrants(ctx, subjectID, permission)
}

func (tx *tenantTx) GetRole(ctx context.Context, roleID amsonia.RoleID) (amsonia.Role, error) {
	return (&tenantReader{tenant: tx.tenant, tenantID: tx.tenantID}).GetRole(ctx, roleID)
}

func (tx *tenantTx) ListRolePermissionGrants(ctx context.Context, roleID amsonia.RoleID) ([]amsonia.RolePermissionGrant, error) {
	return (&tenantReader{tenant: tx.tenant, tenantID: tx.tenantID}).ListRolePermissionGrants(ctx, roleID)
}

func (tx *tenantTx) ListSubjectRoleIDs(ctx context.Context, subjectID amsonia.SubjectID) ([]amsonia.RoleID, error) {
	return (&tenantReader{tenant: tx.tenant, tenantID: tx.tenantID}).ListSubjectRoleIDs(ctx, subjectID)
}

func (tx *tenantTx) ListRoleSubjectIDs(ctx context.Context, roleID amsonia.RoleID) ([]amsonia.SubjectID, error) {
	return (&tenantReader{tenant: tx.tenant, tenantID: tx.tenantID}).ListRoleSubjectIDs(ctx, roleID)
}

func (tx *tenantTx) HasGrantPath(ctx context.Context, from amsonia.SubjectID, to amsonia.SubjectID) (bool, error) {
	return (&tenantReader{tenant: tx.tenant, tenantID: tx.tenantID}).HasGrantPath(ctx, from, to)
}

func (tx *tenantTx) HasAdministrator(ctx context.Context, controls amsonia.ControlPermissions) (bool, error) {
	return (&tenantReader{tenant: tx.tenant, tenantID: tx.tenantID}).HasAdministrator(ctx, controls)
}

func (tx *tenantTx) IsBootstrapped(ctx context.Context) (bool, error) {
	return tx.tenant.bootstrapped, nil
}

func (tx *tenantTx) InsertRole(ctx context.Context, role amsonia.Role) error {
	if _, exists := tx.tenant.roles[role.RoleID]; exists {
		return amsonia.ErrConflict
	}
	tx.tenant.roles[role.RoleID] = role
	tx.tenant.rolePerms[role.RoleID] = map[grantKey]amsonia.RolePermissionGrant{}
	tx.tenant.roleVersions[role.RoleID] = map[int64]amsonia.RoleVersion{}
	return nil
}

func (tx *tenantTx) UpdateRole(ctx context.Context, role amsonia.Role, expectedVersion int64) error {
	existing, ok := tx.tenant.roles[role.RoleID]
	if !ok {
		return amsonia.ErrNotFound
	}
	if existing.Version != expectedVersion {
		return amsonia.ErrConflict
	}
	role.Version = expectedVersion + 1
	tx.tenant.roles[role.RoleID] = role
	return nil
}

func (tx *tenantTx) TombstoneRole(ctx context.Context, roleID amsonia.RoleID, expectedVersion int64) error {
	existing, ok := tx.tenant.roles[roleID]
	if !ok {
		return amsonia.ErrNotFound
	}
	if existing.Version != expectedVersion {
		return amsonia.ErrConflict
	}
	existing.Deleted = true
	existing.Version = expectedVersion + 1
	tx.tenant.roles[roleID] = existing
	return nil
}

func (tx *tenantTx) InsertRolePermissionGrant(ctx context.Context, grant amsonia.RolePermissionGrant) (bool, error) {
	role, ok := tx.tenant.roles[grant.RoleID]
	if !ok {
		return false, amsonia.ErrNotFound
	}
	if role.Deleted {
		return false, amsonia.ErrNotFound
	}
	key := grantKey{grant.Permission, grant.Scope, strings.Join(grant.WorkspaceRoles, ",")}
	if _, exists := tx.tenant.rolePerms[grant.RoleID][key]; exists {
		return false, nil
	}
	tx.tenant.rolePerms[grant.RoleID][key] = grant
	return true, nil
}

func (tx *tenantTx) DeleteRolePermissionGrant(ctx context.Context, grant amsonia.RolePermissionGrant) (bool, error) {
	key := grantKey{grant.Permission, grant.Scope, strings.Join(grant.WorkspaceRoles, ",")}
	if _, exists := tx.tenant.rolePerms[grant.RoleID][key]; !exists {
		return false, nil
	}
	delete(tx.tenant.rolePerms[grant.RoleID], key)
	return true, nil
}

func (tx *tenantTx) InsertSubjectRoleGrant(ctx context.Context, grant amsonia.SubjectRoleGrant) (bool, error) {
	if _, ok := tx.tenant.roles[grant.RoleID]; !ok {
		return false, amsonia.ErrNotFound
	}
	if _, exists := tx.tenant.subjectRoles[grant.SubjectID][grant.RoleID]; exists {
		return false, nil
	}
	if tx.tenant.subjectRoles[grant.SubjectID] == nil {
		tx.tenant.subjectRoles[grant.SubjectID] = map[amsonia.RoleID]amsonia.SubjectRoleGrant{}
	}
	tx.tenant.subjectRoles[grant.SubjectID][grant.RoleID] = grant
	return true, nil
}

func (tx *tenantTx) DeleteSubjectRoleGrant(ctx context.Context, subjectID amsonia.SubjectID, roleID amsonia.RoleID) (amsonia.SubjectRoleGrant, bool, error) {
	grant, exists := tx.tenant.subjectRoles[subjectID][roleID]
	if !exists {
		return amsonia.SubjectRoleGrant{}, false, nil
	}
	delete(tx.tenant.subjectRoles[subjectID], roleID)
	return grant, true, nil
}

func (tx *tenantTx) InsertGrantEdge(ctx context.Context, edge amsonia.GrantEdge) error {
	key := edgeKey{edge.Grantor, edge.Grantee, edge.RoleID}
	tx.tenant.grantEdges[key] = edge
	return nil
}

func (tx *tenantTx) DeleteGrantEdge(ctx context.Context, edge amsonia.GrantEdge) error {
	key := edgeKey{edge.Grantor, edge.Grantee, edge.RoleID}
	delete(tx.tenant.grantEdges, key)
	return nil
}

func (tx *tenantTx) InsertRoleVersion(ctx context.Context, snapshot amsonia.RoleVersion) error {
	if tx.tenant.roleVersions[snapshot.RoleID] == nil {
		tx.tenant.roleVersions[snapshot.RoleID] = map[int64]amsonia.RoleVersion{}
	}
	tx.tenant.roleVersions[snapshot.RoleID][snapshot.Version] = snapshot
	return nil
}

func (tx *tenantTx) InsertMutationAudit(ctx context.Context, event amsonia.MutationAuditEvent) error {
	tx.tenant.audit = append(tx.tenant.audit, event)
	return nil
}

func (tx *tenantTx) MarkBootstrapped(ctx context.Context, provenance amsonia.HostProvenance) error {
	tx.tenant.bootstrapped = true
	tx.tenant.bootstrapProvenance = provenance
	return nil
}

// MaintainTenant serializes maintenance against normal mutations.
func (s *Store) MaintainTenant(ctx context.Context, tenantID amsonia.TenantID, fn func(amsonia.MaintenanceTx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tenant(tenantID)
	if t.purged {
		return amsonia.ErrNotFound
	}
	return fn(&maintenanceTx{tenant: t, tenantID: tenantID})
}

type maintenanceTx struct {
	tenant   *tenantState
	tenantID amsonia.TenantID
}

func (mt *maintenanceTx) ExportTenant(ctx context.Context) ([]byte, error) {
	type export struct {
		Format   string                        `json:"format"`
		TenantID amsonia.TenantID              `json:"tenant_id"`
		Roles    []amsonia.Role                `json:"roles"`
		Grants   []amsonia.RolePermissionGrant `json:"grants"`
		Versions []amsonia.RoleVersion         `json:"versions"`
		Edges    []amsonia.GrantEdge           `json:"edges"`
		Audit    []amsonia.MutationAuditEvent  `json:"audit"`
	}
	e := export{Format: "amsonia.tenant.v1", TenantID: mt.tenantID}
	for _, role := range mt.tenant.roles {
		e.Roles = append(e.Roles, role)
	}
	for _, grants := range mt.tenant.rolePerms {
		for _, g := range grants {
			e.Grants = append(e.Grants, g)
		}
	}
	for _, versions := range mt.tenant.roleVersions {
		for _, v := range versions {
			e.Versions = append(e.Versions, v)
		}
	}
	for _, edge := range mt.tenant.grantEdges {
		e.Edges = append(e.Edges, edge)
	}
	e.Audit = append(e.Audit, mt.tenant.audit...)
	data, err := marshalSorted(e)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (mt *maintenanceTx) PurgeTenant(ctx context.Context, event amsonia.MutationAuditEvent) (amsonia.PurgeResult, error) {
	// Idempotent retry: a previous purge committed and left a ledger entry.
	if canonical, ok := mt.tenant.purgeLedger[event.RequestID]; ok {
		return amsonia.PurgeResult{
			Changed:          false,
			AlreadyCommitted: true,
			CanonicalEvent:   canonical,
		}, nil
	}
	// Purge all tenant authorization state except the ledger and the
	// tombstone.
	mt.tenant.roles = map[amsonia.RoleID]amsonia.Role{}
	mt.tenant.rolePerms = map[amsonia.RoleID]map[grantKey]amsonia.RolePermissionGrant{}
	mt.tenant.subjectRoles = map[amsonia.SubjectID]map[amsonia.RoleID]amsonia.SubjectRoleGrant{}
	mt.tenant.grantEdges = map[edgeKey]amsonia.GrantEdge{}
	mt.tenant.roleVersions = map[amsonia.RoleID]map[int64]amsonia.RoleVersion{}
	mt.tenant.audit = nil
	mt.tenant.bootstrapped = false
	mt.tenant.purged = true
	mt.tenant.purgeLedger[event.RequestID] = event
	return amsonia.PurgeResult{
		Changed:        true,
		CanonicalEvent: event,
	}, nil
}

// Purged reports whether a tenant has a permanent purge tombstone.
func (s *Store) Purged(tenantID amsonia.TenantID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[tenantID]
	return ok && t.purged
}

// DebugAuditEvents returns the recorded mutation audit events for a tenant
// (test and debugging aid).
func (s *Store) DebugAuditEvents(tenantID amsonia.TenantID) []amsonia.MutationAuditEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[tenantID]
	if !ok {
		return nil
	}
	return append([]amsonia.MutationAuditEvent(nil), t.audit...)
}

func marshalSorted(v any) ([]byte, error) {
	// json.MarshalIndent is deterministic; maps are already materialized into
	// slices above.
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("amsonia: export failed: %w", err)
	}
	return data, nil
}
