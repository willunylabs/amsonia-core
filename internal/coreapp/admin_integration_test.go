//go:build postgres

package coreapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/postgres"
)

type testHost struct{}

func (testHost) AuthorizeBootstrap(context.Context, amsonia.TenantID) (amsonia.HostProvenance, error) {
	return amsonia.HostProvenance{Initiator: "integration", At: time.Now().UTC()}, nil
}

type testWorkspaces struct{}

func (testWorkspaces) LookupWorkspaceMembership(context.Context, amsonia.TenantID, amsonia.WorkspaceID, amsonia.SubjectID) (amsonia.WorkspaceMembership, error) {
	return amsonia.WorkspaceMembership{}, amsonia.ErrNotFound
}

func coreFixture(t *testing.T) (*Service, *amsonia.Manager, http.Handler, *pgxpool.Pool) {
	return coreApplicationFixture(t, nil)
}

func coreApplicationFixture(t *testing.T, options *ApplicationOptions) (*Service, *amsonia.Manager, http.Handler, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("TEST_DATABASE_ADMIN_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_ADMIN_URL not set")
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	suffix := fmt.Sprint(time.Now().UnixNano())
	database := "core_admin_" + suffix
	runtimeRole := "core_runtime_" + suffix
	maintenanceRole := "core_maintenance_" + suffix
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{database}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{database}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		for _, role := range []string{runtimeRole, maintenanceRole} {
			if _, err := admin.Exec(ctx, "DROP ROLE IF EXISTS "+pgx.Identifier{role}.Sanitize()); err != nil {
				t.Error(err)
			}
		}
	})
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = database
	owner, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	if err := postgres.Migrate(ctx, owner); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{runtimeRole, maintenanceRole} {
		if _, err := owner.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN PASSWORD 'integration-only' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT"); err != nil {
			t.Fatal(err)
		}
	}
	secretHex := strings.Repeat("61", 32)
	raw, err := os.ReadFile(filepath.Join("..", "..", "ops", "configure_database_roles.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	sql = strings.ReplaceAll(sql[strings.Index(sql, "BEGIN;"):], `\gexec`, ";")
	for key, value := range map[string]string{`:"runtime_role"`: pgx.Identifier{runtimeRole}.Sanitize(), `:"maintenance_role"`: pgx.Identifier{maintenanceRole}.Sanitize(), `:'runtime_role'`: "'" + runtimeRole + "'", `:'maintenance_role'`: "'" + maintenanceRole + "'", `:'runtime_secret_hex'`: "'" + secretHex + "'", `:'maintenance_secret_hex'`: "'" + secretHex + "'"} {
		sql = strings.ReplaceAll(sql, key, value)
	}
	if _, err := owner.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
	runtimeConfig := config.Copy()
	runtimeConfig.ConnConfig.User = runtimeRole
	runtimeConfig.ConnConfig.Password = "integration-only"
	pool, err := pgxpool.NewWithConfig(ctx, runtimeConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
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
	service, err := NewService(pool, store, bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if options != nil {
		service, err = NewApplicationService(pool, store, bootstrap, *options)
		if err != nil {
			t.Fatal(err)
		}
	}
	manager, err := amsonia.NewManager(catalog, service.PolicyStore(), testWorkspaces{}, controls, service, amsonia.RealClock{})
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := amsonia.NewAuthorizer(catalog, store, testWorkspaces{})
	if err != nil {
		t.Fatal(err)
	}
	api, err := NewAPI(service, manager, authorizer, catalog, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return service, manager, api.Handler(), owner
}

func TestCoreAdministrationLifecycle(t *testing.T) {
	s, manager, handler, owner := coreFixture(t)
	ctx := context.Background()
	password := "initial-password-123"
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "admin@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "First tenant"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Other tenant"})
	if err != nil {
		t.Fatal(err)
	}
	login := func(email, password string) Session {
		t.Helper()
		value, err := s.Login(ctx, LoginInput{Email: email, Password: password, UserAgent: "integration-browser"})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	adminSession := login(admin.Email, password)
	call := func(method, path, token string, input any, status int) *httptest.ResponseRecorder {
		t.Helper()
		var body []byte
		if input != nil {
			body, _ = json.Marshal(input)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		recordContract(t, req, body, res, true)
		if res.Code != status {
			t.Fatalf("%s %s: %d want %d: %s", method, path, res.Code, status, res.Body.String())
		}
		if res.Header().Get("X-Request-ID") == "" {
			t.Fatal("missing request ID")
		}
		return res
	}
	base := "/api/v1/tenants/" + tenant.ID
	call("PATCH", "/api/v1/auth/profile", adminSession.AccessToken, map[string]any{"display_name": "Admin 用户"}, 200)
	call("PATCH", "/api/v1/auth/profile", adminSession.AccessToken, map[string]any{"display_name": "bad\x01name"}, 400)
	res := call("POST", base+"/invitations", adminSession.AccessToken, map[string]any{"email": "Member@example.test"}, 201)
	var invite CreatedInvitation
	if err := json.Unmarshal(res.Body.Bytes(), &invite); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(invite.Token, " ") {
		t.Fatal("invalid token")
	}
	call("POST", "/api/v1/invitations/preview", "", map[string]any{"token": invite.Token}, 200)
	call("POST", "/api/v1/invitations/accept", "", map[string]any{"token": invite.Token, "password": "short"}, 400)
	accepted := call("POST", "/api/v1/invitations/accept", "", map[string]any{"token": invite.Token, "password": password}, 200)
	var member Account
	if err := json.Unmarshal(accepted.Body.Bytes(), &member); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/v1/invitations/accept", "", map[string]any{"token": invite.Token, "password": password}, 404)
	memberSession := login(member.Email, password)
	call("GET", base+"/capabilities", memberSession.AccessToken, nil, 200)
	call("GET", base+"/members", memberSession.AccessToken, nil, 403)
	call("POST", base+"/invitations", memberSession.AccessToken, map[string]any{"email": "intruder@example.test"}, 403)
	call("GET", "/api/v1/tenants/"+other.ID+"/roles", memberSession.AccessToken, nil, 404)
	call("POST", base+"/roles", adminSession.AccessToken, map[string]any{"id": "support", "name": "Support", "description": "", "permissions": []string{"iam:member:read", "iam:role:read"}}, 201)
	call("PATCH", base+"/roles/support", adminSession.AccessToken, map[string]any{"name": "Support team", "description": "Updated", "expected_version": 1}, 200)
	call("PATCH", base+"/roles/support", adminSession.AccessToken, map[string]any{"name": "Stale", "expected_version": 1}, 409)
	call("POST", base+"/roles/support/grants", adminSession.AccessToken, map[string]any{"permission": "iam:audit:read", "scope": "own", "workspace_roles": []string{}, "expected_version": 2}, 200)
	detailResponse := call("GET", base+"/roles/support", adminSession.AccessToken, nil, 200)
	var detail RoleDetail
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Versions) != 3 || len(detail.Grants) != 3 {
		t.Fatalf("role detail %#v", detail)
	}
	binding := base + "/members/" + member.ID + "/roles/support"
	call("PUT", binding, adminSession.AccessToken, map[string]any{"expected_version": 3}, 200)
	call("GET", base+"/members", memberSession.AccessToken, nil, 200)
	call("GET", base+"/audit-events", memberSession.AccessToken, nil, 403) // own scope never authorizes a tenant action
	// Role composition requires grant-management authority in addition to actual coverage.
	call("POST", base+"/roles", adminSession.AccessToken, map[string]any{"id": "builder", "name": "Builder", "permissions": []string{"iam:role:manage", "iam:member:read"}}, 201)
	call("PUT", base+"/members/"+member.ID+"/roles/builder", adminSession.AccessToken, map[string]any{"expected_version": 1}, 200)
	call("POST", base+"/roles", memberSession.AccessToken, map[string]any{"id": "empty", "name": "Empty"}, 201)
	call("POST", base+"/roles", memberSession.AccessToken, map[string]any{"id": "escalated", "name": "Escalated", "permissions": []string{"iam:member:read"}}, 403)
	call("POST", base+"/roles/empty/grants", memberSession.AccessToken, map[string]any{"permission": "iam:member:read", "scope": "tenant", "expected_version": 1}, 403)

	call("PATCH", base+"/members/"+admin.ID, adminSession.AccessToken, map[string]any{"status": "suspended"}, 409)
	call("DELETE", base+"/members/"+admin.ID, adminSession.AccessToken, nil, 409)
	call("PATCH", base+"/members/"+member.ID, adminSession.AccessToken, map[string]any{"status": "suspended"}, 204)
	call("GET", base+"/members", memberSession.AccessToken, nil, 404)
	stale := withPolicyGuard(ctx, policyGuard{Actor: member.ID})
	if _, _, err := manager.CreateRole(stale, amsonia.Principal{TenantID: amsonia.TenantID(tenant.ID), SubjectID: amsonia.SubjectID(member.ID)}, amsonia.MutationMetadata{ReasonCode: "test"}, amsonia.CreateRoleInput{RoleID: "stale_actor", Name: "Stale"}); !errors.Is(err, amsonia.ErrForbidden) {
		t.Fatalf("suspended actor transaction guard: %v", err)
	}

	call("PUT", binding, adminSession.AccessToken, map[string]any{"expected_version": 3}, 409)
	call("PATCH", base+"/members/"+member.ID, adminSession.AccessToken, map[string]any{"status": "active"}, 204)
	call("DELETE", binding, adminSession.AccessToken, map[string]any{"expected_version": 2}, 409)
	call("DELETE", binding, adminSession.AccessToken, map[string]any{"expected_version": 3}, 200)
	call("DELETE", base+"/roles/support", adminSession.AccessToken, map[string]any{"expected_version": 3}, 409)
	for _, grant := range detail.Grants {
		payload := map[string]any{"permission": grant.Permission, "scope": grant.Scope, "workspace_roles": grant.WorkspaceRoles, "expected_version": detail.Role.Version}
		call("DELETE", base+"/roles/support/grants", adminSession.AccessToken, payload, 200)
		detail.Role.Version++
	}
	call("DELETE", base+"/roles/support", adminSession.AccessToken, map[string]any{"expected_version": detail.Role.Version}, 204)
	call("DELETE", base+"/members/"+member.ID, adminSession.AccessToken, nil, 204)
	call("GET", base+"/capabilities", memberSession.AccessToken, nil, 404)
	staleTarget := withPolicyGuard(ctx, policyGuard{Actor: admin.ID, Target: member.ID, Assign: true})
	if _, err := manager.AssignRole(staleTarget, amsonia.Principal{TenantID: amsonia.TenantID(tenant.ID), SubjectID: amsonia.SubjectID(admin.ID)}, amsonia.MutationMetadata{ReasonCode: "test"}, amsonia.AssignRoleInput{SubjectID: amsonia.SubjectID(member.ID), RoleID: "builder", ExpectedRoleVersion: 1}); !errors.Is(err, amsonia.ErrNotFound) {
		t.Fatalf("removed target transaction guard: %v", err)
	}

	// Existing accounts prove their password without resetting it; revocation and expiry invalidate links.
	existing, err := s.CreateInvitation(ctx, other.ID, admin.ID, member.Email)
	if err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/v1/invitations/accept", "", map[string]any{"token": existing.Token, "password": "different-password"}, 400)
	if _, err := s.AcceptInvitation(ctx, existing.Token, password); err != nil {
		t.Fatal(err)
	}
	login(member.Email, password)
	revoked, err := s.CreateInvitation(ctx, tenant.ID, admin.ID, "revoked@example.test")
	if err != nil {
		t.Fatal(err)
	}
	reissued, err := s.CreateInvitation(ctx, tenant.ID, admin.ID, "revoked@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewInvitation(ctx, revoked.Token); !errors.Is(err, amsonia.ErrNotFound) {
		t.Fatalf("reissued token: %v", err)
	}
	if err := s.RevokeInvitation(ctx, tenant.ID, admin.ID, reissued.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewInvitation(ctx, reissued.Token); !errors.Is(err, amsonia.ErrNotFound) {
		t.Fatalf("revoked token: %v", err)
	}
	expired, err := s.CreateInvitation(ctx, tenant.ID, admin.ID, "expired@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `UPDATE amsonia.member_invitations SET expires_at=now()-interval '1 second' WHERE invitation_id=$1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewInvitation(ctx, expired.Token); !errors.Is(err, amsonia.ErrNotFound) {
		t.Fatalf("expired token: %v", err)
	}
	// Last administrator must remain active even if a suspended member retains administrator grants.
	roles, err := s.ListRoles(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor := amsonia.Principal{TenantID: amsonia.TenantID(other.ID), SubjectID: amsonia.SubjectID(admin.ID)}
	if _, err := manager.AssignRole(ctx, actor, amsonia.MutationMetadata{ReasonCode: "test"}, amsonia.AssignRoleInput{RoleID: roles[0].RoleID, ExpectedRoleVersion: roles[0].Version, SubjectID: amsonia.SubjectID(member.ID)}); err != nil {
		t.Fatal(err)
	}
	if err := s.ManageMember(ctx, other.ID, admin.ID, member.ID, "suspended", false); err != nil {
		t.Fatal(err)
	}
	_, err = manager.UnassignRole(ctx, actor, amsonia.MutationMetadata{ReasonCode: "test"}, amsonia.UnassignRoleInput{RoleID: roles[0].RoleID, SubjectID: actor.SubjectID})
	if !errors.Is(err, amsonia.ErrLastAdministrator) {
		t.Fatalf("suspended administrator counted: %v", err)
	}
	// Cursor paging and filters use stable IDs and remain tenant scoped.
	page, err := s.AuditLog(ctx, tenant.ID, AuditFilter{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor == 0 {
		t.Fatalf("audit page: %#v %v", page, err)
	}
	next, err := s.AuditLog(ctx, tenant.ID, AuditFilter{Limit: 2, Before: page.NextCursor})
	if err != nil || len(next.Items) == 0 || next.Items[0].ID >= page.Items[1].ID {
		t.Fatalf("audit cursor: %#v %v", next, err)
	}
	searched, err := s.AuditLog(ctx, tenant.ID, AuditFilter{Query: member.ID, Operation: "invitation.accept"})
	if err != nil || len(searched.Items) != 1 {
		t.Fatalf("audit search: %#v %v", searched, err)
	}

	// Role history cursor keeps each immutable version once across pages.
	historyActor := amsonia.Principal{TenantID: amsonia.TenantID(tenant.ID), SubjectID: amsonia.SubjectID(admin.ID)}
	for v := int64(1); v <= 22; v++ {
		if _, _, err := manager.UpdateRole(ctx, historyActor, amsonia.MutationMetadata{ReasonCode: "history_test"}, amsonia.UpdateRoleInput{RoleID: "empty", ExpectedVersion: v, Name: fmt.Sprintf("History %d", v)}); err != nil {
			t.Fatal(err)
		}
	}
	firstHistory, err := s.RoleDetail(ctx, tenant.ID, "empty", 0)
	if err != nil || len(firstHistory.Versions) != 20 || firstHistory.NextVersion != 4 {
		t.Fatalf("history first page: %#v %v", firstHistory, err)
	}
	olderHistory, err := s.RoleDetail(ctx, tenant.ID, "empty", firstHistory.NextVersion)
	if err != nil || len(olderHistory.Versions) != 3 || olderHistory.Versions[0].Version != 3 || olderHistory.NextVersion != 0 {
		t.Fatalf("history second page: %#v %v", olderHistory, err)
	}
	// Session family survives rotation, cannot be revoked by another account, and is fully revoked after password change.
	sessions, err := s.ListSessions(ctx, admin.ID, adminSession.AccessToken)
	if err != nil || len(sessions) != 1 || !sessions[0].Current {
		t.Fatalf("sessions: %#v %v", sessions, err)
	}
	refreshed, err := s.Refresh(ctx, RefreshInput{RefreshToken: adminSession.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err = s.ListSessions(ctx, admin.ID, refreshed.AccessToken)
	if err != nil || len(sessions) != 1 || !sessions[0].Current {
		t.Fatalf("rotated sessions: %#v %v", sessions, err)
	}
	if err := s.RevokeSession(ctx, member.ID, sessions[0].ID); !errors.Is(err, amsonia.ErrNotFound) {
		t.Fatalf("foreign session: %v", err)
	}
	if err := s.RevokeSession(ctx, admin.ID, sessions[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, refreshed.AccessToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked access: %v", err)
	}
	if _, err := s.Refresh(ctx, RefreshInput{RefreshToken: refreshed.RefreshToken}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked refresh: %v", err)
	}
	current := login(admin.Email, password)
	newPassword := "replacement-password-123"
	call("POST", "/api/v1/auth/password", current.AccessToken, map[string]any{"current_password": "wrong", "new_password": newPassword}, 400)
	changed := call("POST", "/api/v1/auth/password", current.AccessToken, map[string]any{"current_password": password, "new_password": newPassword}, 204)
	if !strings.Contains(changed.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatal("refresh cookie not cleared")
	}
	if _, err := s.Authenticate(ctx, current.AccessToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("password access: %v", err)
	}
	if _, err := s.Login(ctx, LoginInput{Email: admin.Email, Password: password}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password: %v", err)
	}
	login(admin.Email, newPassword)
	var visible int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM amsonia.tenant_memberships`).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("unbound RLS: %d %v", visible, err)
	}
}
