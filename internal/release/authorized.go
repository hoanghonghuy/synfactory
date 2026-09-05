package release

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

var ErrReleasePromotionForbidden = errors.New("release promotion forbidden")

// PromotionAuthorizer keeps release promotion behind the same Go-owned
// authorization policy as other sensitive operator capabilities.
type PromotionAuthorizer interface {
	AuthorizeToken(ctx context.Context, token string, permission authz.Permission, repositoryID string) (authz.Principal, error)
}

// AuthorizedPublisher requires an independently granted release_promote
// capability before any registry mutation can occur. Ordinary operator or
// repository_mutate authority is intentionally insufficient.
type AuthorizedPublisher struct {
	Publisher  Publisher
	Authorizer PromotionAuthorizer
}

func (p AuthorizedPublisher) Publish(ctx context.Context, token, repositoryID string, input PublishInput) (Manifest, error) {
	repositoryID = strings.TrimSpace(repositoryID)
	if repositoryID == "" {
		return Manifest{}, fmt.Errorf("%w: repository id is required", ErrReleasePromotionForbidden)
	}
	if p.Authorizer == nil {
		return Manifest{}, fmt.Errorf("%w: authorizer unavailable", ErrReleasePromotionForbidden)
	}
	if _, err := p.Authorizer.AuthorizeToken(ctx, token, authz.PermissionReleasePromote, repositoryID); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrReleasePromotionForbidden, err)
	}
	return p.Publisher.Publish(ctx, input)
}
