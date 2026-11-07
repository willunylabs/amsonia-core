package coreapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
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
	cookiePath string
}

func NewAPI(service *Service, manager *amsonia.Manager, authorizer *amsonia.Authorizer, catalog *amsonia.Catalog, logger *slog.Logger, pathPrefix string) (*API, error) {
	if service == nil || manager == nil || authorizer == nil || catalog == nil || logger == nil {
		return nil, amsonia.ErrInvalidInput
	}
	if err := validateAPIPathPrefix(pathPrefix); err != nil {
		return nil, err
	}
	loginConcurrency := min(4, max(1, runtime.GOMAXPROCS(0)/2))
	return &API{service: service, manager: manager, authorizer: authorizer, catalog: catalog, logger: logger, loginSlots: make(chan struct{}, loginConcurrency), startedAt: time.Now().UTC(), cookiePath: pathPrefix + "/api/v1/auth"}, nil
}

func validateAPIPathPrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	for _, segment := range strings.Split(strings.TrimPrefix(prefix, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("AMSONIA_API_PREFIX must be an absolute path prefix without a trailing slash")
		}
		for _, c := range segment {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", c)) {
				return fmt.Errorf("AMSONIA_API_PREFIX contains an unsupported path character")
			}
		}
	}
	if !strings.HasPrefix(prefix, "/") {
		return fmt.Errorf("AMSONIA_API_PREFIX must start with / or be empty")
	}
	return nil
}

func (api *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.health)
	mux.HandleFunc("GET /readyz", api.ready)
	mux.HandleFunc("POST /api/v1/auth/login", api.login)
	mux.HandleFunc("POST /api/v1/auth/refresh", api.refresh)
	mux.HandleFunc("POST /api/v1/auth/restore", api.restoreSession)
	mux.HandleFunc("POST /api/v1/auth/logout", api.logout)
	mux.HandleFunc("POST /api/v1/password-recovery/preview", api.previewPasswordRecovery)
	mux.HandleFunc("POST /api/v1/password-recovery/complete", api.completePasswordRecovery)
	mux.HandleFunc("POST /api/v1/invitations/preview", api.previewInvitation)
	mux.HandleFunc("POST /api/v1/invitations/accept", api.acceptInvitation)
	for pattern, handler := range map[string]http.HandlerFunc{
		"GET /api/v1/auth/profile": api.profile, "PATCH /api/v1/auth/profile": api.updateProfile,
		"POST /api/v1/auth/password": api.changePassword, "GET /api/v1/auth/sessions": api.sessions,
		"DELETE /api/v1/auth/sessions/{session_id}": api.revokeSession,
		"POST /api/v1/auth/sessions/revoke-others":  api.revokeOtherSessions,
	} {
		mux.Handle(pattern, api.auth(handler))
	}
	for pattern, handler := range map[string]http.HandlerFunc{
		"POST /api/v1/tenants/{tenant_id}/members/{account_id}/password-recovery": api.createPasswordRecovery,
		"GET /api/v1/tenants/{tenant_id}/settings":                                api.organizationSettings,
		"PATCH /api/v1/tenants/{tenant_id}/settings":                              api.renameOrganization,
		"GET /api/v1/tenants/{tenant_id}/members/{account_id}":                    api.memberDetail,
		"GET /api/v1/tenants/{tenant_id}/invitations":                             api.invitations,
		"POST /api/v1/tenants/{tenant_id}/invitations":                            api.createInvitation,
		"DELETE /api/v1/tenants/{tenant_id}/invitations/{invitation_id}":          api.revokeInvitation,
		"PATCH /api/v1/tenants/{tenant_id}/members/{account_id}":                  api.manageMember,
		"DELETE /api/v1/tenants/{tenant_id}/members/{account_id}":                 api.manageMember,
		"GET /api/v1/tenants/{tenant_id}/members/{account_id}/roles":              api.memberRoles,
		"PUT /api/v1/tenants/{tenant_id}/members/{account_id}/roles/{role_id}":    api.bindRole,
		"DELETE /api/v1/tenants/{tenant_id}/members/{account_id}/roles/{role_id}": api.bindRole,
		"GET /api/v1/tenants/{tenant_id}/roles/{role_id}":                         api.roleDetail,
		"GET /api/v1/tenants/{tenant_id}/roles/{role_id}/members":                 api.roleMembers,
		"PATCH /api/v1/tenants/{tenant_id}/roles/{role_id}":                       api.updateRole,
		"DELETE /api/v1/tenants/{tenant_id}/roles/{role_id}":                      api.deleteRole,
		"POST /api/v1/tenants/{tenant_id}/roles/{role_id}/grants":                 api.changeGrant,
		"DELETE /api/v1/tenants/{tenant_id}/roles/{role_id}/grants":               api.changeGrant,
	} {
		mux.Handle(pattern, api.tenant(handler))
	}
	mux.Handle("GET /api/v1/application", api.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"tenancy_mode": api.service.application.TenancyMode})
	})))
	mux.Handle("GET /api/v1/auth/me", api.auth(http.HandlerFunc(api.me)))
	mux.Handle("GET /api/v1/tenants", api.auth(http.HandlerFunc(api.listTenants)))
	mux.Handle("POST /api/v1/tenants", api.auth(http.HandlerFunc(api.createTenant)))
	mux.Handle("GET /api/v1/permissions", api.auth(http.HandlerFunc(api.listPermissions)))
	mux.Handle("POST /api/v1/authorization/check", api.auth(http.HandlerFunc(api.checkAuthorization)))
	mux.Handle("GET /api/v1/tenants/{tenant_id}/members", api.tenant(http.HandlerFunc(api.listMembers)))
	mux.Handle("GET /api/v1/tenants/{tenant_id}/roles", api.tenant(http.HandlerFunc(api.listRoles)))
	mux.Handle("GET /api/v1/tenants/{tenant_id}/capabilities", api.tenant(http.HandlerFunc(api.capabilities)))
	mux.Handle("POST /api/v1/tenants/{tenant_id}/roles", api.tenant(http.HandlerFunc(api.createRole)))
	mux.Handle("GET /api/v1/tenants/{tenant_id}/audit-events", api.tenant(http.HandlerFunc(api.auditEvents)))
	mux.Handle("GET /api/v1/tenants/{tenant_id}/audit-events/export", api.tenant(http.HandlerFunc(api.exportAudit)))
	return api.securityHeaders(api.requestLog(api.recover(mux)))
}

type loggedResponse struct {
	http.ResponseWriter
	status int
	logger *slog.Logger
}

func (w *loggedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *loggedResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *loggedResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}
func (w *loggedResponse) logError(err error) {
	w.logger.Error("http request failed", "error", err)
}

func (api *API) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		id, err := randomID("req_", 18)
		if err != nil {
			writeDomainError(response, err)
			return
		}
		response.Header().Set("X-Request-ID", id)
		request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, id))
		logger := api.logger.With("method", request.Method, "path", request.URL.Path, "request_id", id)
		logged := &loggedResponse{ResponseWriter: response, logger: logger}
		next.ServeHTTP(logged, request)
		status := logged.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.Info("http request", "status", status, "duration_ms", time.Since(started).Milliseconds())
	})
}

func (api *API) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				api.logger.Error("panic recovered", "path", request.URL.Path, "request_id", mutationMetadata(request.Context()).RequestID, "error", fmt.Sprint(recovered))
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
		ctx = withPolicyGuard(ctx, policyGuard{Actor: account.ID})
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
	body, err := io.ReadAll(request.Body)
	if err != nil || !utf8.Valid(body) || len(bytes.TrimSpace(body)) == 0 || bytes.TrimSpace(body)[0] != '{' {
		writeError(response, http.StatusBadRequest, "invalid_request", "Check the request fields and try again.")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
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
	var pgError *pgconn.PgError
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		writeError(response, http.StatusBadRequest, "invalid_credentials", "The current password is incorrect.")
	case errors.Is(err, ErrAccountLocked):
		writeError(response, http.StatusTooManyRequests, "login_temporarily_locked", "Too many attempts. Try again later.")
	case errors.Is(err, amsonia.ErrLastAdministrator):
		writeError(response, http.StatusConflict, "last_administrator", "Assign another active tenant administrator before removing this access.")
	case errors.Is(err, amsonia.ErrGrantCycle):
		writeError(response, http.StatusConflict, "grant_cycle", "This assignment would create a delegation cycle.")
	case errors.As(err, &pgError) && (pgError.Code == "23505" || pgError.Code == "40001" || pgError.Code == "40P01"):
		writeError(response, http.StatusConflict, "conflict", "The resource changed. Refresh and try again.")
	case errors.Is(err, amsonia.ErrInvalidInput):
		writeError(response, http.StatusBadRequest, "invalid_request", "Check the request fields and try again.")
	case errors.Is(err, amsonia.ErrForbidden):
		writeError(response, http.StatusForbidden, "forbidden", "You do not have permission to do that.")
	case errors.Is(err, amsonia.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
	case errors.Is(err, amsonia.ErrConflict):
		writeError(response, http.StatusConflict, "conflict", "The resource changed. Refresh and try again.")
	default:
		if logged, ok := response.(interface{ logError(error) }); ok {
			logged.logError(err)
		}
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
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(response, request, &input) {
		return
	}
	select {
	case api.loginSlots <- struct{}{}:
		defer func() { <-api.loginSlots }()
	default:
		writeError(response, http.StatusTooManyRequests, "login_busy", "Sign-in is busy. Try again shortly.")
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
	api.rotateSession(response, request, false)
}

// restoreSession makes an absent/expired cookie an ordinary anonymous result.
func (api *API) restoreSession(response http.ResponseWriter, request *http.Request) {
	api.rotateSession(response, request, true)
}

func (api *API) rotateSession(response http.ResponseWriter, request *http.Request, optional bool) {
	cookie, err := request.Cookie("amsonia_refresh")
	if err != nil {
		api.clearRefreshCookie(response, request)
		if optional {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(response, http.StatusUnauthorized, "refresh_invalid", "Your session has expired. Sign in again.")
		return
	}
	session, err := api.service.Refresh(request.Context(), RefreshInput{
		RefreshToken:  cookie.Value,
		RemoteAddress: request.RemoteAddr,
		UserAgent:     request.UserAgent(),
	})
	if err != nil {
		if !errors.Is(err, ErrSessionInvalid) {
			api.logger.Error("session refresh failed", "error", err)
			writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		api.clearRefreshCookie(response, request)
		if optional {
			response.WriteHeader(http.StatusNoContent)
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
	if err := api.service.logoutSessions(request.Context(), accessToken, refreshToken); err != nil {
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
		Path:     api.cookiePath,
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
	if !api.requirePermission(response, request, PermissionMemberRead) {
		return
	}
	filter, ok := directoryQuery(response, request)
	if !ok {
		return
	}
	items, err := api.service.MemberDirectory(request.Context(), request.PathValue("tenant_id"), filter)
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (api *API) listRoles(response http.ResponseWriter, request *http.Request) {
	if !api.requirePermission(response, request, PermissionRoleRead) {
		return
	}
	filter, ok := directoryQuery(response, request)
	if !ok {
		return
	}
	items, err := api.service.RoleDirectory(request.Context(), request.PathValue("tenant_id"), filter)
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (api *API) capabilities(response http.ResponseWriter, request *http.Request) {
	permissions := []amsonia.PermissionKey{
		PermissionRoleRead,
		PermissionRoleManage,
		PermissionGrantManage,
		PermissionRoleAssign,
		PermissionMemberRead,
		PermissionMemberManage,
		PermissionAuditRead,
	}
	items := make(map[amsonia.PermissionKey]bool, len(permissions))
	for _, permission := range permissions {
		allowed, err := api.permissionAllowed(request, permission)
		if err != nil {
			api.logger.Error("capability check failed", "tenant", request.PathValue("tenant_id"), "permission", permission, "error", err)
			writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		items[permission] = allowed
	}
	writeJSON(response, http.StatusOK, map[string]any{"permissions": items})
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
	ctx := request.Context()
	if len(permissions) > 0 {
		ctx = withPolicyGuard(ctx, policyGuard{Actor: account.ID, Grant: true})
	}
	role, _, err := api.manager.CreateRoleWithPermissions(ctx, amsonia.Principal{TenantID: amsonia.TenantID(request.PathValue("tenant_id")), SubjectID: amsonia.SubjectID(account.ID)}, mutationMetadata(request.Context()), amsonia.CreateRoleWithPermissionsInput{RoleID: amsonia.RoleID(input.ID), Name: input.Name, Description: input.Description, Permissions: permissions})
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, role)
}

func (api *API) requirePermission(response http.ResponseWriter, request *http.Request, permission amsonia.PermissionKey) bool {
	allowed, err := api.permissionAllowed(request, permission)
	if err != nil {
		api.logger.Error("permission check failed", "tenant", request.PathValue("tenant_id"), "permission", permission, "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return false
	}
	if !allowed {
		writeError(response, http.StatusForbidden, "forbidden", "You do not have permission to do that.")
		return false
	}
	return true
}

func (api *API) permissionAllowed(request *http.Request, permission amsonia.PermissionKey) (bool, error) {
	account := accountFromContext(request.Context())
	tenantID := amsonia.TenantID(request.PathValue("tenant_id"))
	decision, err := api.authorizer.Check(request.Context(), amsonia.CheckRequest{
		Principal:  amsonia.Principal{TenantID: tenantID, SubjectID: amsonia.SubjectID(account.ID)},
		Permission: permission,
		Mode:       amsonia.ResourceTenantAction,
		Resource:   amsonia.ResourceContext{TenantID: tenantID},
	})
	if err != nil {
		return false, err
	}
	return decision.Allowed, nil
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
