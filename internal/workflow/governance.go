package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	factoryruntime "github.com/hoanghonghuy/synfactory/internal/runtime"
)

type RuntimeExecutor interface {
	Execute(ctx context.Context, request factoryruntime.Request, observer factoryruntime.Observer) (factoryruntime.Result, []factoryruntime.Attempt, error)
}

type TaskProposal struct {
	Title      string `json:"title"`
	Capability string `json:"capability"`
	Scope      string `json:"scope"`
	Body       string `json:"body"`
	Ready      bool   `json:"ready"`
}

type Handoff struct {
	Action   ActionKind
	Decision string
	Tasks    []TaskProposal
	Summary  string
}

type HandoffSink interface {
	Handle(ctx context.Context, request factoryruntime.Request, handoff Handoff) error
}

type GovernanceEngine struct {
	Next RuntimeExecutor
	Sink HandoffSink
}

func (g GovernanceEngine) Execute(ctx context.Context, request factoryruntime.Request, observer factoryruntime.Observer) (factoryruntime.Result, []factoryruntime.Attempt, error) {
	if g.Next == nil { return factoryruntime.Result{}, nil, errors.New("runtime executor is required") }
	result, attempts, err := g.Next.Execute(ctx, request, observer)
	if err != nil || result.Outcome != factoryruntime.OutcomeSucceeded { return result, attempts, err }
	action := ActionKind(request.Metadata["workflow_action"])
	if !requiresDecision(action) { return result, attempts, nil }
	combined := result.Summary + "\n" + result.Output
	decision, ok := parseDecision(combined)
	if !ok { return result, attempts, factoryruntime.Failure(factoryruntime.FailurePermanent, fmt.Errorf("%s did not emit a valid SYNFACTORY_DECISION handoff", action)) }
	if err := validateDecision(action, decision); err != nil { return result, attempts, factoryruntime.Failure(factoryruntime.FailurePermanent, err) }
	tasks, err := parseTaskProposals(combined)
	if err != nil { return result, attempts, factoryruntime.Failure(factoryruntime.FailurePermanent, err) }
	if action != ActionBacklogRefill && len(tasks) > 0 { return result, attempts, factoryruntime.Failure(factoryruntime.FailurePermanent, fmt.Errorf("%s may not create backlog task proposals", action)) }
	handoff := Handoff{Action:action, Decision:decision, Tasks:tasks, Summary:result.Summary}
	if g.Sink != nil {
		if err := g.Sink.Handle(ctx, request, handoff); err != nil { return result, attempts, factoryruntime.Failure(factoryruntime.FailurePermanent, fmt.Errorf("apply governance handoff: %w", err)) }
	}
	result.Events = append(result.Events, factoryruntime.Event{Kind:"synfactory_handoff", Data:map[string]any{"action":string(action),"decision":decision,"task_count":len(tasks)}})
	return result, attempts, nil
}

func requiresDecision(action ActionKind) bool {
	switch action { case ActionPMTriage,ActionReview,ActionMergeGate,ActionEscalateBlocker,ActionBacklogRefill: return true; default: return false }
}
func validateDecision(action ActionKind, decision string) error {
	switch action {
	case ActionMergeGate: if decision!="APPROVE" { return fmt.Errorf("merge gate returned %s",decision) }
	case ActionReview: if decision!="APPROVE" && decision!="REQUEST_CHANGES" { return fmt.Errorf("review returned invalid decision %s",decision) }
	case ActionPMTriage,ActionEscalateBlocker,ActionBacklogRefill: if decision!="DONE" && decision!="BLOCKED" { return fmt.Errorf("%s returned invalid decision %s",action,decision) }
	}
	return nil
}
func parseDecision(output string) (string,bool) {
	const marker="SYNFACTORY_DECISION:"
	lines:=strings.Split(output,"\n")
	for i:=len(lines)-1;i>=0;i-- { line:=strings.TrimSpace(lines[i]); if !strings.HasPrefix(line,marker){continue}; d:=strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(line,marker))); switch d{case "APPROVE","REQUEST_CHANGES","DONE","BLOCKED": return d,true; default:return d,false} }
	return "",false
}
func parseTaskProposals(output string) ([]TaskProposal,error) {
	const marker="SYNFACTORY_TASK:"
	var tasks []TaskProposal
	for _,raw:=range strings.Split(output,"\n") { line:=strings.TrimSpace(raw); if !strings.HasPrefix(line,marker){continue}; if len(tasks)>=10{return nil,errors.New("at most 10 backlog task proposals are allowed per run")}; var p TaskProposal; if err:=json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line,marker))),&p);err!=nil{return nil,fmt.Errorf("decode SYNFACTORY_TASK proposal: %w",err)}; p.Title=strings.TrimSpace(p.Title);p.Capability=strings.TrimSpace(p.Capability);p.Scope=strings.TrimSpace(p.Scope);p.Body=strings.TrimSpace(p.Body); if p.Title==""||p.Capability==""||p.Scope==""||p.Body==""{return nil,errors.New("task proposal requires title, capability, scope and body")}; if len(p.Title)>200||len(p.Body)>20000{return nil,errors.New("task proposal exceeds size limits")};tasks=append(tasks,p) }
	return tasks,nil
}
