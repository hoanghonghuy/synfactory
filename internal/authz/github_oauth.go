package authz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultGitHubOAuthTokenURL = "https://github.com/login/oauth/access_token"
	defaultGitHubOAuthUserURL  = "https://api.github.com/user"
	maxGitHubOAuthResponseBody = 1 << 20
)

// GitHubOAuthProvider exchanges a GitHub OAuth authorization code for the
// immutable GitHub user ID used as SynFactory's external identity subject.
type GitHubOAuthProvider struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
	UserURL      string
	Client       *http.Client
}

func (p GitHubOAuthProvider) Exchange(ctx context.Context, code, redirectURI string) (ExternalIdentity, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return ExternalIdentity{}, errors.New("github oauth code is required")
	}
	if strings.TrimSpace(p.ClientID) == "" || strings.TrimSpace(p.ClientSecret) == "" {
		return ExternalIdentity{}, errors.New("github oauth client credentials are required")
	}

	tokenURL := strings.TrimSpace(p.TokenURL)
	if tokenURL == "" {
		tokenURL = defaultGitHubOAuthTokenURL
	}
	form := url.Values{
		"client_id":     {strings.TrimSpace(p.ClientID)},
		"client_secret": {strings.TrimSpace(p.ClientSecret)},
		"code":          {code},
	}
	if redirectURI = strings.TrimSpace(redirectURI); redirectURI != "" {
		form.Set("redirect_uri", redirectURI)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return ExternalIdentity{}, fmt.Errorf("create github oauth token request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return ExternalIdentity{}, fmt.Errorf("exchange github oauth code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ExternalIdentity{}, fmt.Errorf("exchange github oauth code: unexpected status %d", resp.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxGitHubOAuthResponseBody)).Decode(&token); err != nil {
		return ExternalIdentity{}, fmt.Errorf("decode github oauth token response: %w", err)
	}
	if token.Error != "" {
		return ExternalIdentity{}, fmt.Errorf("exchange github oauth code: %s", token.Error)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return ExternalIdentity{}, errors.New("exchange github oauth code: access token missing")
	}

	userURL := strings.TrimSpace(p.UserURL)
	if userURL == "" {
		userURL = defaultGitHubOAuthUserURL
	}
	userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, userURL, nil)
	if err != nil {
		return ExternalIdentity{}, fmt.Errorf("create github oauth user request: %w", err)
	}
	userReq.Header.Set("Accept", "application/vnd.github+json")
	userReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token.AccessToken))
	userReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	userResp, err := client.Do(userReq)
	if err != nil {
		return ExternalIdentity{}, fmt.Errorf("load github oauth user: %w", err)
	}
	defer userResp.Body.Close()
	if userResp.StatusCode < http.StatusOK || userResp.StatusCode >= http.StatusMultipleChoices {
		return ExternalIdentity{}, fmt.Errorf("load github oauth user: unexpected status %d", userResp.StatusCode)
	}
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(userResp.Body, maxGitHubOAuthResponseBody)).Decode(&user); err != nil {
		return ExternalIdentity{}, fmt.Errorf("decode github oauth user: %w", err)
	}
	if user.ID <= 0 {
		return ExternalIdentity{}, errors.New("github oauth user has no immutable id")
	}
	name := strings.TrimSpace(user.Name)
	if name == "" {
		name = strings.TrimSpace(user.Login)
	}
	return ValidateExternalIdentity(ExternalIdentity{
		Provider: "github",
		Subject:  strconv.FormatInt(user.ID, 10),
		Name:     name,
		Email:    user.Email,
	})
}
