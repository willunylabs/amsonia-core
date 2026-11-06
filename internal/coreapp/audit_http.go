package coreapp

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/willunylabs/amsonia-core"
)

func auditTimeQuery(r *http.Request, key string) (*time.Time, error) {
	if !r.URL.Query().Has(key) {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get(key))
	if err != nil {
		return nil, amsonia.ErrInvalidInput
	}
	return &value, nil
}

func auditFilterQuery(w http.ResponseWriter, r *http.Request, paging bool) (AuditFilter, bool) {
	q := r.URL.Query()
	filter := AuditFilter{Query: q.Get("q"), Operation: q.Get("operation"), Outcome: q.Get("outcome")}
	if !paging && (q.Has("before") || q.Has("limit")) {
		writeDomainError(w, amsonia.ErrInvalidInput)
		return filter, false
	}
	if paging {
		before, ok := queryNumber(w, r, "before")
		if !ok {
			return filter, false
		}
		limit, ok := queryNumber(w, r, "limit")
		if !ok || limit > 200 {
			if ok {
				writeDomainError(w, amsonia.ErrInvalidInput)
			}
			return filter, false
		}
		filter.Before, filter.Limit = before, int(limit)
	}
	var err error
	filter.From, err = auditTimeQuery(r, "from")
	if err == nil {
		filter.Until, err = auditTimeQuery(r, "until")
	}
	if err != nil {
		writeDomainError(w, err)
		return filter, false
	}
	return filter, true
}

func (api *API) auditEvents(w http.ResponseWriter, r *http.Request) {
	if !api.requirePermission(w, r, PermissionAuditRead) {
		return
	}
	filter, ok := auditFilterQuery(w, r, true)
	if !ok {
		return
	}
	page, err := api.service.AuditLog(r.Context(), r.PathValue("tenant_id"), filter)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (api *API) exportAudit(w http.ResponseWriter, r *http.Request) {
	if !api.requirePermission(w, r, PermissionAuditRead) {
		return
	}
	filter, ok := auditFilterQuery(w, r, false)
	if !ok {
		return
	}
	data, err := api.service.auditExport(r.Context(), r.PathValue("tenant_id"), filter)
	if errors.Is(err, errAuditExportTooLarge) {
		writeError(w, http.StatusUnprocessableEntity, "audit_export_too_large", "Export exceeds 10,000 events or 10 MiB. Narrow the date range or filters and try again.")
		return
	}
	if err != nil {
		writeDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="amsonia-audit-`+api.service.now().UTC().Format("20060102T150405Z")+`.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
