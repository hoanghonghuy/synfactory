package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// RepositoryTokenSource resolves credentials for one GitHub repository.
// Client uses this interface when the request path identifies owner/repository.
type RepositoryTokenSource interface {
	TokenForRepository(ctx context.Context, owner, repo string) (string, error)
}

// RepositoryTokenInvalidator allows Client to discard a cached repository token
// after an authentication failure before performing one bounded retry.
type RepositoryTokenInvalidator interface {
	InvalidateRepositoryToken(owner, repo string)
}

// InstallationError classifies GitHub App installation failures without exposing
// private keys or access tokens.
type InstallationError struct {
	Repository string
	StatusCode int
	Permanent  bool
	Message    string
}

func (e *InstallationError) Error() string {
	return fmt.Sprintf("github app installation for %s: status=%d permanent=%t: %s", e.Repository, e.StatusCode, e.Permanent, e.Message)
}

func IsPermanentInstallationError(err error) (*InstallationError, bool) {
	var installationErr *InstallationError
	if !errors.As(err, &installationErr) {
		return nil, false
	}
	return installationErr, installationErr.Permanent
}

// AppRepositoryTokenSource discovers the GitHub App installation associated
// with each repository and delegates installation-token minting/caching to
// AppTokenSource. Installation IDs are safe metadata; access tokens remain
// memory-only.
type AppRepositoryTokenSource struct {
	baseURL    string
	appID      int64
	privateKey []byte
	httpClient *http.Client
	now        func() time.Time

	mu            sync.Mutex
	installations map[string]int64
	tokens        map[int64]*AppTokenSource
}

func NewAppRepositoryTokenSource(baseURL string, appID int64, privateKeyPEM []byte, httpClient *http.Client) (*AppRepositoryTokenSource, error) {
	if appID <= 0 {
		return nil, fmt.Errorf("github app id must be positive")
	}
	if _, err := parseRSAPrivateKey(privateKeyPEM); err != nil {
		return nil, err
	}
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &AppRepositoryTokenSource{
		baseURL:       strings.TrimRight(baseURL, "/"),
		appID:         appID,
		privateKey:    append([]byte(nil), privateKeyPEM...),
		httpClient:    httpClient,
		now:           func() time.Time { return time.Now().UTC() },
		installations: make(map[string]int64),
		tokens:        make(map[int64]*AppTokenSource),
	}, nil
}

// Token deliberately rejects repository-less use so App mode cannot silently
// fall back to one installation across multiple managed repositories.
func (s *AppRepositoryTokenSource) Token(context.Context) (string, error) {
	return "", fmt.Errorf("github app authentication requires repository-scoped token resolution")
}

func (s *AppRepositoryTokenSource) TokenForRepository(ctx context.Context, owner, repo string) (string, error) {
	key, err := repositoryKey(owner, repo)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	installationID := s.installations[key]
	if installationID == 0 {
		installationID, err = s.discoverInstallation(ctx, owner, repo)
		if err != nil {
			return "", err
		}
		s.installations[key] = installationID
	}

	source := s.tokens[installationID]
	if source == nil {
		source, err = NewAppTokenSource(s.baseURL, s.appID, installationID, s.privateKey, s.httpClient)
		if err != nil {
			return "", err
		}
		source.now = s.now
		s.tokens[installationID] = source
	}

	return source.Token(ctx)
}

func (s *AppRepositoryTokenSource) InvalidateRepositoryToken(owner, repo string) {
	key, err := repositoryKey(owner, repo)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	installationID := s.installations[key]
	delete(s.installations, key)
	if installationID != 0 {
		delete(s.tokens, installationID)
	}
}

func (s *AppRepositoryTokenSource) discoverInstallation(ctx context.Context, owner, repo string) (int64, error) {
	key, err := parseRSAPrivateKey(s.privateKey)
	if err != nil {
		return 0, err
	}
	jwt, err := signAppJWT(key, s.appID, s.now())
	if err != nil {
		return 0, fmt.Errorf("sign github app jwt: %w", err)
	}
	path := fmt.Sprintf("/repos/%s/%s/installation", url.PathEscape(owner), url.PathEscape(repo))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return 0, fmt.Errorf("create github installation discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "SynFactory")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("github installation discovery request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		message := sanitizeGitHubError(body)
		return 0, &InstallationError{
			Repository: owner + "/" + repo,
			StatusCode: resp.StatusCode,
			Permanent:  resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden,
			Message:    message,
		}
	}
	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode github installation discovery response: %w", err)
	}
	if result.ID <= 0 {
		return 0, &InstallationError{Repository: owner + "/" + repo, StatusCode: resp.StatusCode, Permanent: true, Message: "installation response missing id"}
	}
	return result.ID, nil
}

func repositoryKey(owner, repo string) (string, error) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if owner == "" || repo == "" || strings.ContainsAny(owner+repo, "\r\n\t ") {
		return "", fmt.Errorf("github repository owner and name are required")
	}
	return strings.ToLower(owner + "/" + repo), nil
}

func sanitizeGitHubError(body []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil && strings.TrimSpace(payload.Message) != "" {
		return strings.TrimSpace(payload.Message)
	}
	message := strings.TrimSpace(string(body))
	if len(message) > 512 {
		message = message[:512]
	}
	if message == "" {
		return "github app installation request failed"
	}
	return message
}
