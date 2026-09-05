package attention

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebhookProviderDeliversSafeNotification(t *testing.T) {
	var got Notification
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected webhook request: method=%s content-type=%s", r.Method, r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	notification := Notification{
		AttentionID: "att-1", Severity: SeverityCritical, Title: "Action required", Summary: "Repair budget exhausted",
		RepositoryID: "repo-1", Metadata: map[string]string{"kind": string(KindRepairExhausted)},
	}
	provider := WebhookProvider{URL: server.URL, Timeout: time.Second}
	if err := provider.Deliver(context.Background(), notification); err != nil {
		t.Fatal(err)
	}
	if got.AttentionID != notification.AttentionID || got.RepositoryID != notification.RepositoryID || got.Metadata["kind"] != string(KindRepairExhausted) {
		t.Fatalf("unexpected webhook payload: %+v", got)
	}
}

func TestWebhookProviderRejectsNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "downstream unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	provider := WebhookProvider{URL: server.URL}
	err := provider.Deliver(context.Background(), Notification{AttentionID: "att-1", Severity: SeverityWarning, Title: "Attention", Summary: "Provider unavailable"})
	if err == nil {
		t.Fatal("expected non-2xx webhook response to fail delivery")
	}
}
