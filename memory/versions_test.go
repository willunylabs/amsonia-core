package memory

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/willunylabs/amsonia-core"
)

func TestRoleVersionCannotBeOverwritten(t *testing.T) {
	store := NewStore()
	ctx := t.Context()
	const tenant amsonia.TenantID = "tenant-a"
	original := amsonia.RoleVersion{TenantID: tenant, RoleID: "reader", Version: 1, Name: "Original"}
	if err := store.MutateTenant(ctx, tenant, func(tx amsonia.TenantTx) error {
		if err := tx.InsertRole(ctx, amsonia.Role{TenantID: tenant, RoleID: original.RoleID, Version: 1}); err != nil {
			return err
		}
		return tx.InsertRoleVersion(ctx, original)
	}); err != nil {
		t.Fatal(err)
	}
	for _, sameTransaction := range []bool{false, true} {
		err := store.MutateTenant(ctx, tenant, func(tx amsonia.TenantTx) error {
			replacement := original
			replacement.Name = "Replacement"
			if sameTransaction {
				replacement.Version = 2
				if err := tx.InsertRoleVersion(ctx, replacement); err != nil {
					return err
				}
			}
			return tx.InsertRoleVersion(ctx, replacement)
		})
		if !errors.Is(err, amsonia.ErrConflict) {
			t.Fatalf("duplicate version accepted (same transaction=%t): %v", sameTransaction, err)
		}
	}
	if err := store.MaintainTenant(ctx, tenant, func(tx amsonia.MaintenanceTx) error {
		data, err := tx.ExportTenant(ctx)
		if err != nil {
			return err
		}
		var export struct {
			Versions []amsonia.RoleVersion `json:"versions"`
		}
		if err := json.Unmarshal(data, &export); err != nil {
			return err
		}
		if len(export.Versions) != 1 || export.Versions[0].Name != original.Name || export.Versions[0].Version != original.Version {
			t.Fatalf("duplicate insert changed immutable history or escaped rollback: %+v", export.Versions)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
