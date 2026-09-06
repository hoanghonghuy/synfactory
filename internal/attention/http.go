package attention

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

type AttentionQuery interface {
	ActiveAttention(context.Context, string, time.Time) ([]Item, error)
}

type HTTPHandler struct {
	Service    Service
	Query      AttentionQuery
	Authorizer authz.RequestAuthorizer
	Token      string
	Now        func() time.Time
}

func (h HTTPHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/attention", h.list)
	mux.HandleFunc("POST /api/v1/attention/{id}/acknowledge", h.acknowledge)
	mux.HandleFunc("POST /api/v1/attention/{id}/snooze", h.snooze)
	mux.HandleFunc("POST /api/v1/attention/{id}/resolve", h.resolve)
}

type actorRequest struct {
	Actor string `json:"actor"`
}

type snoozeRequest struct {
	Actor string    `json:"actor"`
	Until time.Time `json:"until"`
}

type attentionPage struct {
	Items []Item `json:"items"`
}

func (h HTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id"))
	if _, ok := h.authorize(w, r, authz.PermissionRead, repositoryID); !ok {
		return
	}
	if h.Query == nil {
		writeError(w, http.StatusServiceUnavailable, "attention_query_unavailable")
		return
	}
	items, err := h.Query.ActiveAttention(r.Context(), repositoryID, h.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "attention_query_failed")
		return
	}
	if items == nil {
		items = []Item{}
	}
	writeJSON(w, http.StatusOK, attentionPage{Items: items})
}

func (h HTTPHandler) acknowledge(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorizeAction(w, r)
	if !ok {
		return
	}
	var req actorRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.Service.Acknowledge(r.Context(), r.PathValue("id"), principal.Subject)
	if err != nil {
		writeActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h HTTPHandler) snooze(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorizeAction(w, r)
	if !ok {
		return
	}
	var req snoozeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Until.IsZero() {
		writeError(w, http.StatusBadRequest, "snooze deadline is required")
		return
	}
	item, err := h.Service.Snooze(r.Context(), r.PathValue("id"), principal.Subject, req.Until)
	if err != nil {
		writeActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h HTTPHandler) resolve(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorizeAction(w, r)
	if !ok {
		return
	}
	var req actorRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.Service.Resolve(r.Context(), r.PathValue("id"), principal.Subject)
	if err != nil {
		writeActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h HTTPHandler) authorizeAction(w http.ResponseWriter, r *http.Request) (authz.Principal, bool) {
	if h.Service.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "attention_store_unavailable")
		return authz.Principal{}, false
	}
	item, err := h.Service.Store.AttentionByID(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "attention_action_failed")
		return authz.Principal{}, false
	}
	return h.authorize(w, r, authz.PermissionRepositoryMutate, item.RepositoryID)
}

func (h HTTPHandler) authorize(w http.ResponseWriter, r *http.Request, permission authz.Permission, repositoryID string) (authz.Principal, bool) {
	authorizer := h.Authorizer
	if authorizer == nil {
		authorizer = authz.LegacyTokenAuthorizer{Token: h.Token}
	}
	principal, err := authorizer.Authorize(r, permission, strings.TrimSpace(repositoryID))
	if err == nil {
		return principal, true
	}
	switch {
	case errors.Is(err, authz.ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, "operator_auth_required")
	case errors.Is(err, authz.ErrForbidden):
		writeError(w, http.StatusForbidden, "operator_permission_denied")
	default:
		writeError(w, http.StatusServiceUnavailable, "authorization_unavailable")
	}
	return authz.Principal{}, false
}

func (h HTTPHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

func writeActionError(w http.ResponseWriter, err error) {
	message := err.Error()
	switch {
	case strings.Contains(message, "revalidate underlying blocker") || strings.Contains(message, "revalidator is required"):
		writeError(w, http.StatusConflict, "underlying blocker could not be revalidated")
	case strings.Contains(message, "required"), strings.Contains(message, "must be"):
		writeError(w, http.StatusBadRequest, message)
	default:
		writeError(w, http.StatusInternalServerError, "attention_action_failed")
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
