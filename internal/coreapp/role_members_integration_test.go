//go:build postgres

package coreapp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRoleMemberAdministration(t *testing.T) {
	s, _, handler, owner := coreFixture(t)
	ctx := context.Background()
	const password = "role-members-password-123"
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "admin@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Role members"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	login := func(email string) string {
		t.Helper()
		session, err := s.Login(ctx, LoginInput{Email: email, Password: password})
		if err != nil {
			t.Fatal(err)
		}
		return session.AccessToken
	}
	adminToken := login(admin.Email)
	call := func(method, path, token string, input any, status int) []byte {
		t.Helper()
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "/api/v1/tenants/"+path, strings.NewReader(string(data)))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w.Body.Bytes()
	}
	for name, permissions := range map[string][]string{
		"support": {"iam:member:read"}, "reader": {"iam:role:read"},
		"operator": {"iam:role:read", "iam:member:read", "iam:role:assign"}, "auditor": {"iam:audit:read"},
	} {
		call("POST", tenant.ID+"/roles", adminToken, map[string]any{"id": name, "name": name, "permissions": permissions}, 201)
	}
	call("POST", other.ID+"/roles", adminToken, map[string]any{"id": "support", "name": "support", "permissions": []string{}}, 201)
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.accounts(account_id,email,normalized_email,password_hash)
	 SELECT 'member_' || lpad(i::text,3,'0'),'member' || i || '@example.test','member' || i || '@example.test',a.password_hash
	 FROM generate_series(1,65) i CROSS JOIN amsonia.accounts a WHERE a.account_id=$1`, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.tenant_memberships(tenant_id,account_id,status)
	 SELECT $1,account_id,CASE WHEN account_id IN ('member_034','member_035') OR account_id>'member_060' THEN 'suspended' ELSE 'active' END
	 FROM amsonia.accounts WHERE account_id LIKE 'member_%'`, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.subject_roles(tenant_id,subject_id,role_id)
	 SELECT $1,account_id,'support' FROM amsonia.accounts WHERE account_id BETWEEN 'member_001' AND 'member_035'`, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.subject_roles(tenant_id,subject_id,role_id) VALUES ($1,'member_003','reader'),($1,'member_036','reader'),($1,'member_037','operator')`, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	base := tenant.ID + "/roles/support"
	var summary roleSummary
	if err := json.Unmarshal(call("GET", base+"?summary=true", adminToken, nil, 200), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.SubjectCount != 35 || len(summary.Subjects) != 0 || len(summary.Grants) != 1 || len(summary.Versions) == 0 {
		t.Fatalf("invalid summary: %+v", summary)
	}
	var legacy RoleDetail
	if err := json.Unmarshal(call("GET", base, adminToken, nil, 200), &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy.Subjects) != 35 {
		t.Fatal("default detail contract changed")
	}
	serviceDetail, err := s.RoleDetail(ctx, tenant.ID, "support", 0)
	if err != nil || len(serviceDetail.Subjects) != 35 {
		t.Fatalf("Service contract changed: %+v %v", serviceDetail, err)
	}
	for _, invalid := range []string{"", "1", "yes"} {
		call("GET", base+"?summary="+invalid, adminToken, nil, 400)
	}
	var firstCursor string
	for _, assignment := range []string{"assigned", "available"} {
		seen := map[string]bool{}
		after := ""
		want := int64(35)
		if assignment == "available" {
			want = 31
		}
		for n := 0; n < 10; n++ {
			var page DirectoryPage[Member]
			path := base + "/members?assignment=" + assignment + "&limit=9&after=" + url.QueryEscape(after)
			if err := json.Unmarshal(call("GET", path, adminToken, nil, 200), &page); err != nil {
				t.Fatal(err)
			}
			if page.Total != want || len(page.Items) > 9 {
				t.Fatalf("bad page: %+v", page)
			}
			for _, member := range page.Items {
				if seen[member.AccountID] {
					t.Fatal("duplicate", member.AccountID)
				}
				seen[member.AccountID] = true
			}
			if assignment == "assigned" && firstCursor == "" {
				firstCursor = page.NextCursor
			}
			if page.NextCursor == "" {
				break
			}
			after = page.NextCursor
		}
		if int64(len(seen)) != want {
			t.Fatalf("%s: %d members want %d", assignment, len(seen), want)
		}
	}
	for path, want := range map[string]int64{
		"?status=suspended": 2, "?status=active": 33, "?q=MEMBER3": 7,
		"?q=%25_": 0, "?assignment=available&status=active": 26, "?assignment=available&status=suspended": 5,
	} {
		var page DirectoryPage[Member]
		json.Unmarshal(call("GET", base+"/members"+path, adminToken, nil, 200), &page)
		if page.Total != want {
			t.Fatalf("%s: %+v want %d", path, page, want)
		}
	}
	for _, path := range []string{
		base + "/members?assignment=available&after=" + firstCursor,
		tenant.ID + "/roles/reader/members?after=" + firstCursor,
		other.ID + "/roles/support/members?after=" + firstCursor,
		tenant.ID + "/members?after=" + firstCursor,
		base + "/members?limit=101", base + "/members?assignment=unknown", base + "/members?status=disabled",
	} {
		call("GET", path, adminToken, nil, 400)
	}
	call("GET", tenant.ID+"/roles/missing/members", adminToken, nil, 404)
	roleOnly := login("member36@example.test")
	memberOnly := login("member2@example.test")
	readOnly := login("member3@example.test")
	for _, token := range []string{roleOnly, memberOnly} {
		call("GET", base+"/members?limit=1", token, nil, 403)
	}
	call("GET", base+"/members", readOnly, nil, 200)
	call("GET", other.ID+"/roles/support/members", readOnly, nil, 404)
	call("PUT", tenant.ID+"/members/member_036/roles/support", readOnly, map[string]any{"expected_version": 1}, 403)
	operator := login("member37@example.test")
	call("PUT", tenant.ID+"/members/member_036/roles/auditor", operator, map[string]any{"expected_version": 1}, 403)
	call("PUT", tenant.ID+"/members/member_061/roles/support", adminToken, map[string]any{"expected_version": 1}, 409)
	call("PUT", tenant.ID+"/members/member_036/roles/support", operator, map[string]any{"expected_version": 1}, 200)
	call("PATCH", base, adminToken, map[string]any{"expected_version": 1, "name": "Updated support", "description": ""}, 200)
	call("DELETE", tenant.ID+"/members/member_036/roles/support", adminToken, map[string]any{"expected_version": 1}, 409)
	call("DELETE", tenant.ID+"/members/member_036/roles/support", adminToken, map[string]any{"expected_version": 2}, 200)
	// Suspended bindings can be removed, while the last active administrator cannot.
	call("DELETE", tenant.ID+"/members/member_034/roles/support", adminToken, map[string]any{"expected_version": 2}, 200)
	call("DELETE", tenant.ID+"/members/"+admin.ID+"/roles/role_owner", adminToken, map[string]any{"expected_version": 1}, 409)
	call("DELETE", base, adminToken, map[string]any{"expected_version": 2}, 409)
	// Clearing the role's memberships enables retirement after its grants are removed.
	page, err := s.RoleMemberDirectory(ctx, tenant.ID, "support", "assigned", DirectoryFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range page.Items {
		call("DELETE", tenant.ID+"/members/"+member.AccountID+"/roles/support", adminToken, map[string]any{"expected_version": 2}, 200)
	}
	call("DELETE", base+"/grants", adminToken, map[string]any{"expected_version": 2, "permission": "iam:member:read", "scope": "tenant", "workspace_roles": []string{}}, 200)
	call("DELETE", base, adminToken, map[string]any{"expected_version": 3}, 204)
	call("GET", base+"/members", adminToken, nil, 404)
	var auditCount int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM amsonia.audit_events WHERE tenant_id=$1 AND operation='unassign.role' AND outcome='success'`, tenant.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount < 35 {
		t.Fatalf("missing binding audit events: %d", auditCount)
	}
}
