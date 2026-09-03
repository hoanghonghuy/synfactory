package onboarding

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
	Store  Store
	GitHub GitHub
	Token  string
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
	mux.Handle("GET /api/v1/repository-config", h.authorize(http.HandlerFunc(h.list)))
	mux.Handle("POST /api/v1/repository-config", h.authorize(http.HandlerFunc(h.create)))
	mux.Handle("PATCH /api/v1/repository-config/{id}", h.authorize(http.HandlerFunc(h.update)))
	mux.Handle("GET /api/v1/repository-config/{id}/audit", h.authorize(http.HandlerFunc(h.audit)))
}

func (h Handler) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := strings.TrimSpace(h.Token)
		if expected == "" {
			writeError(w, http.StatusServiceUnavailable, "operator_api_disabled", "operator API is disabled")
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			writeError(w, http.StatusUnauthorized, "operator_auth_invalid", "valid operator bearer token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h Handler) list(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	fullName, owner, repo, err := parseFullName(request.FullName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_repository", err.Error())
		return
	}
	if request.DefaultBranch == "" {
		request.DefaultBranch = "main"
	}
	if request.IntegrationBranch == "" {
		request.IntegrationBranch = "develop"
	}
	if err := h.validateBranches(r.Context(), owner, repo, request.DefaultBranch, request.IntegrationBranch); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "github_validation_failed", err.Error())
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	config, _ := json.Marshal(repositoryConfig{IntegrationBranch: request.IntegrationBranch, WorkspacePolicy: request.WorkspacePolicy})
	item, err := h.Store.MutateRepository(r.Context(), postgres.Repository{
		ID: repositoryID(fullName), Provider: "github", FullName: fullName,
		DefaultBranch: request.DefaultBranch, Enabled: enabled, Config: config,
	}, "register", "operator-api")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, response(item))
}

func (h Handler) update(w http.ResponseWriter, r *http.Request) {
	item, err := h.Store.GetRepository(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var request repositoryRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	current := readConfig(item.Config)
	if request.FullName != "" && request.FullName != item.FullName {
		writeError(w, http.StatusBadRequest, "immutable_repository_identity", "full_name cannot be changed")
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
		writeError(w, http.StatusBadRequest, "invalid_repository", err.Error())
		return
	}
	if item.Enabled {
		if err := h.validateBranches(r.Context(), owner, repo, item.DefaultBranch, current.IntegrationBranch); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "github_validation_failed", err.Error())
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
	saved, err := h.Store.MutateRepository(r.Context(), item, action, "operator-api")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response(saved))
}

func (h Handler) audit(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.ListRepositoryConfigAudit(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "repository not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "store_error", "repository operation failed")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
