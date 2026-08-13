package coreapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/willunylabs/amsonia-core"
)

type contextKey int

const accountContextKey contextKey = 1

type API struct {
	service    *Service
	manager    *amsonia.Manager
	authorizer *amsonia.Authorizer
	catalog    *amsonia.Catalog
	logger     *slog.Logger
	loginSlots chan struct{}
	startedAt  time.Time
}

func NewAPI(service *Service, manager *amsonia.Manager, authorizer *amsonia.Authorizer, catalog *amsonia.Catalog, logger *slog.Logger) (*API, error) {
	if service == nil || manager == nil || authorizer == nil || catalog == nil || logger == nil {
		return nil, amsonia.ErrInvalidInput
	}
	loginConcurrency := min(4, max(1, runtime.GOMAXPROCS(0)/2))
	return &API{service: service, manager: manager, authorizer: authorizer, catalog: catalog, logger: logger, loginSlots: make(chan struct{}, loginConcurrency), startedAt: time.Now().UTC()}, nil
}

func (api *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.health)
	mux.HandleFunc("GET /readyz", api.ready)
	mux.HandleFunc("POST /api/v1/auth/login", api.login)
	mux.HandleFunc("POST /api/v1/auth/refresh", api.refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", api.logout)
	mux.Handle("GET /api/v1/auth/me", api.auth(http.HandlerFunc(api.me)))
	mux.Handle("GET /api/v1/tenants", api.auth(http.HandlerFunc(api.listTenants)))
	mux.Handle("POST /api/v1/tenants", api.auth(http.HandlerFunc(api.createTenant)))
	mux.Handle("GET /api/v1/permissions", api.auth(http.HandlerFunc(api.listPermissions)))
	mux.Handle("POST /api/v1/authorization/check", api.auth(http.HandlerFunc(api.checkAuthorization)))
	mux.Handle("GET /api/v1/tenants/{tenant_id}/members", api.tenant(http.HandlerFunc(api.listMembers)))
	mux.Handle("GET /api/v1/tenants/{tenant_id}/roles", api.tenant(http.HandlerFunc(api.listRoles)))
	mux.Handle("POST /api/v1/tenants/{tenant_id}/roles", api.tenant(http.HandlerFunc(api.createRole)))
	mux.Handle("GET /api/v1/tenants/{tenant_id}/audit-events", api.tenant(http.HandlerFunc(api.auditEvents)))
	return api.securityHeaders(api.recover(api.requestLog(mux)))
}

func (api *API) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		next.ServeHTTP(response, request)
		api.logger.Info("http request", "method", request.Method, "path", request.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

func (api *API) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				api.logger.Error("panic recovered", "path", request.URL.Path, "error", fmt.Sprint(recovered))
				writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func (api *API) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(response, request)
	})
}

func (api *API) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		token := bearerToken(request.Header.Get("Authorization"))
		if token == "" {
			writeError(response, http.StatusUnauthorized, "authentication_required", "Sign in to continue.")
			return
		}
		account, err := api.service.Authenticate(request.Context(), token)
		if err != nil {
			if !errors.Is(err, ErrSessionInvalid) {
				api.logger.Error("session authentication failed", "error", err)
				writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
				return
			}
			writeError(response, http.StatusUnauthorized, "session_invalid", "Your session has expired. Sign in again.")
			return
		}
		ctx := context.WithValue(request.Context(), accountContextKey, account)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func (api *API) tenant(next http.Handler) http.Handler {
	return api.auth(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		account := accountFromContext(request.Context())
		tenantID := request.PathValue("tenant_id")
		if amsonia.TenantID(tenantID).Validate() != nil {
			writeError(response, http.StatusNotFound, "not_found", "The requested tenant was not found.")
			return
		}
		if err := api.service.RequireMembership(request.Context(), tenantID, account.ID); err != nil {
			if errors.Is(err, amsonia.ErrNotFound) {
				writeError(response, http.StatusNotFound, "not_found", "The requested tenant was not found.")
				return
			}
			api.logger.Error("tenant membership check failed", "tenant", tenantID, "error", err)
			writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		next.ServeHTTP(response, request)
	}))
}

func bearerToken(header string) string {
	prefix, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(prefix, "Bearer") || strings.TrimSpace(token) != token {
		return ""
	}
	return token
}

func accountFromContext(ctx context.Context) Account {
	account, _ := ctx.Value(accountContextKey).(Account)
	return account
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(response, request.Body, 1<<20)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Check the request fields and try again.")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_request", "The request must contain one JSON object.")
		return false
	}
	return true
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeDomainError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, amsonia.ErrInvalidInput):
		writeError(response, http.StatusBadRequest, "invalid_request", "Check the request fields and try again.")
	case errors.Is(err, amsonia.ErrForbidden):
		writeError(response, http.StatusForbidden, "forbidden", "You do not have permission to do that.")
	case errors.Is(err, amsonia.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
	case errors.Is(err, amsonia.ErrConflict):
		writeError(response, http.StatusConflict, "conflict", "The resource changed. Refresh and try again.")
	default:
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

func (api *API) health(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{"status": "ok", "uptime_seconds": int(time.Since(api.startedAt).Seconds())})
}

func (api *API) ready(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := api.service.Ready(ctx); err != nil {
		writeError(response, http.StatusServiceUnavailable, "not_ready", "The service is not ready.")
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "ready"})
}

func (api *API) login(response http.ResponseWriter, request *http.Request) {
	select {
	case api.loginSlots <- struct{}{}:
		defer func() { <-api.loginSlots }()
	default:
		writeError(response, http.StatusTooManyRequests, "login_busy", "Sign-in is busy. Try again shortly.")
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(response, request, &input) {
		return
	}
	session, err := api.service.Login(request.Context(), LoginInput{Email: input.Email, Password: input.Password, RemoteAddress: request.RemoteAddr, UserAgent: request.UserAgent()})
	if err != nil {
		if errors.Is(err, ErrAccountLocked) {
			writeError(response, http.StatusTooManyRequests, "login_temporarily_locked", "Too many attempts. Try again later.")
			return
		}
		if !errors.Is(err, ErrInvalidCredentials) {
			api.logger.Error("login failed", "error", err)
			writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		writeError(response, http.StatusUnauthorized, "invalid_credentials", "The email or password is incorrect.")
		return
	}
	api.setRefreshCookie(response, request, session.RefreshToken, int(api.service.refresh.Seconds()))
	session.RefreshToken = ""
	writeJSON(response, http.StatusOK, session)
}

func (api *API) refresh(response http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie("amsonia_refresh")
	if err != nil {
		api.clearRefreshCookie(response, request)
		writeError(response, http.StatusUnauthorized, "refresh_invalid", "Your session has expired. Sign in again.")
		return
	}
	session, err := api.service.Refresh(request.Context(), RefreshInput{
		RefreshToken:  cookie.Value,
		RemoteAddress: request.RemoteAddr,
		UserAgent:     request.UserAgent(),
	})
	if err != nil {
		api.clearRefreshCookie(response, request)
		if !errors.Is(err, ErrSessionInvalid) {
			api.logger.Error("session refresh failed", "error", err)
			writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		writeError(response, http.StatusUnauthorized, "refresh_invalid", "Your session has expired. Sign in again.")
		return
	}
	api.setRefreshCookie(response, request, session.RefreshToken, int(api.service.refresh.Seconds()))
	session.RefreshToken = ""
	writeJSON(response, http.StatusOK, session)
}

func (api *API) logout(response http.ResponseWriter, request *http.Request) {
	accessToken := bearerToken(request.Header.Get("Authorization"))
	refreshToken := ""
	if cookie, err := request.Cookie("amsonia_refresh"); err == nil {
		refreshToken = cookie.Value
	}
	if err := api.service.Logout(request.Context(), accessToken); err != nil {
		writeDomainError(response, err)
		return
	}
	if err := api.service.RevokeRefresh(request.Context(), refreshToken); err != nil {
		writeDomainError(response, err)
		return
	}
	api.clearRefreshCookie(response, request)
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) setRefreshCookie(response http.ResponseWriter, request *http.Request, token string, maxAge int) {
	http.SetCookie(response, &http.Cookie{
		Name:     "amsonia_refresh",
		Value:    token,
		Path:     "/api/v1/auth",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteStrictMode,
	})
}

func (api *API) clearRefreshCookie(response http.ResponseWriter, request *http.Request) {
	api.setRefreshCookie(response, request, "", -1)
}

func (api *API) me(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, accountFromContext(request.Context()))
}

func (api *API) listTenants(response http.ResponseWriter, request *http.Request) {
	tenants, err := api.service.ListTenants(request.Context(), accountFromContext(request.Context()).ID)
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": tenants})
}

func (api *API) createTenant(response http.ResponseWriter, request *http.Request) {
	var input CreateTenantInput
	if !decodeJSON(response, request, &input) {
		return
	}
	tenant, err := api.service.CreateTenant(request.Context(), accountFromContext(request.Context()), input)
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, tenant)
}

func (api *API) listPermissions(response http.ResponseWriter, request *http.Request) {
	items := make([]amsonia.PermissionDefinition, 0, len(api.catalog.Keys()))
	for _, key := range api.catalog.Keys() {
		definition, _ := api.catalog.Lookup(key)
		items = append(items, definition)
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (api *API) listMembers(response http.ResponseWriter, request *http.Request) {
	if !api.requirePermission(response, request, "iam:member:manage") {
		return
	}
	items, err := api.service.ListMembers(request.Context(), request.PathValue("tenant_id"))
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (api *API) listRoles(response http.ResponseWriter, request *http.Request) {
	if !api.requirePermission(response, request, "iam:role:manage") {
		return
	}
	items, err := api.service.ListRoles(request.Context(), request.PathValue("tenant_id"))
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (api *API) createRole(response http.ResponseWriter, request *http.Request) {
	var input struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if !decodeJSON(response, request, &input) {
		return
	}
	account := accountFromContext(request.Context())
	permissions := make([]amsonia.PermissionKey, len(input.Permissions))
	for index, permission := range input.Permissions {
		permissions[index] = amsonia.PermissionKey(permission)
	}
	role, _, err := api.manager.CreateRoleWithPermissions(request.Context(), amsonia.Principal{TenantID: amsonia.TenantID(request.PathValue("tenant_id")), SubjectID: amsonia.SubjectID(account.ID)}, amsonia.MutationMetadata{ReasonCode: "api_request"}, amsonia.CreateRoleWithPermissionsInput{RoleID: amsonia.RoleID(input.ID), Name: input.Name, Description: input.Description, Permissions: permissions})
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, role)
}

func (api *API) auditEvents(response http.ResponseWriter, request *http.Request) {
	if !api.requirePermission(response, request, "iam:audit:read") {
		return
	}
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	items, err := api.service.AuditEvents(request.Context(), request.PathValue("tenant_id"), limit)
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (api *API) requirePermission(response http.ResponseWriter, request *http.Request, permission amsonia.PermissionKey) bool {
	account := accountFromContext(request.Context())
	tenantID := amsonia.TenantID(request.PathValue("tenant_id"))
	decision, err := api.authorizer.Check(request.Context(), amsonia.CheckRequest{
		Principal:  amsonia.Principal{TenantID: tenantID, SubjectID: amsonia.SubjectID(account.ID)},
		Permission: permission,
		Mode:       amsonia.ResourceTenantAction,
		Resource:   amsonia.ResourceContext{TenantID: tenantID},
	})
	if err != nil {
		api.logger.Error("permission check failed", "tenant", tenantID, "permission", permission, "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return false
	}
	if !decision.Allowed {
		writeError(response, http.StatusForbidden, "forbidden", "You do not have permission to do that.")
		return false
	}
	return true
}

func (api *API) checkAuthorization(response http.ResponseWriter, request *http.Request) {
	var input struct {
		TenantID   string `json:"tenant_id"`
		SubjectID  string `json:"subject_id"`
		Permission string `json:"permission"`
		Mode       string `json:"mode"`
		Resource   struct {
			TenantID       string `json:"tenant_id"`
			ResourceID     string `json:"resource_id"`
			OwnerSubjectID string `json:"owner_subject_id"`
			WorkspaceID    string `json:"workspace_id"`
		} `json:"resource"`
	}
	if !decodeJSON(response, request, &input) {
		return
	}
	account := accountFromContext(request.Context())
	if err := api.service.RequireMembership(request.Context(), input.TenantID, account.ID); err != nil {
		if errors.Is(err, amsonia.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "The requested tenant was not found.")
			return
		}
		api.logger.Error("authorization membership check failed", "tenant", input.TenantID, "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}
	if input.SubjectID == "" {
		input.SubjectID = account.ID
	}
	if input.SubjectID != account.ID && !account.SystemAdmin {
		writeError(response, http.StatusForbidden, "forbidden", "You can only evaluate your own access.")
		return
	}
	decision, err := api.authorizer.Check(request.Context(), amsonia.CheckRequest{
		Principal:  amsonia.Principal{TenantID: amsonia.TenantID(input.TenantID), SubjectID: amsonia.SubjectID(input.SubjectID)},
		Permission: amsonia.PermissionKey(input.Permission),
		Mode:       amsonia.ResourceMode(input.Mode),
		Resource:   amsonia.ResourceContext{TenantID: amsonia.TenantID(input.Resource.TenantID), ResourceID: amsonia.ResourceID(input.Resource.ResourceID), OwnerSubjectID: amsonia.SubjectID(input.Resource.OwnerSubjectID), WorkspaceID: amsonia.WorkspaceID(input.Resource.WorkspaceID)},
	})
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, decision)
}
