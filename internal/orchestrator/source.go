package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	githubfactory "github.com/hoanghonghuy/synfactory/internal/github"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type SourceStore interface {
	ListRepositories(ctx context.Context) ([]postgres.Repository, error)
	ListActiveWorkflows(ctx context.Context) ([]workflow.Instance, error)
}

type GitHubReader interface {
	ListOpenIssues(ctx context.Context, owner, repo string) ([]githubfactory.Issue, error)
	GetIssueDetails(ctx context.Context, owner, repo string, number int64) (githubfactory.IssueDetails, error)
	ListOpenPullDetails(ctx context.Context, owner, repo string) ([]githubfactory.PullRequestDetails, error)
	GetPullRequestDetails(ctx context.Context, owner, repo string, number int64) (githubfactory.PullRequestDetails, error)
	ListReviewDetails(ctx context.Context, owner, repo string, number int64) ([]githubfactory.ReviewDetails, error)
	ListCheckRuns(ctx context.Context, owner, repo, ref string) ([]githubfactory.CheckRun, error)
}

type GitHubSnapshotSource struct {
	store  SourceStore
	github GitHubReader
}

func NewGitHubSnapshotSource(store SourceStore, github GitHubReader) *GitHubSnapshotSource {
	return &GitHubSnapshotSource{store: store, github: github}
}

func (s *GitHubSnapshotSource) Snapshots(ctx context.Context) ([]workflow.Snapshot, error) {
	if s == nil || s.store == nil || s.github == nil {
		return nil, fmt.Errorf("workflow snapshot store and github reader are required")
	}
	repositories, err := s.store.ListRepositories(ctx)
	if err != nil {
		return nil, err
	}
	active, err := s.store.ListActiveWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	byRepository := map[string][]workflow.Instance{}
	for _, item := range active {
		if item.Kind == workflow.KindIssue {
			byRepository[item.RepositoryID] = append(byRepository[item.RepositoryID], item)
		}
	}

	var snapshots []workflow.Snapshot
	var failures []error
	for _, repository := range repositories {
		owner, repo, err := splitRepository(repository.FullName)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		openIssues, err := s.github.ListOpenIssues(ctx, owner, repo)
		if err != nil {
			failures = append(failures, fmt.Errorf("list open issues for %s: %w", repository.FullName, err))
			continue
		}
		openPulls, err := s.github.ListOpenPullDetails(ctx, owner, repo)
		if err != nil {
			failures = append(failures, fmt.Errorf("list open pulls for %s: %w", repository.FullName, err))
			continue
		}

		seen := map[string]bool{}
		for _, issue := range openIssues {
			key := strconv.FormatInt(issue.Number, 10)
			seen[key] = true
			instance := findWorkflow(byRepository[repository.ID], key)
			if instance.ID == "" {
				instance = workflow.NewInstance(repository.ID, workflow.KindIssue, key, issue.UpdatedAt, 100)
			}
			snapshot, err := s.issueSnapshot(ctx, repository, owner, repo, instance, issue.Number, openPulls)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			snapshots = append(snapshots, snapshot)
		}

		for _, instance := range byRepository[repository.ID] {
			if seen[instance.Subject] {
				continue
			}
			number, err := strconv.ParseInt(instance.Subject, 10, 64)
			if err != nil {
				failures = append(failures, fmt.Errorf("workflow %s has invalid issue subject %q", instance.ID, instance.Subject))
				continue
			}
			snapshot, err := s.issueSnapshot(ctx, repository, owner, repo, instance, number, openPulls)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			snapshots = append(snapshots, snapshot)
		}
	}
	return snapshots, errorsJoin(failures)
}

func (s *GitHubSnapshotSource) issueSnapshot(ctx context.Context, repository postgres.Repository, owner, repo string, instance workflow.Instance, issueNumber int64, openPulls []githubfactory.PullRequestDetails) (workflow.Snapshot, error) {
	issue, err := s.github.GetIssueDetails(ctx, owner, repo, issueNumber)
	if err != nil {
		return workflow.Snapshot{}, fmt.Errorf("get issue %s#%d: %w", repository.FullName, issueNumber, err)
	}
	facts := workflow.Facts{
		IssueOpen:  strings.EqualFold(issue.State, "open"),
		IssueReady: issueReady(issue.Labels),
		Review:     workflow.ReviewPending,
		CI:         workflow.CIUnknown,
	}
	if blockedLabel(issue.Labels) {
		facts.BlockingReason = "github_label_blocked"
	}

	prNumber := metadataInt64(instance.Metadata, "pull_request_number")
	var pull githubfactory.PullRequestDetails
	if prNumber > 0 {
		pull, err = s.github.GetPullRequestDetails(ctx, owner, repo, prNumber)
		if err != nil {
			return workflow.Snapshot{}, fmt.Errorf("get linked pull request %s#%d: %w", repository.FullName, prNumber, err)
		}
	} else if match, ok := findLinkedPull(openPulls, issueNumber); ok {
		pull = match
		prNumber = match.Number
	}

	metadata := metadataMap(instance.Metadata)
	metadata["repository_full_name"] = repository.FullName
	if prNumber > 0 {
		metadata["pull_request_number"] = prNumber
	}
	instance.Metadata, _ = json.Marshal(metadata)
	if issue.UpdatedAt != "" && instance.Revision == "" {
		instance.Revision = issue.UpdatedAt
	}

	if prNumber > 0 {
		facts.HasImplementationPR = true
		facts.PullRequestNumber = prNumber
		facts.PRMerged = pull.Merged
		facts.HeadSHA = pull.Head.SHA
		instance.Revision = pull.Head.SHA
		if pull.Head.SHA != "" {
			reviews, err := s.github.ListReviewDetails(ctx, owner, repo, prNumber)
			if err != nil {
				return workflow.Snapshot{}, fmt.Errorf("list reviews %s#%d: %w", repository.FullName, prNumber, err)
			}
			facts.Review, facts.ReviewedHeadSHA = reviewState(reviews, pull.Head.SHA)
			checks, err := s.github.ListCheckRuns(ctx, owner, repo, pull.Head.SHA)
			if err != nil {
				return workflow.Snapshot{}, fmt.Errorf("list checks %s#%d: %w", repository.FullName, prNumber, err)
			}
			facts.CI = ciState(checks)
		}
	}
	return workflow.Snapshot{Instance: instance, Facts: facts}, nil
}

func findWorkflow(items []workflow.Instance, subject string) workflow.Instance {
	for _, item := range items {
		if item.Subject == subject {
			return item
		}
	}
	return workflow.Instance{}
}

func issueReady(labels []githubfactory.Label) bool {
	for _, label := range labels {
		switch strings.ToLower(strings.TrimSpace(label.Name)) {
		case "ready", "ready-for-dev", "status:ready", "status/ready", "in-progress":
			return true
		}
	}
	return false
}

func blockedLabel(labels []githubfactory.Label) bool {
	for _, label := range labels {
		name := strings.ToLower(strings.TrimSpace(label.Name))
		if name == "blocked" || name == "status:blocked" || name == "status/blocked" {
			return true
		}
	}
	return false
}

func findLinkedPull(pulls []githubfactory.PullRequestDetails, issueNumber int64) (githubfactory.PullRequestDetails, bool) {
	for _, pull := range pulls {
		if referencesIssue(pull.Body, issueNumber) || branchReferencesIssue(pull.Head.Ref, issueNumber) {
			return pull, true
		}
	}
	return githubfactory.PullRequestDetails{}, false
}

func referencesIssue(body string, issueNumber int64) bool {
	pattern := fmt.Sprintf(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s*#%d\b`, issueNumber)
	matched, _ := regexp.MatchString(pattern, body)
	return matched
}

func branchReferencesIssue(branch string, issueNumber int64) bool {
	needle := strconv.FormatInt(issueNumber, 10)
	for _, token := range regexp.MustCompile(`[^0-9]+`).Split(branch, -1) {
		if token == needle {
			return true
		}
	}
	return false
}

func reviewState(reviews []githubfactory.ReviewDetails, headSHA string) (workflow.ReviewState, string) {
	sort.SliceStable(reviews, func(i, j int) bool { return reviews[i].SubmittedAt < reviews[j].SubmittedAt })
	state := workflow.ReviewPending
	reviewedHead := ""
	for _, review := range reviews {
		if review.CommitID != "" && review.CommitID != headSHA {
			continue
		}
		switch strings.ToUpper(review.State) {
		case "APPROVED":
			state = workflow.ReviewApproved
			reviewedHead = headSHA
		case "CHANGES_REQUESTED":
			state = workflow.ReviewChangesRequested
			reviewedHead = headSHA
		case "DISMISSED":
			state = workflow.ReviewPending
			reviewedHead = ""
		}
	}
	return state, reviewedHead
}

func ciState(checks []githubfactory.CheckRun) workflow.CIState {
	if len(checks) == 0 {
		return workflow.CIUnknown
	}
	for _, check := range checks {
		if !strings.EqualFold(check.Status, "completed") {
			return workflow.CIPending
		}
	}
	for _, check := range checks {
		switch strings.ToLower(check.Conclusion) {
		case "success", "neutral", "skipped":
			continue
		default:
			return workflow.CIFailing
		}
	}
	return workflow.CIPassing
}

func metadataMap(raw json.RawMessage) map[string]any {
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	return result
}

func metadataInt64(raw json.RawMessage, key string) int64 {
	values := metadataMap(raw)
	switch value := values[key].(type) {
	case float64:
		return int64(value)
	case string:
		number, _ := strconv.ParseInt(value, 10, 64)
		return number
	default:
		return 0
	}
}

func splitRepository(fullName string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(fullName), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repository %q must be owner/name", fullName)
	}
	return parts[0], parts[1], nil
}

func errorsJoin(items []error) error {
	if len(items) == 0 {
		return nil
	}
	var b strings.Builder
	for i, err := range items {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(err.Error())
	}
	return fmt.Errorf("%s", b.String())
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}
