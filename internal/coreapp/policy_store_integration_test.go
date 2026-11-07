//go:build postgres

package coreapp

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/willunylabs/amsonia-core"
)

// Count policy/membership reads while still delegating to the real PostgreSQL
// transaction. The bound must not grow with ordinary tenant membership.
type countingPolicyStore struct {
	amsonia.Store
	reads *int
}

func (s countingPolicyStore) MutateTenant(ctx context.Context, tenant amsonia.TenantID, fn func(amsonia.TenantTx) error) error {
	return s.Store.MutateTenant(ctx, tenant, func(tx amsonia.TenantTx) error {
		return fn(countingPolicyTx{TenantTx: tx, query: tx.(policyQueryer), reads: s.reads})
	})
}

type countingPolicyTx struct {
	amsonia.TenantTx
	query policyQueryer
	reads *int
}

func (tx countingPolicyTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	*tx.reads++
	return tx.query.Query(ctx, sql, args...)
}

func (tx countingPolicyTx) ListEffectiveGrants(ctx context.Context, subject amsonia.SubjectID, permission amsonia.PermissionKey) ([]amsonia.EffectiveGrant, error) {
	*tx.reads++
	return tx.TenantTx.ListEffectiveGrants(ctx, subject, permission)
}

func TestCoreAdministratorChecksStayBoundedAndFailClosed(t *testing.T) {
	s, _, _, owner := coreFixture(t)
	ctx := context.Background()
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "admin@example.test", Password: "administrator-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Bounded administrator checks"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO amsonia.accounts(account_id,email,normalized_email,password_hash)
		SELECT 'bulk_'||n, 'bulk_'||n||'@example.test', 'bulk_'||n||'@example.test', 'not-a-login-hash'
		FROM generate_series(1,64) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO amsonia.tenant_memberships(tenant_id,account_id,status)
		SELECT $1,account_id,'active' FROM amsonia.accounts WHERE account_id LIKE 'bulk_%'`, tenant.ID); err != nil {
		t.Fatal(err)
	}
	reads := 0
	catalog, controls, err := CoreCatalog()
	if err != nil {
		t.Fatal(err)
	}
	store := corePolicyStore{Store: countingPolicyStore{Store: s.store, reads: &reads}}
	manager, err := amsonia.NewManager(catalog, store, testWorkspaces{}, controls, s, amsonia.RealClock{})
	if err != nil {
		t.Fatal(err)
	}
	actor := amsonia.Principal{TenantID: amsonia.TenantID(tenant.ID), SubjectID: amsonia.SubjectID(admin.ID)}
	guarded := withPolicyGuard(ctx, policyGuard{Actor: admin.ID})
	metadata := amsonia.MutationMetadata{ReasonCode: "bounded_admin_test", RequestID: "last-admin-revoke"}
	_, err = manager.RevokePermission(guarded, actor, metadata, amsonia.RevokePermissionInput{
		RoleID: "role_owner", ExpectedVersion: 1, Permission: PermissionMemberManage, Scope: amsonia.ScopeTenant,
	})
	if !errors.Is(err, amsonia.ErrLastAdministrator) {
		t.Fatalf("last active administrator was not protected: %v", err)
	}
	if reads > 8 {
		t.Fatalf("administrator check made %d policy/membership reads for 65 members; expected a constant bound", reads)
	}
	var grants int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM amsonia.role_permission_grants
		WHERE tenant_id=$1 AND role_id='role_owner' AND permission_key=$2`, tenant.ID, string(PermissionMemberManage)).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("rejected revoke did not roll back: grants=%d error=%v", grants, err)
	}

	// The batched read must validate a malformed control grant even if another
	// subject already has all four valid administrator permissions.
	if _, err := owner.Exec(ctx, `INSERT INTO amsonia.roles(tenant_id,role_id,name) VALUES($1,'corrupt','Corrupt policy')`, tenant.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO amsonia.subject_roles(tenant_id,subject_id,role_id) VALUES($1,'bulk_1','corrupt')`, tenant.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO amsonia.role_permission_grants(tenant_id,role_id,permission_key,scope,workspace_roles)
		VALUES($1,'corrupt',$2,'tenant',ARRAY['invalid-role'])`, tenant.ID, string(PermissionMemberManage)); err != nil {
		t.Fatal(err)
	}
	_, _, err = manager.CreateRole(guarded, actor, amsonia.MutationMetadata{ReasonCode: "bounded_admin_test", RequestID: "invalid-control-grant"}, amsonia.CreateRoleInput{RoleID: "must_not_commit", Name: "Must not commit"})
	if !errors.Is(err, amsonia.ErrInvalidPolicy) {
		t.Fatalf("invalid persisted control grant was hidden by a valid administrator: %v", err)
	}
	// Give the malformed candidate all four controls. Both member-removal
	// entrypoints must apply the same validation as policy mutations.
	if _, err := owner.Exec(ctx, `INSERT INTO amsonia.role_permission_grants(tenant_id,role_id,permission_key,scope,workspace_roles)
		SELECT $1,'corrupt',permission,'tenant',ARRAY[]::text[] FROM unnest($2::text[]) permission
		WHERE permission<>$3`, tenant.ID, administratorPermissions(), string(PermissionMemberManage)); err != nil {
		t.Fatal(err)
	}
	for _, remove := range []bool{false, true} {
		if err := s.ManageMember(ctx, tenant.ID, admin.ID, admin.ID, "suspended", remove); !errors.Is(err, amsonia.ErrInvalidPolicy) {
			t.Fatalf("malformed candidate permitted administrator removal=%t: %v", remove, err)
		}
		status, err := s.MemberStatus(ctx, tenant.ID, admin.ID)
		if err != nil || status != "active" {
			t.Fatalf("administrator removal did not roll back: %s %v", status, err)
		}
	}
	var exists bool
	if err := owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM amsonia.roles WHERE tenant_id=$1 AND role_id='must_not_commit')`, tenant.ID).Scan(&exists); err != nil || exists {
		t.Fatalf("invalid policy mutation committed: exists=%t error=%v", exists, err)
	}
}

func TestCanceledPolicyMutationStillPersistsFailureAudit(t *testing.T) {
	s, manager, _, owner := coreFixture(t)
	ctx := context.Background()
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "admin@example.test", Password: "administrator-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Canceled mutation audit"})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = manager.CreateRole(canceled,
		amsonia.Principal{TenantID: amsonia.TenantID(tenant.ID), SubjectID: amsonia.SubjectID(admin.ID)},
		amsonia.MutationMetadata{ReasonCode: "canceled_request", RequestID: "canceled-policy-request"},
		amsonia.CreateRoleInput{RoleID: "not_created", Name: "Canceled"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was masked: %v", err)
	}
	var events int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM amsonia.audit_events
		WHERE tenant_id=$1 AND actor_subject=$2 AND operation='role.create' AND outcome='failed'
		AND target_id='not_created' AND request_id='canceled-policy-request'`, tenant.ID, admin.ID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("canceled mutation audit was not persisted: events=%d error=%v", events, err)
	}
}
