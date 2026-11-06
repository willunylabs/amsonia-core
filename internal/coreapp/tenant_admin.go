package coreapp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/willunylabs/amsonia-core"
)

type requestIDKey struct{}

func mutationMetadata(ctx context.Context) amsonia.MutationMetadata {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return amsonia.MutationMetadata{ReasonCode: "console_request", RequestID: id}
}

// Tenant writes recheck authority under the same advisory lock as Manager.
func requireTenantPermission(ctx context.Context, tx pgx.Tx, tenant, actor string, permission amsonia.PermissionKey) error {
	rows, err := tx.Query(ctx, `SELECT g.role_id, g.permission_key, g.scope, g.workspace_roles
	    FROM amsonia.subject_roles s JOIN amsonia.roles r USING (tenant_id, role_id)
	    JOIN amsonia.role_permission_grants g USING (tenant_id, role_id)
	    JOIN amsonia.tenant_memberships m ON m.tenant_id = s.tenant_id AND m.account_id = s.subject_id
	    WHERE s.tenant_id = $1 AND s.subject_id = $2 AND g.permission_key = $3 AND NOT r.deleted AND m.status = 'active'`, tenant, actor, string(permission))
	if err != nil {
		return err
	}
	defer rows.Close()
	allowed := false
	for rows.Next() {
		var grant amsonia.EffectiveGrant
		if err := rows.Scan(&grant.RoleID, &grant.Permission, &grant.Scope, &grant.WorkspaceRoles); err != nil {
			return err
		}
		if err := grant.Validate(permission); err != nil {
			return err
		}
		allowed = allowed || grant.Scope == amsonia.ScopeTenant
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !allowed {
		return amsonia.ErrForbidden
	}
	return nil
}

func (s *Service) tenantAudit(ctx context.Context, tx pgx.Tx, tenant, actor, operation, targetType, target string, changed bool) error {
	outcome := amsonia.AuditOutcomeSuccess
	if !changed {
		outcome = amsonia.AuditOutcomeNoop
	}
	meta := mutationMetadata(ctx)
	_, err := tx.Exec(ctx, `INSERT INTO amsonia.audit_events
	    (tenant_id, actor_subject, operation, phase, target_type, target_id, outcome, reason_code, request_id, at)
	    VALUES ($1, $2, $3, 'result', $4, $5, $6, $7, $8, $9)`, tenant, actor, operation, targetType, target, string(outcome), meta.ReasonCode, meta.RequestID, s.now())
	return err
}

// InvitationPreview is readable only with the secret invitation token.
type InvitationPreview struct {
	TenantID      string    `json:"tenant_id"`
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	TenantName    string    `json:"tenant_name"`
	ExpiresAt     time.Time `json:"expires_at"`
	AccountExists bool      `json:"account_exists"`
}

type InvitationItem struct {
	Invitation
	Status string `json:"status"`
}

type CreatedInvitation struct {
	Invitation
	Token string `json:"token"`
}

func (s *Service) PreviewInvitation(ctx context.Context, token string) (InvitationPreview, error) {
	if !strings.HasPrefix(token, "inv_") || len(token) < 40 || len(token) > 160 {
		return InvitationPreview{}, amsonia.ErrNotFound
	}
	var item InvitationPreview
	err := s.pool.QueryRow(ctx, `SELECT tenant_id, invitation_id, display_email, tenant_name, expires_at, account_exists FROM amsonia.inspect_invitation($1)`, tokenHash(token)).
		Scan(&item.TenantID, &item.ID, &item.Email, &item.TenantName, &item.ExpiresAt, &item.AccountExists)
	if err != nil {
		return InvitationPreview{}, accountNotFound(err)
	}
	if err := s.allowOrganization(ctx, item.TenantID); err != nil {
		return InvitationPreview{}, err
	}
	return item, nil
}

func (s *Service) CreateInvitation(ctx context.Context, tenant, actor, email string) (CreatedInvitation, error) {
	normalized, err := normalizeEmail(email)
	if err != nil {
		return CreatedInvitation{}, err
	}
	id, err := randomID("invite_", 18)
	if err != nil {
		return CreatedInvitation{}, err
	}
	token, err := randomID("inv_", 32)
	if err != nil {
		return CreatedInvitation{}, err
	}
	item := CreatedInvitation{Invitation: Invitation{ID: id, Email: strings.TrimSpace(email), CreatedAt: s.now(), ExpiresAt: s.now().Add(7 * 24 * time.Hour)}, Token: token}
	err = s.store.RunTenant(ctx, amsonia.TenantID(tenant), false, func(tx pgx.Tx) error {
		if err := requireTenantPermission(ctx, tx, tenant, actor, PermissionMemberManage); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM amsonia.tenant_memberships m JOIN amsonia.accounts a ON a.account_id = m.account_id WHERE m.tenant_id = $1 AND a.normalized_email = $2)`, tenant, normalized).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return amsonia.ErrConflict
		}
		// Reissuing replaces the earlier unaccepted link for this email.
		if _, err := tx.Exec(ctx, `UPDATE amsonia.member_invitations SET revoked_at = $1 WHERE tenant_id = $2 AND normalized_email = $3 AND accepted_at IS NULL AND revoked_at IS NULL`, s.now(), tenant, normalized); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO amsonia.member_invitations
		    (tenant_id, invitation_id, normalized_email, display_email, token_hash, created_by, expires_at, created_at)
		    VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, tenant, id, normalized, item.Email, tokenHash(token), actor, item.ExpiresAt, item.CreatedAt); err != nil {
			return err
		}
		return s.tenantAudit(ctx, tx, tenant, actor, "invitation.create", "invitation", id, true)
	})
	if err != nil {
		return CreatedInvitation{}, err
	}
	return item, nil
}

func (s *Service) RevokeInvitation(ctx context.Context, tenant, actor, id string) error {
	return s.store.RunTenant(ctx, amsonia.TenantID(tenant), false, func(tx pgx.Tx) error {
		if err := requireTenantPermission(ctx, tx, tenant, actor, PermissionMemberManage); err != nil {
			return err
		}
		var accepted, revoked *time.Time
		if err := tx.QueryRow(ctx, `SELECT accepted_at, revoked_at FROM amsonia.member_invitations WHERE tenant_id=$1 AND invitation_id=$2 FOR UPDATE`, tenant, id).Scan(&accepted, &revoked); err != nil {
			return accountNotFound(err)
		}
		if accepted != nil {
			return amsonia.ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE amsonia.member_invitations SET revoked_at=COALESCE(revoked_at,$1) WHERE tenant_id=$2 AND invitation_id=$3`, s.now(), tenant, id); err != nil {
			return err
		}
		return s.tenantAudit(ctx, tx, tenant, actor, "invitation.revoke", "invitation", id, revoked == nil)
	})
}

// AcceptInvitation verifies an existing account's password; it never replaces
// existing credentials. New accounts receive no roles until an admin assigns one.
func (s *Service) AcceptInvitation(ctx context.Context, token, password string) (Account, error) {
	preview, err := s.PreviewInvitation(ctx, token)
	if err != nil {
		return Account{}, err
	}
	if !preview.AccountExists && !validBootstrapPassword(password) {
		return Account{}, amsonia.ErrInvalidInput
	}
	var newHash string
	if !preview.AccountExists {
		newHash, err = s.passwords.Hash(password)
		if err != nil {
			return Account{}, amsonia.ErrInvalidInput
		}
	}
	newID, err := randomID("acc_", 18)
	if err != nil {
		return Account{}, err
	}
	var account Account
	invalidPassword := false
	err = s.store.RunTenant(ctx, amsonia.TenantID(preview.TenantID), false, func(tx pgx.Tx) error {
		var email string
		if err := tx.QueryRow(ctx, `SELECT normalized_email FROM amsonia.member_invitations WHERE tenant_id=$1 AND invitation_id=$2 AND token_hash=$3 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $4 FOR UPDATE`, preview.TenantID, preview.ID, tokenHash(token), s.now()).Scan(&email); err != nil {
			return accountNotFound(err)
		}
		var hash, status string
		var locked *time.Time
		err := tx.QueryRow(ctx, `SELECT account_id,email,password_hash,status,locked_until,created_at,EXISTS(SELECT 1 FROM amsonia.system_administrators sa WHERE sa.account_id=accounts.account_id) FROM amsonia.accounts WHERE normalized_email=$1 FOR UPDATE`, email).Scan(&account.ID, &account.Email, &hash, &status, &locked, &account.CreatedAt, &account.SystemAdmin)
		if errors.Is(err, pgx.ErrNoRows) {
			if newHash == "" {
				return amsonia.ErrConflict
			}
			account = Account{ID: newID, Email: preview.Email, CreatedAt: s.now()}
			if _, err := tx.Exec(ctx, `INSERT INTO amsonia.accounts(account_id,email,normalized_email,password_hash,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5)`, account.ID, account.Email, email, newHash, account.CreatedAt); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if status != "active" {
				return ErrInvalidCredentials
			}
			if locked != nil && locked.After(s.now()) {
				return ErrAccountLocked
			}
			if !s.passwords.Verify(password, hash) {
				invalidPassword = true
				return s.failedPassword(ctx, tx, account.ID)
			}
		}
		var memberStatus string
		err = tx.QueryRow(ctx, `SELECT status FROM amsonia.tenant_memberships WHERE tenant_id=$1 AND account_id=$2`, preview.TenantID, account.ID).Scan(&memberStatus)
		if err == nil && memberStatus != "active" {
			return amsonia.ErrConflict
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO amsonia.tenant_memberships(tenant_id,account_id,status,joined_at) VALUES($1,$2,'active',$3) ON CONFLICT DO NOTHING`, preview.TenantID, account.ID, s.now()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE amsonia.member_invitations SET accepted_at=$1 WHERE tenant_id=$2 AND invitation_id=$3`, s.now(), preview.TenantID, preview.ID); err != nil {
			return err
		}
		return s.tenantAudit(ctx, tx, preview.TenantID, account.ID, "invitation.accept", "invitation", preview.ID, true)
	})
	if err != nil {
		return Account{}, err
	}
	if invalidPassword {
		return Account{}, ErrInvalidCredentials
	}
	return account, nil
}

func (s *Service) MemberStatus(ctx context.Context, tenant, account string) (string, error) {
	var status string
	err := s.store.RunTenant(ctx, amsonia.TenantID(tenant), true, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM amsonia.tenant_memberships WHERE tenant_id=$1 AND account_id=$2`, tenant, account).Scan(&status)
	})
	return status, accountNotFound(err)
}

func (s *Service) ManageMember(ctx context.Context, tenant, actor, account, status string, remove bool) error {
	if amsonia.SubjectID(account).Validate() != nil || (!remove && status != "active" && status != "suspended") {
		return amsonia.ErrInvalidInput
	}
	return s.store.RunTenant(ctx, amsonia.TenantID(tenant), false, func(tx pgx.Tx) error {
		if err := requireTenantPermission(ctx, tx, tenant, actor, PermissionMemberManage); err != nil {
			return err
		}
		var old string
		if err := tx.QueryRow(ctx, `SELECT status FROM amsonia.tenant_memberships WHERE tenant_id=$1 AND account_id=$2 FOR UPDATE`, tenant, account).Scan(&old); err != nil {
			return accountNotFound(err)
		}
		if remove || status == "suspended" {
			var hasAdmin bool
			permissions := administratorPermissions()
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT s.subject_id FROM amsonia.subject_roles s JOIN amsonia.roles r USING(tenant_id,role_id) JOIN amsonia.role_permission_grants g USING(tenant_id,role_id) JOIN amsonia.tenant_memberships m ON m.tenant_id=s.tenant_id AND m.account_id=s.subject_id WHERE s.tenant_id=$1 AND s.subject_id<>$2 AND NOT r.deleted AND m.status='active' AND g.scope='tenant' AND g.permission_key=ANY($3::text[]) GROUP BY s.subject_id HAVING count(DISTINCT g.permission_key)=$4)`, tenant, account, permissions, len(permissions)).Scan(&hasAdmin); err != nil {
				return err
			}
			if !hasAdmin {
				return amsonia.ErrLastAdministrator
			}
		}
		op := "member.resume"
		if status == "suspended" {
			op = "member.suspend"
		}
		if remove {
			op = "member.remove"
			if _, err := tx.Exec(ctx, `DELETE FROM amsonia.grant_edges WHERE tenant_id=$1 AND grantee_id=$2`, tenant, account); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM amsonia.subject_roles WHERE tenant_id=$1 AND subject_id=$2`, tenant, account); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM amsonia.tenant_memberships WHERE tenant_id=$1 AND account_id=$2`, tenant, account); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE amsonia.tenant_memberships SET status=$1 WHERE tenant_id=$2 AND account_id=$3`, status, tenant, account); err != nil {
			return err
		}
		return s.tenantAudit(ctx, tx, tenant, actor, op, "member", account, remove || old != status)
	})
}
