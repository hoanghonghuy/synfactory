package attention

import (
	"fmt"
	"strings"
	"time"
)

type Severity string

type State string

type Kind string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

const (
	StateOpen         State = "open"
	StateAcknowledged State = "acknowledged"
	StateSnoozed      State = "snoozed"
	StateResolved     State = "resolved"
)

const (
	KindProductDecision Kind = "product_decision"
	KindRepairExhausted Kind = "repair_exhausted"
	KindCredential      Kind = "credential_failure"
	KindReleaseBlocker  Kind = "release_blocker"
	KindSecurityBlocker Kind = "security_blocker"
	KindFleetOutage     Kind = "worker_fleet_outage"
)

type Item struct {
	ID            string     `json:"id"`
	DedupeKey     string     `json:"dedupe_key"`
	RepositoryID  string     `json:"repository_id,omitempty"`
	WorkflowID    string     `json:"workflow_id,omitempty"`
	Kind          Kind       `json:"kind"`
	Severity      Severity   `json:"severity"`
	State         State      `json:"state"`
	Title         string     `json:"title"`
	Summary       string     `json:"summary"`
	AssignedTo    string     `json:"assigned_to,omitempty"`
	SnoozedUntil  *time.Time `json:"snoozed_until,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
}

func DedupeKey(repositoryID, workflowID string, kind Kind, subject string) (string, error) {
	repositoryID = normalize(repositoryID)
	workflowID = normalize(workflowID)
	subject = normalize(subject)
	if strings.TrimSpace(string(kind)) == "" || subject == "" {
		return "", fmt.Errorf("attention kind and subject are required")
	}
	return strings.Join([]string{repositoryID, workflowID, string(kind), subject}, ":"), nil
}

func normalize(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func (i Item) Active(now time.Time) bool {
	if i.State == StateResolved {
		return false
	}
	if i.State == StateSnoozed && i.SnoozedUntil != nil && i.SnoozedUntil.After(now) {
		return false
	}
	return true
}
