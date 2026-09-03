package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	KindIssueChanged           = "github.issue.changed"
	KindIssueCommentChanged    = "github.issue_comment.changed"
	KindPRChanged              = "github.pr.changed"
	KindPRReviewChanged        = "github.pr_review.changed"
	KindPRReviewCommentChanged = "github.pr_review_comment.changed"
	KindCICheckChanged         = "github.ci.changed"
	KindBranchChanged          = "github.branch.changed"
	KindWorkflowChanged        = "github.workflow.changed"
)

var (
	ErrUnsupportedEvent = errors.New("unsupported github webhook event")
	ErrInvalidPayload   = errors.New("invalid github webhook payload")
)

type LogicalEvent struct {
	RepositoryID       string
	RepositoryFullName string
	DefaultBranch      string
	Kind               string
	Subject            string
	Revision           string
	DeliveryID         string
	Payload            json.RawMessage
}

type webhookEnvelope struct {
	Action     string `json:"action"`
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Repository struct {
		ID            int64  `json:"id"`
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
	Issue struct {
		Number      int64            `json:"number"`
		UpdatedAt   string           `json:"updated_at"`
		PullRequest *json.RawMessage `json:"pull_request"`
	} `json:"issue"`
	Comment struct {
		ID        int64  `json:"id"`
		UpdatedAt string `json:"updated_at"`
	} `json:"comment"`
	PullRequest struct {
		Number    int64  `json:"number"`
		UpdatedAt string `json:"updated_at"`
		Head      struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Review struct {
		ID          int64  `json:"id"`
		State       string `json:"state"`
		SubmittedAt string `json:"submitted_at"`
	} `json:"review"`
	CheckRun struct {
		ID          int64  `json:"id"`
		Status      string `json:"status"`
		Conclusion  string `json:"conclusion"`
		HeadSHA     string `json:"head_sha"`
		UpdatedAt   string `json:"updated_at"`
		CompletedAt string `json:"completed_at"`
	} `json:"check_run"`
	CheckSuite struct {
		ID         int64  `json:"id"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadSHA    string `json:"head_sha"`
		UpdatedAt  string `json:"updated_at"`
	} `json:"check_suite"`
	WorkflowRun struct {
		ID         int64  `json:"id"`
		RunAttempt int    `json:"run_attempt"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadSHA    string `json:"head_sha"`
		UpdatedAt  string `json:"updated_at"`
	} `json:"workflow_run"`
}

func NormalizeWebhook(eventName, deliveryID string, body []byte) (LogicalEvent, error) {
	var envelope webhookEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return LogicalEvent{}, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	if envelope.Repository.ID == 0 || envelope.Repository.FullName == "" {
		return LogicalEvent{}, fmt.Errorf("%w: repository identity missing", ErrInvalidPayload)
	}

	event := LogicalEvent{
		RepositoryID:       RepositoryID(envelope.Repository.ID),
		RepositoryFullName: envelope.Repository.FullName,
		DefaultBranch:      envelope.Repository.DefaultBranch,
		DeliveryID:         deliveryID,
		Payload:            append(json.RawMessage(nil), body...),
	}

	switch eventName {
	case "issues":
		event.Kind = KindIssueChanged
		event.Subject = strconv.FormatInt(envelope.Issue.Number, 10)
		event.Revision = envelope.Issue.UpdatedAt
	case "issue_comment":
		event.Kind = KindIssueCommentChanged
		event.Subject = fmt.Sprintf("%d:%d", envelope.Issue.Number, envelope.Comment.ID)
		event.Revision = firstNonEmpty(envelope.Comment.UpdatedAt, envelope.Issue.UpdatedAt)
	case "pull_request":
		event.Kind = KindPRChanged
		event.Subject = strconv.FormatInt(envelope.PullRequest.Number, 10)
		event.Revision = firstNonEmpty(envelope.PullRequest.UpdatedAt, envelope.PullRequest.Head.SHA)
	case "pull_request_review":
		event.Kind = KindPRReviewChanged
		event.Subject = fmt.Sprintf("%d:%d", envelope.PullRequest.Number, envelope.Review.ID)
		event.Revision = strings.Join([]string{envelope.Review.SubmittedAt, envelope.Review.State}, ":")
	case "pull_request_review_comment":
		event.Kind = KindPRReviewCommentChanged
		event.Subject = fmt.Sprintf("%d:%d", envelope.PullRequest.Number, envelope.Comment.ID)
		event.Revision = envelope.Comment.UpdatedAt
	case "check_run":
		event.Kind = KindCICheckChanged
		event.Subject = "check_run:" + strconv.FormatInt(envelope.CheckRun.ID, 10)
		event.Revision = firstNonEmpty(envelope.CheckRun.UpdatedAt, envelope.CheckRun.CompletedAt, envelope.CheckRun.HeadSHA)
	case "check_suite":
		event.Kind = KindCICheckChanged
		event.Subject = "check_suite:" + strconv.FormatInt(envelope.CheckSuite.ID, 10)
		event.Revision = firstNonEmpty(envelope.CheckSuite.UpdatedAt, envelope.CheckSuite.HeadSHA)
	case "push":
		event.Kind = KindBranchChanged
		event.Subject = envelope.Ref
		event.Revision = envelope.After
	case "workflow_run":
		event.Kind = KindWorkflowChanged
		event.Subject = strconv.FormatInt(envelope.WorkflowRun.ID, 10)
		event.Revision = strings.Join([]string{
			strconv.Itoa(envelope.WorkflowRun.RunAttempt),
			envelope.WorkflowRun.UpdatedAt,
			envelope.WorkflowRun.Status,
			envelope.WorkflowRun.Conclusion,
			envelope.WorkflowRun.HeadSHA,
		}, ":")
	default:
		return LogicalEvent{}, fmt.Errorf("%w: %s", ErrUnsupportedEvent, eventName)
	}

	if event.Subject == "" || event.Revision == "" {
		return LogicalEvent{}, fmt.Errorf("%w: %s missing subject or revision", ErrInvalidPayload, eventName)
	}
	return event, nil
}

func RepositoryID(id int64) string {
	return "github:" + strconv.FormatInt(id, 10)
}

func CanonicalRevision(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
