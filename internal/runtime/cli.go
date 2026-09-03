package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type commandBuilder func(request Request, sessionID string) CommandSpec
type outputParser func(process ProcessResult, runtimeName, model string) (Result, error)

type CLIAdapter struct {
	name       string
	binary     string
	probeArgs  []string
	probeLimit time.Duration
	supervisor *Supervisor
	build      commandBuilder
	parse      outputParser
}

func newCLIAdapter(name, binary string, supervisor *Supervisor, probeLimit time.Duration, probeArgs []string, build commandBuilder, parse outputParser) *CLIAdapter {
	if supervisor == nil { supervisor = NewSupervisor() }
	if probeLimit <= 0 { probeLimit = 5 * time.Second }
	return &CLIAdapter{name: name, binary: binary, probeArgs: append([]string(nil), probeArgs...), probeLimit: probeLimit, supervisor: supervisor, build: build, parse: parse}
}
func (a *CLIAdapter) Name() string { return a.name }
func (a *CLIAdapter) Probe(ctx context.Context) error {
	if _, err := exec.LookPath(a.binary); err != nil { return Failure(FailureUnavailable, fmt.Errorf("%w: %s: %v", ErrRuntimeUnavailable, a.binary, err)) }
	if len(a.probeArgs) == 0 { return nil }
	probeCtx, cancel := context.WithTimeout(ctx, a.probeLimit); defer cancel()
	cmd := exec.CommandContext(probeCtx, a.binary, a.probeArgs...)
	if err := cmd.Run(); err != nil {
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) { return Failure(FailureTransient, fmt.Errorf("probe %s timed out", a.name)) }
		return Failure(FailureUnavailable, fmt.Errorf("probe %s: %w", a.name, err))
	}
	return nil
}
func (a *CLIAdapter) Run(ctx context.Context, request Request) (Result, error) { return a.execute(ctx, "", request) }
func (a *CLIAdapter) Resume(ctx context.Context, sessionID string, request Request) (Result, error) {
	if sessionID == "" { return Result{}, Failure(FailurePermanent, errors.New("session id is required for resume")) }
	return a.execute(ctx, sessionID, request)
}
func (a *CLIAdapter) Cancel(_ context.Context, runID string) error { return a.supervisor.Cancel(a.executionID(runID)) }
func (a *CLIAdapter) execute(ctx context.Context, sessionID string, request Request) (Result, error) {
	if request.RunID == "" { return Result{}, Failure(FailurePermanent, errors.New("run id is required")) }
	spec := a.build(request, sessionID)
	spec.ExecutionID = a.executionID(request.RunID)
	spec.Sandbox = request.Sandbox
	process, err := a.supervisor.Run(ctx, spec)
	result, parseErr := a.parse(process, a.name, request.Model)
	if result.Runtime == "" { result.Runtime = a.name }
	if result.Model == "" { result.Model = request.Model }
	if err != nil {
		result.Outcome = outcomeForFailure(ClassifyFailure(err))
		if parseErr != nil { return result, errors.Join(err, parseErr) }
		return result, err
	}
	if parseErr != nil { result.Outcome = OutcomeFailed; return result, Failure(FailurePermanent, parseErr) }
	if result.Outcome == "" { result.Outcome = OutcomeSucceeded }
	return result, nil
}
func (a *CLIAdapter) executionID(runID string) string { return runID + ":" + a.name }
func outcomeForFailure(class FailureClass) Outcome {
	switch class { case FailureUnavailable: return OutcomeUnavailable; case FailureTimeout: return OutcomeTimedOut; case FailureCanceled: return OutcomeCanceled; default: return OutcomeFailed }
}
func parseGenericJSON(process ProcessResult, runtimeName, model string) (Result, error) {
	result := Result{Runtime: runtimeName, Model: model, ExitCode: process.ExitCode, Output: process.Stdout, Diagnostics: process.Stderr, StartedAt: process.StartedAt, FinishedAt: process.FinishedAt}
	objects, err := decodeJSONObjects(process.Stdout)
	if err != nil {
		if strings.TrimSpace(process.Stdout) != "" { result.Summary = strings.TrimSpace(process.Stdout); return result, nil }
		return result, err
	}
	for _, object := range objects {
		result.Events = append(result.Events, normalizeJSONEvent(object))
		if session := findString(object, "conversation_id", "session_id", "thread_id", "sessionID", "id"); session != "" { result.SessionID = session }
		if summary := findString(object, "response", "result", "text", "output_text"); summary != "" { result.Summary = summary }
		if item, ok := object["item"].(map[string]any); ok { if summary := findString(item, "text", "content", "message"); summary != "" { result.Summary = summary } }
	}
	if result.Summary == "" { result.Summary = strings.TrimSpace(process.Stdout) }
	return result, nil
}
func decodeJSONObjects(output string) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" { return nil, errors.New("runtime returned empty output") }
	var single map[string]any
	if json.Unmarshal([]byte(trimmed), &single) == nil { return []map[string]any{single}, nil }
	var objects []map[string]any
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line); if line == "" { continue }
		var object map[string]any
		if err := json.Unmarshal([]byte(line), &object); err != nil { return nil, fmt.Errorf("decode runtime JSON line: %w", err) }
		objects = append(objects, object)
	}
	if len(objects) == 0 { return nil, errors.New("runtime returned no JSON events") }
	return objects, nil
}
func normalizeJSONEvent(object map[string]any) Event {
	kind := findString(object, "event", "type", "kind"); if kind == "" { kind = "runtime_event" }
	message := findString(object, "message", "text", "response", "result")
	return Event{Kind: kind, Message: message, Data: object}
}
func findString(value any, keys ...string) string {
	keySet := make(map[string]bool, len(keys)); for _, key := range keys { keySet[key] = true }
	var walk func(any) string
	walk = func(current any) string {
		switch typed := current.(type) {
		case map[string]any:
			for _, key := range keys { if raw, ok := typed[key]; ok { if v, ok := raw.(string); ok && strings.TrimSpace(v) != "" { return v } } }
			for key, raw := range typed { if keySet[key] { continue }; if found := walk(raw); found != "" { return found } }
		case []any:
			for i := len(typed)-1; i >= 0; i-- { if found := walk(typed[i]); found != "" { return found } }
		}
		return ""
	}
	return walk(value)
}
