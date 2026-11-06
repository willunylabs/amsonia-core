//go:build postgres

package coreapp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Only synthetic fixture traffic is written, into the CI runner's temporary
// directory. Never enable this capture for production or upload its secrets.
func recordContract(t *testing.T, request *http.Request, body []byte, response *httptest.ResponseRecorder, valid bool) {
	t.Helper()
	filename := os.Getenv("AMSONIA_CONTRACT_OUTPUT")
	if filename == "" {
		return
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var input any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &input); err != nil {
			t.Fatal(err)
		}
	}
	err = json.NewEncoder(file).Encode(map[string]any{
		"method": request.Method, "url": request.URL.RequestURI(), "request_body": input, "request_valid": valid,
		"status": response.Code, "content_type": response.Header().Get("Content-Type"), "body": response.Body.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticationAPIContract(t *testing.T) {
	s, _, handler, _ := coreFixture(t)
	ctx := context.Background()
	const password = "contract-test-password"
	account, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "contract@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := s.CreateTenant(ctx, account, CreateTenantInput{Name: "Contract organization"})
	if err != nil {
		t.Fatal(err)
	}
	var cookie *http.Cookie
	var token string
	call := func(method, path string, input any, status int, valid bool) *httptest.ResponseRecorder {
		t.Helper()
		var body []byte
		if input != nil {
			body, _ = json.Marshal(input)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != status {
			t.Fatalf("%s %s: %d want %d: %s", method, path, res.Code, status, res.Body.String())
		}
		recordContract(t, req, body, res, valid)
		return res
	}
	call("GET", "/health", nil, 200, true)
	call("GET", "/readyz", nil, 200, true)
	call("POST", "/api/v1/auth/restore", nil, 204, true)
	call("GET", "/api/v1/auth/me", nil, 401, true)
	call("POST", "/api/v1/auth/login", map[string]any{"email": account.Email, "password": password, "unknown": true}, 400, false)
	res := call("POST", "/api/v1/auth/login", map[string]any{"email": account.Email, "password": password}, 200, true)
	var session Session
	if err := json.Unmarshal(res.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	token = session.AccessToken
	for _, c := range res.Result().Cookies() {
		if c.Name == "amsonia_refresh" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("refresh cookie contract")
	}
	base := "/api/v1/tenants/" + tenant.ID
	for _, path := range []string{"/api/v1/auth/me", "/api/v1/auth/profile", "/api/v1/auth/sessions", "/api/v1/application", "/api/v1/tenants", "/api/v1/permissions", base + "/settings", base + "/members", base + "/roles", base + "/invitations", base + "/audit-events", base + "/capabilities", base + "/members/" + account.ID, base + "/members/" + account.ID + "/roles"} {
		call("GET", path, nil, 200, true)
	}
	call("POST", "/api/v1/authorization/check", map[string]any{"tenant_id": tenant.ID, "permission": "iam:member:read", "mode": "tenant_action", "resource": map[string]string{"tenant_id": tenant.ID}}, 200, true)
	call("POST", "/api/v1/auth/sessions/revoke-others", map[string]any{}, 204, true)
	call("POST", "/api/v1/auth/refresh", nil, 200, true)
	call("POST", "/api/v1/auth/logout", nil, 204, true)
}
