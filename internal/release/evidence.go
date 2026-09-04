package release

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Evidence struct {
	SourceSHA string            `json:"source_sha"`
	Gates     map[string]string `json:"gates"`
	Images    map[string]struct {
		LocalID string `json:"local_id"`
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

func ParseEvidence(raw []byte, expectedSourceSHA string) (Evidence, error) {
	var evidence Evidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		return Evidence{}, fmt.Errorf("%w: decode release evidence: %v", ErrInvalidRelease, err)
	}
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
	for _, gate := range requiredGates {
		if e.Gates[gate] != "passed" {
			return fmt.Errorf("%w: required security gate %s is not passed", ErrInvalidRelease, gate)
		}
	}
	for _, name := range requiredImages {
		image, ok := e.Images[name]
		if !ok || strings.TrimSpace(image.LocalID) == "" {
			return fmt.Errorf("%w: evidence missing image identity for %s", ErrInvalidRelease, name)
		}
		sbom, ok := e.SBOMs[name]
		if !ok || strings.TrimSpace(sbom.Path) == "" || !isHexSHA(sbom.SHA256, 64) {
			return fmt.Errorf("%w: evidence missing valid SBOM linkage for %s", ErrInvalidRelease, name)
		}
	}
	return nil
}
