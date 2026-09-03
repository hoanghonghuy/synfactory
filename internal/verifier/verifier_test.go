package verifier

import (
	"context"
	"testing"

	factoryruntime "github.com/hoanghonghuy/synfactory/internal/runtime"
	"github.com/hoanghonghuy/synfactory/internal/workspace"
)

func TestVerifierUsesProcessExitStatusAndHashesEvidence(t *testing.T) {
	v := Verifier{Supervisor: factoryruntime.NewSupervisor()}
	h := workspace.Handle{ID: "w1", Path: t.TempDir(), Revision: "abc", Sandbox: factoryruntime.SandboxSpec{Mode: factoryruntime.SandboxHost}}
	report, err := v.Verify(context.Background(), h, Plan{Checks: []Check{
		{Name: "pass", Command: "sh", Args: []string{"-c", "printf ok"}, Required: true},
		{Name: "optional-fail", Command: "sh", Args: []string{"-c", "exit 2"}, Required: false},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.SHA256 == "" || len(report.Checks) != 2 || !report.Checks[0].Passed || report.Checks[1].Passed {
		t.Fatalf("unexpected report %+v", report)
	}
}

func TestVerifierRequiredFailureFailsReport(t *testing.T) {
	v := Verifier{Supervisor: factoryruntime.NewSupervisor()}
	h := workspace.Handle{ID: "w2", Path: t.TempDir(), Revision: "abc", Sandbox: factoryruntime.SandboxSpec{Mode: factoryruntime.SandboxHost}}
	report, err := v.Verify(context.Background(), h, Plan{Checks: []Check{{Name: "fail", Command: "sh", Args: []string{"-c", "exit 3"}, Required: true}}})
	if err == nil || report.Passed {
		t.Fatalf("expected required verification failure")
	}
}
