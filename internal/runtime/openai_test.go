package runtime

import (
	"context"
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
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":123,"output_tokens":45}}`))
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
	if result.Usage.RequestCount != 1 || result.Usage.InputTokens != 123 || result.Usage.OutputTokens != 45 || result.Usage.RuntimeMS < 0 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
}

func TestOpenAIChatCompletionAndSecretRedaction(t *testing.T) {
	t.Setenv("TEST_API_KEY", "top-secret-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer top-secret-key" {
			t.Fatalf("unexpected auth %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chat_1","choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":9,"completion_tokens":4}}`))
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
	if result.Summary != "hello" || strings.Contains(result.Output, "top-secret-key") {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Usage.RequestCount != 1 || result.Usage.InputTokens != 9 || result.Usage.OutputTokens != 4 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
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
	result, err := adapter.Run(context.Background(), Request{RunID: "r3", Prompt: "x", Timeout: time.Second})
	if ClassifyFailure(err) != FailureTransient {
		t.Fatalf("expected transient rate limit, got %v (%v)", ClassifyFailure(err), err)
	}
	if result.Usage.RequestCount != 1 {
		t.Fatalf("expected failed request to remain attributable, got %+v", result.Usage)
	}
}
