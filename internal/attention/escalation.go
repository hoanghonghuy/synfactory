package attention

import (
	"fmt"
	"strings"
	"time"
)

// EscalationRule routes active attention items to one or more notification
// providers after a minimum age. An empty RepositoryID applies globally.
type EscalationRule struct {
	RepositoryID string
	MinSeverity  Severity
	After        time.Duration
	Providers    []string
}

// EscalationRouter evaluates repository, severity and age without mutating the
// attention item. Notification delivery remains transport state, not workflow truth.
type EscalationRouter struct {
	Rules []EscalationRule
}

func (r EscalationRouter) Route(item Item, now time.Time) ([]string, error) {
	now = now.UTC()
	if !item.Active(now) {
		return nil, nil
	}
	itemSeverity, ok := severityRank(item.Severity)
	if !ok {
		return nil, fmt.Errorf("unsupported attention severity %q", item.Severity)
	}

	providers := make([]string, 0)
	seen := make(map[string]struct{})
	for index, rule := range r.Rules {
		if rule.After < 0 {
			return nil, fmt.Errorf("escalation rule %d has negative age threshold", index)
		}
		minimum, ok := severityRank(rule.MinSeverity)
		if !ok {
			return nil, fmt.Errorf("escalation rule %d has unsupported minimum severity %q", index, rule.MinSeverity)
		}
		repositoryID := strings.TrimSpace(rule.RepositoryID)
		if repositoryID != "" && repositoryID != strings.TrimSpace(item.RepositoryID) {
			continue
		}
		if itemSeverity < minimum || now.Sub(item.CreatedAt.UTC()) < rule.After {
			continue
		}
		for _, provider := range rule.Providers {
			provider = strings.TrimSpace(provider)
			if provider == "" {
				return nil, fmt.Errorf("escalation rule %d contains an empty provider", index)
			}
			if _, exists := seen[provider]; exists {
				continue
			}
			seen[provider] = struct{}{}
			providers = append(providers, provider)
		}
	}
	return providers, nil
}

func severityRank(severity Severity) (int, bool) {
	switch severity {
	case SeverityInfo:
		return 1, true
	case SeverityWarning:
		return 2, true
	case SeverityCritical:
		return 3, true
	default:
		return 0, false
	}
}
