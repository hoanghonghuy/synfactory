package attention

import (
	"testing"
	"time"
)

func TestDeliveryPolicyBoundsRetriesAndBackoff(t *testing.T) {
	policy := DeliveryPolicy{MaxAttempts: 4, BaseDelay: time.Second, MaxDelay: 3 * time.Second}
	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}
	for attempt, expected := range want {
		delay, retry := policy.Delay(attempt + 1)
		if !retry || delay != expected {
			t.Fatalf("attempt %d: delay=%s retry=%v want delay=%s retry=true", attempt+1, delay, retry, expected)
		}
	}
	if delay, retry := policy.Delay(4); retry || delay != 0 {
		t.Fatalf("exhausted delivery retried: delay=%s retry=%v", delay, retry)
	}
}

func TestDeliveryPolicyRejectsInvalidAttempt(t *testing.T) {
	if _, retry := (DeliveryPolicy{}).Delay(0); retry {
		t.Fatal("attempt zero must not be retried")
	}
}

func TestValidateNotificationRequiresSecretSafeDisplayFields(t *testing.T) {
	valid := Notification{AttentionID: "attn-1", Severity: SeverityCritical, Title: "Release blocked", Summary: "Required check failed"}
	if err := ValidateNotification(valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidateNotification(Notification{AttentionID: "attn-1"}); err == nil {
		t.Fatal("notification without display-safe title/summary must be rejected")
	}
}
