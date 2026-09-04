package release

import (
	"context"
	"errors"
	"testing"
)

func TestBuildAndVerifyEvidenceImagesMatchesGatedIDs(t *testing.T) {
	evidence := validEvidence()
	runner := &fakeCommandRunner{}
	for _, spec := range releaseBuildSpecs {
		runner.outputs = append(runner.outputs, nil, []byte(evidence.Images[spec.name].LocalID+"\n"))
		runner.errs = append(runner.errs, nil, nil)
	}
	if err := BuildAndVerifyEvidenceImages(context.Background(), evidence, runner); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 6 {
		t.Fatalf("calls=%d want 6", len(runner.calls))
	}
}

func TestBuildAndVerifyEvidenceImagesRejectsIdentityDrift(t *testing.T) {
	evidence := validEvidence()
	runner := &fakeCommandRunner{
		outputs: [][]byte{nil, []byte("sha256:unexpected\n")},
		errs:    []error{nil, nil},
	}
	err := BuildAndVerifyEvidenceImages(context.Background(), evidence, runner)
	if !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("error=%v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("builder continued after identity mismatch: calls=%d", len(runner.calls))
	}
}
