package release

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

var ErrReleasePromotionForbidden = errors.New("release promotion forbidden")

// AuthorizedPublisher requires an independently granted release_promote
// capability before any registry mutation can occur. Ordinary operator or
// repository_mutate authority is intentionally insufficient.
type AuthorizedPublisher struct {
	Publisher  Publisher
	Authorizer authz.RequestAuthorizer
}

func (p AuthorizedPublisher) Publish(ctx context.Context, request *http.Request, repositoryID string, input PublishInput) (Manifest, error) {
	repositoryID = strings.TrimSpace(repositoryID)
	if repositoryID == "" {
		return Manifest{}, fmt.Errorf("%w: repository id is required", ErrReleasePromotionForbidden)
	}
	if p.Authorizer == nil || request == nil {
		return Manifest{}, fmt.Errorf("%w: authorizer unavailable", ErrReleasePromotionForbidden)
	}
	request = request.Clone(ctx)
	if _, err := p.Authorizer.Authorize(request, authz.PermissionReleasePromote, repositoryID); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrReleasePromotionForbidden, err)
	}
	return p.Publisher.Publish(ctx, input)
}
