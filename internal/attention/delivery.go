package attention

import (
	"context"
	"fmt"
	"time"
)

type Notification struct {
	AttentionID  string            `json:"attention_id"`
	Severity     Severity          `json:"severity"`
	Title        string            `json:"title"`
	Summary      string            `json:"summary"`
	RepositoryID string            `json:"repository_id,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Provider interface {
	Name() string
	Deliver(ctx context.Context, notification Notification) error
}

type DeliveryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func (p DeliveryPolicy) Delay(attempt int) (time.Duration, bool) {
	maxAttempts := p.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if attempt < 1 || attempt >= maxAttempts {
		return 0, false
	}
	base := p.BaseDelay
	if base <= 0 {
		base = 30 * time.Second
	}
	maxDelay := p.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 10 * time.Minute
	}
	delay := base
	for i := 1; i < attempt && delay < maxDelay; i++ {
		if delay > maxDelay/2 {
			delay = maxDelay
			break
		}
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay, true
}

func ValidateNotification(notification Notification) error {
	if notification.AttentionID == "" || notification.Title == "" || notification.Summary == "" {
		return fmt.Errorf("attention id, title and summary are required")
	}
	return nil
}
