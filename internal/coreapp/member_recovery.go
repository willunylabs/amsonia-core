package coreapp

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/willunylabs/amsonia-core"
)

// PasswordRecovery is disclosed once to the issuing system administrator.
// Only the hash of Token is persisted; the link is delivered privately by them.
type PasswordRecovery struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
	Token     string    `json:"token"`
}

type PasswordRecoveryPreview struct {
	Email      string    `json:"email"`
	TenantName string    `json:"tenant_name"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// Account credentials span organizations. A tenant manager alone must never be
// able to reset a global account, even when that account is their tenant member.
func (s *Service) CreatePasswordRecovery(ctx context.Context, tenant, actor, target, password string) (PasswordRecovery, error) {
	if err := s.allowOrganization(ctx, tenant); err != nil {
		return PasswordRecovery{}, err
	}
	id, err := randomID("recovery_", 18)
	if err != nil {
		return PasswordRecovery{}, err
	}
	token, err := randomID("pr_", 32)
	if err != nil {
		return PasswordRecovery{}, err
	}
	item := PasswordRecovery{ID: id, Token: token, ExpiresAt: s.now().Add(30 * time.Minute)}
	invalidPassword := false
	err = s.store.RunTenant(ctx, amsonia.TenantID(tenant), false, func(tx pgx.Tx) error {
		if err := requireTenantPermission(ctx, tx, tenant, actor, PermissionMemberManage); err != nil {
			return err
		}
		var hash string
		var locked *time.Time
		// Lock accounts before recovery rows, as password changes and completion do.
		if err := tx.QueryRow(ctx, `SELECT a.password_hash, a.locked_until FROM amsonia.accounts a
   JOIN amsonia.system_administrators sa USING(account_id)
   WHERE a.account_id=$1 AND a.status='active' FOR UPDATE OF a`, actor).Scan(&hash, &locked); err != nil {
			if accountNotFound(err) == amsonia.ErrNotFound {
				return amsonia.ErrForbidden
			}
			return err
		}
		if locked != nil && locked.After(s.now()) {
			return ErrAccountLocked
		}
		if !s.passwords.Verify(password, hash) {
			invalidPassword = true
			return s.failedPassword(ctx, tx, actor)
		}
		if err := tx.QueryRow(ctx, `SELECT a.email FROM amsonia.accounts a
   JOIN amsonia.tenant_memberships m USING(account_id)
   WHERE a.account_id=$1 AND a.status='active' AND m.tenant_id=$2 AND m.status='active'
   AND NOT EXISTS(SELECT 1 FROM amsonia.system_administrators sa WHERE sa.account_id=a.account_id)
   FOR UPDATE OF a`, target, tenant).Scan(&item.Email); err != nil {
			return accountNotFound(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE amsonia.member_password_recoveries SET revoked_at=$2
   WHERE account_id=$1 AND consumed_at IS NULL AND revoked_at IS NULL`, target, s.now()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO amsonia.member_password_recoveries
   (recovery_id,account_id,issued_tenant_id,created_by,token_hash,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`,
			item.ID, target, tenant, actor, tokenHash(token), s.now(), item.ExpiresAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE amsonia.accounts SET failed_login_count=0,locked_until=NULL WHERE account_id=$1`, actor); err != nil {
			return err
		}
		return s.tenantAudit(ctx, tx, tenant, actor, "account.password_recovery.create", "account", target, true)
	})
	if err != nil {
		return PasswordRecovery{}, err
	}
	if invalidPassword {
		return PasswordRecovery{}, ErrInvalidCredentials
	}
	return item, nil
}

func validRecoveryToken(token string) bool {
	return strings.HasPrefix(token, "pr_") && len(token) == 46
}

func (s *Service) recoveryTarget(ctx context.Context, token string) (tenant, account string, err error) {
	if !validRecoveryToken(token) {
		return "", "", amsonia.ErrNotFound
	}
	err = s.pool.QueryRow(ctx, `SELECT issued_tenant_id,account_id FROM amsonia.member_password_recoveries
  WHERE token_hash=$1 AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at>$2`, tokenHash(token), s.now()).Scan(&tenant, &account)
	if err != nil {
		return "", "", accountNotFound(err)
	}
	if err := s.allowOrganization(ctx, tenant); err != nil {
		return "", "", err
	}
	return tenant, account, nil
}

func (s *Service) PreviewPasswordRecovery(ctx context.Context, token string) (PasswordRecoveryPreview, error) {
	tenant, account, err := s.recoveryTarget(ctx, token)
	if err != nil {
		return PasswordRecoveryPreview{}, err
	}
	var item PasswordRecoveryPreview
	err = s.store.RunTenant(ctx, amsonia.TenantID(tenant), true, func(tx pgx.Tx) error {
		return accountNotFound(tx.QueryRow(ctx, `SELECT a.email,o.name,r.expires_at
   FROM amsonia.member_password_recoveries r JOIN amsonia.accounts a USING(account_id)
   JOIN amsonia.tenant_memberships m USING(account_id) CROSS JOIN amsonia.organization_settings() o
   WHERE r.token_hash=$1 AND r.account_id=$2 AND m.tenant_id=$3 AND m.status='active' AND a.status='active'
   AND r.consumed_at IS NULL AND r.revoked_at IS NULL AND r.expires_at>$4
   AND NOT EXISTS(SELECT 1 FROM amsonia.system_administrators sa WHERE sa.account_id=a.account_id)`,
			tokenHash(token), account, tenant, s.now()).Scan(&item.Email, &item.TenantName, &item.ExpiresAt))
	})
	return item, err
}

func (s *Service) CompletePasswordRecovery(ctx context.Context, token, password string) error {
	tenant, account, err := s.recoveryTarget(ctx, token)
	if err != nil {
		return err
	}
	if !validBootstrapPassword(password) {
		return amsonia.ErrInvalidInput
	}
	hash, err := s.passwords.Hash(password)
	if err != nil {
		return err
	}
	return s.store.RunTenant(ctx, amsonia.TenantID(tenant), false, func(tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx, `SELECT a.account_id FROM amsonia.accounts a
   JOIN amsonia.tenant_memberships m USING(account_id) CROSS JOIN amsonia.organization_settings() o
   WHERE a.account_id=$1 AND a.status='active' AND m.tenant_id=$2 AND m.status='active'
   AND NOT EXISTS(SELECT 1 FROM amsonia.system_administrators sa WHERE sa.account_id=a.account_id)
   FOR UPDATE OF a`, account, tenant).Scan(&id); err != nil {
			return accountNotFound(err)
		}
		tag, err := tx.Exec(ctx, `UPDATE amsonia.member_password_recoveries SET consumed_at=$2
   WHERE token_hash=$1 AND account_id=$3 AND issued_tenant_id=$4 AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at>$2`, tokenHash(token), s.now(), account, tenant)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return amsonia.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `UPDATE amsonia.accounts SET password_hash=$1,failed_login_count=0,locked_until=NULL,updated_at=$2 WHERE account_id=$3`, hash, s.now(), account); err != nil {
			return err
		}
		if err := revokeCredentialSessions(ctx, tx, account); err != nil {
			return err
		}
		return s.tenantAudit(ctx, tx, tenant, account, "account.password_recovery.complete", "account", account, true)
	})
}
