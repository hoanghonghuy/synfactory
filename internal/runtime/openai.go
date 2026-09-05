package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type OpenAIAdapter struct {
	name       string
	baseURL    string
	apiStyle   string
	apiKeyEnv  string
	model      string
	httpClient *http.Client
	redactor   Redactor
}

func NewOpenAIAdapter(name string, cfg RuntimeConfig, httpClient *http.Client) (*OpenAIAdapter, error) {
	parsed, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid OpenAI-compatible base URL %q", cfg.BaseURL)
	}
	style := strings.ToLower(strings.TrimSpace(cfg.APIStyle))
	if style == "" {
		style = "responses"
	}
	if style != "responses" && style != "chat_completions" {
		return nil, fmt.Errorf("unsupported OpenAI-compatible API style %q", cfg.APIStyle)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Minute}
	}
	return &OpenAIAdapter{
		name: name, baseURL: strings.TrimRight(cfg.BaseURL, "/"), apiStyle: style,
		apiKeyEnv: cfg.APIKeyEnv, model: cfg.Model, httpClient: httpClient,
		redactor: NewRedactor(cfg.secretValues()...),
	}, nil
}

func (a *OpenAIAdapter) Name() string { return a.name }

func (a *OpenAIAdapter) Probe(context.Context) error {
	if a == nil || a.baseURL == "" {
		return Failure(FailureUnavailable, ErrRuntimeUnavailable)
	}
	if a.apiKeyEnv != "" && os.Getenv(a.apiKeyEnv) == "" {
		return Failure(FailureUnavailable, fmt.Errorf("%w: environment variable %s is empty", ErrRuntimeUnavailable, a.apiKeyEnv))
	}
	return nil
}

func (a *OpenAIAdapter) Run(ctx context.Context, request Request) (Result, error) {
	return a.execute(ctx, "", request)
}

func (a *OpenAIAdapter) Resume(ctx context.Context, sessionID string, request Request) (Result, error) {
	if a.apiStyle != "responses" {
		return Result{}, Failure(FailurePermanent, errors.New("session resume requires Responses API style"))
	}
	if sessionID == "" {
		return Result{}, Failure(FailurePermanent, errors.New("session id is required for resume"))
	}
	return a.execute(ctx, sessionID, request)
}

func (a *OpenAIAdapter) Cancel(context.Context, string) error { return nil }

func (a *OpenAIAdapter) execute(ctx context.Context, previousResponseID string, request Request) (Result, error) {
	model := request.Model
	if model == "" {
		model = a.model
	}
	if model == "" {
		return Result{}, Failure(FailurePermanent, errors.New("model is required"))
	}
	started := time.Now().UTC()
	payload := map[string]any{"model": model}
	path := "/responses"
	if a.apiStyle == "responses" {
		payload["input"] = request.Prompt
		if previousResponseID != "" {
			payload["previous_response_id"] = previousResponseID
		}
	} else {
		path = "/chat/completions"
		payload["messages"] = []map[string]string{{"role": "user", "content": request.Prompt}}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}

	runCtx := ctx
	cancel := func() {}
	if request.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, request.Timeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(runCtx, http.MethodPost, a.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := os.Getenv(a.apiKeyEnv); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		finished := time.Now().UTC()
		result := Result{Runtime: a.name, Model: model, ExitCode: -1, StartedAt: started, FinishedAt: finished, Usage: Usage{RequestCount: 1, RuntimeMS: finished.Sub(started).Milliseconds()}}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			result.Outcome = OutcomeTimedOut
			return result, Failure(FailureTimeout, ErrRunTimedOut)
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			result.Outcome = OutcomeCanceled
			return result, Failure(FailureCanceled, ErrRunCanceled)
		}
		result.Outcome = OutcomeFailed
		return result, Failure(FailureTransient, fmt.Errorf("OpenAI-compatible request: %w", err))
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, defaultOutputLimit+1))
	finished := time.Now().UTC()
	output := a.redactor.String(string(data))
	result := Result{
		Runtime: a.name, Model: model, ExitCode: 0, Output: output,
		StartedAt: started, FinishedAt: finished,
		Usage: Usage{RequestCount: 1, RuntimeMS: finished.Sub(started).Milliseconds()},
	}
	if readErr != nil {
		result.Outcome = OutcomeFailed
		return result, Failure(FailureTransient, fmt.Errorf("read OpenAI-compatible response: %w", readErr))
	}
	if len(data) > defaultOutputLimit {
		result.Outcome = OutcomeFailed
		return result, Failure(FailurePermanent, errors.New("OpenAI-compatible response exceeds output limit"))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.ExitCode = resp.StatusCode
		class := FailurePermanent
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			class = FailureUnavailable
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			class = FailureTransient
		}
		result.Outcome = outcomeForFailure(class)
		return result, Failure(class, fmt.Errorf("OpenAI-compatible API returned HTTP %d: %s", resp.StatusCode, compactDiagnostic(output)))
	}

	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		result.Outcome = OutcomeFailed
		return result, Failure(FailurePermanent, fmt.Errorf("decode OpenAI-compatible response: %w", err))
	}
	result.Events = []Event{{Kind: "api_response", Data: object}}
	result.SessionID = findString(object, "id")
	result.Usage = extractOpenAIUsage(object, result.Usage)
	if a.apiStyle == "chat_completions" {
		result.Summary = extractChatCompletion(object)
	} else {
		result.Summary = extractResponseText(object)
	}
	if result.Summary == "" {
		result.Summary = findString(object, "output_text", "text", "content")
	}
	result.Outcome = OutcomeSucceeded
	return result, nil
}

func extractOpenAIUsage(object map[string]any, base Usage) Usage {
	usage, ok := object["usage"].(map[string]any)
	if !ok {
		return base
	}
	base.InputTokens = firstInt64(usage, "input_tokens", "prompt_tokens")
	base.OutputTokens = firstInt64(usage, "output_tokens", "completion_tokens")
	return base
}

func firstInt64(object map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := object[key].(type) {
		case float64:
			if value >= 0 {
				return int64(value)
			}
		case int64:
			if value >= 0 {
				return value
			}
		case json.Number:
			parsed, err := value.Int64()
			if err == nil && parsed >= 0 {
				return parsed
			}
		}
	}
	return 0
}

func extractChatCompletion(object map[string]any) string {
	choices, ok := object["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	message, ok := choice["message"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := message["content"].(string)
	return content
}

func extractResponseText(object map[string]any) string {
	if direct, _ := object["output_text"].(string); direct != "" {
		return direct
	}
	output, ok := object["output"].([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, itemRaw := range output {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			continue
		}
		content, ok := item["content"].([]any)
		if !ok {
			continue
		}
		for _, partRaw := range content {
			part, ok := partRaw.(map[string]any)
			if !ok {
				continue
			}
			if text, _ := part["text"].(string); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func compactDiagnostic(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 1024 {
		return value[:1024] + "…"
	}
	return value
}
