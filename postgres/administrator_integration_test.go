//go:build postgres

package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/memory"
)

func TestAdministratorCheckValidatesPersistedGrants(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) amsonia.Store
	}{
		{"memory", func(*testing.T) amsonia.Store { return memory.NewStore() }},
		{"postgres", func(t *testing.T) amsonia.Store { return setupPostgres(t) }},
	} {
		t.Run(backend.name, func(t *testing.T) {
			store := backend.open(t)
			ctx := t.Context()
			catalog := testCatalog(t)
			bootstrapper, err := amsonia.NewBootstrapper(catalog, store, testControls, hostAuthorizer{}, fixedTime())
			if err != nil {
				t.Fatal(err)
			}
			manager, err := amsonia.NewManager(catalog, store, noMemberships{}, testControls, nullAudit{}, fixedTime())
			if err != nil {
				t.Fatal(err)
			}
			actor := amsonia.Principal{TenantID: "test-tenant", SubjectID: "owner"}
			const ownerRole = amsonia.RoleID("owner-role")
			const backupRole = amsonia.RoleID("backup-role")
			const backup = amsonia.SubjectID("backup")
			if _, err := bootstrapper.BootstrapTenant(ctx, bootstrapInput(actor.TenantID, actor.SubjectID, ownerRole)); err != nil {
				t.Fatal(err)
			}
			malformed := amsonia.RolePermissionGrant{RoleID: backupRole, Permission: permRoleManage, Scope: amsonia.ScopeTenant, WorkspaceRoles: []string{"*"}}
			// Model bad persisted data through the low-level store used by hosts.
			if err := store.MutateTenant(ctx, actor.TenantID, func(tx amsonia.TenantTx) error {
				if err := tx.InsertRole(ctx, amsonia.Role{TenantID: actor.TenantID, RoleID: backupRole, Name: "Backup", Version: 1}); err != nil {
					return err
				}
				for _, grant := range []amsonia.RolePermissionGrant{
					malformed,
					{RoleID: backupRole, Permission: permGrantManage, Scope: amsonia.ScopeTenant},
					{RoleID: backupRole, Permission: permRoleAssign, Scope: amsonia.ScopeTenant},
				} {
					if _, err := tx.InsertRolePermissionGrant(ctx, grant); err != nil {
						return err
					}
				}
				_, err := tx.InsertSubjectRoleGrant(ctx, amsonia.SubjectRoleGrant{
					TenantID: actor.TenantID, SubjectID: backup, RoleID: backupRole,
					Provenance: amsonia.GrantProvenanceBootstrap, GrantedAt: fixedTime().Now(),
				})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if err := store.ReadTenant(ctx, actor.TenantID, func(reader amsonia.TenantReader) error {
				ok, err := reader.HasAdministrator(ctx, testControls)
				if ok || !errors.Is(err, amsonia.ErrInvalidPolicy) {
					return fmt.Errorf("malformed grant hidden by valid administrator: got %t, %v", ok, err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			assertOwnerProtected := func(want error) {
				t.Helper()
				result, err := manager.UnassignRole(ctx, actor, amsonia.MutationMetadata{ReasonCode: "check_admin"}, amsonia.UnassignRoleInput{
					SubjectID: actor.SubjectID, RoleID: ownerRole, ExpectedRoleVersion: 1,
				})
				if !errors.Is(err, want) || result.Changed {
					t.Fatalf("unassign owner: result=%+v, error=%v, want %v", result, err, want)
				}
				if err := store.ReadTenant(ctx, actor.TenantID, func(reader amsonia.TenantReader) error {
					roles, err := reader.ListSubjectRoleIDs(ctx, actor.SubjectID)
					if err != nil {
						return err
					}
					if len(roles) != 1 || roles[0] != ownerRole {
						return fmt.Errorf("owner binding did not roll back: %v", roles)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			assertOwnerProtected(amsonia.ErrInvalidPolicy)

			// A retired role can retain rows after a faulty import. Those rows
			// must confer neither administrator status nor individual controls.
			if err := store.MutateTenant(ctx, actor.TenantID, func(tx amsonia.TenantTx) error {
				return tx.TombstoneRole(ctx, backupRole, 1)
			}); err != nil {
				t.Fatal(err)
			}
			assertOwnerProtected(amsonia.ErrLastAdministrator)
			if err := store.ReadTenant(ctx, actor.TenantID, func(reader amsonia.TenantReader) error {
				grants, err := reader.ListEffectiveGrants(ctx, backup, permGrantManage)
				if err != nil {
					return err
				}
				if len(grants) != 0 {
					return fmt.Errorf("retired role still confers grants: %v", grants)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
