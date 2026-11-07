//go:build postgres

package coreapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/willunylabs/amsonia-core"
)

func TestHTTPSProxyRefreshCookieWithPathPrefix(t *testing.T) {
	s, manager, _, _ := coreFixture(t)
	ctx := context.Background()
	const password = "prefixed-session-password"
	account, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "prefix@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	catalog, _, err := CoreCatalog()
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := amsonia.NewAuthorizer(catalog, s.store, testWorkspaces{})
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"", "/backend", "/app/backend"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			api, err := NewAPI(s, manager, authorizer, catalog, slog.New(slog.NewTextHandler(io.Discard, nil)), prefix)
			if err != nil {
				t.Fatal(err)
			}
			// The external TLS proxy strips only the configured external prefix.
			server := httptest.NewTLSServer(http.StripPrefix(prefix, api.Handler()))
			defer server.Close()
			client := server.Client()
			client.Jar, err = cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			call := func(path, body string, status int) Session {
				t.Helper()
				response, err := client.Post(server.URL+prefix+"/api/v1/auth/"+path, "application/json", strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != status {
					b, _ := io.ReadAll(response.Body)
					t.Fatalf("%s: %d %s", path, response.StatusCode, b)
				}
				for _, cookie := range response.Cookies() {
					if cookie.Name != "amsonia_refresh" || cookie.Path != prefix+"/api/v1/auth" || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
						t.Fatalf("cookie scope: %+v", cookie)
					}
				}
				var session Session
				if status == 200 {
					if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
						t.Fatal(err)
					}
					if session.RefreshToken != "" {
						t.Fatal("refresh token leaked into JSON")
					}
				}
				return session
			}
			call("login", `{"email":"`+account.Email+`","password":"`+password+`"}`, 200)
			call("refresh", "", 200)
			latest := call("restore", "", 200)
			// No bearer supplied: the path-scoped cookie must revoke the family.
			call("logout", "", 204)
			if _, err := s.Authenticate(ctx, latest.AccessToken); !errors.Is(err, ErrSessionInvalid) {
				t.Fatalf("logout did not revoke cookie family: %v", err)
			}
			call("restore", "", 204)
		})
	}
}
