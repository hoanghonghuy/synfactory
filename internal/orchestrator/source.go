package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	githubfactory "github.com/hoanghonghuy/synfactory/internal/github"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type SourceStore interface{ ListRepositories(context.Context)([]postgres.Repository,error); ListActiveWorkflows(context.Context)([]workflow.Instance,error) }
type GitHubReader interface{ ListOpenIssues(context.Context,string,string)([]githubfactory.Issue,error); GetIssueDetails(context.Context,string,string,int64)(githubfactory.IssueDetails,error); ListOpenPullDetails(context.Context,string,string)([]githubfactory.PullRequestDetails,error); GetPullRequestDetails(context.Context,string,string,int64)(githubfactory.PullRequestDetails,error); ListReviewDetails(context.Context,string,string,int64)([]githubfactory.ReviewDetails,error); ListCheckRuns(context.Context,string,string,string)([]githubfactory.CheckRun,error) }
type GitHubSnapshotSource struct{ store SourceStore; github GitHubReader }
func NewGitHubSnapshotSource(store SourceStore,github GitHubReader)*GitHubSnapshotSource{return &GitHubSnapshotSource{store:store,github:github}}

func (s *GitHubSnapshotSource) Snapshots(ctx context.Context)([]workflow.Snapshot,error){
	if s==nil||s.store==nil||s.github==nil{return nil,fmt.Errorf("workflow snapshot store and github reader are required")}
	repositories,err:=s.store.ListRepositories(ctx);if err!=nil{return nil,err};active,err:=s.store.ListActiveWorkflows(ctx);if err!=nil{return nil,err};byRepo:=map[string][]workflow.Instance{};for _,item:=range active{if item.Kind==workflow.KindIssue{byRepo[item.RepositoryID]=append(byRepo[item.RepositoryID],item)}}
	var snapshots []workflow.Snapshot;var failures []error
	for _,repository:=range repositories{owner,repo,err:=splitRepository(repository.FullName);if err!=nil{failures=append(failures,err);continue};issues,err:=s.github.ListOpenIssues(ctx,owner,repo);if err!=nil{failures=append(failures,err);continue};pulls,err:=s.github.ListOpenPullDetails(ctx,owner,repo);if err!=nil{failures=append(failures,err);continue};seen:=map[string]bool{}
		for _,issue:=range issues{key:=strconv.FormatInt(issue.Number,10);seen[key]=true;instance:=findWorkflow(byRepo[repository.ID],key);if instance.ID==""{instance=workflow.NewInstance(repository.ID,workflow.KindIssue,key,issue.UpdatedAt,100)};snap,err:=s.issueSnapshot(ctx,repository,owner,repo,instance,issue.Number,pulls);if err!=nil{failures=append(failures,err);continue};snapshots=append(snapshots,snap)}
		for _,instance:=range byRepo[repository.ID]{if seen[instance.Subject]{continue};number,err:=strconv.ParseInt(instance.Subject,10,64);if err!=nil{failures=append(failures,err);continue};snap,err:=s.issueSnapshot(ctx,repository,owner,repo,instance,number,pulls);if err!=nil{failures=append(failures,err);continue};snapshots=append(snapshots,snap)}
	}
	return snapshots,joinErrors(failures)
}

func (s *GitHubSnapshotSource) issueSnapshot(ctx context.Context,repository postgres.Repository,owner,repo string,instance workflow.Instance,issueNumber int64,openPulls []githubfactory.PullRequestDetails)(workflow.Snapshot,error){issue,err:=s.github.GetIssueDetails(ctx,owner,repo,issueNumber);if err!=nil{return workflow.Snapshot{},err};facts:=workflow.Facts{IssueOpen:strings.EqualFold(issue.State,"open"),IssueReady:issueReady(issue.Labels),Review:workflow.ReviewPending,CI:workflow.CIUnknown};if blockedLabel(issue.Labels){facts.BlockingReason="github_label_blocked"};prNumber:=metadataInt64(instance.Metadata,"pull_request_number");var pull githubfactory.PullRequestDetails;if prNumber>0{pull,err=s.github.GetPullRequestDetails(ctx,owner,repo,prNumber);if err!=nil{return workflow.Snapshot{},err}}else if match,ok:=findLinkedPull(openPulls,issueNumber);ok{pull=match;prNumber=match.Number};metadata:=metadataMap(instance.Metadata);metadata["repository_full_name"]=repository.FullName;if prNumber>0{metadata["pull_request_number"]=prNumber};instance.Metadata,_=json.Marshal(metadata);if issue.UpdatedAt!=""&&instance.Revision==""{instance.Revision=issue.UpdatedAt};if prNumber>0{facts.HasImplementationPR=true;facts.PullRequestNumber=prNumber;facts.PRMerged=pull.Merged;facts.HeadSHA=pull.Head.SHA;instance.Revision=pull.Head.SHA;if pull.Head.SHA!=""{reviews,err:=s.github.ListReviewDetails(ctx,owner,repo,prNumber);if err!=nil{return workflow.Snapshot{},err};facts.Review,facts.ReviewedHeadSHA=reviewState(reviews,pull.Head.SHA);checks,err:=s.github.ListCheckRuns(ctx,owner,repo,pull.Head.SHA);if err!=nil{return workflow.Snapshot{},err};facts.CI=ciState(checks)}};return workflow.Snapshot{Instance:instance,Facts:facts},nil}
func findWorkflow(items []workflow.Instance,subject string)workflow.Instance{for _,item:=range items{if item.Subject==subject{return item}};return workflow.Instance{}}
func issueReady(labels []githubfactory.Label)bool{for _,label:=range labels{switch strings.ToLower(strings.TrimSpace(label.Name)){case "ready","ready-for-dev","status:ready","status/ready","in-progress":return true}};return false}
func blockedLabel(labels []githubfactory.Label)bool{for _,label:=range labels{name:=strings.ToLower(strings.TrimSpace(label.Name));if name=="blocked"||name=="status:blocked"||name=="status/blocked"{return true}};return false}
func findLinkedPull(pulls []githubfactory.PullRequestDetails,issueNumber int64)(githubfactory.PullRequestDetails,bool){for _,pull:=range pulls{if referencesIssue(pull.Body,issueNumber)||branchReferencesIssue(pull.Head.Ref,issueNumber){return pull,true}};return githubfactory.PullRequestDetails{},false}
func referencesIssue(body string,issueNumber int64)bool{pattern:=fmt.Sprintf(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s*#%d\b`,issueNumber);matched,_:=regexp.MatchString(pattern,body);return matched}
func branchReferencesIssue(branch string,issueNumber int64)bool{needle:=strconv.FormatInt(issueNumber,10);for _,token:=range regexp.MustCompile(`[^0-9]+`).Split(branch,-1){if token==needle{return true}};return false}
func reviewState(reviews []githubfactory.ReviewDetails,head string)(workflow.ReviewState,string){sort.SliceStable(reviews,func(i,j int)bool{return reviews[i].SubmittedAt<reviews[j].SubmittedAt});state:=workflow.ReviewPending;reviewed:="";for _,review:=range reviews{if review.CommitID!=""&&review.CommitID!=head{continue};switch strings.ToUpper(review.State){case "APPROVED":state,reviewed=workflow.ReviewApproved,head;case "CHANGES_REQUESTED":state,reviewed=workflow.ReviewChangesRequested,head;case "DISMISSED":state,reviewed=workflow.ReviewPending,""}};return state,reviewed}
func ciState(checks []githubfactory.CheckRun)workflow.CIState{if len(checks)==0{return workflow.CIUnknown};for _,check:=range checks{if !strings.EqualFold(check.Status,"completed"){return workflow.CIPending}};for _,check:=range checks{switch strings.ToLower(check.Conclusion){case "success","neutral","skipped":default:return workflow.CIFailing}};return workflow.CIPassing}
func metadataMap(raw json.RawMessage)map[string]any{result:=map[string]any{};_ = json.Unmarshal(raw,&result);return result}
func metadataInt64(raw json.RawMessage,key string)int64{values:=metadataMap(raw);switch value:=values[key].(type){case float64:return int64(value);case string:number,_:=strconv.ParseInt(value,10,64);return number};return 0}
func splitRepository(fullName string)(string,string,error){parts:=strings.Split(strings.TrimSpace(fullName),"/");if len(parts)!=2||parts[0]==""||parts[1]==""{return "","",fmt.Errorf("repository %q must be owner/name",fullName)};return parts[0],parts[1],nil}
func joinErrors(items []error)error{if len(items)==0{return nil};var parts []string;for _,err:=range items{parts=append(parts,err.Error())};return fmt.Errorf("%s",strings.Join(parts,"; "))}
