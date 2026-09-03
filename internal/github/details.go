package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type Label struct {
	Name string `json:"name"`
}

type IssueDetails struct {
	Number    int64   `json:"number"`
	State     string  `json:"state"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	Labels    []Label `json:"labels"`
	UpdatedAt string  `json:"updated_at"`
}

type PullRequestDetails struct {
	Number    int64  `json:"number"`
	State     string `json:"state"`
	Merged    bool   `json:"merged"`
	Body      string `json:"body"`
	UpdatedAt string `json:"updated_at"`
	Head      struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

type ReviewDetails struct {
	ID          int64  `json:"id"`
	State       string `json:"state"`
	CommitID    string `json:"commit_id"`
	SubmittedAt string `json:"submitted_at"`
	User        struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (c *Client) GetIssueDetails(ctx context.Context, owner, repo string, number int64) (IssueDetails, error) {
	var result IssueDetails
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	if err := c.getJSON(ctx, path, &result); err != nil {
		return IssueDetails{}, err
	}
	return result, nil
}

func (c *Client) GetPullRequestDetails(ctx context.Context, owner, repo string, number int64) (PullRequestDetails, error) {
	var result PullRequestDetails
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	if err := c.getJSON(ctx, path, &result); err != nil {
		return PullRequestDetails{}, err
	}
	return result, nil
}

func (c *Client) ListOpenPullDetails(ctx context.Context, owner, repo string) ([]PullRequestDetails, error) {
	var result []PullRequestDetails
	for page := 1; ; page++ {
		var batch []PullRequestDetails
		path := fmt.Sprintf("/repos/%s/%s/pulls?state=open&per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), page)
		if err := c.getJSON(ctx, path, &batch); err != nil {
			return nil, err
		}
		result = append(result, batch...)
		if len(batch) < 100 {
			return result, nil
		}
	}
}

func (c *Client) ListReviewDetails(ctx context.Context, owner, repo string, number int64) ([]ReviewDetails, error) {
	var result []ReviewDetails
	for page := 1; ; page++ {
		var batch []ReviewDetails
		path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), number, page)
		if err := c.getJSON(ctx, path, &batch); err != nil {
			return nil, err
		}
		result = append(result, batch...)
		if len(batch) < 100 {
			return result, nil
		}
	}
}

type IssueMarkerResult struct {
	Number      int64           `json:"number"`
	Body        string          `json:"body"`
	HTMLURL     string          `json:"html_url"`
	PullRequest json.RawMessage `json:"pull_request"`
}

func (c *Client) FindIssueByFingerprint(ctx context.Context, repository, fingerprint string) (CreatedIssue, bool, error) {
	owner, repo, err := splitRepository(repository)
	if err != nil {
		return CreatedIssue{}, false, err
	}
	marker := "synfactory-task-fingerprint:" + strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return CreatedIssue{}, false, fmt.Errorf("task fingerprint is required")
	}
	for page := 1; ; page++ {
		var batch []IssueMarkerResult
		path := fmt.Sprintf("/repos/%s/%s/issues?state=all&per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), page)
		if err := c.getJSON(ctx, path, &batch); err != nil {
			return CreatedIssue{}, false, err
		}
		for _, item := range batch {
			if len(item.PullRequest) > 0 && string(item.PullRequest) != "null" {
				continue
			}
			if strings.Contains(item.Body, marker) {
				return CreatedIssue{Number: item.Number, URL: item.HTMLURL}, true, nil
			}
		}
		if len(batch) < 100 {
			return CreatedIssue{}, false, nil
		}
	}
}
