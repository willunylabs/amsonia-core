//go:build postgres

package coreapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/willunylabs/amsonia-core"
)

func TestOrganizationSettingsLifecycle(t *testing.T) {
	s, _, handler, owner := coreFixture(t)
	ctx := context.Background()
	const password = "organization-settings-password"
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "settings@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.ProvisionMember(ctx, admin, ProvisionMemberInput{TenantID: first.ID, Email: "reader@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	login := func(email string) string {
		t.Helper()
		value, err := s.Login(ctx, LoginInput{Email: email, Password: password})
		if err != nil {
			t.Fatal(err)
		}
		return value.AccessToken
	}
	adminToken, memberToken := login(admin.Email), login(member.Email)
	call := func(method, tenant, body, token string, want int) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, "/api/v1/tenants/"+tenant+"/settings", strings.NewReader(body))
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("%s settings: %d want %d: %s", method, response.Code, want, response.Body.String())
		}
		return response
	}
	read := func(token string) organizationSettings {
		t.Helper()
		var value organizationSettings
		response := call("GET", first.ID, "", token, 200)
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	call("GET", first.ID, "", "", 401)
	value := read(adminToken)
	if value.Name != "First" || value.Version != 1 || !value.CanManage || value.CreatedAt.IsZero() || value.UpdatedAt.IsZero() {
		t.Fatalf("initial: %+v", value)
	}
	if read(memberToken).CanManage {
		t.Fatal("ordinary member can manage settings")
	}
	call("PATCH", first.ID, `{"name":"Forbidden","expected_version":1}`, memberToken, 403)
	if _, err := s.renameOrganization(ctx, first.ID, member.ID, "Bypass", 1); !errors.Is(err, amsonia.ErrForbidden) {
		t.Fatal("service bypass", err)
	}
	for _, body := range []string{`{}`, `{"name":" ","expected_version":1}`, `{"name":"Bad\nname","expected_version":1}`, `{"name":"Good","expected_version":0}`, `{"name":"Good","expected_version":1,"tenant_id":"other"}`, `{"name":"Good","expected_version":1.5}`} {
		call("PATCH", first.ID, body, adminToken, 400)
	}
	call("PATCH", first.ID, `{"name":"  Renamed  ","expected_version":1}`, adminToken, 200)
	value = read(adminToken)
	if value.Name != "Renamed" || value.Version != 2 {
		t.Fatalf("update: %+v", value)
	}
	call("PATCH", first.ID, `{"name":"Stale","expected_version":1}`, adminToken, 409)
	call("PATCH", first.ID, `{"name":"Renamed","expected_version":2}`, adminToken, 200)
	if read(adminToken).Version != 2 {
		t.Fatal("noop advanced version")
	}
	var success, noop int
	if err := owner.QueryRow(ctx, `SELECT count(*) FILTER(WHERE outcome='success'), count(*) FILTER(WHERE outcome='noop') FROM amsonia.audit_events WHERE tenant_id=$1 AND operation='organization.update' AND actor_subject=$2 AND target_id=$1 AND request_id <> ''`, first.ID, admin.ID).Scan(&success, &noop); err != nil || success != 1 || noop != 1 {
		t.Fatalf("audit: %d/%d %v", success, noop, err)
	}
	// A failed audit must roll back the name and settings version.
	if _, err := owner.Exec(ctx, `CREATE FUNCTION public.reject_settings_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation='organization.update' THEN RAISE EXCEPTION 'injected audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_settings_audit BEFORE INSERT ON amsonia.audit_events FOR EACH ROW EXECUTE FUNCTION public.reject_settings_audit()`); err != nil {
		t.Fatal(err)
	}
	call("PATCH", first.ID, `{"name":"Must roll back","expected_version":2}`, adminToken, 500)
	if _, err := owner.Exec(ctx, "DROP TRIGGER reject_settings_audit ON amsonia.audit_events; DROP FUNCTION public.reject_settings_audit()"); err != nil {
		t.Fatal(err)
	}
	if after := read(adminToken); after.Name != "Renamed" || after.Version != 2 {
		t.Fatal("audit rollback lost", after)
	}
	// The runtime role still cannot read or update the global tenant table.
	if _, err := s.pool.Exec(ctx, "SELECT * FROM amsonia.tenants"); err == nil {
		t.Fatal("global tenant SELECT granted")
	}
	if _, err := s.pool.Exec(ctx, "UPDATE amsonia.tenants SET name='bypass'"); err == nil {
		t.Fatal("global tenant UPDATE granted")
	}
	var signedRows int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM amsonia.organization_settings()").Scan(&signedRows); err != nil || signedRows != 0 {
		t.Fatalf("unsigned settings exposed: %d %v", signedRows, err)
	}
	// Membership and single-mode selection both remain mandatory.
	call("GET", other.ID, "", memberToken, 404)
	s.application = ApplicationOptions{TenancyMode: "single", OrganizationID: first.ID}
	call("GET", other.ID, "", adminToken, 404)
	call("PATCH", other.ID, `{"name":"Bypass","expected_version":1}`, adminToken, 404)
	if _, err := owner.Exec(ctx, `UPDATE amsonia.tenant_memberships SET status='suspended' WHERE tenant_id=$1 AND account_id=$2`, first.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	call("GET", first.ID, "", adminToken, 404)
	if _, err := s.renameOrganization(ctx, first.ID, admin.ID, "Suspended", 2); !errors.Is(err, amsonia.ErrForbidden) {
		t.Fatal("suspended authority accepted", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE amsonia.tenant_memberships SET status='active' WHERE tenant_id=$1 AND account_id=$2;`, first.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `DELETE FROM amsonia.system_administrators WHERE account_id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.renameOrganization(ctx, first.ID, admin.ID, "Former administrator", 2); !errors.Is(err, amsonia.ErrForbidden) {
		t.Fatal("removed administrator accepted", err)
	}
}

func TestOrganizationConcurrentUpdate(t *testing.T) {
	s, _, _, _ := coreApplicationFixture(t, &ApplicationOptions{})
	ctx := context.Background()
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "concurrent@example.test", Password: "organization-settings-password"})
	if err != nil {
		t.Fatal(err)
	}
	tenants, err := s.ListTenants(ctx, admin.ID)
	if err != nil || len(tenants) != 1 {
		t.Fatalf("tenants: %+v %v", tenants, err)
	}
	var group sync.WaitGroup
	errs := make([]error, 2)
	for index := range errs {
		group.Add(1)
		go func() {
			defer group.Done()
			_, errs[index] = s.renameOrganization(ctx, tenants[0].ID, admin.ID, []string{"One", "Two"}[index], 1)
		}()
	}
	group.Wait()
	writes := 0
	for _, err := range errs {
		if err == nil {
			writes++
			continue
		}
		var databaseError *pgconn.PgError
		if !errors.Is(err, amsonia.ErrConflict) && !(errors.As(err, &databaseError) && databaseError.Code == "40001") {
			t.Fatal(err)
		}
	}
	if writes != 1 {
		t.Fatalf("concurrent writes: %d %+v", writes, errs)
	}
	settings, err := s.readOrganization(ctx, tenants[0].ID)
	if err != nil || settings.Version != 2 {
		t.Fatalf("settings: %+v %v", settings, err)
	}
}
