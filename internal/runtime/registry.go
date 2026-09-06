package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
	config   Config
	budget   BudgetGate
}

func BuildRegistry(cfg Config, supervisor *Supervisor, httpClient *http.Client) (*Registry, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	registry := &Registry{adapters: make(map[string]Adapter, len(cfg.Runtimes)), config: cfg}
	for name, runtimeCfg := range cfg.Runtimes {
		var adapter Adapter
		var err error
		if runtimeCfg.Kind == ProviderOpenAI {
			adapter, err = NewOpenAIAdapter(name, runtimeCfg, httpClient)
		} else {
			adapter, err = newPresetAdapter(name, runtimeCfg, supervisor)
		}
		if err != nil {
			return nil, fmt.Errorf("build runtime %q: %w", name, err)
		}
		registry.adapters[name] = adapter
	}
	return registry, nil
}

func (r *Registry) WithBudgetGate(gate BudgetGate) *Registry {
	if r == nil {
		return r
	}
	r.mu.Lock()
	r.budget = gate
	r.mu.Unlock()
	return r
}

func (r *Registry) Adapter(name string) (Adapter, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[name]
	return adapter, ok
}

func (r *Registry) budgetGate() BudgetGate {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.budget
}

func (r *Registry) Probe(ctx context.Context) map[string]error {
	result := map[string]error{}
	if r == nil {
		return result
	}
	r.mu.RLock()
	adapters := make(map[string]Adapter, len(r.adapters))
	for name, adapter := range r.adapters {
		adapters[name] = adapter
	}
	r.mu.RUnlock()
	for name, adapter := range adapters {
		if err := adapter.Probe(ctx); err != nil {
			result[name] = err
		}
	}
	return result
}

func (r *Registry) Execute(ctx context.Context, request Request, observer Observer) (Result, []Attempt, error) {
	if r == nil {
		return Result{}, nil, ErrNoRuntimeRoute
	}
	roleCfg, ok := r.config.Roles[request.Role]
	if !ok || len(roleCfg.Chain) == 0 {
		return Result{}, nil, fmt.Errorf("%w for role %q", ErrNoRuntimeRoute, request.Role)
	}
	fallbackOn := roleCfg.effectiveFallbackOn()
	attempts := make([]Attempt, 0, len(roleCfg.Chain))

	for index, candidate := range roleCfg.Chain {
		runtimeCfg := r.config.Runtimes[candidate.Runtime]
		provider := string(runtimeCfg.Kind)
		adapter, ok := r.Adapter(candidate.Runtime)
		if !ok {
			attempt := Attempt{Sequence: index + 1, Runtime: candidate.Runtime, Provider: provider, FailureClass: FailureUnavailable, Err: ErrRuntimeUnavailable}
			attempts = append(attempts, attempt)
			if !fallbackOn[FailureUnavailable] {
				return Result{}, attempts, attempt.Err
			}
			continue
		}

		model := candidate.Model
		if model == "" {
			model = runtimeCfg.Model
		}
		attemptRequest := request
		attemptRequest.Model = model
		attemptRequest.RunID = scopedRunID(request.RunID, index+1)

		budgetRelease := func() {}
		releaseBudget := func() {
			budgetRelease()
			budgetRelease = func() {}
		}
		if gate := r.budgetGate(); gate != nil {
			var decision BudgetDecision
			var err error
			if leaseGate, ok := gate.(BudgetLeaseGate); ok {
				decision, budgetRelease, err = leaseGate.Acquire(ctx, budgetRequest(request, candidate.Runtime, provider, model))
				if budgetRelease == nil {
					budgetRelease = func() {}
				}
			} else {
				decision, err = gate.Evaluate(ctx, budgetRequest(request, candidate.Runtime, provider, model))
			}
			if err != nil {
				releaseBudget()
				return Result{}, attempts, Failure(FailureBudget, errors.Join(ErrBudgetPolicyUnavailable, err))
			}
			decision = normalizeBudgetDecision(decision)
			switch decision.Outcome {
			case BudgetContinue:
			case BudgetFallback:
				releaseBudget()
				attempts = append(attempts, budgetAttempt(index+1, candidate.Runtime, provider, model, ErrBudgetExhausted, decision.Reason))
				continue
			case BudgetPark:
				releaseBudget()
				attempt := budgetAttempt(index+1, candidate.Runtime, provider, model, ErrBudgetExhausted, decision.Reason)
				return attempt.Result, append(attempts, attempt), attempt.Err
			case BudgetEscalate:
				releaseBudget()
				attempt := budgetAttempt(index+1, candidate.Runtime, provider, model, ErrBudgetApprovalRequired, decision.Reason)
				return attempt.Result, append(attempts, attempt), attempt.Err
			}
		}

		attempt := Attempt{Sequence: index + 1, Runtime: candidate.Runtime, Provider: provider, Model: model}
		if observer != nil {
			if err := observer.AttemptStarted(ctx, attempt); err != nil {
				releaseBudget()
				return Result{}, attempts, fmt.Errorf("observe runtime attempt start: %w", err)
			}
		}

		probeErr := adapter.Probe(ctx)
		if probeErr != nil {
			attempt.Err = probeErr
			attempt.FailureClass = ClassifyFailure(probeErr)
			attempt.Result = Result{Runtime: candidate.Runtime, Model: model, Outcome: outcomeForFailure(attempt.FailureClass), ExitCode: -1}
			if observer != nil {
				if err := observer.AttemptFinished(ctx, attempt); err != nil {
					releaseBudget()
					return attempt.Result, append(attempts, attempt), errors.Join(probeErr, err)
				}
			}
			releaseBudget()
			attempts = append(attempts, attempt)
			if fallbackOn[attempt.FailureClass] {
				continue
			}
			return attempt.Result, attempts, probeErr
		}

		var result Result
		var runErr error
		if sessionID := request.Metadata["resume_session_id"]; sessionID != "" {
			result, runErr = adapter.Resume(ctx, sessionID, attemptRequest)
		} else {
			result, runErr = adapter.Run(ctx, attemptRequest)
		}
		attempt.Result = result
		attempt.Err = runErr
		attempt.FailureClass = ClassifyFailure(runErr)
		if observer != nil {
			if err := observer.AttemptFinished(ctx, attempt); err != nil {
				releaseBudget()
				return result, append(attempts, attempt), errors.Join(runErr, err)
			}
		}
		releaseBudget()
		attempts = append(attempts, attempt)
		if runErr == nil && result.Outcome == OutcomeSucceeded {
			return result, attempts, nil
		}
		if !fallbackOn[attempt.FailureClass] {
			if runErr == nil {
				runErr = Failure(FailurePermanent, fmt.Errorf("runtime %s ended with outcome %s", candidate.Runtime, result.Outcome))
			}
			return result, attempts, runErr
		}
	}

	if len(attempts) == 0 {
		return Result{}, attempts, ErrNoRuntimeRoute
	}
	last := attempts[len(attempts)-1]
	if last.Err == nil {
		last.Err = ErrRuntimeUnavailable
	}
	return last.Result, attempts, last.Err
}

func (r *Registry) Cancel(ctx context.Context, runtimeName, runID string, sequence int) error {
	adapter, ok := r.Adapter(runtimeName)
	if !ok {
		return ErrRuntimeUnavailable
	}
	return adapter.Cancel(ctx, scopedRunID(runID, sequence))
}

func budgetAttempt(sequence int, runtimeName, provider, model string, base error, reason string) Attempt {
	err := base
	if strings.TrimSpace(reason) != "" {
		err = fmt.Errorf("%w: %s", base, strings.TrimSpace(reason))
	}
	return Attempt{
		Sequence:     sequence,
		Runtime:      runtimeName,
		Provider:     provider,
		Model:        model,
		FailureClass: FailureBudget,
		Result:       Result{Runtime: runtimeName, Model: model, Outcome: OutcomeUnavailable, ExitCode: -1},
		Err:          Failure(FailureBudget, err),
	}
}

func scopedRunID(runID string, sequence int) string {
	if strings.TrimSpace(runID) == "" {
		runID = "run"
	}
	return fmt.Sprintf("%s.%d", runID, sequence)
}
