// Command amsonia provides operator workflows for Amsonia Core.
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"

	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/internal/coreapp"
	"github.com/willunylabs/amsonia-core/postgres"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 1 || (args[0] != "migrate" && args[0] != "migration-status" && args[0] != "bootstrap-admin" && args[0] != "provision-demo-viewer") {
		return errors.New("usage: amsonia migrate | migration-status | bootstrap-admin | provision-demo-viewer")
	}
	dsnName := "AMSONIA_MIGRATION_DSN"
	if args[0] == "bootstrap-admin" || args[0] == "provision-demo-viewer" {
		dsnName = "AMSONIA_DATABASE_DSN"
	}
	dsn := os.Getenv(dsnName)
	if dsn == "" {
		return fmt.Errorf("%s is required", dsnName)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect migration database: %w", err)
	}
	defer pool.Close()
	switch args[0] {
	case "migrate":
		return postgres.Migrate(ctx, pool)
	case "migration-status":
		states, err := postgres.MigrationStates(ctx, pool)
		if err != nil {
			return err
		}
		for _, state := range states {
			fmt.Printf("%06d  %-32s  dirty=%-5t  %s\n", state.Version, state.Name, state.Dirty, state.AppliedAt.UTC().Format(time.RFC3339))
		}
	case "bootstrap-admin":
		return bootstrapAdmin(ctx, pool)
	case "provision-demo-viewer":
		return provisionDemoViewer(ctx, pool)
	}
	return nil
}

func bootstrapAdmin(ctx context.Context, pool *pgxpool.Pool) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stdout, "Administrator email: ")
	email, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read email: %w", err)
	}
	if !term.IsTerminal(int(syscall.Stdin)) {
		return errors.New("bootstrap-admin requires an interactive terminal")
	}
	fmt.Fprint(os.Stdout, "Password: ")
	password, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	// Bootstrap only uses the identity tables, but NewService deliberately
	// requires the complete runtime composition. Build a minimal safe
	// bootstrapper and Store using the configured tenant binding.
	secret, err := decodeBindingSecret()
	if err != nil {
		return err
	}
	store, err := postgres.NewStore(pool, secret)
	if err != nil {
		return err
	}
	catalog, controls, err := coreapp.CoreCatalog()
	if err != nil {
		return err
	}
	bootstrapper, err := amsonia.NewBootstrapper(catalog, store, controls, cliHostAuthority{}, amsonia.RealClock{})
	if err != nil {
		return err
	}
	service, err := coreapp.NewService(pool, store, bootstrapper)
	if err != nil {
		return err
	}
	account, err := service.BootstrapAdmin(ctx, coreapp.BootstrapInput{Email: strings.TrimSpace(email), Password: string(password)})
	for index := range password {
		password[index] = 0
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Created system administrator %s (%s)\n", account.Email, account.ID)
	return nil
}

func provisionDemoViewer(ctx context.Context, pool *pgxpool.Pool) error {
	email := strings.TrimSpace(os.Getenv("AMSONIA_DEMO_VIEWER_EMAIL"))
	password := os.Getenv("AMSONIA_DEMO_VIEWER_PASSWORD")
	tenantName := strings.TrimSpace(os.Getenv("AMSONIA_DEMO_TENANT_NAME"))
	if email == "" || password == "" || tenantName == "" {
		return errors.New("AMSONIA_DEMO_VIEWER_EMAIL, AMSONIA_DEMO_VIEWER_PASSWORD, and AMSONIA_DEMO_TENANT_NAME are required")
	}
	secret, err := decodeBindingSecret()
	if err != nil {
		return err
	}
	store, err := postgres.NewStore(pool, secret)
	if err != nil {
		return err
	}
	catalog, controls, err := coreapp.CoreCatalog()
	if err != nil {
		return err
	}
	bootstrapper, err := amsonia.NewBootstrapper(catalog, store, controls, cliHostAuthority{}, amsonia.RealClock{})
	if err != nil {
		return err
	}
	manager, err := amsonia.NewManager(catalog, store, cliNoMemberships{}, controls, cliAudit{}, amsonia.RealClock{})
	if err != nil {
		return err
	}
	authorizer, err := amsonia.NewAuthorizer(catalog, store, cliNoMemberships{})
	if err != nil {
		return err
	}
	service, err := coreapp.NewService(pool, store, bootstrapper)
	if err != nil {
		return err
	}
	admin, err := loadOnlySystemAdministrator(ctx, pool)
	if err != nil {
		return err
	}
	tenant, err := ensureDemoTenant(ctx, service, admin, tenantName)
	if err != nil {
		return err
	}
	viewer, err := service.ProvisionMember(ctx, admin, coreapp.ProvisionMemberInput{
		TenantID: tenant.ID,
		Email:    email,
		Password: password,
	})
	if err != nil {
		return fmt.Errorf("provision demo member: %w", err)
	}
	principal := amsonia.Principal{TenantID: amsonia.TenantID(tenant.ID), SubjectID: amsonia.SubjectID(admin.ID)}
	role, err := ensureDemoViewerRole(ctx, service, manager, principal, tenant.ID)
	if err != nil {
		return err
	}
	if _, err := manager.AssignRole(ctx, principal, amsonia.MutationMetadata{ReasonCode: "demo_viewer_provisioning"}, amsonia.AssignRoleInput{
		SubjectID:           amsonia.SubjectID(viewer.ID),
		RoleID:              role.RoleID,
		ExpectedRoleVersion: role.Version,
	}); err != nil {
		return fmt.Errorf("assign demo viewer role: %w", err)
	}
	if err := verifyDemoViewerPolicy(ctx, authorizer, tenant.ID, viewer.ID); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Provisioned read-only demo viewer %s for tenant %s (%s)\n", viewer.Email, tenant.Name, tenant.ID)
	return nil
}

func loadOnlySystemAdministrator(ctx context.Context, pool *pgxpool.Pool) (coreapp.Account, error) {
	rows, err := pool.Query(ctx, `
		SELECT a.account_id, a.email, a.created_at
		FROM amsonia.system_administrators sa
		JOIN amsonia.accounts a ON a.account_id = sa.account_id
		WHERE a.status = 'active'
		ORDER BY sa.appointed_at LIMIT 2
	`)
	if err != nil {
		return coreapp.Account{}, err
	}
	defer rows.Close()
	admins := make([]coreapp.Account, 0, 2)
	for rows.Next() {
		var account coreapp.Account
		if err := rows.Scan(&account.ID, &account.Email, &account.CreatedAt); err != nil {
			return coreapp.Account{}, err
		}
		account.SystemAdmin = true
		admins = append(admins, account)
	}
	if err := rows.Err(); err != nil {
		return coreapp.Account{}, err
	}
	if len(admins) != 1 {
		return coreapp.Account{}, fmt.Errorf("expected exactly one active system administrator, found %d", len(admins))
	}
	return admins[0], nil
}

func ensureDemoTenant(ctx context.Context, service *coreapp.Service, admin coreapp.Account, name string) (coreapp.Tenant, error) {
	tenants, err := service.ListTenants(ctx, admin.ID)
	if err != nil {
		return coreapp.Tenant{}, err
	}
	for _, tenant := range tenants {
		if tenant.Name == name {
			return tenant, nil
		}
	}
	if len(tenants) > 0 {
		return coreapp.Tenant{}, fmt.Errorf("system administrator already owns %d tenant(s), but none is named %q", len(tenants), name)
	}
	return service.CreateTenant(ctx, admin, coreapp.CreateTenantInput{Name: name})
}

func ensureDemoViewerRole(ctx context.Context, service *coreapp.Service, manager *amsonia.Manager, actor amsonia.Principal, tenantID string) (amsonia.Role, error) {
	const roleID amsonia.RoleID = "role_demo_viewer"
	roles, err := service.ListRoles(ctx, tenantID)
	if err != nil {
		return amsonia.Role{}, err
	}
	for _, role := range roles {
		if role.RoleID == roleID {
			return role, nil
		}
	}
	role, _, err := manager.CreateRoleWithPermissions(ctx, actor, amsonia.MutationMetadata{ReasonCode: "demo_viewer_provisioning"}, amsonia.CreateRoleWithPermissionsInput{
		RoleID:      roleID,
		Name:        "Demo viewer",
		Description: "Read-only access for the public Amsonia Core demonstration.",
		Permissions: []amsonia.PermissionKey{
			coreapp.PermissionMemberRead,
			coreapp.PermissionRoleRead,
			coreapp.PermissionAuditRead,
		},
	})
	if err != nil {
		return amsonia.Role{}, fmt.Errorf("create demo viewer role: %w", err)
	}
	return role, nil
}

func verifyDemoViewerPolicy(ctx context.Context, authorizer *amsonia.Authorizer, tenantID, viewerID string) error {
	principal := amsonia.Principal{TenantID: amsonia.TenantID(tenantID), SubjectID: amsonia.SubjectID(viewerID)}
	readPermissions := []amsonia.PermissionKey{
		coreapp.PermissionMemberRead,
		coreapp.PermissionRoleRead,
		coreapp.PermissionAuditRead,
	}
	writePermissions := []amsonia.PermissionKey{
		coreapp.PermissionRoleManage,
		coreapp.PermissionGrantManage,
		coreapp.PermissionRoleAssign,
		coreapp.PermissionMemberManage,
	}
	for _, permission := range append(readPermissions, writePermissions...) {
		decision, err := authorizer.Check(ctx, amsonia.CheckRequest{
			Principal:  principal,
			Permission: permission,
			Mode:       amsonia.ResourceTenantAction,
			Resource:   amsonia.ResourceContext{TenantID: amsonia.TenantID(tenantID)},
		})
		if err != nil {
			return fmt.Errorf("verify %s: %w", permission, err)
		}
		wantAllowed := false
		for _, readPermission := range readPermissions {
			if permission == readPermission {
				wantAllowed = true
				break
			}
		}
		if decision.Allowed != wantAllowed {
			return fmt.Errorf("unsafe demo viewer policy: permission %s allowed=%t, want %t", permission, decision.Allowed, wantAllowed)
		}
	}
	return nil
}

type cliNoMemberships struct{}

func (cliNoMemberships) LookupWorkspaceMembership(context.Context, amsonia.TenantID, amsonia.WorkspaceID, amsonia.SubjectID) (amsonia.WorkspaceMembership, error) {
	return amsonia.WorkspaceMembership{}, amsonia.ErrNotFound
}

type cliAudit struct{}

func (cliAudit) RecordSecurityEvent(context.Context, amsonia.MutationAuditEvent) error { return nil }

type cliHostAuthority struct{}

func (cliHostAuthority) AuthorizeBootstrap(context.Context, amsonia.TenantID) (amsonia.HostProvenance, error) {
	return amsonia.HostProvenance{Initiator: "amsonia-cli", At: time.Now().UTC()}, nil
}

func decodeBindingSecret() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("AMSONIA_TENANT_BINDING_SECRET"))
	secret, err := base64.RawURLEncoding.DecodeString(raw)
	if raw == "" || err != nil || len(secret) < 32 {
		return nil, errors.New("AMSONIA_TENANT_BINDING_SECRET must be unpadded base64url encoding of at least 32 random bytes")
	}
	return secret, nil
}
