//go:build postgres

package coreapp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestExactMemberDetail(t *testing.T) {
	s, _, handler, owner := coreFixture(t)
	ctx := context.Background()
	const password = "member-detail-password"
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "detail@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Member forms"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Other tenant"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(ctx, LoginInput{Email: admin.Email, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.accounts(account_id,email,normalized_email,password_hash) SELECT 'viewer','viewer@example.test','viewer@example.test',password_hash FROM amsonia.accounts WHERE account_id=$1`, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.tenant_memberships(tenant_id,account_id) VALUES($1,'viewer')`, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := s.Login(ctx, LoginInput{Email: "viewer@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	call := func(tenant, id, token string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/v1/tenants/"+tenant+"/members/"+id, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("detail: %d want %d: %s", w.Code, status, w.Body.String())
		}
		return w
	}
	response := call(tenant.ID, "viewer", session.AccessToken, 200)
	var member Member
	if err := json.Unmarshal(response.Body.Bytes(), &member); err != nil {
		t.Fatal(err)
	}
	if member.AccountID != "viewer" || member.Email != "viewer@example.test" || member.Status != "active" || member.JoinedAt.IsZero() {
		t.Fatalf("wrong exact member: %+v", member)
	}
	var fields map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || len(fields) != 4 {
		t.Fatalf("unexpected identity fields: %v %v", fields, err)
	}
	call(tenant.ID, "view", session.AccessToken, 404) // no substring lookup
	call(other.ID, "viewer", session.AccessToken, 404)
	call(tenant.ID, "viewer", viewer.AccessToken, 403)
	call(other.ID, admin.ID, viewer.AccessToken, 404)
	call(tenant.ID, "viewer", "", 401)
	if _, err := owner.Exec(ctx, `UPDATE amsonia.tenant_memberships SET status='suspended' WHERE tenant_id=$1 AND account_id='viewer'`, tenant.ID); err != nil {
		t.Fatal(err)
	}
	response = call(tenant.ID, "viewer", session.AccessToken, 200)
	if err := json.Unmarshal(response.Body.Bytes(), &member); err != nil || member.Status != "suspended" {
		t.Fatalf("suspended member unavailable: %+v %v", member, err)
	}
	call(tenant.ID, admin.ID, viewer.AccessToken, 404)
}
