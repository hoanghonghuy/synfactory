package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyCheckoutSourceMatchesExactCleanHead(t *testing.T) {
	sha := strings.Repeat("a", 40)
	runner := &fakeCommandRunner{
		outputs: [][]byte{[]byte(sha + "\n"), nil},
		errs:    []error{nil, nil},
	}
	if err := VerifyCheckoutSource(context.Background(), sha, runner); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 || strings.Join(runner.calls[0], " ") != "git rev-parse HEAD" || strings.Join(runner.calls[1], " ") != "git diff --name-only HEAD --" {
		t.Fatalf("calls=%v", runner.calls)
	}
}

func TestVerifyCheckoutSourceRejectsDifferentHead(t *testing.T) {
	runner := &fakeCommandRunner{outputs: [][]byte{[]byte(strings.Repeat("b", 40) + "\n")}, errs: []error{nil}}
	err := VerifyCheckoutSource(context.Background(), strings.Repeat("a", 40), runner)
	if !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("error=%v", err)
	}
}

func TestVerifyCheckoutSourceRejectsTrackedModifications(t *testing.T) {
	sha := strings.Repeat("a", 40)
	runner := &fakeCommandRunner{
		outputs: [][]byte{[]byte(sha + "\n"), []byte("cmd/synfactory-release/main.go\n")},
		errs:    []error{nil, nil},
	}
	err := VerifyCheckoutSource(context.Background(), sha, runner)
	if !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("error=%v", err)
	}
}

func TestLoadAndVerifyEvidenceImagesMatchesGatedArchives(t *testing.T) {
	evidence := validEvidence()
	dir := t.TempDir()
	runner := &fakeCommandRunner{}
	for _, name := range requiredImages {
		archivePath := filepath.Join(dir, filepath.FromSlash(evidence.Images[name].ArchivePath))
		if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
			t.Fatal(err)
		}
		content := []byte("exact-scanned-image-" + name)
		if err := os.WriteFile(archivePath, content, 0o644); err != nil {
			t.Fatal(err)
		}
		digest, err := fileSHA256(archivePath)
		if err != nil {
			t.Fatal(err)
		}
		image := evidence.Images[name]
		image.ArchiveSHA256 = digest
		evidence.Images[name] = image
		runner.outputs = append(runner.outputs, []byte("Loaded image\n"), []byte(image.LocalID+"\n"))
		runner.errs = append(runner.errs, nil, nil)
	}
	if err := LoadAndVerifyEvidenceImages(context.Background(), evidence, dir, runner); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 6 {
		t.Fatalf("calls=%d want 6", len(runner.calls))
	}
}

func TestLoadAndVerifyEvidenceImagesRejectsArchiveTampering(t *testing.T) {
	evidence := validEvidence()
	dir := t.TempDir()
	archivePath := filepath.Join(dir, filepath.FromSlash(evidence.Images["control"].ArchivePath))
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeCommandRunner{}
	err := LoadAndVerifyEvidenceImages(context.Background(), evidence, dir, runner)
	if !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("error=%v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("docker called before archive integrity check: calls=%d", len(runner.calls))
	}
}

func TestLoadAndVerifyEvidenceImagesRejectsIdentityDrift(t *testing.T) {
	evidence := validEvidence()
	dir := t.TempDir()
	for _, name := range requiredImages {
		archivePath := filepath.Join(dir, filepath.FromSlash(evidence.Images[name].ArchivePath))
		if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(archivePath, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		digest, err := fileSHA256(archivePath)
		if err != nil {
			t.Fatal(err)
		}
		image := evidence.Images[name]
		image.ArchiveSHA256 = digest
		evidence.Images[name] = image
	}
	runner := &fakeCommandRunner{
		outputs: [][]byte{[]byte("Loaded image\n"), []byte("sha256:" + strings.Repeat("9", 64) + "\n")},
		errs:    []error{nil, nil},
	}
	err := LoadAndVerifyEvidenceImages(context.Background(), evidence, dir, runner)
	if !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("error=%v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("loader continued after identity mismatch: calls=%d", len(runner.calls))
	}
}
