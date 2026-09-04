package release

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeCommandRunner struct {
	calls   [][]string
	outputs [][]byte
	errs    []error
}

func (f *fakeCommandRunner) CombinedOutput(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	var output []byte
	if len(f.outputs) > 0 {
		output = f.outputs[0]
		f.outputs = f.outputs[1:]
	}
	var err error
	if len(f.errs) > 0 {
		err = f.errs[0]
		f.errs = f.errs[1:]
	}
	return output, err
}

func TestDockerRegistryPushCapturesDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	runner := &fakeCommandRunner{
		outputs: [][]byte{nil, []byte("latest: digest: " + digest + " size: 1234\n")},
		errs:    []error{nil, nil},
	}
	got, err := (DockerRegistry{Runner: runner}).Push(context.Background(), "registry.example/synfactory/control", "v1.2.3", "sha256:"+strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if got != digest {
		t.Fatalf("digest=%q want %q", got, digest)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls=%d", len(runner.calls))
	}
	if strings.Join(runner.calls[0], " ") != "docker image tag sha256:"+strings.Repeat("b", 64)+" registry.example/synfactory/control:v1.2.3" {
		t.Fatalf("tag call=%v", runner.calls[0])
	}
}

func TestDockerRegistryClassifiesAuthFailure(t *testing.T) {
	runner := &fakeCommandRunner{
		outputs: [][]byte{nil, []byte("unauthorized: authentication required")},
		errs:    []error{nil, errors.New("exit status 1")},
	}
	_, err := (DockerRegistry{Runner: runner}).Push(context.Background(), "registry.example/repo", "v1", "local:image")
	if !errors.Is(err, ErrRegistryAuth) {
		t.Fatalf("error=%v", err)
	}
}

func TestDockerRegistryClassifiesTransientFailure(t *testing.T) {
	runner := &fakeCommandRunner{
		outputs: [][]byte{nil, []byte("received unexpected HTTP status: 503 Service Unavailable")},
		errs:    []error{nil, errors.New("exit status 1")},
	}
	_, err := (DockerRegistry{Runner: runner}).Push(context.Background(), "registry.example/repo", "v1", "local:image")
	if !errors.Is(err, ErrRegistryTransient) {
		t.Fatalf("error=%v", err)
	}
}

func TestPublishInputFromEvidenceUsesRetainedImageAndSBOMIdentity(t *testing.T) {
	evidence := validEvidence()
	input, err := PublishInputFromEvidence("v1.0.0", evidence.SourceSHA, "registry.example/synfactory/", evidence)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range requiredImages {
		image := input.Images[name]
		if image.Repository != "registry.example/synfactory/"+name {
			t.Fatalf("repository[%s]=%q", name, image.Repository)
		}
		if image.SourceImage != evidence.Images[name].LocalID || image.SBOMSHA256 != evidence.SBOMs[name].SHA256 {
			t.Fatalf("evidence identity not preserved for %s", name)
		}
	}
}
