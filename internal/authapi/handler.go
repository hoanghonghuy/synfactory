package authapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

type Store interface {
	UpsertAuthUser(ctx context.Context, id, provider, providerSubject, displayName string) error
	ReplaceAuthGrants(ctx context.Context, userID string, roles []authz.RoleGrant, permissions []authz.PermissionGrant) error
	RevokeAuthSession(ctx context.Context, id string, revokedAt time.Time) error
}

type Handler struct {
	Store      Store
	Authorizer authz.RequestAuthorizer
	Issuer     authz.SessionIssuer
	Now        func() time.Time
}

type issueSessionRequest struct {
	Provider        string                  `json:"provider"`
	ProviderSubject string                  `json:"provider_subject"`
	DisplayName     string                  `json:"display_name,omitempty"`
	Roles           []authz.RoleGrant       `json:"roles,omitempty"`
	Permissions     []authz.PermissionGrant `json:"permissions,omitempty"`
	TTLSeconds      int64                   `json:"ttl_seconds,omitempty"`
}

func (h Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/users/{id}/sessions", h.issueSession)
	mux.HandleFunc("DELETE /api/v1/auth/sessions/{id}", h.revokeSession)
}

func (h Handler) issueSession(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	if h.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth_store_unavailable"})
		return
	}
	var request issueSessionRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" || strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.ProviderSubject) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "identity_required"})
		return
	}
	if err := h.Store.UpsertAuthUser(r.Context(), userID, request.Provider, request.ProviderSubject, request.DisplayName); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "identity_conflict"})
		return
	}
	if err := h.Store.ReplaceAuthGrants(r.Context(), userID, request.Roles, request.Permissions); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "grant_update_failed"})
		return
	}
	issuer := h.Issuer
	if request.TTLSeconds > 0 {
		issuer.TTL = time.Duration(request.TTLSeconds) * time.Second
	}
	issued, err := issuer.Issue(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_issue_failed"})
		return
	}
	writeJSON(w, http.StatusCreated, issued)
}

func (h Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	if h.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth_store_unavailable"})
		return
	}
	now := time.Now().UTC()
	if h.Now != nil {
		now = h.Now().UTC()
	}
	if err := h.Store.RevokeAuthSession(r.Context(), strings.TrimSpace(r.PathValue("id")), now); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session_not_found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if h.Authorizer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth_authorizer_unavailable"})
		return false
	}
	_, err := h.Authorizer.Authorize(r, authz.PermissionSecurityPolicy, "")
	if err == nil {
		return true
	}
	if errors.Is(err, authz.ErrForbidden) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return false
	}
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

var _ = strconv.IntSize
