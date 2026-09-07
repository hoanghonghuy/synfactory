package main

import (
	"context"
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

type securityAuditResponse struct {
	Events []securityaudit.Event `json:"events"`
}

func registerSecurityAudit(mux *http.ServeMux, authorizer authz.RequestAuthorizer, reader securityAuditReader, audit securityAuditWriter) {
	mux.HandleFunc("GET /api/security/audit", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authorizer.Authorize(r, authz.PermissionSecurityPolicy, "")
		if err != nil {
			status := http.StatusForbidden
			if errors.Is(err, authz.ErrUnauthenticated) {
				status = http.StatusUnauthorized
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
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
