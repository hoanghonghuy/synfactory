package controlcenter

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type runtimeUsageSummaryStore interface {
	RuntimeUsageSummary(ctx context.Context, repository string, since time.Time, limit int) (postgres.RuntimeUsageTotals, []postgres.RuntimeUsageAggregate, error)
}

type runtimeUsageResponse struct {
	GeneratedAt time.Time                        `json:"generated_at"`
	Since       time.Time                        `json:"since"`
	Repository  string                           `json:"repository,omitempty"`
	Totals      postgres.RuntimeUsageTotals      `json:"totals"`
	Items       []postgres.RuntimeUsageAggregate `json:"items"`
}

// RegisterRuntimeUsage adds authenticated usage/cost visibility without widening
// the core control-center Store interface. This keeps the operator endpoint
// optional for alternate stores while the production PostgreSQL store supports it.
func (h Handler) RegisterRuntimeUsage(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/runtime-usage", h.authorize(http.HandlerFunc(h.runtimeUsage)))
}

func (h Handler) runtimeUsage(w http.ResponseWriter, r *http.Request) {
	store, ok := h.Store.(runtimeUsageSummaryStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "runtime_usage_unavailable")
		return
	}

	now := time.Now().UTC()
	if h.Now != nil {
		now = h.Now().UTC()
	}
	since := now.Add(-24 * time.Hour)
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_since")
			return
		}
		since = parsed.UTC()
		if since.After(now) {
			writeError(w, http.StatusBadRequest, "invalid_since")
			return
		}
	}

	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "invalid_limit")
			return
		}
		limit = parsed
	}
	repository := strings.TrimSpace(r.URL.Query().Get("repository"))
	totals, items, err := store.RuntimeUsageSummary(r.Context(), repository, since, limit)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if items == nil {
		items = []postgres.RuntimeUsageAggregate{}
	}
	writeJSON(w, http.StatusOK, runtimeUsageResponse{
		GeneratedAt: now,
		Since:       since,
		Repository:  repository,
		Totals:      totals,
		Items:       items,
	})
}
