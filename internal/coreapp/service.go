// Package coreapp implements the standalone Amsonia Core identity and tenant
// application service around the public authorization kernel.
package coreapp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/internal/security"
	"github.com/willunylabs/amsonia-core/postgres"
)

var (
	ErrInvalidCredentials = errors.New("coreapp: invalid credentials")
	ErrBootstrapComplete  = errors.New("coreapp: system administrator already exists")
	ErrAccountLocked      = errors.New("coreapp: account temporarily locked")
	ErrSessionInvalid     = errors.New("coreapp: session invalid")
)

type Account struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	SystemAdmin bool      `json:"system_admin"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at,omitempty"`
}

type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}

type Member struct {
	AccountID string    `json:"account_id"`
	Email     string    `json:"email"`
	Status    string    `json:"status"`
	JoinedAt  time.Time `json:"joined_at"`
}

type Invitation struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type Session struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	Account      Account   `json:"account"`
}

type BootstrapInput struct {
	Email    string
	Password string
}

type LoginInput struct {
	Email         string
	Password      string
	RemoteAddress string
	UserAgent     string
}

type RefreshInput struct {
	RefreshToken  string
	RemoteAddress string
	UserAgent     string
}

type CreateTenantInput struct {
	Name string
}

// ProvisionMemberInput is accepted only by an operator workflow authenticated
// as a system administrator. It is intentionally not exposed through HTTP.
type ProvisionMemberInput struct {
	TenantID string
	Email    string
	Password string
}

type Service struct {
	pool      *pgxpool.Pool
	store     *postgres.Store
	passwords *security.PasswordHasher
	dummyHash string
	bootstrap *amsonia.Bootstrapper
	sessions  time.Duration
	refresh   time.Duration
	now       func() time.Time
}

func NewService(pool *pgxpool.Pool, store *postgres.Store, bootstrap *amsonia.Bootstrapper) (*Service, error) {
	if pool == nil || store == nil || bootstrap == nil {
		return nil, amsonia.ErrInvalidInput
	}
	passwords := security.NewPasswordHasher(64*1024, 3, 2, 32)
	dummyPassword, err := randomID("dummy_", 24)
	if err != nil {
		return nil, fmt.Errorf("generate dummy password: %w", err)
	}
	dummyHash, err := passwords.Hash(dummyPassword)
	if err != nil {
		return nil, fmt.Errorf("hash dummy password: %w", err)
	}
	return &Service{
		pool:      pool,
		store:     store,
		passwords: passwords,
		dummyHash: dummyHash,
		bootstrap: bootstrap,
		sessions:  15 * time.Minute,
		refresh:   30 * 24 * time.Hour,
		now:       func() time.Time { return time.Now().UTC() },
	}, nil
}

// Ready verifies that the database schema exactly matches this binary.
func (s *Service) Ready(ctx context.Context) error {
	return postgres.VerifyMigrationState(ctx, s.pool)
}

func normalizeEmail(raw string) (string, error) {
	email := strings.TrimSpace(raw)
	if len(email) < 3 || len(email) > 254 {
		return "", amsonia.ErrInvalidInput
	}
	address, err := mail.ParseAddress(email)
	if err != nil || !strings.EqualFold(address.Address, email) {
		return "", amsonia.ErrInvalidInput
	}
	return strings.ToLower(address.Address), nil
}

func randomID(prefix string, bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func tokenHash(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}

func (s *Service) BootstrapAdmin(ctx context.Context, input BootstrapInput) (Account, error) {
	normalized, err := normalizeEmail(input.Email)
	if err != nil {
		return Account{}, err
	}
	if !validBootstrapPassword(input.Password) {
		return Account{}, amsonia.ErrInvalidInput
	}
	hash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return Account{}, amsonia.ErrInvalidInput
	}
	accountID, err := randomID("acc_", 18)
	if err != nil {
		return Account{}, fmt.Errorf("generate account id: %w", err)
	}
	createdAt := s.now()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback(context.Background())
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM amsonia.system_administrators)").Scan(&exists); err != nil {
		return Account{}, err
	}
	if exists {
		return Account{}, ErrBootstrapComplete
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO amsonia.accounts (account_id, email, normalized_email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
	`, accountID, strings.TrimSpace(input.Email), normalized, hash, createdAt); err != nil {
		return Account{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO amsonia.system_administrators (account_id, appointed_at) VALUES ($1, $2)
	`, accountID, createdAt); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Account{}, err
	}
	return Account{ID: accountID, Email: strings.TrimSpace(input.Email), SystemAdmin: true, CreatedAt: createdAt}, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput) (Session, error) {
	normalized, err := normalizeEmail(input.Email)
	if err != nil {
		return Session{}, ErrInvalidCredentials
	}
	var account Account
	var passwordHash, status string
	var lockedUntil *time.Time
	var lastLoginAt *time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT a.account_id, a.email, a.password_hash, a.status,
		       a.locked_until, a.created_at, a.last_login_at,
		       EXISTS (SELECT 1 FROM amsonia.system_administrators sa WHERE sa.account_id = a.account_id)
		FROM amsonia.accounts a WHERE a.normalized_email = $1
	`, normalized).Scan(&account.ID, &account.Email, &passwordHash, &status, &lockedUntil, &account.CreatedAt, &lastLoginAt, &account.SystemAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		// Keep a valid-but-unknown email on the same expensive password path as
		// a known account so response timing does not disclose registration.
		_ = s.passwords.Verify(input.Password, s.dummyHash)
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, fmt.Errorf("lookup account: %w", err)
	}
	if status != "active" {
		return Session{}, ErrInvalidCredentials
	}
	if lastLoginAt != nil {
		account.LastLoginAt = *lastLoginAt
	}
	now := s.now()
	if lockedUntil != nil && lockedUntil.After(now) {
		return Session{}, ErrAccountLocked
	}
	if !s.passwords.Verify(input.Password, passwordHash) {
		// Increment against the value currently stored in PostgreSQL. Concurrent
		// failed attempts therefore cannot overwrite one another and weaken the
		// lockout threshold with a read-then-write race.
		if _, err := s.pool.Exec(ctx, `
			UPDATE amsonia.accounts
			SET failed_login_count = CASE
			        WHEN failed_login_count + 1 >= 5 THEN 0
			        ELSE failed_login_count + 1
			    END,
			    locked_until = CASE
			        WHEN failed_login_count + 1 >= 5 THEN $1
			        ELSE locked_until
			    END,
			    updated_at = $2
			WHERE account_id = $3
		`, now.Add(15*time.Minute), now, account.ID); err != nil {
			return Session{}, fmt.Errorf("record failed login: %w", err)
		}
		return Session{}, ErrInvalidCredentials
	}
	return s.issueSession(ctx, account, input.RemoteAddress, input.UserAgent)
}

func (s *Service) issueSession(ctx context.Context, account Account, remoteAddress, userAgent string) (Session, error) {
	access, err := randomID("as_", 32)
	if err != nil {
		return Session{}, err
	}
	refresh, err := randomID("rs_", 40)
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	expiresAt := now.Add(s.sessions)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `
		UPDATE amsonia.accounts SET failed_login_count = 0, locked_until = NULL, last_login_at = $1, updated_at = $1
		WHERE account_id = $2
	`, now, account.ID); err != nil {
		return Session{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO amsonia.access_sessions (token_hash, account_id, expires_at, remote_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)
	`, tokenHash(access), account.ID, expiresAt, bounded(remoteAddress, 128), bounded(userAgent, 512)); err != nil {
		return Session{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO amsonia.refresh_sessions (token_hash, account_id, expires_at, remote_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)
	`, tokenHash(refresh), account.ID, now.Add(s.refresh), bounded(remoteAddress, 128), bounded(userAgent, 512)); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	account.LastLoginAt = now
	return Session{AccessToken: access, RefreshToken: refresh, ExpiresAt: expiresAt, Account: account}, nil
}

// Refresh rotates a valid refresh token and issues a new short-lived access
// token. Reuse of an already rotated token revokes every session for the
// account, limiting the blast radius of a stolen token family.
func (s *Service) Refresh(ctx context.Context, input RefreshInput) (Session, error) {
	if !strings.HasPrefix(input.RefreshToken, "rs_") || len(input.RefreshToken) > 160 {
		return Session{}, ErrSessionInvalid
	}
	access, err := randomID("as_", 32)
	if err != nil {
		return Session{}, err
	}
	nextRefresh, err := randomID("rs_", 40)
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(context.Background())

	var account Account
	var accountStatus string
	var refreshExpires time.Time
	var revokedAt *time.Time
	var rotatedTo []byte
	var lastLoginAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT a.account_id, a.email, a.status, a.created_at,
		       a.last_login_at,
		       EXISTS (SELECT 1 FROM amsonia.system_administrators sa WHERE sa.account_id = a.account_id),
		       r.expires_at, r.revoked_at, r.rotated_to_hash
		FROM amsonia.refresh_sessions r
		JOIN amsonia.accounts a ON a.account_id = r.account_id
		WHERE r.token_hash = $1
		FOR UPDATE OF r
	`, tokenHash(input.RefreshToken)).Scan(
		&account.ID, &account.Email, &accountStatus, &account.CreatedAt, &lastLoginAt,
		&account.SystemAdmin, &refreshExpires, &revokedAt, &rotatedTo,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrSessionInvalid
	}
	if err != nil {
		return Session{}, fmt.Errorf("lookup refresh session: %w", err)
	}
	if accountStatus != "active" {
		return Session{}, ErrSessionInvalid
	}
	if lastLoginAt != nil {
		account.LastLoginAt = *lastLoginAt
	}
	if revokedAt != nil || len(rotatedTo) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE amsonia.refresh_sessions SET revoked_at = COALESCE(revoked_at, $1) WHERE account_id = $2`, now, account.ID); err != nil {
			return Session{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE amsonia.access_sessions SET revoked_at = COALESCE(revoked_at, $1) WHERE account_id = $2`, now, account.ID); err != nil {
			return Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Session{}, err
		}
		return Session{}, ErrSessionInvalid
	}
	if !refreshExpires.After(now) {
		if _, err := tx.Exec(ctx, `UPDATE amsonia.refresh_sessions SET revoked_at = $1 WHERE token_hash = $2`, now, tokenHash(input.RefreshToken)); err != nil {
			return Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Session{}, err
		}
		return Session{}, ErrSessionInvalid
	}

	if _, err := tx.Exec(ctx, `
		UPDATE amsonia.refresh_sessions
		SET revoked_at = $1, rotated_to_hash = $2
		WHERE token_hash = $3 AND revoked_at IS NULL AND rotated_to_hash IS NULL
	`, now, tokenHash(nextRefresh), tokenHash(input.RefreshToken)); err != nil {
		return Session{}, err
	}
	expiresAt := now.Add(s.sessions)
	if _, err := tx.Exec(ctx, `
		INSERT INTO amsonia.access_sessions (token_hash, account_id, expires_at, remote_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)
	`, tokenHash(access), account.ID, expiresAt, bounded(input.RemoteAddress, 128), bounded(input.UserAgent, 512)); err != nil {
		return Session{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO amsonia.refresh_sessions (token_hash, account_id, expires_at, remote_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)
	`, tokenHash(nextRefresh), account.ID, now.Add(s.refresh), bounded(input.RemoteAddress, 128), bounded(input.UserAgent, 512)); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return Session{AccessToken: access, RefreshToken: nextRefresh, ExpiresAt: expiresAt, Account: account}, nil
}

func bounded(value string, limit int) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "")
	if len(value) > limit {
		value = value[:limit]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}

func validBootstrapPassword(password string) bool {
	if !utf8.ValidString(password) {
		return false
	}
	length := utf8.RuneCountInString(password)
	return length >= 12 && length <= 128
}

func (s *Service) Authenticate(ctx context.Context, accessToken string) (Account, error) {
	if !strings.HasPrefix(accessToken, "as_") || len(accessToken) > 128 {
		return Account{}, ErrSessionInvalid
	}
	var account Account
	var status string
	var lastLoginAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT a.account_id, a.email, a.status, a.created_at,
		       a.last_login_at,
		       EXISTS (SELECT 1 FROM amsonia.system_administrators sa WHERE sa.account_id = a.account_id)
		FROM amsonia.access_sessions s
		JOIN amsonia.accounts a ON a.account_id = s.account_id
		WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now()
	`, tokenHash(accessToken)).Scan(&account.ID, &account.Email, &status, &account.CreatedAt, &lastLoginAt, &account.SystemAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrSessionInvalid
	}
	if err != nil {
		return Account{}, fmt.Errorf("authenticate session: %w", err)
	}
	if status != "active" {
		return Account{}, ErrSessionInvalid
	}
	if lastLoginAt != nil {
		account.LastLoginAt = *lastLoginAt
	}
	return account, nil
}

func (s *Service) Logout(ctx context.Context, accessToken string) error {
	if !strings.HasPrefix(accessToken, "as_") || len(accessToken) > 128 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE amsonia.access_sessions SET revoked_at = now() WHERE token_hash = $1`, tokenHash(accessToken))
	return err
}

func (s *Service) RevokeRefresh(ctx context.Context, refreshToken string) error {
	if !strings.HasPrefix(refreshToken, "rs_") || len(refreshToken) > 160 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE amsonia.refresh_sessions SET revoked_at = COALESCE(revoked_at, now()) WHERE token_hash = $1`, tokenHash(refreshToken))
	return err
}

func (s *Service) ListTenants(ctx context.Context, accountID string) ([]Tenant, error) {
	if amsonia.SubjectID(accountID).Validate() != nil {
		return nil, amsonia.ErrInvalidInput
	}
	rows, err := s.pool.Query(ctx, `
		SELECT tenant_id, name, state, created_at
		FROM amsonia.tenants WHERE state = 'active'
		ORDER BY created_at, tenant_id
	`)
	if err != nil {
		return nil, err
	}
	candidates := make([]Tenant, 0)
	for rows.Next() {
		var tenant Tenant
		if err := rows.Scan(&tenant.ID, &tenant.Name, &tenant.State, &tenant.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, tenant)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Membership rows are protected by FORCE RLS and are deliberately never
	// queried in a global actor-only context. Bind each candidate tenant with
	// the signed runtime secret before checking the account membership.
	tenants := make([]Tenant, 0, len(candidates))
	for _, candidate := range candidates {
		found := false
		err := s.store.RunTenant(ctx, amsonia.TenantID(candidate.ID), true, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM amsonia.tenant_memberships
					WHERE tenant_id = $1 AND account_id = $2 AND status = 'active'
				)
			`, candidate.ID, accountID).Scan(&found)
		})
		if err != nil {
			return nil, err
		}
		if found {
			tenants = append(tenants, candidate)
		}
	}
	return tenants, nil
}

func (s *Service) CreateTenant(ctx context.Context, actor Account, input CreateTenantInput) (Tenant, error) {
	if !actor.SystemAdmin {
		return Tenant{}, amsonia.ErrForbidden
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 80 {
		return Tenant{}, amsonia.ErrInvalidInput
	}
	tenantID, err := randomID("ten_", 16)
	if err != nil {
		return Tenant{}, err
	}
	tenant := Tenant{ID: tenantID, Name: name, State: "pending", CreatedAt: s.now()}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO amsonia.tenants (tenant_id, name, state, created_by, created_at, updated_at)
		VALUES ($1, $2, 'pending', $3, $4, $4)
	`, tenant.ID, tenant.Name, actor.ID, tenant.CreatedAt); err != nil {
		return Tenant{}, err
	}
	ownerRoleID := amsonia.RoleID("role_owner")
	_, err = s.bootstrap.BootstrapTenant(ctx, amsonia.BootstrapInput{
		TenantID:       amsonia.TenantID(tenant.ID),
		OwnerSubjectID: amsonia.SubjectID(actor.ID),
		OwnerRoleID:    ownerRoleID,
		OwnerRoleName:  "Tenant owner",
		Grants:         ownerGrants(ownerRoleID),
		Metadata:       amsonia.MutationMetadata{ReasonCode: "tenant_creation"},
	})
	if err != nil {
		_ = s.failCreatedTenant(ctx, actor.ID, tenant.ID)
		return Tenant{}, err
	}
	err = s.store.RunTenant(ctx, amsonia.TenantID(tenant.ID), false, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO amsonia.tenant_memberships (tenant_id, account_id, status, joined_at)
			VALUES ($1, $2, 'active', $3)
		`, tenant.ID, actor.ID, tenant.CreatedAt); err != nil {
			return err
		}
		var activated bool
		if err := tx.QueryRow(ctx, "SELECT amsonia.activate_tenant()").Scan(&activated); err != nil {
			return err
		}
		if !activated {
			return amsonia.ErrConflict
		}
		return nil
	})
	if err != nil {
		_ = s.failCreatedTenant(ctx, actor.ID, tenant.ID)
		return Tenant{}, err
	}
	tenant.State = "active"
	return tenant, nil
}

// ProvisionMember creates or resets one non-administrator tenant member. The
// operation is reserved for trusted operator tooling; the public API has no
// equivalent endpoint. Existing administrator identities cannot be reset.
func (s *Service) ProvisionMember(ctx context.Context, actor Account, input ProvisionMemberInput) (Account, error) {
	if !actor.SystemAdmin || amsonia.TenantID(input.TenantID).Validate() != nil {
		return Account{}, amsonia.ErrForbidden
	}
	if err := s.RequireMembership(ctx, input.TenantID, actor.ID); err != nil {
		return Account{}, err
	}
	normalized, err := normalizeEmail(input.Email)
	if err != nil || !validBootstrapPassword(input.Password) {
		return Account{}, amsonia.ErrInvalidInput
	}
	passwordHash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return Account{}, amsonia.ErrInvalidInput
	}
	newAccountID, err := randomID("acc_", 18)
	if err != nil {
		return Account{}, fmt.Errorf("generate account id: %w", err)
	}
	now := s.now()
	account := Account{}
	err = s.store.RunTenant(ctx, amsonia.TenantID(input.TenantID), false, func(tx pgx.Tx) error {
		var existing bool
		err := tx.QueryRow(ctx, `
			SELECT a.account_id, a.email, a.created_at,
			       EXISTS (SELECT 1 FROM amsonia.system_administrators sa WHERE sa.account_id = a.account_id)
			FROM amsonia.accounts a WHERE a.normalized_email = $1
		`, normalized).Scan(&account.ID, &account.Email, &account.CreatedAt, &account.SystemAdmin)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			account = Account{ID: newAccountID, Email: strings.TrimSpace(input.Email), CreatedAt: now}
			if _, err := tx.Exec(ctx, `
				INSERT INTO amsonia.accounts
				    (account_id, email, normalized_email, password_hash, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $5)
			`, account.ID, account.Email, normalized, passwordHash, now); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			existing = true
		}
		if existing {
			if account.SystemAdmin {
				return amsonia.ErrForbidden
			}
			if _, err := tx.Exec(ctx, `
				UPDATE amsonia.accounts
				SET email = $1, password_hash = $2, status = 'active', failed_login_count = 0,
				    locked_until = NULL, updated_at = $3
				WHERE account_id = $4
			`, strings.TrimSpace(input.Email), passwordHash, now, account.ID); err != nil {
				return err
			}
			account.Email = strings.TrimSpace(input.Email)
			if _, err := tx.Exec(ctx, `
				UPDATE amsonia.access_sessions SET revoked_at = COALESCE(revoked_at, $1) WHERE account_id = $2
			`, now, account.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE amsonia.refresh_sessions SET revoked_at = COALESCE(revoked_at, $1) WHERE account_id = $2
			`, now, account.ID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO amsonia.tenant_memberships (tenant_id, account_id, status, joined_at)
			VALUES ($1, $2, 'active', $3)
			ON CONFLICT (tenant_id, account_id)
			DO UPDATE SET status = 'active'
		`, input.TenantID, account.ID, now)
		return err
	})
	if err != nil {
		return Account{}, err
	}
	return account, nil
}

func (s *Service) failCreatedTenant(ctx context.Context, accountID, tenantID string) error {
	return s.store.RunActor(ctx, accountID, func(tx pgx.Tx) error {
		var failed bool
		if err := tx.QueryRow(ctx, "SELECT amsonia.fail_created_tenant($1)", tenantID).Scan(&failed); err != nil {
			return err
		}
		if !failed {
			return amsonia.ErrConflict
		}
		return nil
	})
}

func (s *Service) RequireMembership(ctx context.Context, tenantID, accountID string) error {
	return s.store.RunTenant(ctx, amsonia.TenantID(tenantID), true, func(tx pgx.Tx) error {
		var found bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM amsonia.tenant_memberships
				WHERE tenant_id = $1 AND account_id = $2 AND status = 'active'
			)
		`, tenantID, accountID).Scan(&found); err != nil {
			return err
		}
		if !found {
			return amsonia.ErrNotFound
		}
		return nil
	})
}

func (s *Service) ListMembers(ctx context.Context, tenantID string) ([]Member, error) {
	members := make([]Member, 0)
	err := s.store.RunTenant(ctx, amsonia.TenantID(tenantID), true, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT m.account_id, a.email, m.status, m.joined_at
			FROM amsonia.tenant_memberships m
			JOIN amsonia.accounts a ON a.account_id = m.account_id
			WHERE m.tenant_id = $1 ORDER BY m.joined_at, m.account_id
		`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var member Member
			if err := rows.Scan(&member.AccountID, &member.Email, &member.Status, &member.JoinedAt); err != nil {
				return err
			}
			members = append(members, member)
		}
		return rows.Err()
	})
	return members, err
}

func (s *Service) ListRoles(ctx context.Context, tenantID string) ([]amsonia.Role, error) {
	roles := make([]amsonia.Role, 0)
	err := s.store.RunTenant(ctx, amsonia.TenantID(tenantID), true, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT tenant_id, role_id, name, description, version, deleted
			FROM amsonia.roles WHERE tenant_id = $1 AND NOT deleted ORDER BY name, role_id
		`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var role amsonia.Role
			if err := rows.Scan(&role.TenantID, &role.RoleID, &role.Name, &role.Description, &role.Version, &role.Deleted); err != nil {
				return err
			}
			roles = append(roles, role)
		}
		return rows.Err()
	})
	return roles, err
}

func (s *Service) AuditEvents(ctx context.Context, tenantID string, limit int) ([]amsonia.MutationAuditEvent, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	events := make([]amsonia.MutationAuditEvent, 0)
	err := s.store.RunTenant(ctx, amsonia.TenantID(tenantID), true, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT tenant_id, actor_subject, host_initiator, operation, phase,
			       target_type, target_id, outcome, reason_code, request_id, role_version, at
			FROM amsonia.audit_events WHERE tenant_id = $1 ORDER BY id DESC LIMIT $2
		`, tenantID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var event amsonia.MutationAuditEvent
			if err := rows.Scan(&event.TenantID, &event.ActorSubjectID, &event.HostInitiator, &event.Operation, &event.Phase, &event.TargetType, &event.TargetID, &event.Outcome, &event.ReasonCode, &event.RequestID, &event.RoleVersion, &event.At); err != nil {
				return err
			}
			events = append(events, event)
		}
		return rows.Err()
	})
	return events, err
}
