package amsonia

import (
	"context"
)

// Maintenance exposes offline tenant export and purge. It is constructed
// separately from Manager and requires MaintenanceAuthorizer on every call.
type Maintenance struct {
	store         MaintenanceStore
	authorizer    MaintenanceAuthorizer
	securityAudit SecurityAuditSink
	clock         Clock
}

// NewMaintenance constructs the maintenance API.
func NewMaintenance(
	store MaintenanceStore,
	authorizer MaintenanceAuthorizer,
	securityAudit SecurityAuditSink,
	clock Clock,
) (*Maintenance, error) {
	if store == nil || authorizer == nil || securityAudit == nil || clock == nil {
		return nil, ErrInvalidInput
	}
	return &Maintenance{
		store:         store,
		authorizer:    authorizer,
		securityAudit: securityAudit,
		clock:         clock,
	}, nil
}

// ExportTenant returns a canonical, versioned snapshot of one tenant's
// authorization state. Export reads one consistent snapshot under the same
// per-tenant serialization used by normal mutations.
func (m *Maintenance) ExportTenant(ctx context.Context, tenantID TenantID) ([]byte, error) {
	if err := tenantID.Validate(); err != nil {
		return nil, err
	}
	if _, err := m.authorizer.AuthorizeMaintenance(ctx, tenantID, "export"); err != nil {
		return nil, err
	}
	var data []byte
	err := m.store.MaintainTenant(ctx, tenantID, func(tx MaintenanceTx) error {
		var err error
		data, err = tx.ExportTenant(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// PurgeTenant deletes one tenant's authorization state after durable intent
// audit. A successful purge creates a permanent replay tombstone for that
// tenant ID; purged tenant IDs are never reused and cannot be bootstrapped
// again.
//
// Purge requires a non-empty request ID. Retrying with the same request ID
// reads the retained ledger, returns AlreadyCommitted=true, performs no
// deletion, and retries only the missing completion event using the original
// canonical event.
func (m *Maintenance) PurgeTenant(ctx context.Context, tenantID TenantID, meta MutationMetadata) error {
	if err := tenantID.Validate(); err != nil {
		return ErrInvalidInput
	}
	if meta.RequestID == "" || len(meta.RequestID) > 128 {
		return ErrInvalidInput
	}
	if meta.ReasonCode == "" || len(meta.ReasonCode) > 64 {
		return ErrInvalidInput
	}
	provenance, err := m.authorizer.AuthorizeMaintenance(ctx, tenantID, "purge")
	if err != nil {
		return err
	}

	intent := MutationAuditEvent{
		TenantID:      tenantID,
		HostInitiator: provenance.Initiator,
		Operation:     "tenant.purge",
		Phase:         AuditPhaseIntent,
		TargetType:    "tenant",
		TargetID:      string(tenantID),
		Outcome:       AuditOutcomeSuccess,
		ReasonCode:    meta.ReasonCode,
		RequestID:     meta.RequestID,
		At:            m.clock.Now(),
	}
	if err := m.securityAudit.RecordSecurityEvent(ctx, intent); err != nil {
		return ErrAuditUnavailable
	}

	var result PurgeResult
	err = m.store.MaintainTenant(ctx, tenantID, func(tx MaintenanceTx) error {
		var err error
		result, err = tx.PurgeTenant(ctx, intent)
		return err
	})
	if err != nil {
		return err
	}

	completion := result.CanonicalEvent
	completion.Phase = AuditPhaseCompleted
	completion.At = m.clock.Now()
	if err := m.securityAudit.RecordSecurityEvent(ctx, completion); err != nil {
		return ErrAuditUnavailable
	}
	return nil
}
