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

type WebhookProvider struct {
	ProviderName string
	URL          string
	Client       *http.Client
	Timeout      time.Duration
}

func (p WebhookProvider) Name() string {
	if name := strings.TrimSpace(p.ProviderName); name != "" {
		return name
	}
	return "webhook"
}

func (p WebhookProvider) Deliver(ctx context.Context, notification Notification) error {
	if err := ValidateNotification(notification); err != nil {
		return err
	}
	if strings.TrimSpace(p.URL) == "" {
		return fmt.Errorf("webhook URL is required")
	}
	payload, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("encode webhook notification: %w", err)
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, p.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "synfactory-notification/1")
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("deliver webhook notification: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}
