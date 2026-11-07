//go:build postgres

package coreapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/willunylabs/amsonia-core"
)

type canceledBootstrapHost struct{ cancel context.CancelFunc }

func (host canceledBootstrapHost) AuthorizeBootstrap(ctx context.Context, _ amsonia.TenantID) (amsonia.HostProvenance, error) {
	host.cancel()
	return amsonia.HostProvenance{}, ctx.Err()
}

func TestOrganizationCreationFailureCompensation(t *testing.T) {
	for _, scenario := range []string{"activation failure", "canceled bootstrap", "compensation failure"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, _, owner := coreFixture(t)
			ctx := context.Background()
			admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "setup@example.test", Password: "organization-setup-password"})
			if err != nil {
				t.Fatal(err)
			}
			s.application = ApplicationOptions{TenancyMode: "single", OrganizationName: "Recovered organization"}
			pending, cancel := context.WithCancel(ctx)
			defer cancel()
			bootstrap := s.bootstrap
			if scenario == "canceled bootstrap" {
				catalog, controls, err := CoreCatalog()
				if err != nil {
					t.Fatal(err)
				}
				s.bootstrap, err = amsonia.NewBootstrapper(catalog, s.store, controls, canceledBootstrapHost{cancel: cancel}, amsonia.RealClock{})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := owner.Exec(ctx, `CREATE FUNCTION amsonia.reject_fixture_membership() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture activation failure'; END $$;
				CREATE TRIGGER fixture_membership BEFORE INSERT ON amsonia.tenant_memberships FOR EACH ROW EXECUTE FUNCTION amsonia.reject_fixture_membership()`)
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "compensation failure" {
				_, err := owner.Exec(ctx, `CREATE FUNCTION amsonia.reject_fixture_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='failed' THEN RAISE EXCEPTION 'fixture compensation failure'; END IF; RETURN NEW; END $$;
				CREATE TRIGGER fixture_failure BEFORE UPDATE ON amsonia.tenants FOR EACH ROW EXECUTE FUNCTION amsonia.reject_fixture_failure()`)
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err = s.SetupOrganization(pending, admin)
			if err == nil {
				t.Fatal("fault did not interrupt setup")
			}
			if scenario == "canceled bootstrap" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation cause: %v", err)
			}
			if scenario != "canceled bootstrap" && !strings.Contains(err.Error(), "fixture activation failure") {
				t.Fatalf("lost creation cause: %v", err)
			}
			if scenario == "compensation failure" && !strings.Contains(err.Error(), "fixture compensation failure") {
				t.Fatalf("lost compensation cause: %v", err)
			}
			var id, state string
			var memberships int
			if err := owner.QueryRow(ctx, `SELECT tenant_id,state,(SELECT count(*) FROM amsonia.tenant_memberships WHERE tenant_id=t.tenant_id) FROM amsonia.tenants t`).Scan(&id, &state, &memberships); err != nil {
				t.Fatal(err)
			}
			want := "failed"
			if scenario == "compensation failure" {
				want = "pending"
			}
			if state != want || memberships != 0 {
				t.Fatalf("failure state=%s memberships=%d", state, memberships)
			}
			if scenario == "activation failure" {
				var roles int
				if err := owner.QueryRow(ctx, `SELECT count(*) FROM amsonia.roles WHERE tenant_id=$1`, id).Scan(&roles); err != nil || roles == 0 {
					t.Fatalf("fault did not exercise committed bootstrap: %d %v", roles, err)
				}
			}
			// A different actor cannot transition another creator's pending row.
			if err := s.store.FailCreatedTenant(ctx, "foreign-actor", amsonia.TenantID(id)); !errors.Is(err, amsonia.ErrConflict) {
				t.Fatalf("actor boundary: %v", err)
			}
			if _, err := owner.Exec(ctx, `DROP TRIGGER IF EXISTS fixture_membership ON amsonia.tenant_memberships; DROP TRIGGER IF EXISTS fixture_failure ON amsonia.tenants`); err != nil {
				t.Fatal(err)
			}
			s.bootstrap = bootstrap
			recovered, err := s.SetupOrganization(ctx, admin)
			if err != nil || recovered.State != "active" || recovered.ID == id {
				t.Fatalf("setup retry: %+v %v", recovered, err)
			}
			items, err := s.ListTenants(ctx, admin.ID)
			if err != nil || len(items) != 1 || items[0].ID != recovered.ID {
				t.Fatalf("failed/pending tenant entered discovery: %+v %v", items, err)
			}
		})
	}
}
