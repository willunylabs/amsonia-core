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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/postgres"
)

func TestSingleOrganizationBootstrapAndRecovery(t *testing.T) {
	s, _, _, owner := coreApplicationFixture(t, &ApplicationOptions{OrganizationName: "Example organization"})
	ctx := context.Background()
	if err := s.Ready(ctx); err != nil {
		t.Fatal("fresh deployment cannot start", err)
	}
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "owner@example.test", Password: "organization-password-123"})
	if err != nil {
		t.Fatal(err)
	}
	tenants, err := s.ListTenants(ctx, admin.ID)
	if err != nil || len(tenants) != 1 || tenants[0].Name != "Example organization" {
		t.Fatalf("bootstrap: %+v %v", tenants, err)
	}
	if _, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Bypass"}); !errors.Is(err, amsonia.ErrForbidden) {
		t.Fatal("single mode allowed creation", err)
	}
	if _, err := s.SetupOrganization(ctx, Account{}); !errors.Is(err, amsonia.ErrForbidden) {
		t.Fatal("setup allowed non-admin", err)
	}
	for range 2 {
		item, err := s.SetupOrganization(ctx, admin)
		if err != nil || item.ID != tenants[0].ID {
			t.Fatalf("repeat setup: %+v %v", item, err)
		}
	}
	var count int
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM amsonia.tenants WHERE state='active'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate organizations: %d %v", count, err)
	}
}

func TestSingleOrganizationConcurrentRecovery(t *testing.T) {
	s, _, _, _ := coreFixture(t)
	ctx := context.Background()
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "recover@example.test", Password: "organization-password-123"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate interrupted setup and a pool with only one connection.
	config := s.pool.Config()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store, err := postgres.NewStore(pool, []byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	catalog, controls, err := CoreCatalog()
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := amsonia.NewBootstrapper(catalog, store, controls, testHost{}, amsonia.RealClock{})
	if err != nil {
		t.Fatal(err)
	}
	s, err = NewApplicationService(pool, store, bootstrap, ApplicationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	items := make([]Tenant, 3)
	errors := make([]error, 3)
	for index := range items {
		group.Add(1)
		go func() { defer group.Done(); items[index], errors[index] = s.SetupOrganization(ctx, admin) }()
	}
	group.Wait()
	for index, item := range items {
		if errors[index] != nil || item.ID == "" || item.ID != items[0].ID {
			t.Fatalf("concurrent setup: %+v %v", items, errors)
		}
	}
}

func TestSingleOrganizationEnforcesSelection(t *testing.T) {
	s, _, handler, owner := coreFixture(t)
	ctx := context.Background()
	const password = "organization-password-123"
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "admin@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Selected"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Hidden"})
	if err != nil {
		t.Fatal(err)
	}
	invite, err := s.CreateInvitation(ctx, other.ID, admin.ID, "invited@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(ctx, LoginInput{Email: admin.Email, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	s.application, err = normalizeApplicationOptions(ApplicationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ready(ctx); err == nil {
		t.Fatal("ambiguous database became ready")
	}
	if _, err := s.ListTenants(ctx, admin.ID); err == nil {
		t.Fatal("ambiguous directory selected a tenant")
	}
	s.application.OrganizationID = "missing"
	if err := s.Ready(ctx); err == nil {
		t.Fatal("missing explicit organization became ready")
	}
	s.application.OrganizationID = first.ID
	if err := s.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	tenants, err := s.ListTenants(ctx, admin.ID)
	if err != nil || len(tenants) != 1 || tenants[0].ID != first.ID {
		t.Fatalf("directory leaked: %+v %v", tenants, err)
	}
	call := func(method, path, body, token string, want int) string {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, response.Code, want, response.Body.String())
		}
		return response.Body.String()
	}
	call("GET", "/api/v1/application", "", "", 401)
	if !strings.Contains(call("GET", "/api/v1/application", "", session.AccessToken, 200), `"single"`) {
		t.Fatal("incorrect application mode")
	}
	call("GET", "/api/v1/tenants/"+first.ID+"/members", "", session.AccessToken, 200)
	call("GET", "/api/v1/tenants/"+other.ID+"/members", "", session.AccessToken, 404)
	call("POST", "/api/v1/tenants/"+other.ID+"/roles", `{}`, session.AccessToken, 404)
	call("POST", "/api/v1/tenants", `{"Name":"Bypass"}`, session.AccessToken, 403)
	check, _ := json.Marshal(map[string]any{"tenant_id": other.ID, "permission": "iam:member:read", "mode": "tenant_action", "resource": map[string]string{"tenant_id": other.ID}})
	call("POST", "/api/v1/authorization/check", string(check), session.AccessToken, 404)
	for _, path := range []string{"preview", "accept"} {
		body, _ := json.Marshal(map[string]string{"token": invite.Token, "password": password})
		if path == "preview" {
			body, _ = json.Marshal(map[string]string{"token": invite.Token})
		}
		call("POST", "/api/v1/invitations/"+path, string(body), "", 404)
	}
	var consumed bool
	if err := owner.QueryRow(ctx, "SELECT accepted_at IS NOT NULL FROM amsonia.member_invitations WHERE invitation_id=$1", invite.ID).Scan(&consumed); err != nil || consumed {
		t.Fatal("disabled invitation was consumed", err)
	}
	// A selected organization never substitutes for ordinary membership.
	if err := s.RequireMembership(ctx, first.ID, "unknown-account"); !errors.Is(err, amsonia.ErrNotFound) {
		t.Fatal("membership bypass", err)
	}
	// Explicitly returning to multi mode restores the existing workflow.
	s.application = ApplicationOptions{TenancyMode: "multi"}
	call("GET", "/api/v1/tenants/"+other.ID+"/members", "", session.AccessToken, 200)
	if _, err := s.PreviewInvitation(ctx, invite.Token); err != nil {
		t.Fatal("multi mode rejected invitation", err)
	}
}
