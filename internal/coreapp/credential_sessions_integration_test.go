//go:build postgres

package coreapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCredentialChangesEndOldRefreshLineage(t *testing.T) {
	for _, workflow := range []string{"password-change", "operator-recovery", "member-provisioning"} {
		t.Run(workflow, func(t *testing.T) {
			s, _, handler, owner := coreFixture(t)
			ctx := context.Background()
			const oldPassword = "original-credential-password"
			const newPassword = "replacement-credential-password"
			admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "admin@example.test", Password: oldPassword})
			if err != nil {
				t.Fatal(err)
			}
			tenant, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Credential changes"})
			if err != nil {
				t.Fatal(err)
			}
			account := admin
			if workflow == "member-provisioning" {
				account, err = s.ProvisionMember(ctx, admin, ProvisionMemberInput{TenantID: tenant.ID, Email: "member@example.test", Password: oldPassword})
				if err != nil {
					t.Fatal(err)
				}
			}
			first, err := s.Login(ctx, LoginInput{Email: account.Email, Password: oldPassword})
			if err != nil {
				t.Fatal(err)
			}
			rotated, err := s.Refresh(ctx, RefreshInput{RefreshToken: first.RefreshToken})
			if err != nil {
				t.Fatal(err)
			}
			switch workflow {
			case "password-change":
				body, err := json.Marshal(map[string]string{"current_password": oldPassword, "new_password": newPassword})
				if err != nil {
					t.Fatal(err)
				}
				request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(string(body)))
				request.Header.Set("Authorization", "Bearer "+rotated.AccessToken)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusNoContent {
					t.Fatalf("password change: %d %s", response.Code, response.Body.String())
				}
			case "operator-recovery":
				if _, err := RecoverAdministrator(ctx, owner, account.Email, newPassword); err != nil {
					t.Fatal(err)
				}
			case "member-provisioning":
				if _, err := s.ProvisionMember(ctx, admin, ProvisionMemberInput{TenantID: tenant.ID, Email: account.Email, Password: newPassword}); err != nil {
					t.Fatal(err)
				}
			}
			fresh, err := s.Login(ctx, LoginInput{Email: account.Email, Password: newPassword})
			if err != nil {
				t.Fatal(err)
			}
			for _, stale := range []Session{first, rotated} {
				if _, err := s.Authenticate(ctx, stale.AccessToken); !errors.Is(err, ErrSessionInvalid) {
					t.Fatalf("old access survived credential change: %v", err)
				}
				if _, err := s.Refresh(ctx, RefreshInput{RefreshToken: stale.RefreshToken}); !errors.Is(err, ErrSessionInvalid) {
					t.Fatalf("old refresh survived credential change: %v", err)
				}
				if _, err := s.Authenticate(ctx, fresh.AccessToken); err != nil {
					t.Fatalf("old refresh replay revoked new access: %v", err)
				}
			}
			if _, err := s.Refresh(ctx, RefreshInput{RefreshToken: fresh.RefreshToken}); err != nil {
				t.Fatalf("old refresh replay revoked the new refresh lineage: %v", err)
			}
		})
	}
}
