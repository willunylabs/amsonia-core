//go:build postgres

package coreapp

import (
	"testing"
	"time"

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

	// Concurrent writers each call the production sink once.
	event.RequestID = "concurrent-request"
	start := make(chan struct{})
	results := make(chan error, 6)
	for range 6 {
		go func() {
			<-start
			results <- service.RecordSecurityEvent(ctx, event)
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

	// Force a serialization failure at commit after the insert has succeeded.
	// nextval survives the aborted transaction and permits a fresh attempt.
	if _, err := owner.Exec(ctx, `
		CREATE SEQUENCE amsonia.audit_test_attempts;
		CREATE FUNCTION amsonia.fail_first_audit_commit() RETURNS trigger
		LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
		BEGIN
			IF nextval('amsonia.audit_test_attempts'::regclass) = 1 THEN
				RAISE EXCEPTION 'test serialization failure' USING ERRCODE = '40001';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE CONSTRAINT TRIGGER audit_test_fail_once
		AFTER INSERT ON amsonia.audit_events DEFERRABLE INITIALLY DEFERRED
		FOR EACH ROW WHEN (NEW.request_id = 'commit-retry') EXECUTE FUNCTION amsonia.fail_first_audit_commit();`); err != nil {
		t.Fatal(err)
	}
	event.RequestID = "commit-retry"
	record(event)
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM amsonia.audit_events
		WHERE tenant_id=$1 AND request_id=$2`, event.TenantID, event.RequestID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("commit retry lost or duplicated audit: count=%d error=%v", count, err)
	}
}
