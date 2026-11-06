package amsonia_test

import (
	"errors"
	"testing"

	"github.com/willunylabs/amsonia-core"
)

func TestAssignRoleRetriesReportVersionAndRecheckCoverage(t *testing.T) {
	store, _, bootstrapper, catalog := setup(t)
	bootstrapTenant(t, bootstrapper, "tenant-a", "owner", "owner-role")
	audit := &recordingSecurityAudit{}
	manager, err := amsonia.NewManager(catalog, store, noMemberships{}, controls, audit, fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	actor := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	if _, _, err := manager.CreateRoleWithPermissions(ctx, actor, meta(), amsonia.CreateRoleWithPermissionsInput{
		RoleID: "reader", Name: "Reader", Permissions: []amsonia.PermissionKey{permInvoiceRead},
	}); err != nil {
		t.Fatal(err)
	}
	input := amsonia.AssignRoleInput{SubjectID: "alice", RoleID: "reader", ExpectedRoleVersion: 1}
	result, err := manager.AssignRole(ctx, actor, meta(), input)
	if err != nil || result != (amsonia.MutationResult{Changed: true, RoleVersion: 1}) {
		t.Fatalf("initial assignment result = %+v, err = %v", result, err)
	}
	retry := func(version int64, wantErr error, outcome amsonia.AuditOutcome) {
		t.Helper()
		input.ExpectedRoleVersion = version
		beforeMutation := len(store.DebugAuditEvents(actor.TenantID))
		beforeSecurity := len(audit.events)
		result, err := manager.AssignRole(ctx, actor, meta(), input)
		wantResult := amsonia.MutationResult{}
		if wantErr == nil {
			wantResult.RoleVersion = version
		}
		if !errors.Is(err, wantErr) || result != wantResult {
			t.Errorf("retry at version %d: result = %+v, err = %v; want %+v, %v", version, result, err, wantResult, wantErr)
		}
		mutation := store.DebugAuditEvents(actor.TenantID)
		var event amsonia.MutationAuditEvent
		if wantErr == nil {
			if len(mutation) != beforeMutation+1 || len(audit.events) != beforeSecurity {
				t.Fatal("successful retry must commit one no-op audit and no security audit")
			}
			event = mutation[len(mutation)-1]
		} else {
			if len(mutation) != beforeMutation || len(audit.events) != beforeSecurity+1 {
				t.Fatal("rejected retry must record one security audit and no mutation audit")
			}
			event = audit.events[len(audit.events)-1]
		}
		if event.Operation != "assign.role" || event.TargetID != string(input.SubjectID) || event.Outcome != outcome || event.RoleVersion != version {
			t.Errorf("retry audit = %+v; want %s at version %d", event, outcome, version)
		}
	}
	retry(1, nil, amsonia.AuditOutcomeNoop)
	if _, _, err := manager.UpdateRole(ctx, actor, meta(), amsonia.UpdateRoleInput{
		RoleID: "reader", ExpectedVersion: 1, Name: "Invoice Reader",
	}); err != nil {
		t.Fatal(err)
	}
	retry(1, amsonia.ErrConflict, amsonia.AuditOutcomeConflict)
	retry(2, nil, amsonia.AuditOutcomeNoop)
	// An existing binding must not bypass fresh grant coverage on retry.
	if _, err := manager.RevokePermission(ctx, actor, meta(), amsonia.RevokePermissionInput{
		RoleID: "owner-role", ExpectedVersion: 1, Permission: permInvoiceRead, Scope: amsonia.ScopeTenant,
	}); err != nil {
		t.Fatal(err)
	}
	retry(2, amsonia.ErrForbidden, amsonia.AuditOutcomeDenied)
	if err := store.ReadTenant(ctx, actor.TenantID, func(reader amsonia.TenantReader) error {
		role, err := reader.GetRole(ctx, input.RoleID)
		if err != nil {
			return err
		}
		roles, err := reader.ListSubjectRoleIDs(ctx, input.SubjectID)
		if err != nil {
			return err
		}
		path, err := reader.HasGrantPath(ctx, actor.SubjectID, input.SubjectID)
		if err != nil {
			return err
		}
		if role.Version != 2 || len(roles) != 1 || roles[0] != input.RoleID || !path {
			t.Errorf("retries changed role or binding: role=%+v, assignments=%v, path=%t", role, roles, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
