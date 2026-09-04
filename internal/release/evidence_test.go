package release

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func validEvidence() Evidence {
	var evidence Evidence
	evidence.ReleaseReady = true
	evidence.SourceSHA = strings.Repeat("d", 40)
	evidence.WebLockSHA256 = strings.Repeat("e", 64)
	evidence.ManifestSHA256 = strings.Repeat("f", 64)
	evidence.Scanners = map[string]string{
		"govulncheck": "v1.7.0",
		"trivy":       "v0.70.0",
	}
	evidence.Gates = map[string]string{
		"go_vulnerability":    "passed",
		"frontend_dependency": "passed",
		"control_image":       "passed",
		"worker_image":        "passed",
		"web_image":           "passed",
	}
	evidence.Images = make(map[string]struct {
		LocalID       string `json:"local_id"`
		ArchivePath   string `json:"archive_path"`
		ArchiveSHA256 string `json:"archive_sha256"`
	})
	evidence.SBOMs = make(map[string]struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	})
	imageHex := map[string]string{"control": "c", "worker": "d", "web": "e"}
	for _, name := range requiredImages {
		evidence.Images[name] = struct {
			LocalID       string `json:"local_id"`
			ArchivePath   string `json:"archive_path"`
			ArchiveSHA256 string `json:"archive_sha256"`
		}{
			LocalID:       "sha256:" + strings.Repeat(imageHex[name], 64),
			ArchivePath:   "images/" + name + ".tar",
			ArchiveSHA256: strings.Repeat("b", 64),
		}
		evidence.SBOMs[name] = struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		}{Path: name + ".cdx.json", SHA256: strings.Repeat("a", 64)}
	}
	return evidence
}

func TestParseEvidenceAcceptsExactGatedSourceAndFingerprintsRawManifest(t *testing.T) {
	evidence := validEvidence()
	evidence.ManifestSHA256 = ""
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseEvidence(raw, evidence.SourceSHA)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SourceSHA != evidence.SourceSHA {
		t.Fatalf("source SHA = %s", parsed.SourceSHA)
	}
	if !isHexSHA(parsed.ManifestSHA256, 64) {
		t.Fatalf("evidence fingerprint = %q", parsed.ManifestSHA256)
	}
}

func TestEvidenceRejectsNonReleaseReadyPRArtifact(t *testing.T) {
	evidence := validEvidence()
	evidence.ReleaseReady = false
	if !errors.Is(evidence.Validate(evidence.SourceSHA), ErrInvalidRelease) {
		t.Fatal("PR-only evidence must not be publishable")
	}
}

func TestEvidenceRejectsSourceMismatch(t *testing.T) {
	evidence := validEvidence()
	if !errors.Is(evidence.Validate(strings.Repeat("e", 40)), ErrInvalidRelease) {
		t.Fatal("mismatched selected source must be rejected")
	}
}

func TestEvidenceRejectsMissingSecurityGate(t *testing.T) {
	evidence := validEvidence()
	evidence.Gates["web_image"] = "failed"
	if !errors.Is(evidence.Validate(evidence.SourceSHA), ErrInvalidRelease) {
		t.Fatal("failed security gate must reject release")
	}
}

func TestEvidenceRejectsMissingScannerMetadata(t *testing.T) {
	evidence := validEvidence()
	delete(evidence.Scanners, "trivy")
	if !errors.Is(evidence.Validate(evidence.SourceSHA), ErrInvalidRelease) {
		t.Fatal("missing scanner provenance must reject release")
	}
}

func TestEvidenceRejectsInvalidLocalImageIdentity(t *testing.T) {
	evidence := validEvidence()
	image := evidence.Images["web"]
	image.LocalID = "sha256:not-valid"
	evidence.Images["web"] = image
	if !errors.Is(evidence.Validate(evidence.SourceSHA), ErrInvalidRelease) {
		t.Fatal("invalid local image identity must reject release")
	}
}

func TestEvidenceRejectsUnsafeArchivePath(t *testing.T) {
	evidence := validEvidence()
	image := evidence.Images["control"]
	image.ArchivePath = "../control.tar"
	evidence.Images["control"] = image
	if !errors.Is(evidence.Validate(evidence.SourceSHA), ErrInvalidRelease) {
		t.Fatal("path traversal in image archive must reject release")
	}
}

func TestEvidenceRejectsBrokenSBOMLink(t *testing.T) {
	evidence := validEvidence()
	sbom := evidence.SBOMs["worker"]
	sbom.SHA256 = "not-a-sha"
	evidence.SBOMs["worker"] = sbom
	if !errors.Is(evidence.Validate(evidence.SourceSHA), ErrInvalidRelease) {
		t.Fatal("invalid SBOM linkage must reject release")
	}
}
