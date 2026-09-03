package github

import (
	"errors"
	"testing"
)

func TestNormalizePullRequestWebhook(t *testing.T) {
	body := []byte(`{
		"action":"synchronize",
		"repository":{"id":42,"full_name":"owner/repo","default_branch":"develop"},
		"pull_request":{"number":55,"updated_at":"2026-09-03T08:00:00Z","head":{"sha":"abc"}}
	}`)
	event, err := NormalizeWebhook("pull_request", "delivery-1", body)
	if err != nil {
		t.Fatal(err)
	}
	if event.RepositoryID != "github:42" || event.Kind != KindPRChanged || event.Subject != "55" {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.Revision != "2026-09-03T08:00:00Z" {
		t.Fatalf("unexpected revision: %s", event.Revision)
	}
}

func TestNormalizeUnsupportedWebhook(t *testing.T) {
	body := []byte(`{"repository":{"id":42,"full_name":"owner/repo"}}`)
	_, err := NormalizeWebhook("ping", "delivery-1", body)
	if !errors.Is(err, ErrUnsupportedEvent) {
		t.Fatalf("expected ErrUnsupportedEvent, got %v", err)
	}
}
