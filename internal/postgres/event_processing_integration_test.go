package postgres

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestConcurrentEventProcessorsClaimOnce(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()

	_, inserted, err := store.PutEvent(ctx, InboxEvent{
		DedupeKey:    "event-claim-once",
		Provider:     "github",
		RepositoryID: repo.ID,
		Kind:         "github.pr.changed",
		Subject:      "55",
		Revision:     "abc123",
	})
	if err != nil || !inserted {
		t.Fatalf("seed event: inserted=%v err=%v", inserted, err)
	}

	const workers = 8
	var wg sync.WaitGroup
	results := make(chan bool, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, claimed, err := store.ClaimEvent(ctx, "router-"+string(rune('a'+index)), now, time.Minute)
			results <- claimed
			errs <- err
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	claims := 0
	for claimed := range results {
		if claimed {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("expected exactly one event claim, got %d", claims)
	}
}

func TestExpiredEventLeaseCanBeReclaimed(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()

	_, _, err := store.PutEvent(ctx, InboxEvent{
		DedupeKey:    "event-reclaim",
		Provider:     "github",
		RepositoryID: repo.ID,
		Kind:         "github.issue.changed",
		Subject:      "7",
		Revision:     "rev-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	first, claimed, err := store.ClaimEvent(ctx, "router-1", now, time.Second)
	if err != nil || !claimed {
		t.Fatalf("first claim: claimed=%v err=%v", claimed, err)
	}
	if first.ProcessAttempt != 1 {
		t.Fatalf("expected first processing attempt, got %d", first.ProcessAttempt)
	}

	second, claimed, err := store.ClaimEvent(ctx, "router-2", now.Add(2*time.Second), time.Minute)
	if err != nil || !claimed {
		t.Fatalf("reclaim: claimed=%v err=%v", claimed, err)
	}
	if second.ProcessAttempt != 2 || second.ProcessingOwner != "router-2" {
		t.Fatalf("unexpected reclaimed event: %+v", second)
	}
}

func TestRetryAndCompleteEventRequireLeaseOwner(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()

	_, _, err := store.PutEvent(ctx, InboxEvent{
		DedupeKey:    "event-owner",
		Provider:     "github",
		RepositoryID: repo.ID,
		Kind:         "github.pr.changed",
		Subject:      "55",
		Revision:     "rev-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	event, claimed, err := store.ClaimEvent(ctx, "router-1", now, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim: claimed=%v err=%v", claimed, err)
	}

	if err := store.CompleteEvent(ctx, event.ID, "router-2", now.Add(time.Second)); err != ErrLeaseLost {
		t.Fatalf("expected ErrLeaseLost for wrong owner, got %v", err)
	}
	if err := store.RetryEvent(ctx, event.ID, "router-1", now.Add(time.Second), now.Add(time.Minute), "temporary"); err != nil {
		t.Fatal(err)
	}

	reclaimed, claimed, err := store.ClaimEvent(ctx, "router-2", now.Add(2*time.Minute), time.Minute)
	if err != nil || !claimed {
		t.Fatalf("retry claim: claimed=%v err=%v", claimed, err)
	}
	if err := store.CompleteEvent(ctx, reclaimed.ID, "router-2", now.Add(2*time.Minute+time.Second)); err != nil {
		t.Fatal(err)
	}
}
