package coreapp

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/willunylabs/amsonia-core"
)

func (s *Service) memberDetail(ctx context.Context, tenant, account string) (Member, error) {
	var member Member
	if amsonia.SubjectID(account).Validate() != nil {
		return member, amsonia.ErrInvalidInput
	}
	err := s.store.RunTenant(ctx, amsonia.TenantID(tenant), true, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT m.account_id,a.email,m.status,m.joined_at FROM amsonia.tenant_memberships m JOIN amsonia.accounts a ON a.account_id=m.account_id WHERE m.tenant_id=$1 AND m.account_id=$2`, tenant, account).Scan(&member.AccountID, &member.Email, &member.Status, &member.JoinedAt)
	})
	return member, accountNotFound(err)
}

func (api *API) memberDetail(w http.ResponseWriter, r *http.Request) {
	if !api.requirePermission(w, r, PermissionMemberRead) {
		return
	}
	member, err := api.service.memberDetail(r.Context(), r.PathValue("tenant_id"), r.PathValue("account_id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}
