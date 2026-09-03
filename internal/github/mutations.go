package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type MergeResult struct {
	SHA     string `json:"sha"`
	Merged  bool   `json:"merged"`
	Message string `json:"message"`
}

func (c *Client) CloseIssue(ctx context.Context, repository string, issueNumber int64) error {
	owner, repo, err := splitRepository(repository)
	if err != nil {
		return err
	}
	if issueNumber <= 0 {
		return fmt.Errorf("positive issue number is required")
	}
	var response map[string]any
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), issueNumber)
	if err := c.doJSON(ctx, http.MethodPatch, path, map[string]any{"state": "closed", "state_reason": "completed"}, &response); err != nil {
		return fmt.Errorf("close github issue: %w", err)
	}
	return nil
}

func (c *Client) MergePullRequest(ctx context.Context, repository string, number int64, expectedHeadSHA string) (MergeResult, error) {
	owner, repo, err := splitRepository(repository)
	if err != nil {
		return MergeResult{}, err
	}
	if number <= 0 || strings.TrimSpace(expectedHeadSHA) == "" {
		return MergeResult{}, fmt.Errorf("positive pull request number and expected head sha are required")
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", url.PathEscape(owner), url.PathEscape(repo), number)
	var result MergeResult
	if err := c.doJSON(ctx, http.MethodPut, path, map[string]any{"sha": expectedHeadSHA, "merge_method": "squash"}, &result); err != nil {
		return MergeResult{}, fmt.Errorf("merge github pull request: %w", err)
	}
	if !result.Merged {
		return result, fmt.Errorf("github refused merge: %s", result.Message)
	}
	return result, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode github request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create github request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "SynFactory")
	if c.tokenSource != nil {
		token, err := c.tokenSource.Token(ctx)
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
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		message := strings.TrimSpace(string(responseBody))
		if rateErr := c.rateLimitError(resp, message); rateErr != nil {
			return rateErr
		}
		return fmt.Errorf("github API %s: status=%d body=%s", path, resp.StatusCode, message)
	}
	if target == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode github response %s: %w", path, err)
	}
	return nil
}

func splitRepository(repository string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(repository), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repository must be owner/name")
	}
	return parts[0], parts[1], nil
}

func (c *Client) Merge(ctx context.Context, repository string, number int64, expectedHeadSHA string) error {
	_, err := c.MergePullRequest(ctx, repository, number, expectedHeadSHA)
	return err
}

type CreatedIssue struct {
	Number int64  `json:"number"`
	URL    string `json:"html_url"`
}

func (c *Client) CreateIssue(ctx context.Context, repository, title, body string, labels []string) (CreatedIssue, error) {
	owner, repo, err := splitRepository(repository)
	if err != nil {
		return CreatedIssue{}, err
	}
	if strings.TrimSpace(title) == "" || strings.TrimSpace(body) == "" {
		return CreatedIssue{}, fmt.Errorf("issue title and body are required")
	}
	payload := map[string]any{"title": title, "body": body}
	if len(labels) > 0 {
		payload["labels"] = labels
	}
	var result CreatedIssue
	path := fmt.Sprintf("/repos/%s/%s/issues", url.PathEscape(owner), url.PathEscape(repo))
	if err := c.doJSON(ctx, http.MethodPost, path, payload, &result); err != nil {
		return CreatedIssue{}, fmt.Errorf("create github issue: %w", err)
	}
	if result.Number <= 0 {
		return CreatedIssue{}, fmt.Errorf("github create issue returned no issue number")
	}
	return result, nil
}
