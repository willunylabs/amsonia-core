package coreapp

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/willunylabs/amsonia-core"
)

const (
	maxAuditExportRows  = 10000
	maxAuditExportBytes = 10 << 20
)

var errAuditExportTooLarge = errors.New("audit export exceeds limit")

type auditCSVBuffer struct {
	buffer bytes.Buffer
}

func (b *auditCSVBuffer) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > maxAuditExportBytes {
		return 0, errAuditExportTooLarge
	}
	return b.buffer.Write(data)
}

// Prefix formula-like text, including whitespace-prefixed values. CSV quoting
// alone does not stop spreadsheet applications interpreting a cell as a formula.
func auditCSVText(value string) string {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	first, _ := utf8.DecodeRuneInString(trimmed)
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") || (trimmed != "" && strings.ContainsRune("=+-@＝＋－＠", first)) {
		return "\t" + value
	}
	return value
}

func auditCSVFields(e AuditRecord) []string {
	fields := []string{
		strconv.FormatInt(e.ID, 10), string(e.TenantID), e.At.UTC().Format(time.RFC3339Nano),
		string(e.ActorSubjectID), e.HostInitiator, e.Operation, string(e.Phase), e.TargetType,
		e.TargetID, string(e.Outcome), e.ReasonCode, e.RequestID, strconv.FormatInt(e.RoleVersion, 10),
	}
	for i := range fields {
		fields[i] = auditCSVText(fields[i])
	}
	return fields
}

// auditExport buffers one bounded read-only snapshot before any response headers
// are sent. A limit, row/encoding error or transaction failure produces JSON,
// never a silently truncated CSV download.
func (s *Service) auditExport(ctx context.Context, tenant string, input AuditFilter) ([]byte, error) {
	filter, err := normalizeAuditFilter(input)
	if err != nil {
		return nil, err
	}
	filter.Before = 0
	var output auditCSVBuffer
	_, _ = output.Write([]byte("\xef\xbb\xbf"))
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"id", "tenant_id", "at", "actor_subject_id", "host_initiator", "operation", "phase", "target_type", "target_id", "outcome", "reason_code", "request_id", "role_version"}); err != nil {
		return nil, err
	}
	err = s.store.RunTenant(ctx, amsonia.TenantID(tenant), true, func(tx pgx.Tx) error {
		rows, err := queryAuditRows(ctx, tx, tenant, filter, maxAuditExportRows+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			count++
			if count > maxAuditExportRows {
				return errAuditExportTooLarge
			}
			event, err := scanAuditRecord(rows)
			if err != nil {
				return err
			}
			if err := writer.Write(auditCSVFields(event)); err != nil {
				return err
			}
			// Flush regularly to enforce the output byte bound while reading rows.
			if count%64 == 0 {
				writer.Flush()
				if err := writer.Error(); err != nil {
					return err
				}
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return output.buffer.Bytes(), nil
}
