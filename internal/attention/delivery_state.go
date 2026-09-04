package attention

import "time"

type DeliveryState string

const (
	DeliveryPending   DeliveryState = "pending"
	DeliveryDelivered DeliveryState = "delivered"
	DeliveryRetrying  DeliveryState = "retrying"
	DeliveryFailed    DeliveryState = "failed"
)

type Delivery struct {
	ID           string        `json:"id"`
	AttentionID  string        `json:"attention_id"`
	Provider     string        `json:"provider"`
	State        DeliveryState `json:"state"`
	Attempts     int           `json:"attempts"`
	NextAttempt  time.Time     `json:"next_attempt_at"`
	LastError    string        `json:"last_error,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	DeliveredAt *time.Time    `json:"delivered_at,omitempty"`
}
