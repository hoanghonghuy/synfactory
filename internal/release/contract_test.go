package release

import (
	"errors"
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		Version:   "v1.2.3",
		SourceSHA: strings.Repeat("a", 40),
		Images: []Image{
			{Name: "control", Repository: "registry.example/synfactory/control", Digest: "sha256:" + strings.Repeat("1", 64), SBOMSHA256: strings.Repeat("a", 64)},
			{Name: "worker", Repository: "registry.example/synfactory/worker", Digest: "sha256:" + strings.Repeat("2", 64), SBOMSHA256: strings.Repeat("b", 64)},
			{Name: "web", Repository: "registry.example/synfactory/web", Digest: "sha256:" + strings.Repeat("3", 64), SBOMSHA256: strings.Repeat("c", 64)},
		},
	}
}

func TestManifestValidateAndReleaseIDAreOrderIndependent(t *testing.T) {
	manifest := validManifest()
	id1, err := manifest.ReleaseID()
	if err != nil {
		t.Fatal(err)
	}
	manifest.Images[0], manifest.Images[2] = manifest.Images[2], manifest.Images[0]
	id2, err := manifest.ReleaseID()
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("release IDs differ after image reorder: %s != %s", id1, id2)
	}
}

func TestManifestRejectsMutableOrIncompleteImageIdentity(t *testing.T) {
	manifest := validManifest()
	manifest.Images[0].Digest = "latest"
	if !errors.Is(manifest.Validate(), ErrInvalidRelease) {
		t.Fatal("mutable tag must be rejected")
	}

	manifest = validManifest()
	manifest.Images = manifest.Images[:2]
	if !errors.Is(manifest.Validate(), ErrInvalidRelease) {
		t.Fatal("incomplete image set must be rejected")
	}
}

func TestEnsureIdempotentAllowsExactReplayAndRejectsVersionRebind(t *testing.T) {
	existing := validManifest()
	candidate := validManifest()
	if err := EnsureIdempotent(existing, candidate); err != nil {
		t.Fatalf("exact replay should be idempotent: %v", err)
	}

	candidate.Images[0].Digest = "sha256:" + strings.Repeat("f", 64)
	if !errors.Is(EnsureIdempotent(existing, candidate), ErrIdentityConflict) {
		t.Fatal("same version with different immutable content must conflict")
	}
}

func TestPromotionUsesOnlyDigestPinnedImages(t *testing.T) {
	promotion, err := NewPromotion("production", validManifest())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePromotion(promotion); err != nil {
		t.Fatalf("valid promotion rejected: %v", err)
	}
	for name, ref := range promotion.Images {
		if !strings.Contains(ref, "@sha256:") {
			t.Fatalf("%s promotion is not digest pinned: %s", name, ref)
		}
	}

	promotion.Images["web"] = "registry.example/synfactory/web:latest"
	if !errors.Is(ValidatePromotion(promotion), ErrInvalidRelease) {
		t.Fatal("mutable promotion ref must be rejected")
	}
}

func TestRollbackIsPromotionToKnownManifestDigest(t *testing.T) {
	knownGood := validManifest()
	knownGood.Version = "v1.2.2"
	rollback, err := NewPromotion("production", knownGood)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePromotion(rollback); err != nil {
		t.Fatal(err)
	}
	if rollback.ReleaseID == "" || rollback.Images["control"] == "" {
		t.Fatal("rollback must retain known immutable release identity")
	}
}
