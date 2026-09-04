package release

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var pushedDigestPattern = regexp.MustCompile(`(?m)digest:\s*(sha256:[0-9a-f]{64})`)

type CommandRunner interface {
	CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type DockerRegistry struct {
	Runner CommandRunner
}

func (r DockerRegistry) Push(ctx context.Context, repository, tag, sourceImage string) (string, error) {
	repository = strings.TrimSpace(repository)
	tag = strings.TrimSpace(tag)
	sourceImage = strings.TrimSpace(sourceImage)
	if repository == "" || tag == "" || sourceImage == "" {
		return "", fmt.Errorf("%w: repository, tag and source image are required", ErrRegistryPolicy)
	}
	runner := r.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	ref := repository + ":" + tag
	if output, err := runner.CombinedOutput(ctx, "docker", "image", "tag", sourceImage, ref); err != nil {
		return "", classifyDockerRegistryError("tag image", output, err)
	}
	output, err := runner.CombinedOutput(ctx, "docker", "push", ref)
	if err != nil {
		return "", classifyDockerRegistryError("push image", output, err)
	}
	match := pushedDigestPattern.FindSubmatch(output)
	if len(match) != 2 {
		return "", fmt.Errorf("%w: docker push did not return an immutable digest", ErrRegistryPolicy)
	}
	return string(match[1]), nil
}

func classifyDockerRegistryError(operation string, output []byte, cause error) error {
	text := strings.ToLower(string(output))
	switch {
	case strings.Contains(text, "unauthorized"),
		strings.Contains(text, "authentication required"),
		strings.Contains(text, "requested access to the resource is denied"),
		strings.Contains(text, "denied: requested access"):
		return fmt.Errorf("%s: %w: %v", operation, ErrRegistryAuth, cause)
	case strings.Contains(text, "timeout"),
		strings.Contains(text, "connection refused"),
		strings.Contains(text, "connection reset"),
		strings.Contains(text, "network is unreachable"),
		strings.Contains(text, "no such host"),
		strings.Contains(text, "temporary failure"),
		strings.Contains(text, "unexpected eof"),
		strings.Contains(text, "tls handshake timeout"),
		strings.Contains(text, "too many requests"),
		strings.Contains(text, "toomanyrequests"),
		strings.Contains(text, "status: 429"),
		strings.Contains(text, "status: 500"),
		strings.Contains(text, "status: 502"),
		strings.Contains(text, "status: 503"),
		strings.Contains(text, "status: 504"):
		return fmt.Errorf("%s: %w: %v", operation, ErrRegistryTransient, cause)
	default:
		if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
			return cause
		}
		return fmt.Errorf("%s: %w: %v", operation, ErrRegistryPolicy, cause)
	}
}

func PublishInputFromEvidence(version, sourceSHA, registryPrefix string, evidence Evidence) (PublishInput, error) {
	registryPrefix = strings.TrimSuffix(strings.TrimSpace(registryPrefix), "/")
	if registryPrefix == "" {
		return PublishInput{}, fmt.Errorf("%w: registry prefix is required", ErrInvalidRelease)
	}
	if err := evidence.Validate(sourceSHA); err != nil {
		return PublishInput{}, err
	}
	input := PublishInput{Version: version, SourceSHA: sourceSHA, Evidence: evidence}
	input.Images = make(map[string]struct {
		Repository  string
		SourceImage string
		SBOMSHA256  string
	}, len(requiredImages))
	for _, name := range requiredImages {
		input.Images[name] = struct {
			Repository  string
			SourceImage string
			SBOMSHA256  string
		}{
			Repository:  registryPrefix + "/" + name,
			SourceImage: evidence.Images[name].LocalID,
			SBOMSHA256:  evidence.SBOMs[name].SHA256,
		}
	}
	return input, nil
}
