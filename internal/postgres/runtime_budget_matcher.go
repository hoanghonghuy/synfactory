package postgres

import (
	"context"
	"fmt"
	"strings"

	runtimepolicy "github.com/hoanghonghuy/synfactory/internal/runtime"
)

func (s *Store) HasBudgetPolicy(ctx context.Context, request runtimepolicy.BudgetRequest) (bool, error) {
	repository := strings.TrimSpace(request.Repository)
	if repository == "" {
		return false, fmt.Errorf("runtime budget repository is required")
	}
	policies, err := s.RuntimeBudgetPolicies(ctx, repository)
	if err != nil {
		return false, err
	}
	for _, policy := range policies {
		if runtimeBudgetPolicyMatchesRequest(policy, request) {
			return true, nil
		}
	}
	return false, nil
}
