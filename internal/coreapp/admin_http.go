package coreapp

import (
	"net/http"
	"strconv"

	"github.com/willunylabs/amsonia-core"
)

func queryNumber(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		writeDomainError(w, amsonia.ErrInvalidInput)
		return 0, false
	}
	return n, true
}

func (api *API) passwordSlot(w http.ResponseWriter) bool {
	select {
	case api.loginSlots <- struct{}{}:
		return true
	default:
		writeError(w, http.StatusTooManyRequests, "login_busy", "Try again shortly.")
		return false
	}
}
func (api *API) profile(w http.ResponseWriter, r *http.Request) {
	value, err := api.service.Profile(r.Context(), accountFromContext(r.Context()).ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (api *API) updateProfile(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DisplayName string `json:"display_name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	value, err := api.service.UpdateProfile(r.Context(), accountFromContext(r.Context()).ID, input.DisplayName)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (api *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Current string `json:"current_password"`
		Next    string `json:"new_password"`
	}
	if !decodeJSON(w, r, &input) || !api.passwordSlot(w) {
		return
	}
	defer func() { <-api.loginSlots }()
	if err := api.service.ChangePassword(r.Context(), accountFromContext(r.Context()).ID, input.Current, input.Next); err != nil {
		writeDomainError(w, err)
		return
	}
	api.clearRefreshCookie(w, r)
	w.WriteHeader(204)
}
func (api *API) sessions(w http.ResponseWriter, r *http.Request) {
	value, err := api.service.ListSessions(r.Context(), accountFromContext(r.Context()).ID, bearerToken(r.Header.Get("Authorization")))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": value})
}
func (api *API) revokeSession(w http.ResponseWriter, r *http.Request) {
	if err := api.service.RevokeSession(r.Context(), accountFromContext(r.Context()).ID, r.PathValue("session_id")); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (api *API) previewInvitation(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	value, err := api.service.PreviewInvitation(r.Context(), input.Token)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (api *API) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) || !api.passwordSlot(w) {
		return
	}
	defer func() { <-api.loginSlots }()
	value, err := api.service.AcceptInvitation(r.Context(), input.Token, input.Password)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (api *API) invitations(w http.ResponseWriter, r *http.Request) {
	if !api.requirePermission(w, r, PermissionMemberManage) {
		return
	}
	filter, ok := directoryQuery(w, r)
	if !ok {
		return
	}
	value, err := api.service.InvitationDirectory(r.Context(), r.PathValue("tenant_id"), filter)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (api *API) createInvitation(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	value, err := api.service.CreateInvitation(r.Context(), r.PathValue("tenant_id"), accountFromContext(r.Context()).ID, input.Email)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 201, value)
}
func (api *API) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	if err := api.service.RevokeInvitation(r.Context(), r.PathValue("tenant_id"), accountFromContext(r.Context()).ID, r.PathValue("invitation_id")); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (api *API) manageMember(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Status string `json:"status"`
	}
	remove := r.Method == http.MethodDelete
	if !remove && !decodeJSON(w, r, &input) {
		return
	}
	if err := api.service.ManageMember(r.Context(), r.PathValue("tenant_id"), accountFromContext(r.Context()).ID, r.PathValue("account_id"), input.Status, remove); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (api *API) memberRoles(w http.ResponseWriter, r *http.Request) {
	if !api.requirePermission(w, r, PermissionMemberRead) || !api.requirePermission(w, r, PermissionRoleRead) {
		return
	}
	value, err := api.service.MemberRoleIDs(r.Context(), r.PathValue("tenant_id"), r.PathValue("account_id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": value})
}
func principal(r *http.Request) amsonia.Principal {
	return amsonia.Principal{TenantID: amsonia.TenantID(r.PathValue("tenant_id")), SubjectID: amsonia.SubjectID(accountFromContext(r.Context()).ID)}
}
func (api *API) bindRole(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version int64 `json:"expected_version"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Version < 1 {
		writeDomainError(w, amsonia.ErrInvalidInput)
		return
	}
	status, err := api.service.MemberStatus(r.Context(), r.PathValue("tenant_id"), r.PathValue("account_id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if r.Method == http.MethodPut && status != "active" {
		writeDomainError(w, amsonia.ErrConflict)
		return
	}
	mutation := amsonia.AssignRoleInput{SubjectID: amsonia.SubjectID(r.PathValue("account_id")), RoleID: amsonia.RoleID(r.PathValue("role_id")), ExpectedRoleVersion: input.Version}
	ctx := withPolicyGuard(r.Context(), policyGuard{Actor: accountFromContext(r.Context()).ID, Target: r.PathValue("account_id"), Assign: r.Method == http.MethodPut, Role: mutation.RoleID, Version: input.Version})
	var result amsonia.MutationResult
	if r.Method == http.MethodPut {
		result, err = api.manager.AssignRole(ctx, principal(r), mutationMetadata(ctx), mutation)
	} else {
		result, err = api.manager.UnassignRole(ctx, principal(r), mutationMetadata(ctx), mutation)
	}
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (api *API) roleDetail(w http.ResponseWriter, r *http.Request) {
	if !api.requirePermission(w, r, PermissionRoleRead) {
		return
	}
	before, ok := queryNumber(w, r, "before_version")
	if !ok {
		return
	}
	value, err := api.service.RoleDetail(r.Context(), r.PathValue("tenant_id"), r.PathValue("role_id"), before)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (api *API) updateRole(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     int64  `json:"expected_version"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Version < 1 {
		writeDomainError(w, amsonia.ErrInvalidInput)
		return
	}
	value, _, err := api.manager.UpdateRole(r.Context(), principal(r), mutationMetadata(r.Context()), amsonia.UpdateRoleInput{RoleID: amsonia.RoleID(r.PathValue("role_id")), Name: input.Name, Description: input.Description, ExpectedVersion: input.Version})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (api *API) deleteRole(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version int64 `json:"expected_version"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Version < 1 {
		writeDomainError(w, amsonia.ErrInvalidInput)
		return
	}
	_, err := api.manager.DeleteRole(r.Context(), principal(r), mutationMetadata(r.Context()), amsonia.DeleteRoleInput{RoleID: amsonia.RoleID(r.PathValue("role_id")), ExpectedVersion: input.Version})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (api *API) changeGrant(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ScopedPermission
		Version int64 `json:"expected_version"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Version < 1 {
		writeDomainError(w, amsonia.ErrInvalidInput)
		return
	}
	ctx := withPolicyGuard(r.Context(), policyGuard{Actor: accountFromContext(r.Context()).ID, Grant: true})
	mutation := amsonia.GrantPermissionInput{RoleID: amsonia.RoleID(r.PathValue("role_id")), ExpectedVersion: input.Version, Permission: input.Permission, Scope: input.Scope, WorkspaceRoles: input.WorkspaceRoles}
	var result amsonia.MutationResult
	var err error
	if r.Method == http.MethodPost {
		result, err = api.manager.GrantPermission(ctx, principal(r), mutationMetadata(ctx), mutation)
	} else {
		result, err = api.manager.RevokePermission(ctx, principal(r), mutationMetadata(ctx), mutation)
	}
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
