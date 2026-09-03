package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientTurnsRetryAfterIntoRateLimitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "10")
		http.Error(w, "secondary rate limit", http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	now := time.Unix(100, 0).UTC()
	client.now = func() time.Time { return now }

	_, err := client.ListOpenIssues(context.Background(), "owner", "repo")
	rateErr, ok := IsRateLimited(err)
	if !ok {
		t.Fatalf("expected RateLimitError, got %v", err)
	}
	if !rateErr.RetryAt.Equal(now.Add(10 * time.Second)) {
		t.Fatalf("unexpected retry time: %s", rateErr.RetryAt)
	}
}

func TestClientFiltersPullRequestsFromIssuesEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"number":1,"updated_at":"a","pull_request":null},
			{"number":2,"updated_at":"b","pull_request":{"url":"x"}}
		]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "", server.Client())
	issues, err := client.ListOpenIssues(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Number != 1 {
		t.Fatalf("unexpected issues: %+v", issues)
	}
}
