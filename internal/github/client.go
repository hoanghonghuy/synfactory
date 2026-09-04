package github

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
	"time"
)

type RateLimitError struct {
	StatusCode int
	RetryAt    time.Time
	Message    string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github rate limited (status %d) until %s: %s", e.StatusCode, e.RetryAt.UTC().Format(time.RFC3339), e.Message)
}

type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type StaticToken string

func (s StaticToken) Token(context.Context) (string, error) {
	return string(s), nil
}

type Client struct {
	baseURL     string
	tokenSource TokenSource
	httpClient  *http.Client
	now         func() time.Time
}

func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	return NewClientWithTokenSource(baseURL, StaticToken(token), httpClient)
}

func NewClientWithTokenSource(baseURL string, tokenSource TokenSource, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		tokenSource: tokenSource,
		httpClient:  httpClient,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

type Issue struct {
	Number      int64           `json:"number"`
	UpdatedAt   string          `json:"updated_at"`
	PullRequest json.RawMessage `json:"pull_request"`
}

type PullRequest struct {
	Number    int64  `json:"number"`
	UpdatedAt string `json:"updated_at"`
	Head      struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

type Review struct {
	ID          int64  `json:"id"`
	State       string `json:"state"`
	SubmittedAt string `json:"submitted_at"`
}

type CheckRun struct {
	ID          int64  `json:"id"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	HeadSHA     string `json:"head_sha"`
	UpdatedAt   string `json:"updated_at"`
	CompletedAt string `json:"completed_at"`
}

type Branch struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

func (c *Client) ListOpenIssues(ctx context.Context, owner, repo string) ([]Issue, error) {
	var result []Issue
	for page := 1; ; page++ {
		var batch []Issue
		if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/%s/issues?state=open&per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), page), &batch); err != nil {
			return nil, err
		}
		for _, issue := range batch {
			if len(issue.PullRequest) == 0 || string(issue.PullRequest) == "null" {
				result = append(result, issue)
			}
		}
		if len(batch) < 100 {
			return result, nil
		}
	}
}

func (c *Client) ListOpenPulls(ctx context.Context, owner, repo string) ([]PullRequest, error) {
	var result []PullRequest
	for page := 1; ; page++ {
		var batch []PullRequest
		if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/%s/pulls?state=open&per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), page), &batch); err != nil {
			return nil, err
		}
		result = append(result, batch...)
		if len(batch) < 100 {
			return result, nil
		}
	}
}

func (c *Client) ListReviews(ctx context.Context, owner, repo string, number int64) ([]Review, error) {
	var result []Review
	for page := 1; ; page++ {
		var batch []Review
		if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), number, page), &batch); err != nil {
			return nil, err
		}
		result = append(result, batch...)
		if len(batch) < 100 {
			return result, nil
		}
	}
}

func (c *Client) ListCheckRuns(ctx context.Context, owner, repo, ref string) ([]CheckRun, error) {
	var result []CheckRun
	for page := 1; ; page++ {
		var response struct {
			CheckRuns []CheckRun `json:"check_runs"`
		}
		path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(ref), page)
		if err := c.getJSON(ctx, path, &response); err != nil {
			return nil, err
		}
		result = append(result, response.CheckRuns...)
		if len(response.CheckRuns) < 100 {
			return result, nil
		}
	}
}

func (c *Client) GetBranch(ctx context.Context, owner, repo, branch string) (Branch, error) {
	var result Branch
	path := fmt.Sprintf("/repos/%s/%s/branches/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(branch))
	if err := c.getJSON(ctx, path, &result); err != nil {
		return Branch{}, err
	}
	return result, nil
}

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
		if err != nil {
			return fmt.Errorf("create github request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", "SynFactory")
		if c.tokenSource != nil {
			token, err := c.tokenForPath(ctx, path)
			if err != nil {
				return fmt.Errorf("resolve github token: %w", err)
			}
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("github request: %w", err)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 && c.invalidateTokenForPath(path) {
			_ = resp.Body.Close()
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			message := strings.TrimSpace(string(body))
			if rateErr := c.rateLimitError(resp, message); rateErr != nil {
				return rateErr
			}
			return fmt.Errorf("github API %s: status=%d body=%s", path, resp.StatusCode, message)
		}
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return fmt.Errorf("decode github response %s: %w", path, err)
		}
		return nil
	}
	return fmt.Errorf("github API %s: authentication retry exhausted", path)
}

func (c *Client) tokenForPath(ctx context.Context, path string) (string, error) {
	if source, ok := c.tokenSource.(RepositoryTokenSource); ok {
		owner, repo, found := repositoryFromAPIPath(path)
		if !found {
			return "", fmt.Errorf("repository-scoped github credential cannot resolve path %q", path)
		}
		return source.TokenForRepository(ctx, owner, repo)
	}
	return c.tokenSource.Token(ctx)
}

func (c *Client) invalidateTokenForPath(path string) bool {
	invalidator, ok := c.tokenSource.(RepositoryTokenInvalidator)
	if !ok {
		return false
	}
	owner, repo, found := repositoryFromAPIPath(path)
	if !found {
		return false
	}
	invalidator.InvalidateRepositoryToken(owner, repo)
	return true
}

func repositoryFromAPIPath(path string) (string, string, bool) {
	trimmed := strings.TrimPrefix(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 || parts[0] != "repos" {
		return "", "", false
	}
	owner, err := url.PathUnescape(parts[1])
	if err != nil || owner == "" {
		return "", "", false
	}
	repo, err := url.PathUnescape(parts[2])
	if err != nil || repo == "" {
		return "", "", false
	}
	return owner, repo, true
}

func (c *Client) rateLimitError(resp *http.Response, message string) error {
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusForbidden {
		return nil
	}
	now := c.now()
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
		seconds, err := strconv.Atoi(retryAfter)
		if err == nil && seconds >= 0 {
			return &RateLimitError{StatusCode: resp.StatusCode, RetryAt: now.Add(time.Duration(seconds) * time.Second), Message: message}
		}
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
		if err == nil && reset > 0 {
			return &RateLimitError{StatusCode: resp.StatusCode, RetryAt: time.Unix(reset, 0).UTC(), Message: message}
		}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return &RateLimitError{StatusCode: resp.StatusCode, RetryAt: now.Add(time.Minute), Message: message}
	}
	return nil
}

func IsRateLimited(err error) (*RateLimitError, bool) {
	var rateErr *RateLimitError
	ok := errors.As(err, &rateErr)
	return rateErr, ok
}
