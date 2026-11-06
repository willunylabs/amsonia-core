package amsonia_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/memory"
)

type mutationErrorStore struct {
	amsonia.Store
	err error
}

type auditSinkFunc func(context.Context, amsonia.MutationAuditEvent) error

func (f auditSinkFunc) RecordSecurityEvent(ctx context.Context, event amsonia.MutationAuditEvent) error {
	return f(ctx, event)
}

func TestMutationAuditOutlivesRequestCancellation(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint("expired=", expired), func(t *testing.T) {
			type requestValueKey struct{}
			ctx := context.WithValue(t.Context(), requestValueKey{}, "request metadata")
			var cancel context.CancelFunc
			if expired {
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
			} else {
				ctx, cancel = context.WithCancel(ctx)
			}
			cancel()
			cause := fmt.Errorf("mutation stopped: %w", ctx.Err())
			calls := 0
			audit := auditSinkFunc(func(auditCtx context.Context, event amsonia.MutationAuditEvent) error {
				calls++
				if auditCtx.Err() != nil || auditCtx.Value(requestValueKey{}) != "request metadata" {
					t.Fatalf("audit inherited cancellation or lost request values: %v", auditCtx.Err())
				}
				deadline, ok := auditCtx.Deadline()
				if remaining := time.Until(deadline); !ok || remaining <= 0 || remaining > 2*time.Second {
					t.Fatalf("audit deadline is not independently bounded: %v %t", deadline, ok)
				}
				if event.Outcome != amsonia.AuditOutcomeFailed {
					t.Fatalf("cancellation outcome: %s", event.Outcome)
				}
				return nil
			})
			manager, err := amsonia.NewManager(testCatalog(t), mutationErrorStore{Store: memory.NewStore(), err: cause}, noMemberships{}, controls, audit, fixedTime())
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = manager.CreateRole(ctx, amsonia.Principal{TenantID: "tenant-a", SubjectID: "alice"}, meta(), amsonia.CreateRoleInput{RoleID: "reader", Name: "Reader"})
			if err != cause || calls != 1 {
				t.Fatalf("mutation error=%v audit calls=%d", err, calls)
			}
		})
	}
}

func TestMutationAuditFailureIsLoggedWithoutReplacingBusinessError(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	audit := auditSinkFunc(func(context.Context, amsonia.MutationAuditEvent) error {
		return errors.New("adapter error containing a private credential")
	})
	manager, err := amsonia.NewManager(testCatalog(t), mutationErrorStore{Store: memory.NewStore(), err: amsonia.ErrForbidden}, noMemberships{}, controls, audit, fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	metadata := meta()
	metadata.RequestID = "audit-failure-request"
	_, _, err = manager.CreateRole(t.Context(), amsonia.Principal{TenantID: "tenant-a", SubjectID: "alice"}, metadata, amsonia.CreateRoleInput{RoleID: "reader", Name: "Reader"})
	if !errors.Is(err, amsonia.ErrForbidden) {
		t.Fatalf("audit failure masked the mutation result: %v", err)
	}
	if text := logs.String(); !strings.Contains(text, "security audit persistence failed") || !strings.Contains(text, metadata.RequestID) || strings.Contains(text, "private credential") {
		t.Fatalf("missing or unsanitized persistence failure log: %s", text)
	}
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
