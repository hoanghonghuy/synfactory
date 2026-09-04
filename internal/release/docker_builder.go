package release

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type buildSpec struct {
	name    string
	target  string
	context string
	tag     string
}

var releaseBuildSpecs = []buildSpec{
	{name: "control", target: "control", context: ".", tag: "synfactory-control:release-candidate"},
	{name: "worker", target: "worker", context: ".", tag: "synfactory-worker:release-candidate"},
	{name: "web", context: "./web", tag: "synfactory-web:release-candidate"},
}

func BuildAndVerifyEvidenceImages(ctx context.Context, evidence Evidence, runner CommandRunner) error {
	if err := evidence.Validate(evidence.SourceSHA); err != nil {
		return err
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	for _, spec := range releaseBuildSpecs {
		args := []string{"build"}
		if spec.target != "" {
			args = append(args, "--target", spec.target)
		}
		args = append(args, "-t", spec.tag, spec.context)
		output, err := runner.CombinedOutput(ctx, "docker", args...)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return fmt.Errorf("%w: build %s image failed: %v: %s", ErrInvalidRelease, spec.name, err, strings.TrimSpace(string(output)))
		}
		output, err = runner.CombinedOutput(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", spec.tag)
		if err != nil {
			return fmt.Errorf("%w: inspect %s image failed: %v", ErrInvalidRelease, spec.name, err)
		}
		builtID := strings.TrimSpace(string(output))
		expectedID := evidence.Images[spec.name].LocalID
		if builtID != expectedID {
			return fmt.Errorf("%w: rebuilt %s image identity %s does not match gated evidence %s", ErrInvalidRelease, spec.name, builtID, expectedID)
		}
	}
	return nil
}
