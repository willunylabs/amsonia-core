//go:build postgres

package coreapp

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/willunylabs/amsonia-core"
)

func TestSecurityAuditDeduplicatesRequestRetries(t *testing.T) {
	service, _, _, owner := coreFixture(t)
	ctx := t.Context()
	event := amsonia.MutationAuditEvent{
		TenantID: "audit-tenant", ActorSubjectID: "actor", Operation: "role.create",
		Phase: amsonia.AuditPhaseResult, TargetType: "role", TargetID: "original",
		Outcome: amsonia.AuditOutcomeDenied, ReasonCode: "audit_test", RequestID: "request-1", At: time.Now().UTC(),
	}
	record := func(event amsonia.MutationAuditEvent) {
		t.Helper()
		if err := service.RecordSecurityEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	record(event)
	retry := event
	retry.TargetID, retry.Outcome = "retry", amsonia.AuditOutcomeFailed
	retry.At = retry.At.Add(time.Minute)
	record(retry)
	var count int
	var target, outcome string
	if err := owner.QueryRow(ctx, `SELECT count(*),min(target_id),min(outcome)
		FROM amsonia.audit_events WHERE tenant_id=$1 AND request_id=$2`, event.TenantID, event.RequestID).
		Scan(&count, &target, &outcome); err != nil || count != 1 || target != event.TargetID || outcome != string(event.Outcome) {
		t.Fatalf("retry changed canonical audit: count=%d target=%s outcome=%s error=%v", count, target, outcome, err)
	}
	for _, variant := range []string{"tenant", "operation", "phase", "no_request", "no_request"} {
		other := event
		switch variant {
		case "tenant":
			other.TenantID = "other-audit-tenant"
		case "operation":
			other.Operation = "role.update"
		case "phase":
			other.Phase = amsonia.AuditPhaseCompleted
		case "no_request":
			other.RequestID = ""
		}
		record(other)
	}
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM amsonia.audit_events`).Scan(&count); err != nil || count != 6 {
		t.Fatalf("distinct events were discarded: count=%d error=%v", count, err)
	}

	// Concurrent serializable snapshots may conflict. Retrying those conflicts
	// must leave exactly one durable event, even though the generated row IDs differ.
	event.RequestID = "concurrent-request"
	start := make(chan struct{})
	results := make(chan error, 6)
	for range 6 {
		go func() {
			<-start
			var err error
			for range 6 {
				err = service.RecordSecurityEvent(ctx, event)
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != "40001" {
					break
				}
			}
			results <- err
		}()
	}
	close(start)
	for range 6 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM amsonia.audit_events
		WHERE tenant_id=$1 AND request_id=$2`, event.TenantID, event.RequestID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent retries duplicated audit: count=%d error=%v", count, err)
	}
}
