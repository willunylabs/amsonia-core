//go:build postgres

package coreapp

import (
	"context"
	"errors"
	"testing"

	"github.com/willunylabs/amsonia-core"
)

func TestOperatorAdministratorRecovery(t *testing.T) {
	s, _, _, owner := coreFixture(t)
	ctx := context.Background()
	const oldPassword = "original-test-password"
	const newPassword = "replacement-test-password"
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "admin@example.test", Password: oldPassword})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Recovery test"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.ProvisionMember(ctx, admin, ProvisionMemberInput{TenantID: tenant.ID, Email: "member@example.test", Password: oldPassword})
	if err != nil {
		t.Fatal(err)
	}
	login := func(email string) Session {
		t.Helper()
		session, err := s.Login(ctx, LoginInput{Email: email, Password: oldPassword})
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	first, second, unrelated := login(admin.Email), login(admin.Email), login(member.Email)
	rotated, err := s.Refresh(ctx, RefreshInput{RefreshToken: first.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverAdministrator(ctx, owner, member.Email, newPassword); !errors.Is(err, amsonia.ErrNotFound) {
		t.Fatalf("ordinary member: %v", err)
	}
	if _, err := RecoverAdministrator(ctx, s.pool, admin.Email, newPassword); err == nil {
		t.Fatal("runtime role performed recovery")
	}
	if _, err := s.Authenticate(ctx, first.AccessToken); err != nil {
		t.Fatal("failed recovery revoked session", err)
	}
	_ = login(admin.Email) // failed recovery must also preserve the password
	if _, err := owner.Exec(ctx, "UPDATE amsonia.accounts SET locked_until=now()+interval '1 hour', failed_login_count=4 WHERE account_id=$1", admin.ID); err != nil {
		t.Fatal(err)
	}
	id, err := RecoverAdministrator(ctx, owner, " ADMIN@example.test ", newPassword)
	if err != nil || id != admin.ID {
		t.Fatalf("recovery: %q %v", id, err)
	}
	for _, session := range []Session{first, second, rotated} {
		if _, err := s.Authenticate(ctx, session.AccessToken); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("access survived: %v", err)
		}
		if _, err := s.Refresh(ctx, RefreshInput{RefreshToken: session.RefreshToken}); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("refresh survived: %v", err)
		}
	}
	if _, err := s.Authenticate(ctx, unrelated.AccessToken); err != nil {
		t.Fatal("another account was affected", err)
	}
	var count int
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM amsonia.administrator_recoveries WHERE account_id=$1 AND database_operator=session_user", admin.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("recovery ledger: %d %v", count, err)
	}
	if err := owner.QueryRow(ctx, "SELECT failed_login_count FROM amsonia.accounts WHERE account_id=$1 AND locked_until IS NULL", admin.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("lockout not reset: %d %v", count, err)
	}
	if _, err := s.Login(ctx, LoginInput{Email: admin.Email, Password: oldPassword}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password accepted: %v", err)
	}
	recovered, err := s.Login(ctx, LoginInput{Email: admin.Email, Password: newPassword})
	if err != nil {
		t.Fatal("new password rejected", err)
	}
	if _, err := s.Refresh(ctx, RefreshInput{RefreshToken: first.RefreshToken}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("pre-recovery replay: %v", err)
	}
	if _, err := s.Refresh(ctx, RefreshInput{RefreshToken: recovered.RefreshToken}); err != nil {
		t.Fatal("old replay revoked recovered session", err)
	}
}
