package amsonia_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/memory"
)

type mutationErrorStore struct {
	amsonia.Store
	err error
}

func (s mutationErrorStore) MutateTenant(context.Context, amsonia.TenantID, func(amsonia.TenantTx) error) error {
	return s.err
}

type recordingSecurityAudit struct {
	events []amsonia.MutationAuditEvent
}

func (s *recordingSecurityAudit) RecordSecurityEvent(ctx context.Context, event amsonia.MutationAuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestMutationAuditPreservesWrappedErrorClassification(t *testing.T) {
	tests := []struct {
		name    string
		cause   error
		outcome amsonia.AuditOutcome
	}{
		{name: "forbidden", cause: amsonia.ErrForbidden, outcome: amsonia.AuditOutcomeDenied},
		{name: "wrapped forbidden", cause: fmt.Errorf("mutation rejected: %w", amsonia.ErrForbidden), outcome: amsonia.AuditOutcomeDenied},
		{name: "conflict", cause: amsonia.ErrConflict, outcome: amsonia.AuditOutcomeConflict},
		{name: "nested conflict", cause: fmt.Errorf("tenant transaction: %w", fmt.Errorf("role version changed: %w", amsonia.ErrConflict)), outcome: amsonia.AuditOutcomeConflict},
		{name: "dependency failure", cause: fmt.Errorf("policy store: %w", amsonia.ErrDependencyUnavailable), outcome: amsonia.AuditOutcomeFailed},
		{name: "cancellation", cause: fmt.Errorf("mutation canceled: %w", context.Canceled), outcome: amsonia.AuditOutcomeFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audit := &recordingSecurityAudit{}
			store := mutationErrorStore{Store: memory.NewStore(), err: tt.cause}
			manager, err := amsonia.NewManager(testCatalog(t), store, noMemberships{}, controls, audit, fixedTime())
			if err != nil {
				t.Fatal(err)
			}
			actor := amsonia.Principal{TenantID: "tenant-a", SubjectID: "alice"}
			_, _, err = manager.CreateRole(t.Context(), actor, meta(), amsonia.CreateRoleInput{RoleID: "reader", Name: "Reader"})
			if err != tt.cause {
				t.Fatalf("error = %v; want original adapter error %v", err, tt.cause)
			}
			if len(audit.events) != 1 {
				t.Fatalf("audit events = %d; want 1", len(audit.events))
			}
			event := audit.events[0]
			if event.Outcome != tt.outcome {
				t.Errorf("audit outcome = %q; want %q", event.Outcome, tt.outcome)
			}
			if event.TenantID != actor.TenantID || event.ActorSubjectID != actor.SubjectID || event.Operation != "role.create" || event.TargetID != "reader" {
				t.Errorf("audit lost mutation context: %+v", event)
			}
		})
	}
}
