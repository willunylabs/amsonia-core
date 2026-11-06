package amsonia_test

import (
	"context"
	"errors"
	"testing"

	"github.com/willunylabs/amsonia-core"
)

type countingMutationStore struct {
	amsonia.Store
	calls int
}

func (s *countingMutationStore) MutateTenant(ctx context.Context, tenantID amsonia.TenantID, fn func(amsonia.TenantTx) error) error {
	s.calls++
	return s.Store.MutateTenant(ctx, tenantID, fn)
}

func TestGrantMutationsRejectInvalidInputBeforeTransaction(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*amsonia.GrantPermissionInput)
	}{
		{name: "empty scope", modify: func(input *amsonia.GrantPermissionInput) { input.Scope = "" }},
		{name: "unknown scope", modify: func(input *amsonia.GrantPermissionInput) { input.Scope = "unknown" }},
		{name: "uppercase scope", modify: func(input *amsonia.GrantPermissionInput) { input.Scope = "TENANT" }},
		{name: "unknown permission", modify: func(input *amsonia.GrantPermissionInput) { input.Permission = "billing:invoice:unknown" }},
		{name: "malformed permission", modify: func(input *amsonia.GrantPermissionInput) { input.Permission = "*" }},
		{name: "malformed role", modify: func(input *amsonia.GrantPermissionInput) { input.RoleID = "role\nowner" }},
		{name: "malformed workspace role", modify: func(input *amsonia.GrantPermissionInput) { input.WorkspaceRoles = []string{"*"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, _, bootstrapper, catalog := setup(t)
			bootstrapTenant(t, bootstrapper, "tenant-a", "owner", "owner-role")
			tracked := &countingMutationStore{Store: store}
			audit := &recordingSecurityAudit{}
			manager, err := amsonia.NewManager(catalog, tracked, noMemberships{}, controls, audit, fixedTime())
			if err != nil {
				t.Fatal(err)
			}
			actor := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner"}
			input := amsonia.GrantPermissionInput{
				RoleID: "owner-role", ExpectedVersion: 1, Permission: permInvoiceRead, Scope: amsonia.ScopeTenant,
			}
			tt.modify(&input)
			before := len(store.DebugAuditEvents(actor.TenantID))
			for _, operation := range []struct {
				name string
				run  func(context.Context, amsonia.Principal, amsonia.MutationMetadata, amsonia.GrantPermissionInput) (amsonia.MutationResult, error)
			}{
				{name: "grant", run: manager.GrantPermission},
				{name: "revoke", run: manager.RevokePermission},
			} {
				t.Run(operation.name, func(t *testing.T) {
					result, err := operation.run(t.Context(), actor, meta(), input)
					if !errors.Is(err, amsonia.ErrInvalidInput) || result != (amsonia.MutationResult{}) {
						t.Errorf("result = %+v, err = %v; want zero result and invalid input", result, err)
					}
					if tracked.calls != 0 {
						t.Errorf("tenant transactions = %d; want 0", tracked.calls)
					}
					if len(store.DebugAuditEvents(actor.TenantID)) != before || len(audit.events) != 0 {
						t.Error("invalid request recorded a mutation or security audit event")
					}
				})
			}
		})
	}
}

func TestRevokePermissionNormalizesAndPreservesVersionChecks(t *testing.T) {
	store, manager, bootstrapper, _ := setup(t)
	bootstrapTenant(t, bootstrapper, "tenant-a", "owner", "owner-role")
	actor := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	ctx := t.Context()
	_, _, err := manager.CreateRole(ctx, actor, meta(), amsonia.CreateRoleInput{RoleID: "reader", Name: "Reader"})
	if err != nil {
		t.Fatal(err)
	}
	input := amsonia.GrantPermissionInput{
		RoleID: "reader", ExpectedVersion: 1, Permission: permInvoiceRead,
		Scope: amsonia.ScopeWorkspace, WorkspaceRoles: []string{"editor", "admin"},
	}
	if _, err := manager.GrantPermission(ctx, actor, meta(), input); err != nil {
		t.Fatal(err)
	}
	// Revoking an equivalent normalized constraint list still checks the version.
	input.WorkspaceRoles = []string{" admin ", "editor", "admin"}
	if _, err := manager.RevokePermission(ctx, actor, meta(), input); !errors.Is(err, amsonia.ErrConflict) {
		t.Fatalf("stale revoke error = %v; want conflict", err)
	}
	input.ExpectedVersion = 2
	result, err := manager.RevokePermission(ctx, actor, meta(), input)
	if err != nil || result != (amsonia.MutationResult{Changed: true, RoleVersion: 3}) {
		t.Fatalf("revoke result = %+v, err = %v; want removal at version 3", result, err)
	}
	input.ExpectedVersion = 3
	result, err = manager.RevokePermission(ctx, actor, meta(), input)
	if err != nil || result != (amsonia.MutationResult{RoleVersion: 3}) {
		t.Fatalf("repeat revoke result = %+v, err = %v; want no-op at version 3", result, err)
	}
	events := store.DebugAuditEvents(actor.TenantID)
	if event := events[len(events)-1]; event.Operation != "revoke.permission" || event.Outcome != amsonia.AuditOutcomeNoop || event.RoleVersion != 3 {
		t.Fatalf("repeat revoke audit = %+v; want no-op at version 3", event)
	}
}
