package coreapp

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/willunylabs/amsonia-core"
)

type AuditFilter struct {
	Query, Outcome, Operation string
	Before                    int64
	Limit                     int
	From, Until               *time.Time
}

type AuditRecord struct {
	ID int64 `json:"id"`
	amsonia.MutationAuditEvent
}

type AuditPage struct {
	Items      []AuditRecord `json:"items"`
	NextCursor int64         `json:"next_cursor,omitempty"`
}

func normalizeAuditFilter(filter AuditFilter) (AuditFilter, error) {
	if filter.Before < 0 || !utf8.ValidString(filter.Query) || utf8.RuneCountInString(filter.Query) > 128 || strings.ContainsRune(filter.Query, 0) || !utf8.ValidString(filter.Operation) || len(filter.Operation) > 64 || strings.ContainsRune(filter.Operation, 0) {
		return filter, amsonia.ErrInvalidInput
	}
	for _, value := range []*time.Time{filter.From, filter.Until} {
		if value != nil && (value.UTC().Year() < 1 || value.UTC().Year() > 9999) {
			return filter, amsonia.ErrInvalidInput
		}
	}
	if filter.From != nil && filter.Until != nil && !filter.From.Before(*filter.Until) {
		return filter, amsonia.ErrInvalidInput
	}
	switch filter.Outcome {
	case "", "success", "noop", "denied", "conflict", "failed":
	default:
		return filter, amsonia.ErrInvalidInput
	}
	filter.Query, filter.Operation = strings.TrimSpace(filter.Query), strings.TrimSpace(filter.Operation)
	if filter.Limit < 1 || filter.Limit > 200 {
		filter.Limit = 50
	}
	return filter, nil
}

// One source of predicates keeps paged JSON and complete CSV exports aligned.
func queryAuditRows(ctx context.Context, tx pgx.Tx, tenant string, filter AuditFilter, limit int) (pgx.Rows, error) {
	return tx.Query(ctx, `SELECT id,tenant_id,actor_subject,host_initiator,operation,phase,target_type,target_id,outcome,reason_code,request_id,role_version,at
	 FROM amsonia.audit_events WHERE tenant_id=$1 AND ($2::bigint=0 OR id<$2)
	 AND ($4::text='' OR position(lower($4) in lower(operation||' '||COALESCE(actor_subject,'')||' '||target_id||' '||COALESCE(reason_code,'')||' '||COALESCE(request_id,'')))>0)
	 AND ($5::text='' OR outcome=$5) AND ($6::text='' OR operation=$6)
	 AND ($7::timestamptz IS NULL OR at >= $7) AND ($8::timestamptz IS NULL OR at < $8)
	 ORDER BY id DESC LIMIT $3`, tenant, filter.Before, limit, filter.Query, filter.Outcome, filter.Operation, filter.From, filter.Until)
}

func scanAuditRecord(row pgx.CollectableRow) (AuditRecord, error) {
	var e AuditRecord
	err := row.Scan(&e.ID, &e.TenantID, &e.ActorSubjectID, &e.HostInitiator, &e.Operation, &e.Phase, &e.TargetType, &e.TargetID, &e.Outcome, &e.ReasonCode, &e.RequestID, &e.RoleVersion, &e.At)
	return e, err
}

func (s *Service) AuditLog(ctx context.Context, tenant string, input AuditFilter) (AuditPage, error) {
	page := AuditPage{Items: make([]AuditRecord, 0)}
	filter, err := normalizeAuditFilter(input)
	if err != nil {
		return page, err
	}
	err = s.store.RunTenant(ctx, amsonia.TenantID(tenant), true, func(tx pgx.Tx) error {
		rows, err := queryAuditRows(ctx, tx, tenant, filter, filter.Limit+1)
		if err != nil {
			return err
		}
		page.Items, err = pgx.CollectRows(rows, scanAuditRecord)
		if err != nil {
			return err
		}
		if len(page.Items) > filter.Limit {
			page.Items = page.Items[:filter.Limit]
			page.NextCursor = page.Items[len(page.Items)-1].ID
		}
		return nil
	})
	return page, err
}
