package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	factoryruntime "github.com/hoanghonghuy/synfactory/internal/runtime"
)

func TestCIFailureStopsAtRepairBudget(t *testing.T){instance:=NewInstance("repo",KindIssue,"42","head",100);instance.CIRepairAttempts=instance.CIRepairLimit;facts:=Facts{IssueOpen:true,IssueReady:true,HasImplementationPR:true,PullRequestNumber:7,HeadSHA:"head",Review:ReviewApproved,ReviewedHeadSHA:"head",CI:CIFailing,DependenciesSatisfied:true};d:=(Policy{}).Decide(instance,facts);if d.TargetState!=StateBlocked||d.BlockedReason!="ci_repair_budget_exhausted"||d.Action==nil||d.Action.Kind!=ActionEscalateBlocker{t.Fatalf("unexpected decision %+v",d)}}
func TestMergeRequiresExactHeadGate(t *testing.T){instance:=NewInstance("repo",KindIssue,"42","head-2",100);facts:=Facts{IssueOpen:true,IssueReady:true,HasImplementationPR:true,PullRequestNumber:7,HeadSHA:"head-2",Review:ReviewApproved,ReviewedHeadSHA:"head-2",CI:CIPassing,DependenciesSatisfied:true};d:=(Policy{}).Decide(instance,facts);if d.Action==nil||d.Action.Kind!=ActionMergeGate{t.Fatalf("expected merge gate %+v",d)};facts.TeamLeadGatePassed=true;d=(Policy{}).Decide(instance,facts);if d.Action==nil||d.Action.Kind!=ActionMergePullRequest||d.Action.Metadata["head_sha"]!="head-2"{t.Fatalf("expected pinned merge %+v",d)}}
func TestRoleWIPDoesNotGloballyBlockReviewer(t *testing.T){dev:=Candidate{Instance:Instance{Priority:200},Decision:Decision{Action:&Action{Mode:ActionJob,Role:domain.RoleDev}}};reviewer:=Candidate{Instance:Instance{Priority:100},Decision:Decision{Action:&Action{Mode:ActionJob,Role:domain.RoleReviewer}}};selected:=SelectRunnable([]Candidate{dev,reviewer},map[domain.Role]int{domain.RoleDev:1},WIPLimits{domain.RoleDev:1,domain.RoleReviewer:1});if len(selected)!=1||selected[0].Decision.Action.Role!=domain.RoleReviewer{t.Fatalf("reviewer starved %+v",selected)}}

type fakeRuntimeExecutor struct{result factoryruntime.Result;err error}
func (f fakeRuntimeExecutor) Execute(context.Context,factoryruntime.Request,factoryruntime.Observer)(factoryruntime.Result,[]factoryruntime.Attempt,error){return f.result,nil,f.err}
func TestMergeGateRequiresStructuredApproval(t *testing.T){engine:=GovernanceEngine{Next:fakeRuntimeExecutor{result:factoryruntime.Result{Outcome:factoryruntime.OutcomeSucceeded,Summary:"looks fine"}}};_,_,err:=engine.Execute(context.Background(),factoryruntime.Request{Metadata:map[string]string{"workflow_action":string(ActionMergeGate)}},nil);if err==nil{t.Fatal("merge gate without handoff must fail")}}
func TestGovernancePreservesRuntimeFailure(t *testing.T){sentinel:=errors.New("runtime failed");engine:=GovernanceEngine{Next:fakeRuntimeExecutor{err:sentinel}};_,_,err:=engine.Execute(context.Background(),factoryruntime.Request{Metadata:map[string]string{"workflow_action":string(ActionMergeGate)}},nil);if !errors.Is(err,sentinel){t.Fatalf("got %v",err)}}
