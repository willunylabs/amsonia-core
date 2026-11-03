package coreapp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/willunylabs/amsonia-core"
)

type ScopedPermission struct {
	Permission     amsonia.PermissionKey `json:"permission"`
	Scope          amsonia.Scope         `json:"scope"`
	WorkspaceRoles []string              `json:"workspace_roles"`
}

type RoleHistory struct {
	Version     int64              `json:"version"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Grants      []ScopedPermission `json:"grants"`
	CreatedAt   time.Time          `json:"created_at"`
	CreatedBy   string             `json:"created_by"`
}

type RoleDetail struct {
	Role        amsonia.Role       `json:"role"`
	Grants      []ScopedPermission `json:"grants"`
	Subjects    []string           `json:"subjects"`
	Versions    []RoleHistory      `json:"versions"`
	NextVersion int64              `json:"next_version,omitempty"`
}

type roleSummary struct {
	RoleDetail
	SubjectCount int64 `json:"subject_count"`
}

func (s *Service) RoleDetail(ctx context.Context, tenant, role string, before int64) (RoleDetail, error) {
	value, err := s.roleDetails(ctx, tenant, role, before, true)
	return value.RoleDetail, err
}

func (s *Service) roleDetails(ctx context.Context, tenant, role string, before int64, includeSubjects bool) (roleSummary, error) {
	if amsonia.RoleID(role).Validate() != nil || before < 0 {
		return roleSummary{}, amsonia.ErrInvalidInput
	}
	item := roleSummary{RoleDetail: RoleDetail{Grants: make([]ScopedPermission, 0), Subjects: make([]string, 0), Versions: make([]RoleHistory, 0)}}
	err := s.store.RunTenant(ctx, amsonia.TenantID(tenant), true, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT tenant_id,role_id,name,description,version,deleted FROM amsonia.roles WHERE tenant_id=$1 AND role_id=$2 AND NOT deleted`, tenant, role).Scan(&item.Role.TenantID, &item.Role.RoleID, &item.Role.Name, &item.Role.Description, &item.Role.Version, &item.Role.Deleted); err != nil {
			return accountNotFound(err)
		}
		rows, err := tx.Query(ctx, `SELECT permission_key,scope,workspace_roles FROM amsonia.role_permission_grants WHERE tenant_id=$1 AND role_id=$2 ORDER BY permission_key,scope,workspace_roles`, tenant, role)
		if err != nil {
			return err
		}
		grants, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ScopedPermission, error) {
			var g ScopedPermission
			err := row.Scan(&g.Permission, &g.Scope, &g.WorkspaceRoles)
			return g, err
		})
		if err != nil {
			return err
		}
		item.Grants = grants
		if includeSubjects {
			rows, err = tx.Query(ctx, `SELECT subject_id FROM amsonia.subject_roles WHERE tenant_id=$1 AND role_id=$2 ORDER BY subject_id`, tenant, role)
			if err != nil {
				return err
			}
			subjects, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			item.Subjects = subjects
			item.SubjectCount = int64(len(item.Subjects))
		} else if err := tx.QueryRow(ctx, `SELECT count(*) FROM amsonia.subject_roles WHERE tenant_id=$1 AND role_id=$2`, tenant, role).Scan(&item.SubjectCount); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT version,name,description,grants,created_at,created_by_subject FROM amsonia.role_versions WHERE tenant_id=$1 AND role_id=$2 AND ($3::bigint=0 OR version<$3) ORDER BY version DESC LIMIT 21`, tenant, role, before)
		if err != nil {
			return err
		}
		versions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (RoleHistory, error) {
			var v RoleHistory
			var data []byte
			var grants []amsonia.RolePermissionGrant
			if err := row.Scan(&v.Version, &v.Name, &v.Description, &data, &v.CreatedAt, &v.CreatedBy); err != nil {
				return v, err
			}
			if err := json.Unmarshal(data, &grants); err != nil {
				return v, err
			}
			v.Grants = make([]ScopedPermission, 0, len(grants))
			for _, g := range grants {
				v.Grants = append(v.Grants, ScopedPermission{Permission: g.Permission, Scope: g.Scope, WorkspaceRoles: g.WorkspaceRoles})
			}
			return v, nil
		})
		if err != nil {
			return err
		}
		item.Versions = versions
		if len(item.Versions) > 20 {
			item.Versions = item.Versions[:20]
			item.NextVersion = item.Versions[19].Version
		}
		return nil
	})
	return item, err
}

func (s *Service) MemberRoleIDs(ctx context.Context, tenant, account string) ([]string, error) {
	items := make([]string, 0)
	err := s.store.RunTenant(ctx, amsonia.TenantID(tenant), true, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM amsonia.tenant_memberships WHERE tenant_id=$1 AND account_id=$2)`, tenant, account).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return amsonia.ErrNotFound
		}
		rows, err := tx.Query(ctx, `SELECT s.role_id FROM amsonia.subject_roles s JOIN amsonia.roles r USING(tenant_id,role_id) WHERE s.tenant_id=$1 AND s.subject_id=$2 AND NOT r.deleted ORDER BY s.role_id`, tenant, account)
		if err != nil {
			return err
		}
		items, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	return items, err
}
