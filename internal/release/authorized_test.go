package release

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

type recordingPromotionAuthorizer struct {
	principal  authz.Principal
	err        error
	permission authz.Permission
	repository string
}

func (a *recordingPromotionAuthorizer) Authorize(_ *http.Request, permission authz.Permission, repositoryID string) (authz.Principal, error) {
	a.permission = permission
	a.repository = repositoryID
	return a.principal, a.err
}

func TestAuthorizedPublisherRequiresReleasePromote(t *testing.T) {
	authorizer := &recordingPromotionAuthorizer{err: authz.ErrForbidden}
	publisher := AuthorizedPublisher{Authorizer: authorizer}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/releases/promote", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = publisher.Publish(context.Background(), req, "repo-1", PublishInput{})
	if !errors.Is(err, ErrReleasePromotionForbidden) {
		t.Fatalf("expected release promotion denial, got %v", err)
	}
	if authorizer.permission != authz.PermissionReleasePromote {
		t.Fatalf("expected release_promote authorization, got %q", authorizer.permission)
	}
	if authorizer.repository != "repo-1" {
		t.Fatalf("expected repository-scoped authorization, got %q", authorizer.repository)
	}
}

func TestAuthorizedPublisherRejectsMissingRepositoryBeforeMutation(t *testing.T) {
	authorizer := &recordingPromotionAuthorizer{principal: authz.Principal{Subject: "user-1"}}
	publisher := AuthorizedPublisher{Authorizer: authorizer}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/releases/promote", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = publisher.Publish(context.Background(), req, "  ", PublishInput{})
	if !errors.Is(err, ErrReleasePromotionForbidden) {
		t.Fatalf("expected missing repository denial, got %v", err)
	}
	if authorizer.permission != "" {
		t.Fatalf("authorizer should not run without repository scope, got %q", authorizer.permission)
	}
}

func TestAuthorizedPublisherDelegatesAfterAuthorization(t *testing.T) {
	authorizer := &recordingPromotionAuthorizer{principal: authz.Principal{Subject: "user-1"}}
	publisher := AuthorizedPublisher{Authorizer: authorizer, Publisher: Publisher{}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/releases/promote", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = publisher.Publish(context.Background(), req, "repo-1", PublishInput{})
	if errors.Is(err, ErrReleasePromotionForbidden) {
		t.Fatalf("authorized request must reach publisher, got %v", err)
	}
	if !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("expected underlying publisher validation error, got %v", err)
	}
}
