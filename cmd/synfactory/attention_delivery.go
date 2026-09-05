package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/attention"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

func configuredAttentionDelivery(store *postgres.Store) (namedComponent, bool) {
	webhookURL := strings.TrimSpace(os.Getenv("SYNFACTORY_SLACK_WEBHOOK_URL"))
	if webhookURL == "" {
		return namedComponent{}, false
	}

	dispatcher := attention.Dispatcher{
		Store: store,
		Router: attention.EscalationRouter{Rules: []attention.EscalationRule{{
			MinSeverity: attention.SeverityInfo,
			Providers:   []string{"slack"},
		}}},
	}
	executor := attention.Executor{
		Store:  store,
		Source: store,
		Providers: map[string]attention.Provider{
			"slack": attention.SlackWebhookProvider{URL: webhookURL},
		},
		Policy: attention.DeliveryPolicy{
			MaxAttempts: 4,
			BaseDelay:   30 * time.Second,
			MaxDelay:    10 * time.Minute,
		},
	}
	return namedComponent{
		name: "attention notification delivery",
		run: func(ctx context.Context) error {
			return runAttentionDelivery(ctx, dispatcher, executor, 10*time.Second, 20)
		},
	}, true
}

func runAttentionDelivery(ctx context.Context, dispatcher attention.Dispatcher, executor attention.Executor, interval time.Duration, batch int) error {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if batch <= 0 {
		batch = 20
	}
	for {
		created, dispatchErr := dispatcher.RunOnce(ctx)
		processed, deliveryErr := executor.RunOnce(ctx, batch)
		if dispatchErr != nil && !errors.Is(dispatchErr, context.Canceled) {
			slog.Warn("attention dispatch pass failed", "error", dispatchErr)
		}
		if deliveryErr != nil && !errors.Is(deliveryErr, context.Canceled) {
			// Notification/provider failures are transport health, never a reason to
			// stop workflow coordination or other software work.
			slog.Warn("attention delivery pass had failures", "error", deliveryErr)
		}
		if created > 0 || processed > 0 {
			slog.Info("attention notification pass", "created", created, "processed", processed)
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}
