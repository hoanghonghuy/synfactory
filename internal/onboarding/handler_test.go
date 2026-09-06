package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/authz"
	githubfactory "github.com/hoanghonghuy/synfactory/internal/github"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type fakeStore struct {
	items   map[string]postgres.Repository
	actions []string
	actors  []string
}

func (s *fakeStore) GetRepository(_ context.Context, id string) (postgres.Repository, error) {
	item, ok := s.items[id]
	if !ok {
		return postgres.Repository{}, postgres.ErrNotFound
	}
	return item, nil
}

func (s *fakeStore) ListAllRepositories(context.Context) ([]postgres.Repository, error) {
	result := make([]postgres.Repository, 0, len(s.items))
	for _, item := range s.items {
		result = append(result, item)
	}
	return result, nil
}

func (s *fakeStore) MutateRepository(_ context.Context, item postgres.Repository, action, actor string) (postgres.Repository, error) {
	if s.items == nil {
		s.items = map[string]postgres.Repository{}
	}
	item.ConfigVersion++
	s.items[item.ID] = item
	s.actions = append(s.actions, action)
	s.actors = append(s.actors, actor)
	return item, nil
}

func (s *fakeStore) ListRepositoryConfigAudit(context.Context, string) ([]postgres.RepositoryConfigAudit, error) {
	return nil, nil
}

type fakeGitHub struct {
	failBranch string
	calls      []string
}

func (g *fakeGitHub) GetBranch(_ context.Context, owner, repo, branch string) (githubfactory.Branch, error) {
	g.calls = append(g.calls, owner+"/"+repo+":"+branch)
	if branch == g.failBranch {
		return githubfactory.Branch{}, errors.New("not found")
	}
	return githubfactory.Branch{Name: branch}, nil
}

type fakeAuthorizer struct {
	principal authz.Principal
	err       error
	seen      []authorizationRequest
}

type authorizationRequest struct {
	permission   authz.Permission
	repositoryID string
}

func (a *fakeAuthorizer) Authorize(_ *http.Request, permission authz.Permission, repositoryID string) (authz.Principal, error) {
	a.seen = append(a.seen, authorizationRequest{permission: permission, repositoryID: repositoryID})
	return a.principal, a.err
}

func TestCreateValidatesBranchesAndPersistsSafeConfiguration(t *testing.T) {
	store := &fakeStore{}
	github := &fakeGitHub{}
	handler := Handler{Store: store, GitHub: github, Token: "operator-secret"}
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/repository-config", strings.NewReader(`{"full_name":"acme/app","default_branch":"main","integration_branch":"develop","workspace_policy":"isolated"}`))
	request.Header.Set("Authorization", "Bearer operator-secret")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if len(github.calls) != 2 || github.calls[0] != "acme/app:main" || github.calls[1] != "acme/app:develop" {
		t.Fatalf("unexpected validation calls: %#v", github.calls)
	}
	if strings.Contains(response.Body.String(), "operator-secret") {
		t.Fatal("operator token leaked in response")
	}
	if len(store.actions) != 1 || store.actions[0] != "register" {
		t.Fatalf("unexpected actions: %#v", store.actions)
	}
}

func TestCreateUsesRepositoryScopedAuthorizationAndNamedActor(t *testing.T) {
	store := &fakeStore{}
	authorizer := &fakeAuthorizer{principal: authz.Principal{Subject: "user-42"}}
	handler := Handler{Store: store, GitHub: &fakeGitHub{}, Authorizer: authorizer}
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/repository-config", strings.NewReader(`{"full_name":"acme/app","enabled":false}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	wantRepositoryID := repositoryID("acme/app")
	if len(authorizer.seen) != 1 || authorizer.seen[0].permission != authz.PermissionRepositoryMutate || authorizer.seen[0].repositoryID != wantRepositoryID {
		t.Fatalf("unexpected authorization requests: %#v", authorizer.seen)
	}
	if len(store.actors) != 1 || store.actors[0] != "user-42" {
		t.Fatalf("unexpected audit actors: %#v", store.actors)
	}
}

func TestRepositoryScopedForbiddenMutationStopsBeforeStoreLookup(t *testing.T) {
	authorizer := &fakeAuthorizer{err: authz.ErrForbidden}
	store := &fakeStore{items: map[string]postgres.Repository{"repo-1": {ID: "repo-1", FullName: "acme/app"}}}
	handler := Handler{Store: store, GitHub: &fakeGitHub{}, Authorizer: authorizer}
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/repository-config/repo-1", strings.NewReader(`{"enabled":false}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if len(store.actions) != 0 {
		t.Fatalf("forbidden mutation reached store: %#v", store.actions)
	}
	if len(authorizer.seen) != 1 || authorizer.seen[0].repositoryID != "repo-1" {
		t.Fatalf("unexpected authorization requests: %#v", authorizer.seen)
	}
}

func TestCreateRejectsInvalidIntegrationBranchWithoutPersistence(t *testing.T) {
	store := &fakeStore{}
	github := &fakeGitHub{failBranch: "develop"}
	handler := Handler{Store: store, GitHub: github, Token: "token"}
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/repository-config", strings.NewReader(`{"full_name":"acme/app","default_branch":"main","integration_branch":"develop"}`))
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if len(store.actions) != 0 {
		t.Fatalf("invalid repository was persisted: %#v", store.actions)
	}
}

func TestDisabledRegistrationCanBeStagedWithoutGitHubValidation(t *testing.T) {
	store := &fakeStore{}
	github := &fakeGitHub{failBranch: "main"}
	handler := Handler{Store: store, GitHub: github, Token: "token"}
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/repository-config", strings.NewReader(`{"full_name":"acme/app","default_branch":"main","integration_branch":"develop","enabled":false}`))
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if len(github.calls) != 0 {
		t.Fatalf("disabled registration unexpectedly validated GitHub: %#v", github.calls)
	}
	if len(store.actions) != 1 || store.actions[0] != "register" {
		t.Fatalf("unexpected actions: %#v", store.actions)
	}
}

func TestEnableRequiresGitHubValidationBeforePersistence(t *testing.T) {
	config, _ := json.Marshal(repositoryConfig{IntegrationBranch: "develop"})
	item := postgres.Repository{ID: "repo-1", Provider: "github", FullName: "acme/app", DefaultBranch: "main", Enabled: false, Config: config}
	store := &fakeStore{items: map[string]postgres.Repository{"repo-1": item}}
	github := &fakeGitHub{failBranch: "main"}
	handler := Handler{Store: store, GitHub: github, Token: "token"}
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/repository-config/repo-1", strings.NewReader(`{"enabled":true}`))
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if len(store.actions) != 0 {
		t.Fatalf("repository was enabled despite failed validation: %#v", store.actions)
	}
	if len(github.calls) != 1 || github.calls[0] != "acme/app:main" {
		t.Fatalf("unexpected validation calls: %#v", github.calls)
	}
}

func TestDisableDoesNotRequireGitHubAndUsesDisableAction(t *testing.T) {
	config, _ := json.Marshal(repositoryConfig{IntegrationBranch: "develop"})
	item := postgres.Repository{ID: "repo-1", Provider: "github", FullName: "acme/app", DefaultBranch: "main", Enabled: true, Config: config}
	store := &fakeStore{items: map[string]postgres.Repository{"repo-1": item}}
	handler := Handler{Store: store, GitHub: &fakeGitHub{failBranch: "main"}, Token: "token"}
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/repository-config/repo-1", strings.NewReader(`{"enabled":false}`))
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if len(store.actions) != 1 || store.actions[0] != "disable" {
		t.Fatalf("unexpected actions: %#v", store.actions)
	}
	if store.items["repo-1"].Enabled {
		t.Fatal("repository remained enabled")
	}
}

func TestAuthorizationRequired(t *testing.T) {
	handler := Handler{Store: &fakeStore{}, GitHub: &fakeGitHub{}, Token: "token"}
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/repository-config", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}
