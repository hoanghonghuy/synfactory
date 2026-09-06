package runtime

import (
	"context"
	"strconv"
	"strings"
)

const routingPolicyVersion = "runtime-score-v1"

type RoutingMetricsRequest struct {
	Repository string
	Role       string
	Runtime    string
	Provider   string
	Model      string
}

type RoutingMetrics struct {
	Attempts            int64
	Successes           int64
	Failures            int64
	AverageRuntimeMS    int64
	AverageCostMicroUSD int64
}

type RoutingMetricsReader interface {
	RuntimeRoutingMetrics(ctx context.Context, request RoutingMetricsRequest) (RoutingMetrics, error)
}

type RoutingDecision struct {
	PolicyVersion       string `json:"policy_version"`
	Score               int64  `json:"score"`
	OriginalOrder       int    `json:"original_order"`
	TaskComplexity      int64  `json:"task_complexity"`
	CapabilityScore     int64  `json:"capability_score"`
	Attempts            int64  `json:"attempts"`
	Successes           int64  `json:"successes"`
	Failures            int64  `json:"failures"`
	AverageRuntimeMS    int64  `json:"average_runtime_ms"`
	AverageCostMicroUSD int64  `json:"average_cost_microusd"`
}

type rankedCandidate struct {
	Candidate CandidateConfig
	Decision  RoutingDecision
}

func rankRoleCandidates(ctx context.Context, reader RoutingMetricsReader, request Request, role RoleConfig, runtimes map[string]RuntimeConfig) ([]rankedCandidate, error) {
	ranked := make([]rankedCandidate, 0, len(role.Chain))
	complexity := taskComplexity(request.Metadata)
	for index, candidate := range role.Chain {
		runtimeCfg := runtimes[candidate.Runtime]
		model := strings.TrimSpace(candidate.Model)
		if model == "" {
			model = strings.TrimSpace(runtimeCfg.Model)
		}
		metrics := RoutingMetrics{}
		if role.DynamicRouting && reader != nil {
			var err error
			metrics, err = reader.RuntimeRoutingMetrics(ctx, RoutingMetricsRequest{
				Repository: strings.TrimSpace(request.Repository),
				Role:       strings.TrimSpace(request.Role),
				Runtime:    strings.TrimSpace(candidate.Runtime),
				Provider:   string(runtimeCfg.Kind),
				Model:      model,
			})
			if err != nil {
				return nil, err
			}
		}
		decision := routingDecision(index, complexity, runtimeCfg.RoutingCapabilityScore, metrics)
		ranked = append(ranked, rankedCandidate{Candidate: candidate, Decision: decision})
	}
	if !role.DynamicRouting {
		return ranked, nil
	}
	// Stable insertion sort keeps configuration order as the deterministic tie-break.
	for index := 1; index < len(ranked); index++ {
		for cursor := index; cursor > 0 && ranked[cursor].Decision.Score > ranked[cursor-1].Decision.Score; cursor-- {
			ranked[cursor], ranked[cursor-1] = ranked[cursor-1], ranked[cursor]
		}
	}
	return ranked, nil
}

func routingDecision(originalOrder int, complexity, capability int64, metrics RoutingMetrics) RoutingDecision {
	if capability < 0 {
		capability = 0
	}
	if capability > 100 {
		capability = 100
	}
	// Operator configuration supplies capability; durable history supplies quality,
	// latency and cost. Chain order remains a small deterministic tie-break only.
	score := capability*complexity*10_000 + int64(10_000-originalOrder)
	if metrics.Attempts > 0 {
		successBasisPoints := metrics.Successes * 10_000 / metrics.Attempts
		score += successBasisPoints * 100
		score -= metrics.Failures * 1_000
	}
	score -= metrics.AverageRuntimeMS / 100
	score -= metrics.AverageCostMicroUSD / 1_000
	return RoutingDecision{
		PolicyVersion:       routingPolicyVersion,
		Score:               score,
		OriginalOrder:       originalOrder,
		TaskComplexity:      complexity,
		CapabilityScore:     capability,
		Attempts:            metrics.Attempts,
		Successes:           metrics.Successes,
		Failures:            metrics.Failures,
		AverageRuntimeMS:    metrics.AverageRuntimeMS,
		AverageCostMicroUSD: metrics.AverageCostMicroUSD,
	}
}

func taskComplexity(metadata map[string]string) int64 {
	if metadata == nil {
		return 3
	}
	value, err := strconv.ParseInt(strings.TrimSpace(metadata["task_complexity"]), 10, 64)
	if err != nil || value < 1 || value > 5 {
		return 3
	}
	return value
}
