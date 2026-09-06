package runtime

import (
	"context"
	"testing"
)

type routingMetricsReaderStub struct {
	metrics map[string]RoutingMetrics
}

func (s routingMetricsReaderStub) RuntimeRoutingMetrics(_ context.Context, request RoutingMetricsRequest) (RoutingMetrics, error) {
	return s.metrics[request.Runtime], nil
}

func TestRankRoleCandidatesUsesCapabilityAndDurableHistory(t *testing.T) {
	role := RoleConfig{
		DynamicRouting: true,
		Chain: []CandidateConfig{
			{Runtime: "cheap"},
			{Runtime: "strong"},
		},
	}
	runtimes := map[string]RuntimeConfig{
		"cheap":  {Kind: ProviderOpenAI, Model: "cheap-model", RoutingCapabilityScore: 30},
		"strong": {Kind: ProviderOpenAI, Model: "strong-model", RoutingCapabilityScore: 80},
	}
	reader := routingMetricsReaderStub{metrics: map[string]RoutingMetrics{
		"cheap":  {Attempts: 10, Successes: 6, Failures: 4, AverageRuntimeMS: 1_000, AverageCostMicroUSD: 100},
		"strong": {Attempts: 10, Successes: 9, Failures: 1, AverageRuntimeMS: 2_000, AverageCostMicroUSD: 2_000},
	}}

	ranked, err := rankRoleCandidates(context.Background(), reader, Request{
		Repository: "owner/repo",
		Role:       "developer",
		Metadata:   map[string]string{"task_complexity": "5"},
	}, role, runtimes)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 2 || ranked[0].Candidate.Runtime != "strong" {
		t.Fatalf("unexpected ranking: %+v", ranked)
	}
	if ranked[0].Decision.PolicyVersion != routingPolicyVersion || ranked[0].Decision.TaskComplexity != 5 {
		t.Fatalf("routing evidence missing policy/complexity: %+v", ranked[0].Decision)
	}
	if ranked[0].Decision.Attempts != 10 || ranked[0].Decision.Successes != 9 {
		t.Fatalf("routing evidence missing durable metrics: %+v", ranked[0].Decision)
	}
}

func TestRankRoleCandidatesPreservesConfiguredChainWhenDynamicRoutingDisabled(t *testing.T) {
	role := RoleConfig{Chain: []CandidateConfig{{Runtime: "first"}, {Runtime: "second"}}}
	runtimes := map[string]RuntimeConfig{
		"first":  {Kind: ProviderOpenAI, Model: "first", RoutingCapabilityScore: 1},
		"second": {Kind: ProviderOpenAI, Model: "second", RoutingCapabilityScore: 100},
	}
	ranked, err := rankRoleCandidates(context.Background(), routingMetricsReaderStub{}, Request{Role: "developer"}, role, runtimes)
	if err != nil {
		t.Fatal(err)
	}
	if ranked[0].Candidate.Runtime != "first" || ranked[1].Candidate.Runtime != "second" {
		t.Fatalf("disabled dynamic routing changed configured order: %+v", ranked)
	}
}

func TestTaskComplexityRejectsUntrustedOutOfRangeValues(t *testing.T) {
	if got := taskComplexity(map[string]string{"task_complexity": "99"}); got != 3 {
		t.Fatalf("taskComplexity() = %d, want safe default 3", got)
	}
	if got := taskComplexity(map[string]string{"task_complexity": "1"}); got != 1 {
		t.Fatalf("taskComplexity() = %d, want 1", got)
	}
}

func TestRoutingDecisionEventContainsReproducibleInputs(t *testing.T) {
	decision := routingDecision(1, 4, 70, RoutingMetrics{Attempts: 5, Successes: 4, Failures: 1, AverageRuntimeMS: 800, AverageCostMicroUSD: 900})
	event := routingDecisionEvent(decision)
	if event.Kind != "routing_decision" {
		t.Fatalf("event kind = %q", event.Kind)
	}
	if event.Data["policy_version"] != routingPolicyVersion || event.Data["score"] != decision.Score {
		t.Fatalf("routing evidence is incomplete: %+v", event.Data)
	}
}
