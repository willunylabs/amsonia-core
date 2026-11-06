//go:build postgres

package coreapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestTenantDirectories(t *testing.T) {
	s, _, handler, owner := coreFixture(t)
	ctx := context.Background()
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "admin@example.test", Password: "directory-password-123"})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Directory"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(ctx, LoginInput{Email: admin.Email, Password: "directory-password-123"})
	if err != nil {
		t.Fatal(err)
	}
	// Reuse the fixture password hash: this data tests querying, not password cost.
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.accounts(account_id,email,normalized_email,password_hash)
	 SELECT 'member_' || lpad(i::text,3,'0'), 'Member' || i || '@example.test', 'member' || i || '@example.test', a.password_hash
	 FROM generate_series(1,65) i CROSS JOIN amsonia.accounts a WHERE a.account_id=$1`, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.tenant_memberships(tenant_id,account_id,status,joined_at)
	 SELECT $1, account_id, CASE WHEN account_id>'member_060' THEN 'suspended' ELSE 'active' END, '2026-01-01'::timestamptz
	 FROM amsonia.accounts WHERE account_id LIKE 'member_%'`, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.roles(tenant_id,role_id,name,description)
	 SELECT $1,'role_' || lpad(i::text,3,'0'),'Support ' || i, CASE WHEN i=65 THEN '文字 100%_literal' ELSE '' END FROM generate_series(1,65) i`, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.member_invitations(tenant_id,invitation_id,normalized_email,display_email,token_hash,created_by,expires_at,created_at,accepted_at,revoked_at)
	 SELECT $1,'invite_' || lpad(i::text,3,'0'),'invite' || i || '@example.test','Invite' || i || '@example.test',decode(md5(i::text),'hex'),$2,
	 CASE WHEN i%4=0 THEN now()-interval '1 day' ELSE now()+interval '7 days' END,
	 '2026-01-01'::timestamptz,
	 CASE WHEN i%4=1 THEN now() END, CASE WHEN i%4=2 THEN now() END FROM generate_series(1,245) i`, tenant.ID, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	call := func(path, token string, status int) []byte {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/v1/tenants/"+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("GET %s: %d want %d: %s", path, w.Code, status, w.Body.String())
		}
		return w.Body.Bytes()
	}
	for _, test := range []struct {
		kind, id string
		total    int
	}{{"members", "account_id", 66}, {"roles", "role_id", 66}, {"invitations", "id", 245}} {
		t.Run(test.kind, func(t *testing.T) {
			seen := map[string]bool{}
			after := ""
			for n := 0; n < 20; n++ {
				var page DirectoryPage[map[string]any]
				data := call(tenant.ID+"/"+test.kind+"?limit=17&after="+url.QueryEscape(after), session.AccessToken, 200)
				if err := json.Unmarshal(data, &page); err != nil {
					t.Fatal(err)
				}
				if page.Total != int64(test.total) || len(page.Items) > 17 || len(page.Items) == 0 {
					t.Fatalf("bad page: %+v", page)
				}
				for _, item := range page.Items {
					id := item[test.id].(string)
					if seen[id] {
						t.Fatalf("duplicate %s", id)
					}
					seen[id] = true
					if _, exists := item["token_hash"]; exists {
						t.Fatal("invitation secret leaked")
					}
				}
				if page.NextCursor == "" {
					break
				}
				if after == "" {
					call(other.ID+"/"+test.kind+"?after="+page.NextCursor, session.AccessToken, 400)
					call(tenant.ID+"/"+test.kind+"?q=changed&after="+page.NextCursor, session.AccessToken, 400)
				}
				after = page.NextCursor
			}
			if len(seen) != test.total {
				t.Fatalf("skipped records: %d want %d", len(seen), test.total)
			}
			var empty DirectoryPage[json.RawMessage]
			json.Unmarshal(call(other.ID+"/"+test.kind, session.AccessToken, 200), &empty)
			want := int64(1)
			if test.kind == "invitations" {
				want = 0
			}
			if empty.Total != want {
				t.Fatalf("tenant leak: %+v", empty)
			}
			for _, query := range []string{"limit=0", "limit=101", "after=bad!", "q=" + strings.Repeat("a", 129), "status=unknown"} {
				call(tenant.ID+"/"+test.kind+"?"+query, session.AccessToken, 400)
			}
		})
	}
	for _, test := range []struct {
		path  string
		total int64
	}{
		{"members?status=suspended", 5}, {"members?status=active", 61}, {"members?q=MEMBER65", 1},
		{"roles?q=%25_", 1}, {"roles?q=" + url.QueryEscape("文字"), 1}, {"roles?q=absent", 0},
		{"invitations?status=pending", 61}, {"invitations?status=accepted", 62}, {"invitations?status=revoked", 61}, {"invitations?status=expired", 61},
		{"invitations?q=INVITE1&status=pending", 28},
	} {
		var page DirectoryPage[json.RawMessage]
		if err := json.Unmarshal(call(tenant.ID+"/"+test.path, session.AccessToken, 200), &page); err != nil {
			t.Fatal(err)
		}
		if page.Total != test.total {
			t.Fatalf("%s: total %d want %d", test.path, page.Total, test.total)
		}
	}
	// Newer inserts and role renames cannot move an existing ID across boundaries.
	page, err := s.RoleDirectory(ctx, tenant.ID, DirectoryFilter{Limit: 30})
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Exec(ctx, `UPDATE amsonia.roles SET name='AAA renamed' WHERE tenant_id=$1 AND role_id='role_065'`, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.RoleDirectory(ctx, tenant.ID, DirectoryFilter{Limit: 100, After: page.NextCursor})
	foundRenamed := false
	for _, item := range next.Items {
		foundRenamed = foundRenamed || string(item.RoleID) == "role_065" && item.Name == "AAA renamed"
	}
	if err != nil || len(next.Items) != 36 || !foundRenamed {
		t.Fatalf("renamed role skipped: %+v %v", next, err)
	}
	invitePage, err := s.InvitationDirectory(ctx, tenant.ID, DirectoryFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.member_invitations(tenant_id,invitation_id,normalized_email,display_email,token_hash,created_by,expires_at) VALUES($1,'newest','new@example.test','new@example.test',$2,$3,$4)`, tenant.ID, []byte("new-token"), admin.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	nextInvites, err := s.InvitationDirectory(ctx, tenant.ID, DirectoryFilter{Limit: 100, After: invitePage.NextCursor})
	if err != nil || len(nextInvites.Items) != 100 || nextInvites.Items[0].ID != "invite_145" {
		t.Fatalf("insert shifted invitation cursor: %+v %v", nextInvites, err)
	}
	// A zero-permission member cannot read even collection totals.
	member, err := s.Login(ctx, LoginInput{Email: "member1@example.test", Password: "directory-password-123"})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"members", "roles", "invitations"} {
		call(fmt.Sprintf("%s/%s?limit=1", tenant.ID, kind), member.AccessToken, 403)
		call(fmt.Sprintf("%s/%s?limit=1", other.ID, kind), member.AccessToken, 404)
	}
}
