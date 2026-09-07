package secrets

import (
	"context"
	"testing"
)

func TestFallbackProviderSnapshotsLegacyThroughRotationBoundary(t *testing.T) {
	primary := rotationTestProvider{values: map[string]Value{}}
	provider := NewRotatingProvider(FallbackProvider{
		Primary: primary,
		Values:  map[string]string{"operator/token": "legacy-token"},
	})

	before, err := provider.Resolve(context.Background(), "operator/token")
	if err != nil {
		t.Fatal(err)
	}
	primary.values["operator/token"] = newValue([]byte("new-primary-token"), "primary")
	afterPrimaryChange, err := provider.Resolve(context.Background(), "operator/token")
	if err != nil {
		t.Fatal(err)
	}
	if string(before.CloneBytes()) != "legacy-token" || string(afterPrimaryChange.CloneBytes()) != "legacy-token" {
		t.Fatal("primary source change must not bypass explicit rotation")
	}
}
