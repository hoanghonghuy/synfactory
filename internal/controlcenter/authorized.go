package controlcenter

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

type AuthorizedHandler struct {
	Handler
	Authorizer authz.RequestAuthorizer
}

func (h AuthorizedHandler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/overview", h.authorizeRequest(authz.PermissionRead, nil, http.HandlerFunc(h.overview)))
	mux.Handle("GET /api/v1/repositories", h.authorizeRequest(authz.PermissionRead, repositoryFromQuery, http.HandlerFunc(h.repositories)))
	mux.Handle("GET /api/v1/repositories/{id}", h.authorizeRequest(authz.PermissionRead, repositoryFromPath, http.HandlerFunc(h.repository)))
	mux.Handle("GET /api/v1/jobs", h.authorizeRequest(authz.PermissionRead, repositoryFromQuery, http.HandlerFunc(h.jobs)))
	mux.Handle("GET /api/v1/jobs/{id}", h.authorizeRequest(authz.PermissionRead, nil, http.HandlerFunc(h.job)))
	mux.Handle("GET /api/v1/workflows", h.authorizeRequest(authz.PermissionRead, repositoryFromQuery, http.HandlerFunc(h.workflows)))
	mux.Handle("GET /api/v1/workflows/{id}", h.authorizeRequest(authz.PermissionRead, nil, http.HandlerFunc(h.workflow)))
	mux.Handle("GET /api/v1/runs", h.authorizeRequest(authz.PermissionRead, nil, http.HandlerFunc(h.runs)))
	mux.Handle("GET /api/v1/runs/{id}", h.authorizeRequest(authz.PermissionRead, nil, http.HandlerFunc(h.run)))
	mux.Handle("GET /api/v1/runs/{id}/evidence", h.authorizeRequest(authz.PermissionRead, nil, http.HandlerFunc(h.evidence)))
	mux.Handle("GET /api/v1/workers", h.authorizeRequest(authz.PermissionRead, nil, http.HandlerFunc(h.workers)))
}

type repositoryResolver func(*http.Request) string

func repositoryFromQuery(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("repository_id"))
}

func repositoryFromPath(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}

func (h AuthorizedHandler) authorizeRequest(permission authz.Permission, repository repositoryResolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.Authorizer == nil {
			writeError(w, http.StatusServiceUnavailable, "operator_api_disabled")
			return
		}
		repositoryID := ""
		if repository != nil {
			repositoryID = repository(r)
		}
		_, err := h.Authorizer.Authorize(r, permission, repositoryID)
		if err != nil {
			switch {
			case errors.Is(err, authz.ErrUnauthenticated):
				writeError(w, http.StatusUnauthorized, "operator_auth_required")
			case errors.Is(err, authz.ErrForbidden):
				writeError(w, http.StatusForbidden, "operator_permission_denied")
			default:
				writeError(w, http.StatusInternalServerError, "operator_auth_failed")
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}
