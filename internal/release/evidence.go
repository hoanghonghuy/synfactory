package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type Evidence struct {
	SourceSHA      string            `json:"source_sha"`
	WebLockSHA256  string            `json:"web_lock_sha256,omitempty"`
	Scanners       map[string]string `json:"scanners"`
	Gates          map[string]string `json:"gates"`
	ManifestSHA256 string            `json:"-"`
	Images         map[string]struct {
		LocalID       string `json:"local_id"`
		ArchivePath   string `json:"archive_path"`
		ArchiveSHA256 string `json:"archive_sha256"`
	} `json:"images"`
	SBOMs map[string]struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"sboms"`
}

var requiredGates = []string{
	"go_vulnerability",
	"frontend_dependency",
	"control_image",
	"worker_image",
	"web_image",
}

var requiredScanners = []string{"govulncheck", "trivy"}

func ParseEvidence(raw []byte, expectedSourceSHA string) (Evidence, error) {
	var evidence Evidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		return Evidence{}, fmt.Errorf("%w: decode release evidence: %v", ErrInvalidRelease, err)
	}
	sum := sha256.Sum256(raw)
	evidence.ManifestSHA256 = hex.EncodeToString(sum[:])
	if err := evidence.Validate(expectedSourceSHA); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

func (e Evidence) Validate(expectedSourceSHA string) error {
	if !isHexSHA(e.SourceSHA, 40) {
		return fmt.Errorf("%w: evidence source_sha must be a 40-character git SHA", ErrInvalidRelease)
	}
	if expectedSourceSHA != "" && e.SourceSHA != expectedSourceSHA {
		return fmt.Errorf("%w: evidence source_sha does not match selected release source", ErrInvalidRelease)
	}
	if e.ManifestSHA256 != "" && !isHexSHA(e.ManifestSHA256, 64) {
		return fmt.Errorf("%w: evidence manifest fingerprint must be a 64-character sha256", ErrInvalidRelease)
	}
	if e.WebLockSHA256 != "" && !isHexSHA(e.WebLockSHA256, 64) {
		return fmt.Errorf("%w: web lock fingerprint must be a 64-character sha256", ErrInvalidRelease)
	}
	for _, scanner := range requiredScanners {
		if strings.TrimSpace(e.Scanners[scanner]) == "" {
			return fmt.Errorf("%w: required scanner metadata %s is missing", ErrInvalidRelease, scanner)
		}
	}
	for _, gate := range requiredGates {
		if e.Gates[gate] != "passed" {
			return fmt.Errorf("%w: required security gate %s is not passed", ErrInvalidRelease, gate)
		}
	}
	for _, name := range requiredImages {
		image, ok := e.Images[name]
		if !ok || !digestPattern.MatchString(image.LocalID) || strings.TrimSpace(image.ArchivePath) == "" || !isHexSHA(image.ArchiveSHA256, 64) {
			return fmt.Errorf("%w: evidence missing valid immutable image archive identity for %s", ErrInvalidRelease, name)
		}
		sbom, ok := e.SBOMs[name]
		if !ok || strings.TrimSpace(sbom.Path) == "" || !isHexSHA(sbom.SHA256, 64) {
			return fmt.Errorf("%w: evidence missing valid SBOM linkage for %s", ErrInvalidRelease, name)
		}
	}
	return nil
}
