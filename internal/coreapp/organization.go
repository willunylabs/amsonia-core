package coreapp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/willunylabs/amsonia-core"
)

type organizationSettings struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	State     string    `json:"state"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CanManage bool      `json:"can_manage"`
}

func normalizeOrganizationName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || len(name) > 80 || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
		return "", amsonia.ErrInvalidInput
	}
	return name, nil
}

func scanOrganization(row pgx.Row) (organizationSettings, error) {
	var value organizationSettings
	err := row.Scan(&value.ID, &value.Name, &value.State, &value.Version, &value.CreatedAt, &value.UpdatedAt)
	return value, accountNotFound(err)
}

func (s *Service) readOrganization(ctx context.Context, tenant string) (organizationSettings, error) {
	var value organizationSettings
	if err := s.allowOrganization(ctx, tenant); err != nil {
		return value, err
	}
	err := s.store.RunTenant(ctx, amsonia.TenantID(tenant), true, func(tx pgx.Tx) error {
		var err error
		value, err = scanOrganization(tx.QueryRow(ctx, "SELECT * FROM amsonia.organization_settings()"))
		return err
	})
	return value, err
}

func (s *Service) renameOrganization(ctx context.Context, tenant, actor, rawName string, expectedVersion int64) (organizationSettings, error) {
	var value organizationSettings
	name, err := normalizeOrganizationName(rawName)
	if err != nil || expectedVersion < 1 {
		return value, amsonia.ErrInvalidInput
	}
	if err := s.allowOrganization(ctx, tenant); err != nil {
		return value, err
	}
	err = s.store.RunTenant(ctx, amsonia.TenantID(tenant), false, func(tx pgx.Tx) error {
		var err error
		value, err = scanOrganization(tx.QueryRow(ctx, "SELECT * FROM amsonia.rename_organization($1, $2, $3)", actor, name, expectedVersion))
		if err != nil {
			var databaseError *pgconn.PgError
			if errors.As(err, &databaseError) {
				switch databaseError.Code {
				case "42501":
					return amsonia.ErrForbidden
				case "P0002":
					return amsonia.ErrNotFound
				}
			}
			return err
		}
		return s.tenantAudit(ctx, tx, tenant, actor, "organization.update", "organization", tenant, value.Version != expectedVersion)
	})
	return value, err
}

func (api *API) organizationSettings(w http.ResponseWriter, r *http.Request) {
	value, err := api.service.readOrganization(r.Context(), r.PathValue("tenant_id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	value.CanManage = accountFromContext(r.Context()).SystemAdmin
	writeJSON(w, http.StatusOK, value)
}

func (api *API) renameOrganization(w http.ResponseWriter, r *http.Request) {
	actor := accountFromContext(r.Context())
	if !actor.SystemAdmin {
		writeDomainError(w, amsonia.ErrForbidden)
		return
	}
	var input struct {
		Name    string `json:"name"`
		Version int64  `json:"expected_version"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	value, err := api.service.renameOrganization(r.Context(), r.PathValue("tenant_id"), actor.ID, input.Name, input.Version)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	value.CanManage = true
	writeJSON(w, http.StatusOK, value)
}
