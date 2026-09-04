package release

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func validEvidence() Evidence {
	var evidence Evidence
	evidence.SourceSHA = strings.Repeat("d", 40)
	evidence.Gates = map[string]string{
		"go_vulnerability":     "passed",
		"frontend_dependency": "passed",
		"control_image":       "passed",
		"worker_image":        "passed",
		"web_image":           "passed",
	}
	evidence.Images = make(map[string]struct{ LocalID string `json:"local_id"` })
	evidence.SBOMs = make(map[string]struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	})
	for _, name := range requiredImages {
		evidence.Images[name] = struct{ LocalID string `json:"local_id"` }{LocalID: "sha256:" + strings.Repeat(name[:1], 64)}
		evidence.SBOMs[name] = struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		}{Path: name + ".cdx.json", SHA256: strings.Repeat("a", 64)}
	}
	return evidence
}

func TestParseEvidenceAcceptsExactGatedSource(t *testing.T) {
	evidence := validEvidence()
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

func TestEvidenceRejectsBrokenSBOMLink(t *testing.T) {
	evidence := validEvidence()
	sbom := evidence.SBOMs["worker"]
	sbom.SHA256 = "not-a-sha"
	evidence.SBOMs["worker"] = sbom
	if !errors.Is(evidence.Validate(evidence.SourceSHA), ErrInvalidRelease) {
		t.Fatal("invalid SBOM linkage must reject release")
	}
}
