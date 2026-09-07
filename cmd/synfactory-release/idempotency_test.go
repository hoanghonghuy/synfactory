package main

import (
	"strings"
	"testing"

	releasefactory "github.com/hoanghonghuy/synfactory/internal/release"
)

func TestVerifyExistingReleaseRecognizesMatchingDurableManifest(t *testing.T) {
	input, manifest := matchingReleaseFixture()
	path := t.TempDir() + "/release.json"
	if err := writeJSON(path, manifest); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, recorded, err := verifyExistingRelease(path, input)
	if err != nil {
		t.Fatalf("verify existing release: %v", err)
	}
	if !recorded {
		t.Fatal("expected durable manifest to suppress a repeated registry publish")
	}
	if got.Version != manifest.Version || got.SourceSHA != manifest.SourceSHA || len(got.Images) != len(manifest.Images) {
		t.Fatalf("unexpected recovered manifest: %#v", got)
	}
}

func TestVerifyExistingReleaseRejectsConflictingIdentity(t *testing.T) {
	input, manifest := matchingReleaseFixture()
	path := t.TempDir() + "/release.json"
	if err := writeJSON(path, manifest); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	input.SourceSHA = strings.Repeat("f", 40)

	if _, recorded, err := verifyExistingRelease(path, input); err == nil || recorded {
		t.Fatalf("expected identity conflict, recorded=%v err=%v", recorded, err)
	}
}

func matchingReleaseFixture() (releasefactory.PublishInput, releasefactory.Manifest) {
	sourceSHA := strings.Repeat("a", 40)
	evidenceSHA := strings.Repeat("b", 64)
	webLockSHA := strings.Repeat("c", 64)
	scanners := map[string]string{"govulncheck": "v1", "trivy": "v2"}
	evidence := releasefactory.Evidence{
		SourceSHA: sourceSHA, ManifestSHA256: evidenceSHA, WebLockSHA256: webLockSHA, Scanners: scanners,
		SBOMs: map[string]struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		}{},
	}
	input := releasefactory.PublishInput{
		Version: "v1.2.3", SourceSHA: sourceSHA, Evidence: evidence,
		Images: map[string]struct {
			Repository  string
			SourceImage string
			SBOMSHA256  string
		}{},
	}
	manifest := releasefactory.Manifest{
		Version: "v1.2.3", SourceSHA: sourceSHA, EvidenceSHA256: evidenceSHA, WebLockSHA256: webLockSHA, Scanners: scanners,
	}
	for index, name := range []string{"control", "web", "worker"} {
		repository := "registry.example/synfactory/" + name
		sbomPath := "sbom/" + name + ".json"
		sbomSHA := strings.Repeat(string(rune('d'+index)), 64)
		digest := "sha256:" + strings.Repeat(string(rune('1'+index)), 64)
		input.Images[name] = struct {
			Repository  string
			SourceImage string
			SBOMSHA256  string
		}{Repository: repository, SourceImage: digest, SBOMSHA256: sbomSHA}
		evidence.SBOMs[name] = struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		}{Path: sbomPath, SHA256: sbomSHA}
		manifest.Images = append(manifest.Images, releasefactory.Image{
			Name: name, Repository: repository, Digest: digest, SBOMPath: sbomPath, SBOMSHA256: sbomSHA,
		})
	}
	input.Evidence = evidence
	return input, manifest
}
