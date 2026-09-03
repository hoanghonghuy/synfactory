package verifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	factoryruntime "github.com/hoanghonghuy/synfactory/internal/runtime"
	"github.com/hoanghonghuy/synfactory/internal/workspace"
)

type Check struct {
	Name     string        `json:"name"`
	Command  string        `json:"command"`
	Args     []string      `json:"args,omitempty"`
	Timeout  time.Duration `json:"-"`
	Required bool          `json:"required"`
}

type Plan struct {
	Checks []Check
}

type CheckResult struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Args        []string `json:"args,omitempty"`
	ExitCode    int `json:"exit_code"`
	Passed      bool `json:"passed"`
	Required    bool `json:"required"`
	Stdout      string `json:"stdout,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
}

type Report struct {
	Passed   bool          `json:"passed"`
	Revision string        `json:"revision"`
	Checks   []CheckResult `json:"checks"`
	SHA256   string        `json:"sha256"`
}

type Verifier struct {
	Supervisor *factoryruntime.Supervisor
}

func (v Verifier) Verify(ctx context.Context, handle workspace.Handle, plan Plan) (Report, error) {
	supervisor := v.Supervisor
	if supervisor == nil {
		supervisor = factoryruntime.NewSupervisor()
	}
	report := Report{Passed: true, Revision: handle.Revision}
	for i, check := range plan.Checks {
		if strings.TrimSpace(check.Name) == "" || strings.TrimSpace(check.Command) == "" {
			return Report{}, errors.New("verification check name and command are required")
		}
		if check.Timeout <= 0 {
			check.Timeout = 15 * time.Minute
		}
		process, err := supervisor.Run(ctx, factoryruntime.CommandSpec{
			ExecutionID: fmt.Sprintf("verify-%s-%d", handle.ID, i+1),
			Name: check.Command, Args: check.Args, Dir: handle.Path,
			Timeout: check.Timeout, Sandbox: handle.Sandbox,
		})
		result := CheckResult{Name: check.Name, Command: check.Command, Args: append([]string(nil), check.Args...), ExitCode: process.ExitCode, Passed: err == nil && process.ExitCode == 0, Required: check.Required, Stdout: process.Stdout, Stderr: process.Stderr, StartedAt: process.StartedAt, FinishedAt: process.FinishedAt}
		report.Checks = append(report.Checks, result)
		if check.Required && !result.Passed {
			report.Passed = false
		}
	}
	encoded, err := json.Marshal(struct {
		Passed bool `json:"passed"`
		Revision string `json:"revision"`
		Checks []CheckResult `json:"checks"`
	}{report.Passed, report.Revision, report.Checks})
	if err != nil {
		return Report{}, err
	}
	sum := sha256.Sum256(encoded)
	report.SHA256 = hex.EncodeToString(sum[:])
	if !report.Passed {
		return report, fmt.Errorf("deterministic verification failed")
	}
	return report, nil
}
