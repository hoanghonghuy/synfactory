package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AppTokenSource mints and caches GitHub App installation tokens.
// Access tokens are kept in memory only and refreshed before expiry.
type AppTokenSource struct {
	baseURL        string
	appID          int64
	installationID int64
	privateKey     *rsa.PrivateKey
	httpClient     *http.Client
	now            func() time.Time
	refreshBefore  time.Duration

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

type InstallationTokenError struct {
	InstallationID int64
	StatusCode     int
	Permanent      bool
	Message        string
}

func (e *InstallationTokenError) Error() string {
	return fmt.Sprintf("github installation token request: installation=%d status=%d permanent=%t: %s", e.InstallationID, e.StatusCode, e.Permanent, e.Message)
}

func NewAppTokenSource(baseURL string, appID, installationID int64, privateKeyPEM []byte, httpClient *http.Client) (*AppTokenSource, error) {
	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	if appID <= 0 || installationID <= 0 {
		return nil, fmt.Errorf("github app id and installation id must be positive")
	}
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &AppTokenSource{
		baseURL:        strings.TrimRight(baseURL, "/"),
		appID:          appID,
		installationID: installationID,
		privateKey:     key,
		httpClient:     httpClient,
		now:            func() time.Time { return time.Now().UTC() },
		refreshBefore:  5 * time.Minute,
	}, nil
}

func (s *AppTokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if s.token != "" && now.Add(s.refreshBefore).Before(s.expiresAt) {
		return s.token, nil
	}

	jwt, err := signAppJWT(s.privateKey, s.appID, now)
	if err != nil {
		return "", fmt.Errorf("sign github app jwt: %w", err)
	}

	path := fmt.Sprintf("/app/installations/%d/access_tokens", s.installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+path, nil)
	if err != nil {
		return "", fmt.Errorf("create github installation token request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "SynFactory")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github installation token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return "", &InstallationTokenError{
			InstallationID: s.installationID,
			StatusCode:     resp.StatusCode,
			Permanent:      resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden,
			Message:        sanitizeGitHubError(body),
		}
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode github installation token response: %w", err)
	}
	if result.Token == "" || result.ExpiresAt.IsZero() {
		return "", fmt.Errorf("github installation token response missing token or expiry")
	}

	s.token = result.Token
	s.expiresAt = result.ExpiresAt.UTC()
	return s.token, nil
}

func parseRSAPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("decode github app private key PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse github app private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("github app private key must be RSA")
	}
	return key, nil
}

func signAppJWT(key *rsa.PrivateKey, appID int64, now time.Time) (string, error) {
	encode := base64.RawURLEncoding.EncodeToString
	headerJSON, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	header := encode(headerJSON)
	payload, err := json.Marshal(map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": strconv.FormatInt(appID, 10),
	})
	if err != nil {
		return "", err
	}
	unsigned := header + "." + encode(payload)
	digest := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + encode(sig), nil
}
