package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	RuntimeBudgetReservationActive    = "active"
	RuntimeBudgetReservationAccounted = "accounted"
	RuntimeBudgetReservationReleased  = "released"
)

type RuntimeBudgetReservation struct {
	ID                   string
	Repository           string
	WorkflowID           string
	TaskID               string
	RunID                string
	Role                 string
	Provider             string
	Model                string
	ReservedCostMicroUSD int64
	State                string
	ExpiresAt            time.Time
	CreatedAt            time.Time
	ResolvedAt           *time.Time
}

func validateRuntimeBudgetReservation(reservation RuntimeBudgetReservation) error {
	if strings.TrimSpace(reservation.ID) == "" ||
		strings.TrimSpace(reservation.Repository) == "" ||
		strings.TrimSpace(reservation.RunID) == "" ||
		strings.TrimSpace(reservation.Role) == "" ||
		strings.TrimSpace(reservation.Provider) == "" ||
		strings.TrimSpace(reservation.Model) == "" {
		return errors.New("runtime budget reservation identity fields are required")
	}
	if reservation.ReservedCostMicroUSD <= 0 {
		return errors.New("runtime budget reservation cost must be positive")
	}
	if reservation.ExpiresAt.IsZero() {
		return errors.New("runtime budget reservation expiry is required")
	}
	return nil
}

func (s *Store) CreateRuntimeBudgetReservation(ctx context.Context, reservation RuntimeBudgetReservation) error {
	reservation.ID = strings.TrimSpace(reservation.ID)
	reservation.Repository = strings.TrimSpace(reservation.Repository)
	reservation.WorkflowID = strings.TrimSpace(reservation.WorkflowID)
	reservation.TaskID = strings.TrimSpace(reservation.TaskID)
	reservation.RunID = strings.TrimSpace(reservation.RunID)
	reservation.Role = strings.TrimSpace(reservation.Role)
	reservation.Provider = strings.TrimSpace(reservation.Provider)
	reservation.Model = strings.TrimSpace(reservation.Model)
	reservation.State = RuntimeBudgetReservationActive
	if reservation.CreatedAt.IsZero() {
		reservation.CreatedAt = time.Now().UTC()
	}
	reservation.ExpiresAt = reservation.ExpiresAt.UTC()
	if !reservation.ExpiresAt.After(reservation.CreatedAt) {
		return errors.New("runtime budget reservation expiry must be after creation")
	}
	if err := validateRuntimeBudgetReservation(reservation); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO runtime_budget_reservations (
    id, repository, workflow_id, task_id, run_id, role, provider, model,
    reserved_cost_microusd, state, expires_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'active', $10, $11)`,
		reservation.ID,
		reservation.Repository,
		reservation.WorkflowID,
		reservation.TaskID,
		reservation.RunID,
		reservation.Role,
		reservation.Provider,
		reservation.Model,
		reservation.ReservedCostMicroUSD,
		reservation.ExpiresAt,
		reservation.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create runtime budget reservation: %w", err)
	}
	return nil
}

func (s *Store) ResolveRuntimeBudgetReservation(ctx context.Context, id, state string, at time.Time) error {
	id = strings.TrimSpace(id)
	state = strings.TrimSpace(state)
	if id == "" {
		return errors.New("runtime budget reservation id is required")
	}
	if state != RuntimeBudgetReservationAccounted && state != RuntimeBudgetReservationReleased {
		return errors.New("runtime budget reservation terminal state must be accounted or released")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE runtime_budget_reservations
   SET state = $2,
       resolved_at = $3
 WHERE id = $1
   AND state = 'active'`, id, state, at.UTC())
	if err != nil {
		return fmt.Errorf("resolve runtime budget reservation: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect runtime budget reservation resolution: %w", err)
	}
	if rows == 0 {
		return errors.New("active runtime budget reservation not found")
	}
	return nil
}

func (s *Store) ResolveRuntimeBudgetReservationByIdentity(ctx context.Context, repository, runID, provider, model, state string, at time.Time) error {
	repository = strings.TrimSpace(repository)
	runID = strings.TrimSpace(runID)
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	state = strings.TrimSpace(state)
	if repository == "" || runID == "" || provider == "" || model == "" {
		return errors.New("runtime budget reservation identity is required")
	}
	if state != RuntimeBudgetReservationAccounted && state != RuntimeBudgetReservationReleased {
		return errors.New("runtime budget reservation terminal state must be accounted or released")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE runtime_budget_reservations
   SET state = $5,
       resolved_at = $6
 WHERE repository = $1
   AND run_id = $2
   AND provider = $3
   AND model = $4
   AND state = 'active'`, repository, runID, provider, model, state, at.UTC())
	if err != nil {
		return fmt.Errorf("resolve runtime budget reservation by identity: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect runtime budget reservation identity resolution: %w", err)
	}
	if rows == 0 {
		return errors.New("active runtime budget reservation not found")
	}
	return nil
}

// ReleaseRuntimeBudgetReservationByIdentity is intentionally idempotent. It is
// only for paths that can prove the provider was not invoked (for example, a
// pre-run health probe failure). Unknown outcomes must never call this method.
func (s *Store) ReleaseRuntimeBudgetReservationByIdentity(ctx context.Context, repository, runID, provider, model string, at time.Time) error {
	repository = strings.TrimSpace(repository)
	runID = strings.TrimSpace(runID)
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if repository == "" || runID == "" || provider == "" || model == "" {
		return errors.New("runtime budget reservation identity is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(ctx, `
UPDATE runtime_budget_reservations
   SET state = 'released',
       resolved_at = $5
 WHERE repository = $1
   AND run_id = $2
   AND provider = $3
   AND model = $4
   AND state = 'active'`, repository, runID, provider, model, at.UTC()); err != nil {
		return fmt.Errorf("release runtime budget reservation by identity: %w", err)
	}
	return nil
}

func (s *Store) ActiveRuntimeBudgetReservations(ctx context.Context, repository string) ([]RuntimeBudgetReservation, error) {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return nil, errors.New("runtime budget reservation repository is required")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, repository, workflow_id, task_id, run_id, role, provider, model,
       reserved_cost_microusd, state, expires_at, created_at, resolved_at
  FROM runtime_budget_reservations
 WHERE repository = $1
   AND state = 'active'
 ORDER BY created_at, id`, repository)
	if err != nil {
		return nil, fmt.Errorf("query active runtime budget reservations: %w", err)
	}
	defer rows.Close()

	reservations := make([]RuntimeBudgetReservation, 0)
	for rows.Next() {
		var reservation RuntimeBudgetReservation
		if err := rows.Scan(
			&reservation.ID,
			&reservation.Repository,
			&reservation.WorkflowID,
			&reservation.TaskID,
			&reservation.RunID,
			&reservation.Role,
			&reservation.Provider,
			&reservation.Model,
			&reservation.ReservedCostMicroUSD,
			&reservation.State,
			&reservation.ExpiresAt,
			&reservation.CreatedAt,
			&reservation.ResolvedAt,
		); err != nil {
			return nil, fmt.Errorf("scan active runtime budget reservation: %w", err)
		}
		reservations = append(reservations, reservation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active runtime budget reservations: %w", err)
	}
	return reservations, nil
}
