package postgres

import (
	"encoding/json"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
)

type Repository struct {
	ID            string
	Provider      string
	FullName      string
	DefaultBranch string
	Enabled       bool
	Config        json.RawMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type InboxEvent struct {
	ID              int64
	DedupeKey       string
	Provider        string
	RepositoryID    string
	Kind            string
	Subject         string
	Revision        string
	DeliveryID      string
	Payload         json.RawMessage
	ReceivedAt      time.Time
	ProcessedAt     *time.Time
	ProcessError    string
	ProcessingOwner string
	ProcessingUntil *time.Time
	ProcessAttempt  int
	NextAttemptAt   time.Time
}

type NewJob struct {
	ID            string
	DedupeKey     string
	RepositoryID  string
	SourceEventID *int64
	Kind          string
	Role          domain.Role
	Subject       string
	Revision      string
	Priority      int
	MaxAttempts   int
	AvailableAt   time.Time
	Metadata      json.RawMessage
}

type Run struct {
	ID         string
	JobID      string
	Attempt    int
	Runtime    string
	Model      string
	SessionID  string
	Status     string
	StartedAt  time.Time
	FinishedAt *time.Time
	ExitCode   *int
	Summary    string
	Metadata   json.RawMessage
}

type Evidence struct {
	ID        int64
	RunID     string
	Kind      string
	Name      string
	URI       string
	SHA256    string
	Metadata  json.RawMessage
	CreatedAt time.Time
}

type Worker struct {
	ID            string
	Host          string
	Capacity      int
	Draining      bool
	LastHeartbeat time.Time
	StartedAt     time.Time
	Metadata      json.RawMessage
}

type ReconcileState struct {
	RepositoryID        string
	LastIncrementalAt   *time.Time
	LastFullReconcileAt *time.Time
	Watermark           json.RawMessage
	UpdatedAt           time.Time
}

func jsonOrEmpty(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return value
}
