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

// PolicyStore adds Core membership and active-administrator invariants without
// changing the reusable authorization kernel's host-independent contract.
func (s *Service) PolicyStore() amsonia.Store { return corePolicyStore{Store: s.store} }
func (store corePolicyStore) MutateTenant(ctx context.Context, tenant amsonia.TenantID, fn func(amsonia.TenantTx) error) error {
	return store.Store.MutateTenant(ctx, tenant, func(tx amsonia.TenantTx) error {
		guard, _ := ctx.Value(policyGuardKey{}).(policyGuard)
		reader, ok := tx.(interface {
			Query(context.Context, string, ...any) (pgx.Rows, error)
		})
		if !ok {
			return amsonia.ErrInvalidInput
		}
		rows, err := reader.Query(ctx, `SELECT account_id,status FROM amsonia.tenant_memberships WHERE tenant_id=$1`, string(tenant))
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
		active := map[amsonia.SubjectID]bool{}
		exists := map[string]bool{}
		for _, member := range members {
			exists[member.AccountID] = true
			if member.Status == "active" {
				active[amsonia.SubjectID(member.AccountID)] = true
			}
		}
		if guard.Actor != "" && !active[amsonia.SubjectID(guard.Actor)] {
			return amsonia.ErrForbidden
		}
		if guard.Target != "" && (!exists[guard.Target] || guard.Assign && !active[amsonia.SubjectID(guard.Target)]) {
			return amsonia.ErrNotFound
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
		wrapped := corePolicyTx{TenantTx: tx, active: active}
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
	active map[amsonia.SubjectID]bool
}

func (tx corePolicyTx) HasAdministrator(ctx context.Context, _ amsonia.ControlPermissions) (bool, error) {
	for subject := range tx.active {
		full := true
		for _, key := range []amsonia.PermissionKey{PermissionRoleManage, PermissionGrantManage, PermissionRoleAssign, PermissionMemberManage} {
			grants, err := tx.ListEffectiveGrants(ctx, subject, key)
			if err != nil {
				return false, err
			}
			allowed := false
			for _, g := range grants {
				if err := g.Validate(key); err != nil {
					return false, err
				}
				allowed = allowed || g.Scope == amsonia.ScopeTenant
			}
			full = full && allowed
		}
		if full {
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
