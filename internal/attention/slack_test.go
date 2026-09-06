package attention

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSlackWebhookProviderDeliversSafePayload(t *testing.T) {
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("content-type = %q, want application/json", contentType)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	provider := SlackWebhookProvider{URL: server.URL, Client: server.Client(), Timeout: time.Second}
	notification := Notification{
		AttentionID:  "attn-1",
		Severity:     SeverityCritical,
		Title:        "Release blocked",
		Summary:      "Required check failed",
		RepositoryID: "repo-1",
		Metadata: map[string]string{
			"credential": "must-not-be-forwarded",
		},
	}
	if err := provider.Deliver(context.Background(), notification); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	text := got["text"]
	for _, expected := range []string{"[CRITICAL] Release blocked", "Required check failed", "Repository: repo-1"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("payload %q missing %q", text, expected)
		}
	}
	if strings.Contains(text, "must-not-be-forwarded") || strings.Contains(text, "credential") {
		t.Fatalf("payload leaked metadata: %q", text)
	}
}

func TestSlackWebhookProviderFailsClosed(t *testing.T) {
	provider := SlackWebhookProvider{}
	if err := provider.Deliver(context.Background(), Notification{AttentionID: "attn-1", Title: "Blocked", Summary: "Needs operator"}); err == nil {
		t.Fatal("Deliver() error = nil, want missing URL error")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	provider.URL = server.URL
	provider.Client = server.Client()
	if err := provider.Deliver(context.Background(), Notification{AttentionID: "attn-1", Title: "Blocked", Summary: "Needs operator"}); err == nil {
		t.Fatal("Deliver() error = nil, want non-2xx error")
	}
}
