package release

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,127}$`)
	digestPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

var (
	ErrInvalidRelease   = errors.New("invalid release contract")
	ErrIdentityConflict = errors.New("release identity conflict")
)

var requiredImages = []string{"control", "web", "worker"}

type Image struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Digest     string `json:"digest"`
	SBOMSHA256 string `json:"sbom_sha256"`
}

type Manifest struct {
	Version   string  `json:"version"`
	SourceSHA string  `json:"source_sha"`
	Images    []Image `json:"images"`
}

type Promotion struct {
	Environment string            `json:"environment"`
	ReleaseID   string            `json:"release_id"`
	Images      map[string]string `json:"images"`
}

func (m Manifest) Validate() error {
	if !versionPattern.MatchString(m.Version) {
		return fmt.Errorf("%w: version must be a safe immutable release label", ErrInvalidRelease)
	}
	if !isHexSHA(m.SourceSHA, 40) {
		return fmt.Errorf("%w: source_sha must be a 40-character git SHA", ErrInvalidRelease)
	}
	if len(m.Images) != len(requiredImages) {
		return fmt.Errorf("%w: release must contain control, worker and web images", ErrInvalidRelease)
	}
	seen := make(map[string]struct{}, len(m.Images))
	for _, image := range m.Images {
		if image.Name == "" || strings.TrimSpace(image.Repository) == "" {
			return fmt.Errorf("%w: image name and repository are required", ErrInvalidRelease)
		}
		if _, ok := seen[image.Name]; ok {
			return fmt.Errorf("%w: duplicate image %q", ErrInvalidRelease, image.Name)
		}
		seen[image.Name] = struct{}{}
		if !digestPattern.MatchString(image.Digest) {
			return fmt.Errorf("%w: image %s digest must be immutable sha256", ErrInvalidRelease, image.Name)
		}
		if !isHexSHA(image.SBOMSHA256, 64) {
			return fmt.Errorf("%w: image %s sbom_sha256 must be 64 lowercase hex characters", ErrInvalidRelease, image.Name)
		}
	}
	for _, name := range requiredImages {
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("%w: missing required image %q", ErrInvalidRelease, name)
		}
	}
	return nil
}

func (m Manifest) ReleaseID() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	images := append([]Image(nil), m.Images...)
	sort.Slice(images, func(i, j int) bool { return images[i].Name < images[j].Name })
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "version=%s\nsource=%s\n", m.Version, m.SourceSHA)
	for _, image := range images {
		_, _ = fmt.Fprintf(h, "%s=%s@%s#%s\n", image.Name, image.Repository, image.Digest, image.SBOMSHA256)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func EnsureIdempotent(existing, candidate Manifest) error {
	existingID, err := existing.ReleaseID()
	if err != nil {
		return err
	}
	candidateID, err := candidate.ReleaseID()
	if err != nil {
		return err
	}
	if existing.Version != candidate.Version {
		return fmt.Errorf("%w: versions differ", ErrIdentityConflict)
	}
	if existingID != candidateID {
		return fmt.Errorf("%w: version %q is already bound to different immutable content", ErrIdentityConflict, candidate.Version)
	}
	return nil
}

func NewPromotion(environment string, manifest Manifest) (Promotion, error) {
	environment = strings.TrimSpace(environment)
	if environment == "" {
		return Promotion{}, fmt.Errorf("%w: promotion environment is required", ErrInvalidRelease)
	}
	releaseID, err := manifest.ReleaseID()
	if err != nil {
		return Promotion{}, err
	}
	images := make(map[string]string, len(manifest.Images))
	for _, image := range manifest.Images {
		images[image.Name] = image.Repository + "@" + image.Digest
	}
	return Promotion{Environment: environment, ReleaseID: releaseID, Images: images}, nil
}

func ValidatePromotion(p Promotion) error {
	if strings.TrimSpace(p.Environment) == "" || !digestPattern.MatchString(p.ReleaseID) {
		return fmt.Errorf("%w: invalid promotion identity", ErrInvalidRelease)
	}
	if len(p.Images) != len(requiredImages) {
		return fmt.Errorf("%w: promotion must contain control, worker and web digests", ErrInvalidRelease)
	}
	for _, name := range requiredImages {
		ref := p.Images[name]
		parts := strings.Split(ref, "@")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || !digestPattern.MatchString(parts[1]) {
			return fmt.Errorf("%w: promotion image %s must use repository@sha256:digest", ErrInvalidRelease, name)
		}
	}
	return nil
}

func isHexSHA(value string, length int) bool {
	if len(value) != length || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
