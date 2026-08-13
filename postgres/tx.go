package postgres

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/willunylabs/amsonia-core"
)

type tenantReader struct {
	tx       pgx.Tx
	tenantID amsonia.TenantID
}

func (r *tenantReader) ListEffectiveGrants(ctx context.Context, subjectID amsonia.SubjectID, permission amsonia.PermissionKey) ([]amsonia.EffectiveGrant, error) {
	sql := `
		SELECT sr.role_id, rpg.permission_key, rpg.scope, rpg.workspace_roles
		FROM amsonia.subject_roles sr
		JOIN amsonia.role_permission_grants rpg
		  ON rpg.tenant_id = sr.tenant_id AND rpg.role_id = sr.role_id
		WHERE sr.tenant_id = $1 AND sr.subject_id = $2 AND rpg.permission_key = $3
	`
	rows, err := r.tx.Query(ctx, sql, string(r.tenantID), string(subjectID), string(permission))
	if err != nil {
		return nil, wrapPgxErr(err)
	}
	defer rows.Close()
	var out []amsonia.EffectiveGrant
	for rows.Next() {
		var g amsonia.EffectiveGrant
		if err := rows.Scan(&g.RoleID, &g.Permission, &g.Scope, &g.WorkspaceRoles); err != nil {
			return nil, wrapPgxErr(err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *tenantReader) GetRole(ctx context.Context, roleID amsonia.RoleID) (amsonia.Role, error) {
	var role amsonia.Role
	err := r.tx.QueryRow(ctx, `
		SELECT tenant_id, role_id, name, description, version, deleted
		FROM amsonia.roles WHERE tenant_id = $1 AND role_id = $2
	`, string(r.tenantID), string(roleID)).Scan(
		&role.TenantID, &role.RoleID, &role.Name, &role.Description, &role.Version, &role.Deleted,
	)
	if err != nil {
		return amsonia.Role{}, wrapPgxErr(err)
	}
	return role, nil
}

func (r *tenantReader) ListRolePermissionGrants(ctx context.Context, roleID amsonia.RoleID) ([]amsonia.RolePermissionGrant, error) {
	rows, err := r.tx.Query(ctx, `
		SELECT role_id, permission_key, scope, workspace_roles
		FROM amsonia.role_permission_grants
		WHERE tenant_id = $1 AND role_id = $2
	`, string(r.tenantID), string(roleID))
	if err != nil {
		return nil, wrapPgxErr(err)
	}
	defer rows.Close()
	var out []amsonia.RolePermissionGrant
	for rows.Next() {
		var g amsonia.RolePermissionGrant
		if err := rows.Scan(&g.RoleID, &g.Permission, &g.Scope, &g.WorkspaceRoles); err != nil {
			return nil, wrapPgxErr(err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
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
	rows, err := r.tx.Query(ctx, `
		SELECT role_id FROM amsonia.subject_roles
		WHERE tenant_id = $1 AND subject_id = $2
	`, string(r.tenantID), string(subjectID))
	if err != nil {
		return nil, wrapPgxErr(err)
	}
	defer rows.Close()
	var out []amsonia.RoleID
	for rows.Next() {
		var id amsonia.RoleID
		if err := rows.Scan(&id); err != nil {
			return nil, wrapPgxErr(err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *tenantReader) ListRoleSubjectIDs(ctx context.Context, roleID amsonia.RoleID) ([]amsonia.SubjectID, error) {
	rows, err := r.tx.Query(ctx, `
		SELECT subject_id FROM amsonia.subject_roles
		WHERE tenant_id = $1 AND role_id = $2
	`, string(r.tenantID), string(roleID))
	if err != nil {
		return nil, wrapPgxErr(err)
	}
	defer rows.Close()
	var out []amsonia.SubjectID
	for rows.Next() {
		var id amsonia.SubjectID
		if err := rows.Scan(&id); err != nil {
			return nil, wrapPgxErr(err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *tenantReader) HasGrantPath(ctx context.Context, from amsonia.SubjectID, to amsonia.SubjectID) (bool, error) {
	// Recursive CTE over grant edges within the tenant.
	sql := `
		WITH RECURSIVE path(tenant_id, grantor_id, grantee_id) AS (
			SELECT tenant_id, grantor_id, grantee_id FROM amsonia.grant_edges
			WHERE tenant_id = $1 AND grantor_id = $2
			UNION
			SELECT ge.tenant_id, ge.grantor_id, ge.grantee_id
			FROM amsonia.grant_edges ge
			JOIN path p ON p.tenant_id = ge.tenant_id AND p.grantee_id = ge.grantor_id
		)
		SELECT EXISTS (
			SELECT 1 FROM path WHERE grantee_id = $3
		)
	`
	var found bool
	if err := r.tx.QueryRow(ctx, sql, string(r.tenantID), string(from), string(to)).Scan(&found); err != nil {
		return false, wrapPgxErr(err)
	}
	return found, nil
}

func (r *tenantReader) HasAdministrator(ctx context.Context, controls amsonia.ControlPermissions) (bool, error) {
	sql := `
		SELECT EXISTS (
			SELECT 1
			FROM (
				SELECT DISTINCT subject_id
				FROM amsonia.subject_roles
				WHERE tenant_id = $1
			) subjects
			WHERE NOT EXISTS (
				SELECT 1 FROM unnest($2::text[]) AS control(key)
				WHERE NOT EXISTS (
					SELECT 1
					FROM amsonia.subject_roles sr
					JOIN amsonia.role_permission_grants rpg
					  ON rpg.tenant_id = sr.tenant_id AND rpg.role_id = sr.role_id
					WHERE sr.tenant_id = $1
					  AND sr.subject_id = subjects.subject_id
					  AND rpg.permission_key = control.key
					  AND rpg.scope = 'tenant'
				)
			)
		)
	`
	controlsArr := []string{
		string(controls.ManageRoles),
		string(controls.ManageGrants),
		string(controls.AssignRoles),
	}
	var found bool
	if err := r.tx.QueryRow(ctx, sql, string(r.tenantID), controlsArr).Scan(&found); err != nil {
		return false, wrapPgxErr(err)
	}
	return found, nil
}

func (r *tenantReader) IsBootstrapped(ctx context.Context) (bool, error) {
	var bootstrapped bool
	err := r.tx.QueryRow(ctx, `
		SELECT COALESCE(bootstrapped, FALSE) FROM amsonia.tenant_state
		WHERE tenant_id = $1
	`, string(r.tenantID)).Scan(&bootstrapped)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, wrapPgxErr(err)
	}
	return bootstrapped, nil
}

type tenantTx struct {
	tx       pgx.Tx
	tenantID amsonia.TenantID
}

func (tx *tenantTx) reader() *tenantReader {
	return &tenantReader{tx: tx.tx, tenantID: tx.tenantID}
}

func (tx *tenantTx) ListEffectiveGrants(ctx context.Context, subjectID amsonia.SubjectID, permission amsonia.PermissionKey) ([]amsonia.EffectiveGrant, error) {
	return tx.reader().ListEffectiveGrants(ctx, subjectID, permission)
}

func (tx *tenantTx) GetRole(ctx context.Context, roleID amsonia.RoleID) (amsonia.Role, error) {
	return tx.reader().GetRole(ctx, roleID)
}

func (tx *tenantTx) ListRolePermissionGrants(ctx context.Context, roleID amsonia.RoleID) ([]amsonia.RolePermissionGrant, error) {
	return tx.reader().ListRolePermissionGrants(ctx, roleID)
}

func (tx *tenantTx) ListSubjectRoleIDs(ctx context.Context, subjectID amsonia.SubjectID) ([]amsonia.RoleID, error) {
	return tx.reader().ListSubjectRoleIDs(ctx, subjectID)
}

func (tx *tenantTx) ListRoleSubjectIDs(ctx context.Context, roleID amsonia.RoleID) ([]amsonia.SubjectID, error) {
	return tx.reader().ListRoleSubjectIDs(ctx, roleID)
}

func (tx *tenantTx) HasGrantPath(ctx context.Context, from amsonia.SubjectID, to amsonia.SubjectID) (bool, error) {
	return tx.reader().HasGrantPath(ctx, from, to)
}

func (tx *tenantTx) HasAdministrator(ctx context.Context, controls amsonia.ControlPermissions) (bool, error) {
	return tx.reader().HasAdministrator(ctx, controls)
}

func (tx *tenantTx) IsBootstrapped(ctx context.Context) (bool, error) {
	return tx.reader().IsBootstrapped(ctx)
}

func (tx *tenantTx) InsertRole(ctx context.Context, role amsonia.Role) error {
	_, err := tx.tx.Exec(ctx, `
		INSERT INTO amsonia.roles (tenant_id, role_id, name, description, version)
		VALUES ($1, $2, $3, $4, $5)
	`, string(tx.tenantID), string(role.RoleID), role.Name, role.Description, role.Version)
	return wrapPgxErr(err)
}

func (tx *tenantTx) UpdateRole(ctx context.Context, role amsonia.Role, expectedVersion int64) error {
	tag, err := tx.tx.Exec(ctx, `
		UPDATE amsonia.roles
		SET name = $1, description = $2, version = $3, updated_at = now()
		WHERE tenant_id = $4 AND role_id = $5 AND version = $6
	`, role.Name, role.Description, role.Version, string(tx.tenantID), string(role.RoleID), expectedVersion)
	if err != nil {
		return wrapPgxErr(err)
	}
	if tag.RowsAffected() == 0 {
		// Distinguish missing role from version conflict.
		if _, err := tx.reader().GetRole(ctx, role.RoleID); err != nil {
			return err
		}
		return amsonia.ErrConflict
	}
	return nil
}

func (tx *tenantTx) TombstoneRole(ctx context.Context, roleID amsonia.RoleID, expectedVersion int64) error {
	tag, err := tx.tx.Exec(ctx, `
		UPDATE amsonia.roles
		SET deleted = TRUE, version = version + 1, updated_at = now()
		WHERE tenant_id = $1 AND role_id = $2 AND version = $3
	`, string(tx.tenantID), string(roleID), expectedVersion)
	if err != nil {
		return wrapPgxErr(err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := tx.reader().GetRole(ctx, roleID); err != nil {
			return err
		}
		return amsonia.ErrConflict
	}
	return nil
}

func (tx *tenantTx) InsertRolePermissionGrant(ctx context.Context, grant amsonia.RolePermissionGrant) (bool, error) {
	roles := grant.WorkspaceRoles
	if roles == nil {
		roles = []string{}
	}
	tag, err := tx.tx.Exec(ctx, `
		INSERT INTO amsonia.role_permission_grants (tenant_id, role_id, permission_key, scope, workspace_roles)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING
	`, string(tx.tenantID), string(grant.RoleID), string(grant.Permission), string(grant.Scope), roles)
	if err != nil {
		return false, wrapPgxErr(err)
	}
	return tag.RowsAffected() > 0, nil
}

func (tx *tenantTx) DeleteRolePermissionGrant(ctx context.Context, grant amsonia.RolePermissionGrant) (bool, error) {
	tag, err := tx.tx.Exec(ctx, `
		DELETE FROM amsonia.role_permission_grants
		WHERE tenant_id = $1 AND role_id = $2 AND permission_key = $3 AND scope = $4 AND workspace_roles = $5
	`, string(tx.tenantID), string(grant.RoleID), string(grant.Permission), string(grant.Scope), grant.WorkspaceRoles)
	if err != nil {
		return false, wrapPgxErr(err)
	}
	return tag.RowsAffected() > 0, nil
}

func (tx *tenantTx) InsertSubjectRoleGrant(ctx context.Context, grant amsonia.SubjectRoleGrant) (bool, error) {
	tag, err := tx.tx.Exec(ctx, `
		INSERT INTO amsonia.subject_roles (tenant_id, subject_id, role_id, grantor_subject_id, provenance, granted_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING
	`, string(tx.tenantID), string(grant.SubjectID), string(grant.RoleID),
		string(grant.GrantorSubjectID), string(grant.Provenance), grant.GrantedAt)
	if err != nil {
		return false, wrapPgxErr(err)
	}
	return tag.RowsAffected() > 0, nil
}

func (tx *tenantTx) DeleteSubjectRoleGrant(ctx context.Context, subjectID amsonia.SubjectID, roleID amsonia.RoleID) (amsonia.SubjectRoleGrant, bool, error) {
	var grant amsonia.SubjectRoleGrant
	err := tx.tx.QueryRow(ctx, `
		DELETE FROM amsonia.subject_roles
		WHERE tenant_id = $1 AND subject_id = $2 AND role_id = $3
		RETURNING grantor_subject_id, provenance, granted_at
	`, string(tx.tenantID), string(subjectID), string(roleID)).Scan(
		&grant.GrantorSubjectID, &grant.Provenance, &grant.GrantedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return amsonia.SubjectRoleGrant{}, false, nil
		}
		return amsonia.SubjectRoleGrant{}, false, wrapPgxErr(err)
	}
	grant.TenantID = tx.tenantID
	grant.SubjectID = subjectID
	grant.RoleID = roleID
	return grant, true, nil
}

func (tx *tenantTx) InsertGrantEdge(ctx context.Context, edge amsonia.GrantEdge) error {
	_, err := tx.tx.Exec(ctx, `
		INSERT INTO amsonia.grant_edges (tenant_id, grantor_id, grantee_id, role_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING
	`, string(tx.tenantID), string(edge.Grantor), string(edge.Grantee), string(edge.RoleID), edge.CreatedAt)
	return wrapPgxErr(err)
}

func (tx *tenantTx) DeleteGrantEdge(ctx context.Context, edge amsonia.GrantEdge) error {
	_, err := tx.tx.Exec(ctx, `
		DELETE FROM amsonia.grant_edges
		WHERE tenant_id = $1 AND grantor_id = $2 AND grantee_id = $3 AND role_id = $4
	`, string(tx.tenantID), string(edge.Grantor), string(edge.Grantee), string(edge.RoleID))
	return wrapPgxErr(err)
}

func (tx *tenantTx) InsertRoleVersion(ctx context.Context, snapshot amsonia.RoleVersion) error {
	grantsJSON, err := json.Marshal(snapshot.Grants)
	if err != nil {
		return amsonia.ErrInvalidInput
	}
	_, err = tx.tx.Exec(ctx, `
		INSERT INTO amsonia.role_versions
			(tenant_id, role_id, version, name, description, grants, deleted, created_at, created_by_subject, bootstrap_initiator)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, string(tx.tenantID), string(snapshot.RoleID), snapshot.Version, snapshot.Name, snapshot.Description,
		grantsJSON, snapshot.Deleted, snapshot.CreatedAt, string(snapshot.CreatedBySubjectID), snapshot.BootstrapInitiator)
	return wrapPgxErr(err)
}

func (tx *tenantTx) InsertMutationAudit(ctx context.Context, event amsonia.MutationAuditEvent) error {
	_, err := tx.tx.Exec(ctx, `
		INSERT INTO amsonia.audit_events
			(tenant_id, actor_subject, host_initiator, operation, phase, target_type, target_id, outcome, reason_code, request_id, role_version, at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, string(tx.tenantID), string(event.ActorSubjectID), event.HostInitiator, event.Operation, string(event.Phase),
		event.TargetType, event.TargetID, string(event.Outcome), event.ReasonCode, event.RequestID, event.RoleVersion, event.At)
	return wrapPgxErr(err)
}

func (tx *tenantTx) MarkBootstrapped(ctx context.Context, provenance amsonia.HostProvenance) error {
	_, err := tx.tx.Exec(ctx, `
		INSERT INTO amsonia.tenant_state (tenant_id, bootstrapped, bootstrap_initiator, bootstrap_at)
		VALUES ($1, TRUE, $2, $3)
		ON CONFLICT (tenant_id) DO UPDATE
		SET bootstrapped = TRUE, bootstrap_initiator = EXCLUDED.bootstrap_initiator, bootstrap_at = EXCLUDED.bootstrap_at
	`, string(tx.tenantID), provenance.Initiator, provenance.At)
	return wrapPgxErr(err)
}

type maintenanceTx struct {
	tx       pgx.Tx
	tenantID amsonia.TenantID
}

func (mt *maintenanceTx) ExportTenant(ctx context.Context) ([]byte, error) {
	var purged bool
	if err := mt.tx.QueryRow(ctx, `
		SELECT COALESCE(purged, FALSE)
		FROM amsonia.tenant_state
		WHERE tenant_id = $1
	`, string(mt.tenantID)).Scan(&purged); err != nil && err != pgx.ErrNoRows {
		return nil, wrapPgxErr(err)
	}
	if purged {
		return nil, amsonia.ErrNotFound
	}
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

	roleRows, err := mt.tx.Query(ctx, `
		SELECT tenant_id, role_id, name, description, version, deleted
		FROM amsonia.roles WHERE tenant_id = $1 ORDER BY role_id
	`, string(mt.tenantID))
	if err != nil {
		return nil, wrapPgxErr(err)
	}
	for roleRows.Next() {
		var r amsonia.Role
		if err := roleRows.Scan(&r.TenantID, &r.RoleID, &r.Name, &r.Description, &r.Version, &r.Deleted); err != nil {
			roleRows.Close()
			return nil, wrapPgxErr(err)
		}
		e.Roles = append(e.Roles, r)
	}
	roleRows.Close()
	if err := roleRows.Err(); err != nil {
		return nil, err
	}

	grantRows, err := mt.tx.Query(ctx, `
		SELECT role_id, permission_key, scope, workspace_roles
		FROM amsonia.role_permission_grants WHERE tenant_id = $1 ORDER BY role_id, permission_key
	`, string(mt.tenantID))
	if err != nil {
		return nil, wrapPgxErr(err)
	}
	for grantRows.Next() {
		var g amsonia.RolePermissionGrant
		if err := grantRows.Scan(&g.RoleID, &g.Permission, &g.Scope, &g.WorkspaceRoles); err != nil {
			grantRows.Close()
			return nil, wrapPgxErr(err)
		}
		e.Grants = append(e.Grants, g)
	}
	grantRows.Close()
	if err := grantRows.Err(); err != nil {
		return nil, err
	}

	verRows, err := mt.tx.Query(ctx, `
		SELECT tenant_id, role_id, version, name, description, grants, deleted, created_at, created_by_subject, bootstrap_initiator
		FROM amsonia.role_versions WHERE tenant_id = $1 ORDER BY role_id, version
	`, string(mt.tenantID))
	if err != nil {
		return nil, wrapPgxErr(err)
	}
	for verRows.Next() {
		var v amsonia.RoleVersion
		var grantsJSON []byte
		if err := verRows.Scan(&v.TenantID, &v.RoleID, &v.Version, &v.Name, &v.Description, &grantsJSON, &v.Deleted, &v.CreatedAt, &v.CreatedBySubjectID, &v.BootstrapInitiator); err != nil {
			verRows.Close()
			return nil, wrapPgxErr(err)
		}
		if err := json.Unmarshal(grantsJSON, &v.Grants); err != nil {
			verRows.Close()
			return nil, err
		}
		e.Versions = append(e.Versions, v)
	}
	verRows.Close()
	if err := verRows.Err(); err != nil {
		return nil, err
	}

	edgeRows, err := mt.tx.Query(ctx, `
		SELECT tenant_id, grantor_id, grantee_id, role_id, created_at
		FROM amsonia.grant_edges WHERE tenant_id = $1 ORDER BY grantor_id, grantee_id, role_id
	`, string(mt.tenantID))
	if err != nil {
		return nil, wrapPgxErr(err)
	}
	for edgeRows.Next() {
		var ed amsonia.GrantEdge
		if err := edgeRows.Scan(&ed.TenantID, &ed.Grantor, &ed.Grantee, &ed.RoleID, &ed.CreatedAt); err != nil {
			edgeRows.Close()
			return nil, wrapPgxErr(err)
		}
		e.Edges = append(e.Edges, ed)
	}
	edgeRows.Close()
	if err := edgeRows.Err(); err != nil {
		return nil, err
	}

	auditRows, err := mt.tx.Query(ctx, `
		SELECT tenant_id, actor_subject, host_initiator, operation, phase, target_type, target_id, outcome, reason_code, request_id, role_version, at
		FROM amsonia.audit_events WHERE tenant_id = $1 ORDER BY id
	`, string(mt.tenantID))
	if err != nil {
		return nil, wrapPgxErr(err)
	}
	for auditRows.Next() {
		var ev amsonia.MutationAuditEvent
		if err := auditRows.Scan(&ev.TenantID, &ev.ActorSubjectID, &ev.HostInitiator, &ev.Operation, &ev.Phase,
			&ev.TargetType, &ev.TargetID, &ev.Outcome, &ev.ReasonCode, &ev.RequestID, &ev.RoleVersion, &ev.At); err != nil {
			auditRows.Close()
			return nil, wrapPgxErr(err)
		}
		e.Audit = append(e.Audit, ev)
	}
	auditRows.Close()
	if err := auditRows.Err(); err != nil {
		return nil, err
	}

	return json.MarshalIndent(e, "", "  ")
}

func (mt *maintenanceTx) PurgeTenant(ctx context.Context, event amsonia.MutationAuditEvent) (amsonia.PurgeResult, error) {
	// Idempotent retry: the ledger row survives the purge.
	var canonical amsonia.MutationAuditEvent
	ledgerExists, err := mt.tenantPurgeLedgerExists(ctx, event)
	if err != nil {
		return amsonia.PurgeResult{}, err
	}
	if ledgerExists {
		canonical, err = mt.readPurgeLedger(ctx, event)
		if err != nil {
			return amsonia.PurgeResult{}, err
		}
		return amsonia.PurgeResult{
			Changed:          false,
			AlreadyCommitted: true,
			CanonicalEvent:   canonical,
		}, nil
	}
	var purged bool
	if err := mt.tx.QueryRow(ctx, `
		SELECT COALESCE(purged, FALSE)
		FROM amsonia.tenant_state
		WHERE tenant_id = $1
	`, string(mt.tenantID)).Scan(&purged); err != nil && err != pgx.ErrNoRows {
		return amsonia.PurgeResult{}, wrapPgxErr(err)
	}
	if purged {
		return amsonia.PurgeResult{}, amsonia.ErrNotFound
	}

	// Delete all tenant authorization data. RLS constrains every DELETE to
	// the current tenant.
	tables := []string{
		"amsonia.role_versions",
		"amsonia.grant_edges",
		"amsonia.subject_roles",
		"amsonia.role_permission_grants",
		"amsonia.audit_events",
		"amsonia.roles",
	}
	for _, table := range tables {
		if _, err := mt.tx.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id = $1", string(mt.tenantID)); err != nil {
			return amsonia.PurgeResult{}, wrapPgxErr(err)
		}
	}
	if _, err := mt.tx.Exec(ctx, `
		INSERT INTO amsonia.purge_ledger (tenant_id, request_id, operation, host_initiator, reason_code, committed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, string(mt.tenantID), event.RequestID, event.Operation, event.HostInitiator, event.ReasonCode, event.At); err != nil {
		return amsonia.PurgeResult{}, wrapPgxErr(err)
	}
	if _, err := mt.tx.Exec(ctx, `
		INSERT INTO amsonia.tenant_state (tenant_id, bootstrapped, purged, purged_at)
		VALUES ($1, FALSE, TRUE, $2)
		ON CONFLICT (tenant_id) DO UPDATE
		SET bootstrapped = FALSE, purged = TRUE, purged_at = EXCLUDED.purged_at
	`, string(mt.tenantID), event.At); err != nil {
		return amsonia.PurgeResult{}, wrapPgxErr(err)
	}
	return amsonia.PurgeResult{
		Changed:        true,
		CanonicalEvent: event,
	}, nil
}

func (mt *maintenanceTx) tenantPurgeLedgerExists(ctx context.Context, event amsonia.MutationAuditEvent) (bool, error) {
	var found bool
	err := mt.tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM amsonia.purge_ledger WHERE tenant_id = $1 AND request_id = $2
		)
	`, string(mt.tenantID), event.RequestID).Scan(&found)
	if err != nil {
		return false, wrapPgxErr(err)
	}
	return found, nil
}

func (mt *maintenanceTx) readPurgeLedger(ctx context.Context, event amsonia.MutationAuditEvent) (amsonia.MutationAuditEvent, error) {
	var canonical amsonia.MutationAuditEvent
	err := mt.tx.QueryRow(ctx, `
		SELECT tenant_id, request_id, operation, host_initiator, reason_code, committed_at
		FROM amsonia.purge_ledger WHERE tenant_id = $1 AND request_id = $2
	`, string(mt.tenantID), event.RequestID).Scan(
		&canonical.TenantID, &canonical.RequestID, &canonical.Operation, &canonical.HostInitiator,
		&canonical.ReasonCode, &canonical.At,
	)
	if err != nil {
		return amsonia.MutationAuditEvent{}, wrapPgxErr(err)
	}
	canonical.Phase = amsonia.AuditPhaseIntent
	canonical.TargetType = "tenant"
	canonical.TargetID = string(mt.tenantID)
	canonical.Outcome = amsonia.AuditOutcomeSuccess
	return canonical, nil
}
