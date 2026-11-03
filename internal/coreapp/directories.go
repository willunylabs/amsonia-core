package coreapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/willunylabs/amsonia-core"
)

// DirectoryFilter is the Core HTTP collection contract, separate from kernel APIs.
type DirectoryFilter struct {
	Query, Status, After string
	Limit                int
}

type DirectoryPage[T any] struct {
	Items      []T    `json:"items"`
	Total      int64  `json:"total"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// Cursors carry no authority. Bind them to the collection and normalized filter
// to reject accidental reuse when switching tenants or changing a search.
type directoryCursor struct {
	Tenant, Kind, Query, Status, ID string
	At                              time.Time
}

func directoryInput(tenant, kind string, filter DirectoryFilter) (DirectoryFilter, directoryCursor, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if !utf8.ValidString(filter.Query) || utf8.RuneCountInString(filter.Query) > 128 || strings.ContainsRune(filter.Query, 0) || filter.Limit < 0 || filter.Limit > 100 {
		return filter, directoryCursor{}, amsonia.ErrInvalidInput
	}
	validStatus := filter.Status == ""
	switch kind {
	case "members":
		validStatus = validStatus || filter.Status == "active" || filter.Status == "suspended"
	case "invitations":
		validStatus = validStatus || filter.Status == "pending" || filter.Status == "accepted" || filter.Status == "revoked" || filter.Status == "expired"
	}
	if !validStatus {
		return filter, directoryCursor{}, amsonia.ErrInvalidInput
	}
	if filter.Limit == 0 {
		filter.Limit = 30
	}
	cursor := directoryCursor{Tenant: tenant, Kind: kind, Query: filter.Query, Status: filter.Status}
	if filter.After != "" {
		if len(filter.After) > 2048 {
			return filter, cursor, amsonia.ErrInvalidInput
		}
		data, err := base64.RawURLEncoding.DecodeString(filter.After)
		var supplied directoryCursor
		if err != nil || json.Unmarshal(data, &supplied) != nil || supplied.Tenant != tenant || supplied.Kind != kind || supplied.Query != filter.Query || supplied.Status != filter.Status || supplied.ID == "" || len(supplied.ID) > 256 || !utf8.ValidString(supplied.ID) || strings.ContainsRune(supplied.ID, 0) || (kind == "invitations" && supplied.At.IsZero()) {
			return filter, cursor, amsonia.ErrInvalidInput
		}
		cursor = supplied
	}
	return filter, cursor, nil
}

func finishDirectory[T any](page *DirectoryPage[T], filter DirectoryFilter, cursor directoryCursor, key func(T) (string, time.Time)) {
	if len(page.Items) > filter.Limit {
		page.Items = page.Items[:filter.Limit]
		cursor.ID, cursor.At = key(page.Items[filter.Limit-1])
		data, _ := json.Marshal(cursor)
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
}

func (s *Service) MemberDirectory(ctx context.Context, tenant string, input DirectoryFilter) (DirectoryPage[Member], error) {
	page := DirectoryPage[Member]{Items: make([]Member, 0)}
	f, cursor, err := directoryInput(tenant, "members", input)
	if err != nil {
		return page, err
	}
	err = s.store.RunTenant(ctx, amsonia.TenantID(tenant), true, func(tx pgx.Tx) error {
		const source = ` FROM amsonia.tenant_memberships m JOIN amsonia.accounts a ON a.account_id=m.account_id
		 WHERE m.tenant_id=$1 AND ($2='' OR position(lower($2) in lower(a.email || ' ' || m.account_id))>0) AND ($3='' OR m.status=$3)`
		if err := tx.QueryRow(ctx, `SELECT count(*)`+source, tenant, f.Query, f.Status).Scan(&page.Total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT m.account_id,a.email,m.status,m.joined_at`+source+` AND ($4='' OR m.account_id>$4) ORDER BY m.account_id LIMIT $5`, tenant, f.Query, f.Status, cursor.ID, f.Limit+1)
		if err != nil {
			return err
		}
		page.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Member, error) {
			var m Member
			err := row.Scan(&m.AccountID, &m.Email, &m.Status, &m.JoinedAt)
			return m, err
		})
		return err
	})
	finishDirectory(&page, f, cursor, func(m Member) (string, time.Time) { return m.AccountID, time.Time{} })
	return page, err
}

func (s *Service) RoleDirectory(ctx context.Context, tenant string, input DirectoryFilter) (DirectoryPage[amsonia.Role], error) {
	page := DirectoryPage[amsonia.Role]{Items: make([]amsonia.Role, 0)}
	f, cursor, err := directoryInput(tenant, "roles", input)
	if err != nil {
		return page, err
	}
	err = s.store.RunTenant(ctx, amsonia.TenantID(tenant), true, func(tx pgx.Tx) error {
		const source = ` FROM amsonia.roles WHERE tenant_id=$1 AND NOT deleted AND ($2='' OR position(lower($2) in lower(role_id || ' ' || name || ' ' || description))>0)`
		if err := tx.QueryRow(ctx, `SELECT count(*)`+source, tenant, f.Query).Scan(&page.Total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT tenant_id,role_id,name,description,version,deleted`+source+` AND ($3='' OR role_id>$3) ORDER BY role_id LIMIT $4`, tenant, f.Query, cursor.ID, f.Limit+1)
		if err != nil {
			return err
		}
		page.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (amsonia.Role, error) {
			var r amsonia.Role
			err := row.Scan(&r.TenantID, &r.RoleID, &r.Name, &r.Description, &r.Version, &r.Deleted)
			return r, err
		})
		return err
	})
	finishDirectory(&page, f, cursor, func(r amsonia.Role) (string, time.Time) { return string(r.RoleID), time.Time{} })
	return page, err
}

func (s *Service) InvitationDirectory(ctx context.Context, tenant string, input DirectoryFilter) (DirectoryPage[InvitationItem], error) {
	page := DirectoryPage[InvitationItem]{Items: make([]InvitationItem, 0)}
	f, cursor, err := directoryInput(tenant, "invitations", input)
	if err != nil {
		return page, err
	}
	err = s.store.RunTenant(ctx, amsonia.TenantID(tenant), true, func(tx pgx.Tx) error {
		const source = ` FROM (SELECT invitation_id,display_email,expires_at,created_at,
		 CASE WHEN accepted_at IS NOT NULL THEN 'accepted' WHEN revoked_at IS NOT NULL THEN 'revoked' WHEN expires_at<=$2 THEN 'expired' ELSE 'pending' END AS status
		 FROM amsonia.member_invitations WHERE tenant_id=$1) i
		 WHERE ($3='' OR position(lower($3) in lower(display_email || ' ' || invitation_id))>0) AND ($4='' OR status=$4)`
		now := s.now()
		if err := tx.QueryRow(ctx, `SELECT count(*)`+source, tenant, now, f.Query, f.Status).Scan(&page.Total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT invitation_id,display_email,expires_at,created_at,status`+source+` AND ($5='' OR (created_at,invitation_id)<($6::timestamptz,$5)) ORDER BY created_at DESC,invitation_id DESC LIMIT $7`, tenant, now, f.Query, f.Status, cursor.ID, cursor.At, f.Limit+1)
		if err != nil {
			return err
		}
		page.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (InvitationItem, error) {
			var i InvitationItem
			err := row.Scan(&i.ID, &i.Email, &i.ExpiresAt, &i.CreatedAt, &i.Status)
			return i, err
		})
		return err
	})
	finishDirectory(&page, f, cursor, func(i InvitationItem) (string, time.Time) { return i.ID, i.CreatedAt })
	return page, err
}

func directoryQuery(w http.ResponseWriter, r *http.Request) (DirectoryFilter, bool) {
	q := r.URL.Query()
	f := DirectoryFilter{Query: q.Get("q"), Status: q.Get("status"), After: q.Get("after")}
	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 1 || n > 100 {
			writeDomainError(w, amsonia.ErrInvalidInput)
			return f, false
		}
		f.Limit = n
	}
	return f, true
}
