package onboarding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hoanghonghuy/synfactory/internal/authz"
	githubfactory "github.com/hoanghonghuy/synfactory/internal/github"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type Store interface {
	GetRepository(ctx context.Context, id string) (postgres.Repository, error)
	ListAllRepositories(ctx context.Context) ([]postgres.Repository, error)
	MutateRepository(ctx context.Context, repository postgres.Repository, action, actor string) (postgres.Repository, error)
	ListRepositoryConfigAudit(ctx context.Context, repositoryID string) ([]postgres.RepositoryConfigAudit, error)
}

type GitHub interface {
	GetBranch(ctx context.Context, owner, repo, branch string) (githubfactory.Branch, error)
}

type Handler struct {
	Store      Store
	GitHub     GitHub
	Token      string
	Authorizer authz.RequestAuthorizer
}

type repositoryConfig struct {
	IntegrationBranch string `json:"integration_branch"`
	WorkspacePolicy   string `json:"workspace_policy,omitempty"`
}

type repositoryRequest struct {
	FullName          string `json:"full_name"`
	DefaultBranch     string `json:"default_branch"`
	IntegrationBranch string `json:"integration_branch"`
	WorkspacePolicy   string `json:"workspace_policy,omitempty"`
	Enabled           *bool  `json:"enabled,omitempty"`
}

type repositoryResponse struct {
	ID                string `json:"id"`
	Provider          string `json:"provider"`
	FullName          string `json:"full_name"`
	DefaultBranch     string `json:"default_branch"`
	IntegrationBranch string `json:"integration_branch"`
	WorkspacePolicy   string `json:"workspace_policy,omitempty"`
	Enabled           bool   `json:"enabled"`
	ConfigVersion     int64  `json:"config_version"`
}

func (h Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/repository-config", h.list)
	mux.HandleFunc("POST /api/v1/repository-config", h.create)
	mux.HandleFunc("PATCH /api/v1/repository-config/{id}", h.update)
	mux.HandleFunc("GET /api/v1/repository-config/{id}/audit", h.audit)
}

func (h Handler) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeResponse(w, r, authz.PermissionRead, ""); !ok {
		return
	}
	items, err := h.Store.ListAllRepositories(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	result := make([]repositoryResponse, 0, len(items))
	for _, item := range items {
		result = append(result, response(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (h Handler) create(w http.ResponseWriter, r *http.Request) {
	var request repositoryRequest
	if err := decodeJSON(r, &request); err != nil {
		writeErrorHTTP(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	fullName, owner, repo, err := parseFullName(request.FullName)
	if err != nil {
		writeErrorHTTP(w, http.StatusBadRequest, "invalid_repository", err.Error())
		return
	}
	id := repositoryID(fullName)
	principal, ok := h.authorizeResponse(w, r, authz.PermissionRepositoryMutate, id)
	if !ok {
		return
	}
	if request.DefaultBranch == "" {
		request.DefaultBranch = "main"
	}
	if request.IntegrationBranch == "" {
		request.IntegrationBranch = "develop"
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	if enabled {
		if err := h.validateBranches(r.Context(), owner, repo, request.DefaultBranch, request.IntegrationBranch); err != nil {
			writeErrorHTTP(w, http.StatusUnprocessableEntity, "github_validation_failed", err.Error())
			return
		}
	}
	config, _ := json.Marshal(repositoryConfig{IntegrationBranch: request.IntegrationBranch, WorkspacePolicy: request.WorkspacePolicy})
	item, err := h.Store.MutateRepository(r.Context(), postgres.Repository{
		ID: id, Provider: "github", FullName: fullName,
		DefaultBranch: request.DefaultBranch, Enabled: enabled, Config: config,
	}, "register", principal.Subject)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, response(item))
}

func (h Handler) update(w http.ResponseWriter, r *http.Request) {
	repositoryID := r.PathValue("id")
	principal, ok := h.authorizeResponse(w, r, authz.PermissionRepositoryMutate, repositoryID)
	if !ok {
		return
	}
	item, err := h.Store.GetRepository(r.Context(), repositoryID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var request repositoryRequest
	if err := decodeJSON(r, &request); err != nil {
		writeErrorHTTP(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	current := readConfig(item.Config)
	if request.FullName != "" && request.FullName != item.FullName {
		writeErrorHTTP(w, http.StatusBadRequest, "immutable_repository_identity", "full_name cannot be changed")
		return
	}
	if request.DefaultBranch != "" {
		item.DefaultBranch = request.DefaultBranch
	}
	if request.IntegrationBranch != "" {
		current.IntegrationBranch = request.IntegrationBranch
	}
	if current.IntegrationBranch == "" {
		current.IntegrationBranch = "develop"
	}
	if request.WorkspacePolicy != "" {
		current.WorkspacePolicy = request.WorkspacePolicy
	}
	wasEnabled := item.Enabled
	if request.Enabled != nil {
		item.Enabled = *request.Enabled
	}
	_, owner, repo, err := parseFullName(item.FullName)
	if err != nil {
		writeErrorHTTP(w, http.StatusBadRequest, "invalid_repository", err.Error())
		return
	}
	if item.Enabled {
		if err := h.validateBranches(r.Context(), owner, repo, item.DefaultBranch, current.IntegrationBranch); err != nil {
			writeErrorHTTP(w, http.StatusUnprocessableEntity, "github_validation_failed", err.Error())
			return
		}
	}
	item.Config, _ = json.Marshal(current)
	action := "update"
	if !wasEnabled && item.Enabled {
		action = "enable"
	} else if wasEnabled && !item.Enabled {
		action = "disable"
	}
	saved, err := h.Store.MutateRepository(r.Context(), item, action, principal.Subject)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response(saved))
}

func (h Handler) audit(w http.ResponseWriter, r *http.Request) {
	repositoryID := r.PathValue("id")
	if _, ok := h.authorizeResponse(w, r, authz.PermissionRead, repositoryID); !ok {
		return
	}
	items, err := h.Store.ListRepositoryConfigAudit(r.Context(), repositoryID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h Handler) authorizeResponse(w http.ResponseWriter, r *http.Request, permission authz.Permission, repositoryID string) (authz.Principal, bool) {
	authorizer := h.Authorizer
	if authorizer == nil {
		legacy := authz.LegacyTokenAuthorizer{Token: h.Token}
		if sessionStore, ok := h.Store.(authz.SessionStore); ok {
			authorizer = authz.HybridAuthorizer{
				Session: authz.SessionAuthorizer{Store: sessionStore},
				Legacy:  legacy,
			}
		} else {
			authorizer = legacy
		}
	}
	principal, err := authorizer.Authorize(r, permission, repositoryID)
	if err == nil {
		if strings.TrimSpace(principal.Subject) == "" {
			principal.Subject = "operator-api"
		}
		return principal, true
	}
	if errors.Is(err, authz.ErrForbidden) {
		writeErrorHTTP(w, http.StatusForbidden, "forbidden", "permission denied")
		return authz.Principal{}, false
	}
	writeErrorHTTP(w, http.StatusUnauthorized, "operator_auth_invalid", "valid bearer session or operator token required")
	return authz.Principal{}, false
}

func (h Handler) validateBranches(ctx context.Context, owner, repo, defaultBranch, integrationBranch string) error {
	if h.GitHub == nil {
		return errors.New("github client is not configured")
	}
	for _, branch := range []string{defaultBranch, integrationBranch} {
		if strings.TrimSpace(branch) == "" {
			return errors.New("default and integration branches are required")
		}
		if _, err := h.GitHub.GetBranch(ctx, owner, repo, branch); err != nil {
			return fmt.Errorf("cannot access %s/%s branch %q: %w", owner, repo, branch, err)
		}
	}
	return nil
}

func response(item postgres.Repository) repositoryResponse {
	config := readConfig(item.Config)
	return repositoryResponse{
		ID: item.ID, Provider: item.Provider, FullName: item.FullName, DefaultBranch: item.DefaultBranch,
		IntegrationBranch: config.IntegrationBranch, WorkspacePolicy: config.WorkspacePolicy,
		Enabled: item.Enabled, ConfigVersion: item.ConfigVersion,
	}
}

func readConfig(raw json.RawMessage) repositoryConfig {
	var config repositoryConfig
	_ = json.Unmarshal(raw, &config)
	return config
}

func parseFullName(value string) (fullName, owner, repo string, err error) {
	value = strings.TrimSpace(strings.Trim(value, "/"))
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(value, " \t\r\n") {
		return "", "", "", errors.New("full_name must be in owner/repository form")
	}
	return parts[0] + "/" + parts[1], parts[0], parts[1], nil
}

func repositoryID(fullName string) string {
	sum := sha256.Sum256([]byte("github:" + strings.ToLower(fullName)))
	return "repo_" + hex.EncodeToString(sum[:12])
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, postgres.ErrNotFound) {
		writeErrorHTTP(w, http.StatusNotFound, "not_found", "repository not found")
		return
	}
	writeErrorHTTP(w, http.StatusInternalServerError, "store_error", "repository operation failed")
}

func writeErrorHTTP(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
