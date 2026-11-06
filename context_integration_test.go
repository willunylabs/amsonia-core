package amsonia_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/willunylabs/amsonia-core"
)

type cancelAfterAuditStore struct {
	amsonia.Store
	cancel context.CancelFunc
}

func (s cancelAfterAuditStore) MutateTenant(ctx context.Context, tenantID amsonia.TenantID, fn func(amsonia.TenantTx) error) error {
	return s.Store.MutateTenant(ctx, tenantID, func(tx amsonia.TenantTx) error {
		return fn(cancelAfterAuditTx{TenantTx: tx, cancel: s.cancel})
	})
}

type cancelAfterAuditTx struct {
	amsonia.TenantTx
	cancel context.CancelFunc
}

func (tx cancelAfterAuditTx) InsertMutationAudit(ctx context.Context, event amsonia.MutationAuditEvent) error {
	if err := tx.TenantTx.InsertMutationAudit(ctx, event); err != nil {
		return err
	}
	tx.cancel()
	return nil
}

func exportMemoryPolicy(t *testing.T, store amsonia.MaintenanceStore) []byte {
	t.Helper()
	var data []byte
	if err := store.MaintainTenant(t.Context(), "tenant-a", func(tx amsonia.MaintenanceTx) error {
		var err error
		data, err = tx.ExportTenant(t.Context())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCanceledContextStopsMemoryAuthorizationAndMutation(t *testing.T) {
	for _, name := range []string{"canceled", "expired"} {
		t.Run(name, func(t *testing.T) {
			store, _, bootstrapper, catalog := setup(t)
			bootstrapTenant(t, bootstrapper, "tenant-a", "owner", "owner-role")
			authorizer, err := amsonia.NewAuthorizer(catalog, store, noMemberships{})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			if name == "expired" {
				cancel()
				ctx, cancel = context.WithDeadline(t.Context(), time.Unix(0, 0))
			} else {
				cancel()
			}
			defer cancel()
			actor := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner"}
			decision, err := authorizer.Check(ctx, amsonia.CheckRequest{Principal: actor, Permission: permInvoiceRead, Mode: amsonia.ResourceTenantAction})
			if !errors.Is(err, ctx.Err()) || decision != (amsonia.Decision{Reason: amsonia.ReasonDependencyUnavailable}) {
				t.Errorf("decision = %+v, err = %v; want canceled dependency denial", decision, err)
			}
			audit := &recordingSecurityAudit{}
			manager, err := amsonia.NewManager(catalog, store, noMemberships{}, controls, audit, fixedTime())
			if err != nil {
				t.Fatal(err)
			}
			before := exportMemoryPolicy(t, store)
			role, result, err := manager.CreateRole(ctx, actor, meta(), amsonia.CreateRoleInput{RoleID: "reader", Name: "Reader"})
			if !errors.Is(err, ctx.Err()) || role != (amsonia.Role{}) || result != (amsonia.MutationResult{}) {
				t.Errorf("role = %+v, result = %+v, err = %v; want cancellation without result", role, result, err)
			}
			if !bytes.Equal(before, exportMemoryPolicy(t, store)) || len(audit.events) != 1 || audit.events[0].Outcome != amsonia.AuditOutcomeFailed {
				t.Error("canceled mutation changed policy or failed to record failed security audit")
			}
		})
	}
}

func TestCanceledMemoryMutationRollsBackPolicyAndAudit(t *testing.T) {
	store, _, bootstrapper, catalog := setup(t)
	bootstrapTenant(t, bootstrapper, "tenant-a", "owner", "owner-role")
	before := exportMemoryPolicy(t, store)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	audit := &recordingSecurityAudit{}
	manager, err := amsonia.NewManager(catalog, cancelAfterAuditStore{Store: store, cancel: cancel}, noMemberships{}, controls, audit, fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	actor := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	role, result, err := manager.CreateRoleWithPermissions(ctx, actor, meta(), amsonia.CreateRoleWithPermissionsInput{
		RoleID: "reader", Name: "Reader", Permissions: []amsonia.PermissionKey{permInvoiceRead},
	})
	if err != context.Canceled || role != (amsonia.Role{}) || result != (amsonia.MutationResult{}) {
		t.Errorf("role = %+v, result = %+v, err = %v; want cancellation without result", role, result, err)
	}
	if !bytes.Equal(before, exportMemoryPolicy(t, store)) || len(audit.events) != 1 || audit.events[0].Outcome != amsonia.AuditOutcomeFailed {
		t.Error("canceled callback committed role/grants/version/audit or lost failure classification")
	}
}

func TestCanceledMemoryPurgeRollsBackStateAndLedger(t *testing.T) {
	store, manager, bootstrapper, _ := setup(t)
	bootstrapTenant(t, bootstrapper, "tenant-a", "owner", "owner-role")
	actor := amsonia.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	if _, err := manager.AssignRole(t.Context(), actor, meta(), amsonia.AssignRoleInput{SubjectID: "alice", RoleID: "owner-role", ExpectedRoleVersion: 1}); err != nil {
		t.Fatal(err)
	}
	before := exportMemoryPolicy(t, store)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	event := amsonia.MutationAuditEvent{TenantID: actor.TenantID, RequestID: "canceled-purge", Operation: "tenant.purge", Phase: amsonia.AuditPhaseCompleted, Outcome: amsonia.AuditOutcomeSuccess}
	err := store.MaintainTenant(ctx, actor.TenantID, func(tx amsonia.MaintenanceTx) error {
		_, err := tx.PurgeTenant(ctx, event)
		cancel()
		return err
	})
	if err != context.Canceled || store.Purged(actor.TenantID) {
		t.Fatalf("purge error = %v, purged = %t; want cancellation without tombstone", err, store.Purged(actor.TenantID))
	}
	if !bytes.Equal(before, exportMemoryPolicy(t, store)) {
		t.Error("canceled purge changed tenant policy")
	}
	if err := store.ReadTenant(t.Context(), actor.TenantID, func(reader amsonia.TenantReader) error {
		roles, err := reader.ListSubjectRoleIDs(t.Context(), "alice")
		if err == nil && (len(roles) != 1 || roles[0] != "owner-role") {
			t.Errorf("canceled purge removed binding: %v", roles)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.MaintainTenant(t.Context(), actor.TenantID, func(tx amsonia.MaintenanceTx) error {
		result, err := tx.PurgeTenant(t.Context(), event)
		if err == nil && (!result.Changed || result.AlreadyCommitted) {
			t.Errorf("retry = %+v; canceled purge must not commit a replay entry", result)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
