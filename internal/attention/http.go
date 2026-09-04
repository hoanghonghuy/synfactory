package attention

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type HTTPHandler struct {
	Service Service
	Token   string
}

func (h HTTPHandler) Register(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/attention/{id}/acknowledge", h.authorize(http.HandlerFunc(h.acknowledge)))
	mux.Handle("POST /api/v1/attention/{id}/snooze", h.authorize(http.HandlerFunc(h.snooze)))
	mux.Handle("POST /api/v1/attention/{id}/resolve", h.authorize(http.HandlerFunc(h.resolve)))
}

type actorRequest struct {
	Actor string `json:"actor"`
}

type snoozeRequest struct {
	Actor string    `json:"actor"`
	Until time.Time `json:"until"`
}

func (h HTTPHandler) acknowledge(w http.ResponseWriter, r *http.Request) {
	var req actorRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.Service.Acknowledge(r.Context(), r.PathValue("id"), req.Actor)
	if err != nil {
		writeActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h HTTPHandler) snooze(w http.ResponseWriter, r *http.Request) {
	var req snoozeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Until.IsZero() {
		writeError(w, http.StatusBadRequest, "snooze deadline is required")
		return
	}
	item, err := h.Service.Snooze(r.Context(), r.PathValue("id"), req.Actor, req.Until)
	if err != nil {
		writeActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h HTTPHandler) resolve(w http.ResponseWriter, r *http.Request) {
	var req actorRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.Service.Resolve(r.Context(), r.PathValue("id"), req.Actor)
	if err != nil {
		writeActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h HTTPHandler) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := strings.TrimSpace(h.Token)
		if expected == "" {
			writeError(w, http.StatusServiceUnavailable, "attention_api_disabled")
			return
		}
		const prefix = "Bearer "
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, prefix) {
			writeError(w, http.StatusUnauthorized, "operator_auth_required")
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			writeError(w, http.StatusUnauthorized, "operator_auth_invalid")
			return
		}
		next.ServeHTTP(w, r)
	})
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
