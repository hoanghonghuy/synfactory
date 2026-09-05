package attention

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SlackWebhookProvider delivers secret-safe attention notifications through a
// Slack incoming webhook without making Slack a workflow source of truth.
type SlackWebhookProvider struct {
	URL     string
	Client  *http.Client
	Timeout time.Duration
}

func (SlackWebhookProvider) Name() string { return "slack" }

func (p SlackWebhookProvider) Deliver(ctx context.Context, notification Notification) error {
	if err := ValidateNotification(notification); err != nil {
		return err
	}
	if strings.TrimSpace(p.URL) == "" {
		return fmt.Errorf("slack webhook URL is required")
	}

	text := fmt.Sprintf("[%s] %s\n%s", strings.ToUpper(string(notification.Severity)), notification.Title, notification.Summary)
	if repositoryID := strings.TrimSpace(notification.RepositoryID); repositoryID != "" {
		text += "\nRepository: " + repositoryID
	}
	payload, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: text})
	if err != nil {
		return fmt.Errorf("encode slack notification: %w", err)
	}

	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, p.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build slack webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "synfactory-notification/1")

	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("deliver slack notification: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}
