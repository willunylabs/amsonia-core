package coreapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/postgres"
)

// ApplicationOptions selects the standalone application's organization model.
// The authorization kernel always retains its tenant isolation boundary.
type ApplicationOptions struct {
	TenancyMode      string
	OrganizationID   string
	OrganizationName string
}

func ApplicationOptionsFromEnv(getenv func(string) string) (ApplicationOptions, error) {
	return normalizeApplicationOptions(ApplicationOptions{
		TenancyMode:      getenv("AMSONIA_TENANCY_MODE"),
		OrganizationID:   getenv("AMSONIA_ORGANIZATION_ID"),
		OrganizationName: getenv("AMSONIA_ORGANIZATION_NAME"),
	})
}

func normalizeApplicationOptions(options ApplicationOptions) (ApplicationOptions, error) {
	options.TenancyMode = strings.TrimSpace(options.TenancyMode)
	options.OrganizationID = strings.TrimSpace(options.OrganizationID)
	options.OrganizationName = strings.TrimSpace(options.OrganizationName)
	if options.TenancyMode == "" {
		options.TenancyMode = "single"
	}
	if options.OrganizationName == "" {
		options.OrganizationName = "My organization"
	}
	if options.TenancyMode != "single" && options.TenancyMode != "multi" {
		return ApplicationOptions{}, fmt.Errorf("AMSONIA_TENANCY_MODE must be single or multi")
	}
	if options.OrganizationID != "" && (options.TenancyMode == "multi" || amsonia.TenantID(options.OrganizationID).Validate() != nil) {
		return ApplicationOptions{}, fmt.Errorf("AMSONIA_ORGANIZATION_ID must be a valid tenant ID in single mode")
	}
	if _, err := normalizeOrganizationName(options.OrganizationName); err != nil {
		return ApplicationOptions{}, fmt.Errorf("AMSONIA_ORGANIZATION_NAME must be a nonempty name without control characters, at most 80 bytes")
	}
	return options, nil
}

// NewApplicationService is used by standalone entrypoints. NewService retains
// the existing multi-tenant composition for embedded callers.
func NewApplicationService(pool *pgxpool.Pool, store *postgres.Store, bootstrap *amsonia.Bootstrapper, options ApplicationOptions) (*Service, error) {
	options, err := normalizeApplicationOptions(options)
	if err != nil {
		return nil, err
	}
	service, err := NewService(pool, store, bootstrap)
	if err != nil {
		return nil, err
	}
	service.application = options
	return service, nil
}

func (s *Service) organizationID(ctx context.Context) (string, error) {
	if s.application.TenancyMode == "multi" {
		return "", nil
	}
	var id *string
	if err := s.pool.QueryRow(ctx, "SELECT amsonia.single_organization_id($1)", s.application.OrganizationID).Scan(&id); err != nil {
		return "", fmt.Errorf("resolve organization (set AMSONIA_TENANCY_MODE=multi or select AMSONIA_ORGANIZATION_ID for an existing deployment): %w", err)
	}
	if id == nil {
		return "", nil
	}
	return *id, nil
}

func (s *Service) allowOrganization(ctx context.Context, tenantID string) error {
	if s.application.TenancyMode == "multi" {
		return nil
	}
	id, err := s.organizationID(ctx)
	if err != nil {
		return err
	}
	if id == "" || id != tenantID {
		return amsonia.ErrNotFound
	}
	return nil
}

// SetupOrganization is an operator-only, idempotent recovery workflow. A
// dedicated connection serializes setup without consuming a pool connection
// needed by the kernel's tenant bootstrap transactions.
func (s *Service) SetupOrganization(ctx context.Context, actor Account) (Tenant, error) {
	if s.application.TenancyMode == "multi" {
		return Tenant{}, nil
	}
	if !actor.SystemAdmin {
		return Tenant{}, amsonia.ErrForbidden
	}
	lock, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig.Copy())
	if err != nil {
		return Tenant{}, err
	}
	defer lock.Close(context.Background())
	if _, err := lock.Exec(ctx, "SELECT pg_advisory_lock(1634562926, 71)"); err != nil {
		return Tenant{}, err
	}
	id, err := s.organizationID(ctx)
	if err != nil {
		return Tenant{}, err
	}
	if id == "" {
		return s.createTenant(ctx, actor, CreateTenantInput{Name: s.application.OrganizationName})
	}
	tenants, err := s.ListTenants(ctx, actor.ID)
	if err != nil {
		return Tenant{}, err
	}
	for _, tenant := range tenants {
		if tenant.ID == id {
			return tenant, nil
		}
	}
	return Tenant{}, fmt.Errorf("system administrator must have an active membership in the configured organization: %w", amsonia.ErrForbidden)
}
