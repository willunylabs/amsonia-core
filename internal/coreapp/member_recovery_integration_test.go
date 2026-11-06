//go:build postgres

package coreapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/willunylabs/amsonia-core"
)

func TestMemberPasswordRecovery(t *testing.T) {
	s, _, handler, owner := coreFixture(t)
	ctx := context.Background()
	const password = "recovery-original-password"
	const next = "recovery-replacement-password"
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "owner@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Recovery organization"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.ProvisionMember(ctx, admin, ProvisionMemberInput{TenantID: tenant.ID, Email: "member@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Other organization"})
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := s.ProvisionMember(ctx, admin, ProvisionMemberInput{TenantID: other.ID, Email: "other@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	login := func(email, pass string) Session {
		t.Helper()
		v, e := s.Login(ctx, LoginInput{Email: email, Password: pass})
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	actor := login(admin.Email, password)
	old := login(member.Email, password)
	rotated, err := s.Refresh(ctx, RefreshInput{RefreshToken: old.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	call := func(path, token string, input any, status int, valid bool) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(input)
		req := httptest.NewRequest("POST", path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != status {
			t.Fatalf("%s: %d want %d %s", path, res.Code, status, res.Body.String())
		}
		recordContract(t, req, body, res, valid)
		return res
	}
	path := "/api/v1/tenants/" + tenant.ID + "/members/" + member.ID + "/password-recovery"
	payload := map[string]string{"current_password": password}
	call(path, "", payload, 401, true)
	call(path, old.AccessToken, payload, 403, true)
	call(path, actor.AccessToken, map[string]string{"current_password": "wrong"}, 400, true)
	var failures int
	if err := owner.QueryRow(ctx, `SELECT failed_login_count FROM amsonia.accounts WHERE account_id=$1`, admin.ID).Scan(&failures); err != nil || failures != 1 {
		t.Fatalf("failed verification wasn't persisted: %d %v", failures, err)
	}
	call(strings.Replace(path, member.ID, admin.ID, 1), actor.AccessToken, payload, 404, true)
	call(strings.Replace(path, member.ID, unrelated.ID, 1), actor.AccessToken, payload, 404, true)
	// Tenant managers have no authority over global credentials, even with member:manage.
	if err := owner.QueryRow(ctx, `INSERT INTO amsonia.subject_roles(tenant_id,subject_id,role_id) SELECT tenant_id,$1,role_id FROM amsonia.subject_roles WHERE tenant_id=$2 AND subject_id=$3 RETURNING subject_id`, member.ID, tenant.ID, admin.ID).Scan(new(string)); err != nil {
		t.Fatal(err)
	}
	call(path, old.AccessToken, payload, 403, true)
	issue := func() PasswordRecovery {
		t.Helper()
		res := call(path, actor.AccessToken, payload, 201, true)
		var v PasswordRecovery
		if err := json.Unmarshal(res.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	first := issue()
	second := issue()
	if _, err := s.PreviewPasswordRecovery(ctx, first.Token); !errors.Is(err, amsonia.ErrNotFound) {
		t.Fatalf("old link survived reissue: %v", err)
	}
	var persisted []byte
	if err := owner.QueryRow(ctx, `SELECT token_hash FROM amsonia.member_password_recoveries WHERE recovery_id=$1`, second.ID).Scan(&persisted); err != nil || bytes.Equal(persisted, []byte(second.Token)) || len(persisted) != 32 {
		t.Fatal("recovery secret storage", err)
	}
	call("/api/v1/password-recovery/preview", "", map[string]string{"token": second.Token}, 200, true)
	call("/api/v1/password-recovery/complete", "", map[string]string{"token": second.Token, "new_password": "short"}, 400, false)
	call("/api/v1/password-recovery/complete", "", map[string]string{"token": second.Token, "new_password": next}, 204, true)
	call("/api/v1/password-recovery/complete", "", map[string]string{"token": second.Token, "new_password": password}, 404, true)
	for _, v := range []Session{old, rotated} {
		if _, err := s.Authenticate(ctx, v.AccessToken); !errors.Is(err, ErrSessionInvalid) {
			t.Fatal("old access survived", err)
		}
	}
	if _, err := s.Login(ctx, LoginInput{Email: member.Email, Password: password}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("old password accepted", err)
	}
	recovered := login(member.Email, next)
	if _, err := s.Refresh(ctx, RefreshInput{RefreshToken: old.RefreshToken}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal("old refresh survived", err)
	}
	if _, err := s.Authenticate(ctx, recovered.AccessToken); err != nil {
		t.Fatal("old replay killed new session", err)
	}
	if _, err := s.Authenticate(ctx, actor.AccessToken); err != nil {
		t.Fatal("issuer session affected", err)
	}
	var audits int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM amsonia.audit_events WHERE tenant_id=$1 AND operation='account.password_recovery.complete'`, tenant.ID).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("completion audit", audits, err)
	}
	pending := issue()
	if err := s.ChangePassword(ctx, member.ID, next, password); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewPasswordRecovery(ctx, pending.Token); !errors.Is(err, amsonia.ErrNotFound) {
		t.Fatal("password change retained recovery link", err)
	}
	expired := issue()
	s.now = func() time.Time { return time.Now().Add(time.Hour) }
	if err := s.CompletePasswordRecovery(ctx, expired.Token, next); !errors.Is(err, amsonia.ErrNotFound) {
		t.Fatal("expired link accepted", err)
	}
	s.now = func() time.Time { return time.Now().UTC() }
	suspended := issue()
	if err := s.ManageMember(ctx, tenant.ID, admin.ID, member.ID, "suspended", false); err != nil {
		t.Fatal(err)
	}
	call(path, actor.AccessToken, payload, 404, true)
	if err := s.CompletePasswordRecovery(ctx, suspended.Token, next); !errors.Is(err, amsonia.ErrNotFound) {
		t.Fatal("suspended member recovered", err)
	}
	if err := s.ManageMember(ctx, tenant.ID, admin.ID, member.ID, "active", false); err != nil {
		t.Fatal(err)
	}
	// Two simultaneous completions cannot both consume a token.
	concurrent := issue()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- s.CompletePasswordRecovery(ctx, concurrent.Token, next) })
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, amsonia.ErrNotFound) {
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != "40001" {
				t.Fatal("parallel completion", err)
			}
		}
	}
	if success != 1 {
		t.Fatalf("consumed %d times", success)
	}
	// Single-organization mode never accepts links issued for another organization.
	hidden, err := s.CreatePasswordRecovery(ctx, other.ID, admin.ID, unrelated.ID, password)
	if err != nil {
		t.Fatal(err)
	}
	s.application = ApplicationOptions{TenancyMode: "single", OrganizationID: tenant.ID}
	if _, err := s.PreviewPasswordRecovery(ctx, hidden.Token); !errors.Is(err, amsonia.ErrNotFound) {
		t.Fatal("cross-organization link exposed", err)
	}
}
