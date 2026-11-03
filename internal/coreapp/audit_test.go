package coreapp

import (
	"bytes"
	"encoding/csv"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/willunylabs/amsonia-core"
)

func TestAuditFilterDates(t *testing.T) {
	start := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	zeroYear := time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := normalizeAuditFilter(AuditFilter{From: &start, Until: &end}); err != nil {
		t.Fatal(err)
	}
	for name, filter := range map[string]AuditFilter{
		"equal": {From: &start, Until: &start}, "reverse": {From: &end, Until: &start},
		"year zero": {From: &zeroYear},
		"query nul": {Query: "x\x00y"}, "query utf8": {Query: "\xff"},
		"operation nul": {Operation: "x\x00y"}, "operation utf8": {Operation: "\xff"},
		"outcome": {Outcome: "invalid"}, "cursor": {Before: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeAuditFilter(filter); err == nil {
				t.Fatal("accepted invalid filter")
			}
		})
	}
	for _, raw := range []string{"from=bad", "from=2026-10-25T00:00:00", "until=", "from=2026-02-30T00:00:00Z"} {
		w := httptest.NewRecorder()
		if _, ok := auditFilterQuery(w, httptest.NewRequest("GET", "/?"+raw, nil), true); ok || w.Code != 400 {
			t.Fatalf("accepted invalid date %s", raw)
		}
	}
	for _, raw := range []string{"before=1", "limit=100"} {
		w := httptest.NewRecorder()
		if _, ok := auditFilterQuery(w, httptest.NewRequest("GET", "/?"+raw, nil), false); ok || w.Code != 400 {
			t.Fatalf("export accepted paging %s", raw)
		}
	}
	w := httptest.NewRecorder()
	f, ok := auditFilterQuery(w, httptest.NewRequest("GET", "/?from=2026-10-25T08:00:00%2B08:00&until=2026-10-25T01:00:00Z", nil), true)
	if !ok || !f.From.Equal(start) || !f.Until.Equal(end) {
		t.Fatalf("offset timestamps changed: %+v", f)
	}
}

func TestAuditCSVTextAndEncoding(t *testing.T) {
	for _, value := range []string{"=SUM(A1)", "+cmd", "-1", "@name", "\tvalue", "\rvalue", "\nvalue", "  =cmd", "\u00a0@cmd", "＝命令", "＋1", "－1", "＠name"} {
		if got := auditCSVText(value); got != "\t"+value {
			t.Fatalf("not neutralized: %q -> %q", value, got)
		}
	}
	for _, value := range []string{"", "ordinary", "租户", "123", "'text", "a,\"b\"\nnext"} {
		if got := auditCSVText(value); got != value {
			t.Fatalf("ordinary text changed: %q -> %q", value, got)
		}
	}
	event := AuditRecord{ID: 42, MutationAuditEvent: amsonia.MutationAuditEvent{
		TenantID: "tenant", ActorSubjectID: "=actor", HostInitiator: "宿主", Operation: "role.update", Phase: amsonia.AuditPhaseResult,
		TargetType: "role", TargetID: "a,\"b\"\nnext", Outcome: amsonia.AuditOutcomeSuccess, ReasonCode: "ok", RequestID: "request", RoleVersion: 9,
		At: time.Date(2026, 10, 25, 8, 0, 0, 123000, time.FixedZone("offset", 8*3600)),
	}}
	fields := auditCSVFields(event)
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(fields); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	if !strings.Contains(buf.String(), "\"\t=actor\"") {
		t.Fatal("formula guard must remain inside a quoted cell")
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil || len(rows) != 1 || len(rows[0]) != 13 {
		t.Fatalf("invalid CSV: %v", err)
	}
	if rows[0][2] != "2026-10-25T00:00:00.000123Z" || rows[0][4] != "宿主" || rows[0][8] != event.TargetID || rows[0][12] != "9" {
		t.Fatalf("fields changed: %+v", rows[0])
	}
}
