package workflow

import (
	"sort"

	"github.com/hoanghonghuy/synfactory/internal/domain"
)

type Candidate struct {
	Instance Instance
	Decision Decision
}

type WIPLimits map[domain.Role]int

func SelectRunnable(candidates []Candidate, active map[domain.Role]int, limits WIPLimits) []Candidate {
	ordered := append([]Candidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Instance.Priority != ordered[j].Instance.Priority {
			return ordered[i].Instance.Priority > ordered[j].Instance.Priority
		}
		left := ordered[i].Instance.LastDispatchedAt
		right := ordered[j].Instance.LastDispatchedAt
		if left == nil && right != nil {
			return true
		}
		if left != nil && right == nil {
			return false
		}
		if left != nil && right != nil && !left.Equal(*right) {
			return left.Before(*right)
		}
		return ordered[i].Instance.CreatedAt.Before(ordered[j].Instance.CreatedAt)
	})

	counts := make(map[domain.Role]int, len(active))
	for role, count := range active {
		counts[role] = count
	}
	selected := make([]Candidate, 0, len(ordered))
	for _, candidate := range ordered {
		if candidate.Decision.Action == nil || candidate.Decision.Action.Mode != ActionJob {
			continue
		}
		role := candidate.Decision.Action.Role
		limit := limits[role]
		if limit > 0 && counts[role] >= limit {
			continue
		}
		selected = append(selected, candidate)
		counts[role]++
	}
	return selected
}
