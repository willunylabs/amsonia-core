package coreapp

import "net/http"

func (api *API) createPasswordRecovery(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CurrentPassword string `json:"current_password"`
	}
	if !decodeJSON(w, r, &input) || !api.passwordSlot(w) {
		return
	}
	defer func() { <-api.loginSlots }()
	item, err := api.service.CreatePasswordRecovery(r.Context(), r.PathValue("tenant_id"), accountFromContext(r.Context()).ID, r.PathValue("account_id"), input.CurrentPassword)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
func (api *API) previewPasswordRecovery(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := api.service.PreviewPasswordRecovery(r.Context(), input.Token)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (api *API) completePasswordRecovery(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if !decodeJSON(w, r, &input) || !api.passwordSlot(w) {
		return
	}
	defer func() { <-api.loginSlots }()
	if err := api.service.CompletePasswordRecovery(r.Context(), input.Token, input.NewPassword); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
