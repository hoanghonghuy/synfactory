package agentpostgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/agent"
	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

var ErrLeaseIdentityMismatch = errors.New("agent lease identity does not match PostgreSQL ownership")

const (
	maxCapabilityItems = 32
	maxCapabilityValue = 128
)

type Store interface {
	HeartbeatAgentWorker(context.Context, postgres.Worker, time.Time) (postgres.Worker, error)
	ActiveAgentRun(context.Context, string, time.Time) (postgres.AgentRun, bool, error)
	AcquireAgentRun(context.Context, string, time.Time, time.Duration) (postgres.AgentRun, bool, error)
	RenewLease(context.Context, string, string, time.Time, time.Duration) (domain.Job, error)
}

type Backend struct {
	Store         Store
	LeaseDuration time.Duration
	Now           func() time.Time
}

func (b Backend) Heartbeat(ctx context.Context, session agent.Session, heartbeat agent.Heartbeat) error {
	if b.Store == nil || session.WorkerID == "" || heartbeat.WorkerID != session.WorkerID {
		return fmt.Errorf("invalid agent heartbeat")
	}
	capabilities, err := boundedCapabilities(heartbeat)
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{"capabilities": capabilities})
	if err != nil {
		return fmt.Errorf("encode worker capabilities: %w", err)
	}
	now := b.now()
	if _, err := b.Store.HeartbeatAgentWorker(ctx, postgres.Worker{
		ID:       session.WorkerID,
		Capacity: 1,
		Metadata: metadata,
	}, now); err != nil {
		return err
	}

	active, ok, err := b.Store.ActiveAgentRun(ctx, session.WorkerID, now)
	if err != nil || !ok {
		return err
	}
	_, err = b.Store.RenewLease(ctx, active.Job.ID, session.WorkerID, now, b.leaseDuration())
	return err
}

func (b Backend) NextLease(ctx context.Context, session agent.Session) (agent.LeaseDelivery, bool, error) {
	if b.Store == nil || session.WorkerID == "" {
		return agent.LeaseDelivery{}, false, fmt.Errorf("agent backend is not configured")
	}
	value, ok, err := b.Store.AcquireAgentRun(ctx, session.WorkerID, b.now(), b.leaseDuration())
	if err != nil || !ok {
		return agent.LeaseDelivery{}, ok, err
	}
	delivery, err := deliveryFor(value, session.WorkerID)
	if err != nil {
		return agent.LeaseDelivery{}, false, err
	}
	return delivery, true, nil
}

func (b Backend) LeaseAcked(ctx context.Context, session agent.Session, ack agent.LeaseAck, _ bool) error {
	return b.verifyIdentity(ctx, session, ack.Identity)
}

func (b Backend) NextCancel(context.Context, agent.Session) (agent.CancelRequest, bool, error) {
	return agent.CancelRequest{}, false, nil
}

func (b Backend) CancelAcked(context.Context, agent.Session, agent.CancelAck, bool) error {
	return nil
}

func (b Backend) Status(ctx context.Context, session agent.Session, status agent.Status) error {
	// Status is transport evidence only in this slice. Checking ownership here
	// prevents stale workers from attaching facts to another run, while the
	// absence of completion methods on Store makes workflow-truth mutation
	// impossible.
	return b.verifyIdentity(ctx, session, status.Identity)
}

func (b Backend) verifyIdentity(ctx context.Context, session agent.Session, identity agent.LeaseIdentity) error {
	if b.Store == nil || identity.WorkerID != session.WorkerID {
		return ErrLeaseIdentityMismatch
	}
	value, ok, err := b.Store.ActiveAgentRun(ctx, session.WorkerID, b.now())
	if err != nil {
		return err
	}
	if !ok {
		return ErrLeaseIdentityMismatch
	}
	expected, err := identityFor(value, session.WorkerID)
	if err != nil {
		return err
	}
	if expected != identity {
		return ErrLeaseIdentityMismatch
	}
	return nil
}

func deliveryFor(value postgres.AgentRun, workerID string) (agent.LeaseDelivery, error) {
	identity, err := identityFor(value, workerID)
	if err != nil {
		return agent.LeaseDelivery{}, err
	}
	return agent.LeaseDelivery{
		DeliveryID: "lease-" + value.Run.ID,
		Identity:   identity,
	}, nil
}

func identityFor(value postgres.AgentRun, workerID string) (agent.LeaseIdentity, error) {
	if value.Job.LeaseOwner != workerID || value.Job.Status != domain.JobRunning ||
		value.Run.ID == "" || value.Run.JobID != value.Job.ID || value.Run.Attempt != value.Job.Attempt ||
		value.Run.Status != "running" || strings.TrimSpace(value.Job.Revision) == "" {
		return agent.LeaseIdentity{}, ErrLeaseIdentityMismatch
	}
	return agent.LeaseIdentity{
		WorkerID: workerID,
		JobID:    value.Job.ID,
		RunID:    value.Run.ID,
		Revision: value.Job.Revision,
	}, nil
}

func boundedCapabilities(heartbeat agent.Heartbeat) (domain.WorkerCapabilities, error) {
	if heartbeat.CapabilityVersion != domain.WorkerCapabilityVersion {
		return domain.WorkerCapabilities{}, fmt.Errorf("unsupported worker capability version")
	}
	raw := heartbeat.Capabilities
	if raw.Version != 0 && raw.Version != domain.WorkerCapabilityVersion {
		return domain.WorkerCapabilities{}, fmt.Errorf("unsupported worker capability payload version")
	}
	scalars := []string{raw.OS, raw.Architecture, raw.CPUClass, raw.MemoryClass, raw.GPU, raw.Region, raw.NetworkClass}
	for _, value := range scalars {
		if len(value) > maxCapabilityValue {
			return domain.WorkerCapabilities{}, fmt.Errorf("worker capability value exceeds limit")
		}
	}
	for _, values := range [][]string{raw.Runtimes, raw.Providers, raw.Labels} {
		if len(values) > maxCapabilityItems {
			return domain.WorkerCapabilities{}, fmt.Errorf("worker capability list exceeds limit")
		}
		for _, value := range values {
			if len(value) > maxCapabilityValue {
				return domain.WorkerCapabilities{}, fmt.Errorf("worker capability item exceeds limit")
			}
		}
	}
	c := raw.Normalized()
	if c.Version != domain.WorkerCapabilityVersion {
		return domain.WorkerCapabilities{}, fmt.Errorf("unsupported worker capability payload version")
	}
	return c, nil
}

func (b Backend) leaseDuration() time.Duration {
	if b.LeaseDuration > 0 {
		return b.LeaseDuration
	}
	return time.Minute
}

func (b Backend) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}
