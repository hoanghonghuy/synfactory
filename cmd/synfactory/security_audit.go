package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/securityaudit"
)

type securityAuditReader interface {
	ListSecurityAudit(context.Context, securityaudit.Filter) ([]securityaudit.Event, error)
}

type securityAuditOperations interface {
	securityAuditReader
	PruneSecurityAuditBefore(context.Context, time.Time, int) (int64, error)
}

type securityAuditResponse struct {
	Events []securityaudit.Event `json:"events"`
}

type securityAuditExportResponse struct {
	SchemaVersion string                `json:"schema_version"`
	Count         int                   `json:"count"`
	Events        []securityaudit.Event `json:"events"`
}

type securityAuditRetentionRequest struct {
	Cutoff string `json:"cutoff"`
	Limit  int    `json:"limit"`
}

type securityAuditRetentionResponse struct {
	Deleted int64 `json:"deleted"`
}

func registerSecurityAudit(mux *http.ServeMux, authorizer authz.RequestAuthorizer, reader securityAuditReader, audit securityAuditWriter) {
	mux.HandleFunc("GET /api/security/audit", func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authorizeSecurityAudit(w, r, authorizer)
		if !ok {
			return
		}
		filter, err := securityAuditFilter(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		events, err := reader.ListSecurityAudit(r.Context(), filter)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "security audit unavailable"})
			return
		}
		if err := appendSecurityAudit(r.Context(), audit, principal, "security.audit.read", "security_audit", "", "success"); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "security audit unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, securityAuditResponse{Events: events})
	})

	mux.HandleFunc("GET /api/security/audit/export", func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authorizeSecurityAudit(w, r, authorizer)
		if !ok {
			return
		}
		operations, ok := reader.(securityAuditOperations)
		if !ok {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "security audit operations unavailable"})
			return
		}
		filter, err := securityAuditFilter(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		events, err := operations.ListSecurityAudit(r.Context(), filter)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "security audit unavailable"})
			return
		}
		if err := validateSecurityAuditExport(events); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "security audit export contains unsupported sensitive metadata"})
			return
		}
		if err := appendSecurityAudit(r.Context(), audit, principal, "security.audit.export", "security_audit", "", "success"); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "security audit unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, securityAuditExportResponse{
			SchemaVersion: "v1",
			Count:         len(events),
			Events:        events,
		})
	})

	mux.HandleFunc("POST /api/security/audit/retention", func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authorizeSecurityAudit(w, r, authorizer)
		if !ok {
			return
		}
		operations, ok := reader.(securityAuditOperations)
		if !ok {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "security audit operations unavailable"})
			return
		}
		request, err := decodeSecurityAuditRetentionRequest(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		cutoff, err := time.Parse(time.RFC3339, request.Cutoff)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cutoff must be RFC3339"})
			return
		}
		if request.Limit <= 0 || request.Limit > 5000 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be between 1 and 5000"})
			return
		}
		if err := appendSecurityAudit(r.Context(), audit, principal, "security.audit.retention", "security_audit", "retention", "requested"); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "security audit unavailable"})
			return
		}
		deleted, err := operations.PruneSecurityAuditBefore(r.Context(), cutoff, request.Limit)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "security audit retention unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, securityAuditRetentionResponse{Deleted: deleted})
	})
}

func authorizeSecurityAudit(w http.ResponseWriter, r *http.Request, authorizer authz.RequestAuthorizer) (authz.Principal, bool) {
	principal, err := authorizer.Authorize(r, authz.PermissionSecurityPolicy, "")
	if err != nil {
		status := http.StatusForbidden
		if errors.Is(err, authz.ErrUnauthenticated) {
			status = http.StatusUnauthorized
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return authz.Principal{}, false
	}
	return principal, true
}

func decodeSecurityAuditRetentionRequest(r *http.Request) (securityAuditRetentionRequest, error) {
	var request securityAuditRetentionRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return securityAuditRetentionRequest{}, errors.New("invalid retention request")
	}
	request.Cutoff = strings.TrimSpace(request.Cutoff)
	if request.Cutoff == "" {
		return securityAuditRetentionRequest{}, errors.New("cutoff is required")
	}
	return request, nil
}

func validateSecurityAuditExport(events []securityaudit.Event) error {
	for _, event := range events {
		if len(event.Metadata) == 0 {
			continue
		}
		var metadata any
		if err := json.Unmarshal(event.Metadata, &metadata); err != nil {
			return errors.New("invalid audit metadata")
		}
		if containsSensitiveAuditField(metadata) {
			return errors.New("sensitive audit metadata")
		}
	}
	return nil
}

func containsSensitiveAuditField(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveAuditFieldKey(key) || containsSensitiveAuditField(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitiveAuditField(child) {
				return true
			}
		}
	}
	return false
}

func sensitiveAuditFieldKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(normalized)
	for _, marker := range []string{
		"secret", "token", "authorization", "cookie", "password", "private_key", "api_key",
		"raw_terminal", "terminal_input", "terminal_output", "stdin", "stdout", "stderr",
	} {
		if normalized == marker || strings.HasPrefix(normalized, marker+"_") ||
			strings.HasSuffix(normalized, "_"+marker) || strings.Contains(normalized, "_"+marker+"_") {
			return true
		}
	}
	return false
}

func securityAuditFilter(r *http.Request) (securityaudit.Filter, error) {
	query := r.URL.Query()
	filter := securityaudit.Filter{
		ActorID:      strings.TrimSpace(query.Get("actor_id")),
		Action:       strings.TrimSpace(query.Get("action")),
		ResourceType: strings.TrimSpace(query.Get("resource_type")),
		ResourceID:   strings.TrimSpace(query.Get("resource_id")),
		Outcome:      strings.TrimSpace(query.Get("outcome")),
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 || limit > 500 {
			return securityaudit.Filter{}, errors.New("limit must be between 1 and 500")
		}
		filter.Limit = limit
	}
	if raw := strings.TrimSpace(query.Get("since")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return securityaudit.Filter{}, errors.New("since must be RFC3339")
		}
		filter.Since = value
	}
	if raw := strings.TrimSpace(query.Get("until")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return securityaudit.Filter{}, errors.New("until must be RFC3339")
		}
		filter.Until = value
	}
	if !filter.Since.IsZero() && !filter.Until.IsZero() && filter.Since.After(filter.Until) {
		return securityaudit.Filter{}, errors.New("since must not be after until")
	}
	return filter, nil
}
