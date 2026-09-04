package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

func TestRepositoryOnboardingIdempotencyVersioningAndDisableFiltering(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	base := Repository{
		ID:            "repo-onboarding",
		Provider:      "github",
		FullName:      "hoanghonghuy/onboarding-test",
		DefaultBranch: "main",
		Enabled:       true,
		Config:        json.RawMessage(`{"integration_branch":"develop"}`),
	}

	first, err := store.MutateRepository(ctx, base, "register", "integration-test")
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigVersion != 1 {
		t.Fatalf("first registration config version = %d, want 1", first.ConfigVersion)
	}

	retry := base
	retry.ID = "different-retry-id"
	second, err := store.MutateRepository(ctx, retry, "register", "integration-test")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.ConfigVersion != 1 {
		t.Fatalf("idempotent retry changed identity/version: first=%+v second=%+v", first, second)
	}

	audit, err := store.ListRepositoryConfigAudit(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "register" || audit[0].ConfigVersion != 1 {
		t.Fatalf("unexpected registration audit: %+v", audit)
	}

	updated := first
	updated.DefaultBranch = "develop"
	updated.Config = json.RawMessage(`{"integration_branch":"release"}`)
	updated, err = store.MutateRepository(ctx, updated, "update", "integration-test")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ConfigVersion != 2 {
		t.Fatalf("updated config version = %d, want 2", updated.ConfigVersion)
	}

	updated.Enabled = false
	disabled, err := store.MutateRepository(ctx, updated, "disable", "integration-test")
	if err != nil {
		t.Fatal(err)
	}
	if disabled.ConfigVersion != 3 || disabled.Enabled {
		t.Fatalf("unexpected disabled repository: %+v", disabled)
	}

	enabledOnly, err := store.ListRepositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, repo := range enabledOnly {
		if repo.ID == disabled.ID {
			t.Fatalf("disabled repository %s leaked into scheduler-visible repository list", disabled.ID)
		}
	}

	all, err := store.ListAllRepositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, repo := range all {
		if repo.ID == disabled.ID {
			found = true
			if repo.Enabled {
				t.Fatalf("disabled repository returned enabled in operator list: %+v", repo)
			}
		}
	}
	if !found {
		t.Fatalf("disabled repository %s missing from operator-visible repository list", disabled.ID)
	}
}

func TestConcurrentRepositoryRegistrationCreatesSingleVersionAndAudit(t *testing.T) {
	store := openTestStore(t)
	base := Repository{
		ID:            "repo-concurrent-onboarding",
		Provider:      "github",
		FullName:      "hoanghonghuy/concurrent-onboarding-test",
		DefaultBranch: "main",
		Enabled:       true,
		Config:        json.RawMessage(`{"integration_branch":"develop"}`),
	}

	const workers = 8
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	resultCh := make(chan Repository, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			candidate := base
			candidate.ID = fmt.Sprintf("retry-id-%d", i)
			result, err := store.MutateRepository(context.Background(), candidate, "register", "integration-test")
			if err != nil {
				errCh <- err
				return
			}
			resultCh <- result
		}(i)
	}
	wg.Wait()
	close(errCh)
	close(resultCh)

	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	var canonicalID string
	count := 0
	for result := range resultCh {
		count++
		if result.ConfigVersion != 1 {
			t.Fatalf("concurrent registration produced config version %d, want 1", result.ConfigVersion)
		}
		if canonicalID == "" {
			canonicalID = result.ID
		} else if result.ID != canonicalID {
			t.Fatalf("concurrent registration returned multiple identities: %s and %s", canonicalID, result.ID)
		}
	}
	if count != workers {
		t.Fatalf("got %d successful concurrent registrations, want %d", count, workers)
	}

	audit, err := store.ListRepositoryConfigAudit(context.Background(), canonicalID)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].ConfigVersion != 1 || audit[0].Action != "register" {
		t.Fatalf("concurrent registration created unexpected audit history: %+v", audit)
	}
}
