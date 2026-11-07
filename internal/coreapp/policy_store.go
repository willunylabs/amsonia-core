package coreapp

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/willunylabs/amsonia-core"
)

type policyGuard struct {
	Actor, Target string
	Role          amsonia.RoleID
	Version       int64
	Assign, Grant bool
}
type policyGuardKey struct{}

func withPolicyGuard(ctx context.Context, guard policyGuard) context.Context {
	return context.WithValue(ctx, policyGuardKey{}, guard)
}

type corePolicyStore struct {
	amsonia.Store
}

type policyQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func administratorPermissions() []string {
	return []string{string(PermissionRoleManage), string(PermissionGrantManage), string(PermissionRoleAssign), string(PermissionMemberManage)}
}

// PolicyStore adds Core membership and active-administrator invariants without
// changing the reusable authorization kernel's host-independent contract.
func (s *Service) PolicyStore() amsonia.Store { return corePolicyStore{Store: s.store} }
func (store corePolicyStore) MutateTenant(ctx context.Context, tenant amsonia.TenantID, fn func(amsonia.TenantTx) error) error {
	return store.Store.MutateTenant(ctx, tenant, func(tx amsonia.TenantTx) error {
		guard, _ := ctx.Value(policyGuardKey{}).(policyGuard)
		reader, ok := tx.(policyQueryer)
		if !ok {
			return amsonia.ErrInvalidInput
		}
		if guard.Actor != "" || guard.Target != "" {
			rows, err := reader.Query(ctx, `SELECT account_id,status FROM amsonia.tenant_memberships
				WHERE tenant_id=$1 AND account_id=ANY($2::text[])`, string(tenant), []string{guard.Actor, guard.Target})
			if err != nil {
				return err
			}
			members, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Member, error) {
				var m Member
				err := row.Scan(&m.AccountID, &m.Status)
				return m, err
			})
			if err != nil {
				return err
			}
			statuses := make(map[string]string, len(members))
			for _, member := range members {
				statuses[member.AccountID] = member.Status
			}
			if guard.Actor != "" && statuses[guard.Actor] != "active" {
				return amsonia.ErrForbidden
			}
			if status, exists := statuses[guard.Target]; guard.Target != "" && (!exists || guard.Assign && status != "active") {
				return amsonia.ErrNotFound
			}
		}
		if guard.Version > 0 {
			role, err := tx.GetRole(ctx, guard.Role)
			if err != nil {
				return err
			}
			if role.Version != guard.Version {
				return amsonia.ErrConflict
			}
		}
		if guard.Grant {
			grants, err := tx.ListEffectiveGrants(ctx, amsonia.SubjectID(guard.Actor), PermissionGrantManage)
			if err != nil {
				return err
			}
			allowed := false
			for _, g := range grants {
				if err := g.Validate(PermissionGrantManage); err != nil {
					return err
				}
				allowed = allowed || g.Scope == amsonia.ScopeTenant
			}
			if !allowed {
				return amsonia.ErrForbidden
			}
		}
		wrapped := corePolicyTx{TenantTx: tx, query: reader, tenant: tenant}
		if err := fn(wrapped); err != nil {
			return err
		}
		allowed, err := wrapped.HasAdministrator(ctx, amsonia.ControlPermissions{})
		if err != nil {
			return err
		}
		if !allowed {
			return amsonia.ErrLastAdministrator
		}
		return nil
	})
}

type corePolicyTx struct {
	amsonia.TenantTx
	query  policyQueryer
	tenant amsonia.TenantID
}

func (tx corePolicyTx) HasAdministrator(ctx context.Context, _ amsonia.ControlPermissions) (bool, error) {
	return hasActiveAdministrator(ctx, tx.query, tx.tenant, "")
}

func hasActiveAdministrator(ctx context.Context, query policyQueryer, tenant amsonia.TenantID, exclude string) (bool, error) {
	permissions := administratorPermissions()
	rows, err := query.Query(ctx, `SELECT s.subject_id,s.role_id,g.permission_key,g.scope,g.workspace_roles
		FROM amsonia.subject_roles s
		JOIN amsonia.roles r USING (tenant_id,role_id)
		JOIN amsonia.role_permission_grants g USING (tenant_id,role_id)
		JOIN amsonia.tenant_memberships m ON m.tenant_id=s.tenant_id AND m.account_id=s.subject_id
		WHERE s.tenant_id=$1 AND m.status='active' AND NOT r.deleted
		AND s.subject_id<>$3 AND g.permission_key=ANY($2::text[])`, string(tenant), permissions, exclude)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	coverage := make(map[amsonia.SubjectID]map[amsonia.PermissionKey]bool)
	for rows.Next() {
		var subject amsonia.SubjectID
		var grant amsonia.EffectiveGrant
		if err := rows.Scan(&subject, &grant.RoleID, &grant.Permission, &grant.Scope, &grant.WorkspaceRoles); err != nil {
			return false, err
		}
		// Validate every returned control grant before accepting any candidate.
		// An invalid persisted row must not be hidden by a valid administrator.
		if err := grant.Validate(grant.Permission); err != nil {
			return false, err
		}
		if grant.Scope == amsonia.ScopeTenant {
			if coverage[subject] == nil {
				coverage[subject] = make(map[amsonia.PermissionKey]bool)
			}
			coverage[subject][grant.Permission] = true
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, grants := range coverage {
		if len(grants) == len(permissions) {
			return true, nil
		}
	}
	return false, nil
}

// RecordSecurityEvent stores failed kernel mutations in their own transaction,
// after rollback. Request IDs make retries idempotent at the audit boundary.
func (s *Service) RecordSecurityEvent(ctx context.Context, event amsonia.MutationAuditEvent) error {
	slog.WarnContext(ctx, "security audit", "tenant", event.TenantID, "operation", event.Operation, "outcome", event.Outcome, "request_id", event.RequestID)
	return s.store.RunTenant(ctx, event.TenantID, false, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO amsonia.audit_events
  (tenant_id,actor_subject,host_initiator,operation,phase,target_type,target_id,outcome,reason_code,request_id,role_version,at)
  VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT DO NOTHING`, string(event.TenantID), string(event.ActorSubjectID), event.HostInitiator, event.Operation, string(event.Phase), event.TargetType, event.TargetID, string(event.Outcome), event.ReasonCode, event.RequestID, event.RoleVersion, event.At)
		return err
	})
}
