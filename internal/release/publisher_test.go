package release

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRegistry struct {
	calls int
	errs  []error
}

func (f *fakeRegistry) Push(context.Context, string, string, string) (string, error) {
	f.calls++
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return "", err
		}
	}
	return "sha256:" + strings.Repeat("9", 64), nil
}

func validPublishInput() PublishInput {
	evidence := validEvidence()
	input := PublishInput{Version: "v2.0.0", SourceSHA: evidence.SourceSHA, Evidence: evidence}
	input.Images = make(map[string]struct {
		Repository  string
		SourceImage string
		SBOMSHA256  string
	})
	for _, name := range requiredImages {
		input.Images[name] = struct {
			Repository  string
			SourceImage string
			SBOMSHA256  string
		}{
			Repository:  "registry.example/synfactory/" + name,
			SourceImage: "synfactory-" + name + ":ci",
			SBOMSHA256:  strings.Repeat("a", 64),
		}
	}
	return input
}

func TestPublisherCapturesRegistryDigests(t *testing.T) {
	registry := &fakeRegistry{}
	manifest, err := (Publisher{Registry: registry, Attempts: 2, Backoff: time.Millisecond}).Publish(context.Background(), validPublishInput())
	if err != nil {
		t.Fatal(err)
	}
	if registry.calls != 3 || len(manifest.Images) != 3 {
		t.Fatalf("calls=%d images=%d", registry.calls, len(manifest.Images))
	}
	for _, image := range manifest.Images {
		if !digestPattern.MatchString(image.Digest) {
			t.Fatalf("non-immutable digest for %s: %s", image.Name, image.Digest)
		}
	}
}

func TestPublisherRetriesOnlyTransientFailures(t *testing.T) {
	registry := &fakeRegistry{errs: []error{ErrRegistryTransient, nil}}
	_, err := (Publisher{Registry: registry, Attempts: 3, Backoff: time.Millisecond}).Publish(context.Background(), validPublishInput())
	if err != nil {
		t.Fatal(err)
	}
	if registry.calls != 4 {
		t.Fatalf("calls=%d want 4 (two for control, one worker, one web)", registry.calls)
	}
}

func TestPublisherDoesNotRetryAuthFailure(t *testing.T) {
	registry := &fakeRegistry{errs: []error{ErrRegistryAuth}}
	_, err := (Publisher{Registry: registry, Attempts: 3, Backoff: time.Millisecond}).Publish(context.Background(), validPublishInput())
	if !errors.Is(err, ErrRegistryAuth) {
		t.Fatalf("expected auth error, got %v", err)
	}
	if registry.calls != 1 {
		t.Fatalf("auth error retried %d times", registry.calls)
	}
}

func TestPublisherRefusesUngatedSourceBeforeRegistryCall(t *testing.T) {
	registry := &fakeRegistry{}
	input := validPublishInput()
	input.Evidence.Gates["control_image"] = "failed"
	_, err := (Publisher{Registry: registry}).Publish(context.Background(), input)
	if !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("expected invalid release, got %v", err)
	}
	if registry.calls != 0 {
		t.Fatalf("registry was called for ungated source: %d", registry.calls)
	}
}
