package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAIResponsesAdapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"content":[{"type":"output_text","text":"done"}]}]}`))
	}))
	defer server.Close()
	adapter, err := NewOpenAIAdapter("router", RuntimeConfig{Kind: ProviderOpenAI, BaseURL: server.URL + "/v1", Model: "model-x"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Run(context.Background(), Request{RunID: "r1", Prompt: "hello", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID != "resp_1" || result.Summary != "done" || result.Outcome != OutcomeSucceeded {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOpenAIChatCompletionAndSecretRedaction(t *testing.T) {
	t.Setenv("TEST_API_KEY", "top-secret-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer top-secret-key" {
			t.Fatalf("unexpected auth %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chat_1","choices":[{"message":{"content":"hello top-secret-key"}}],"echo":"top-secret-key"}`))
	}))
	defer server.Close()
	adapter, err := NewOpenAIAdapter("router", RuntimeConfig{Kind: ProviderOpenAI, BaseURL: server.URL + "/v1", APIStyle: "chat_completions", Model: "m", APIKeyEnv: "TEST_API_KEY"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Run(context.Background(), Request{RunID: "r2", Prompt: "hello", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "hello [REDACTED]" || strings.Contains(result.Output, "top-secret-key") || strings.Contains(result.Summary, "top-secret-key") {
		t.Fatalf("unexpected result: %+v", result)
	}
	encoded := fmt.Sprint(result.Events)
	if strings.Contains(encoded, "top-secret-key") {
		t.Fatalf("secret leaked in normalized events: %s", encoded)
	}
}

func TestOpenAIRateLimitIsTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"slow down"}`))
	}))
	defer server.Close()
	adapter, err := NewOpenAIAdapter("router", RuntimeConfig{Kind: ProviderOpenAI, BaseURL: server.URL + "/v1", Model: "m"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Run(context.Background(), Request{RunID: "r3", Prompt: "x", Timeout: time.Second})
	if ClassifyFailure(err) != FailureTransient {
		t.Fatalf("expected transient rate limit, got %v (%v)", ClassifyFailure(err), err)
	}
}
