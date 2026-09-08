package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
)

var (
	ErrUnauthenticated  = errors.New("agent authentication failed")
	ErrInvalidMessage   = errors.New("invalid agent message")
	ErrStaleSession     = errors.New("stale or superseded agent session")
	ErrReplay           = errors.New("replayed or non-monotonic agent message")
	ErrIdentityMismatch = errors.New("lease identity mismatch")
	ErrUnknownDelivery  = errors.New("unknown lease delivery")
)

type CredentialVerifier interface {
	VerifyAgentCredential(ctx context.Context, workerID, credential string) bool
}

type ConnectRequest struct {
	WorkerID          string `json:"worker_id"`
	CapabilityVersion int    `json:"capability_version"`
}

type Session struct {
	WorkerID   string `json:"worker_id"`
	ID         string `json:"session_id"`
	Generation uint64 `json:"generation"`
}

type Heartbeat struct {
	WorkerID          string `json:"worker_id"`
	SessionID         string `json:"session_id"`
	Sequence          uint64 `json:"sequence"`
	CapabilityVersion int    `json:"capability_version"`
}

type LeaseIdentity struct {
	WorkerID string `json:"worker_id"`
	JobID    string `json:"job_id"`
	RunID    string `json:"run_id"`
	Revision string `json:"revision"`
}

func (i LeaseIdentity) valid() bool {
	return strings.TrimSpace(i.WorkerID) != "" && strings.TrimSpace(i.JobID) != "" && strings.TrimSpace(i.RunID) != "" && strings.TrimSpace(i.Revision) != ""
}

type LeaseDelivery struct {
	DeliveryID string        `json:"delivery_id"`
	Identity   LeaseIdentity `json:"identity"`
}

type LeaseAck struct {
	DeliveryID string        `json:"delivery_id"`
	Identity   LeaseIdentity `json:"identity"`
}

type CancelRequest struct {
	CancelID string        `json:"cancel_id"`
	Identity LeaseIdentity `json:"identity"`
}

type CancelAck struct {
	CancelID string        `json:"cancel_id"`
	Identity LeaseIdentity `json:"identity"`
}

type Status struct {
	Identity LeaseIdentity     `json:"identity"`
	State    string            `json:"state"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type SessionManager struct {
	mu       sync.Mutex
	verifier CredentialVerifier
	random   io.Reader
	workers  map[string]*workerState
}

type workerState struct {
	sessionID  string
	generation uint64
	lastSeq    uint64
	deliveries map[string]deliveryState
	cancels    map[string]cancelState
}

type cancelState struct {
	identity LeaseIdentity
	acked    bool
}

type deliveryState struct {
	identity LeaseIdentity
	acked    bool
}

func NewSessionManager(verifier CredentialVerifier) *SessionManager {
	return &SessionManager{verifier: verifier, random: rand.Reader, workers: map[string]*workerState{}}
}

func newSessionManagerWithRandom(verifier CredentialVerifier, random io.Reader) *SessionManager {
	return &SessionManager{verifier: verifier, random: random, workers: map[string]*workerState{}}
}

func (m *SessionManager) Connect(ctx context.Context, req ConnectRequest, credential string) (Session, error) {
	workerID := strings.TrimSpace(req.WorkerID)
	if workerID == "" || strings.TrimSpace(credential) == "" || m.verifier == nil || !m.verifier.VerifyAgentCredential(ctx, workerID, credential) {
		return Session{}, ErrUnauthenticated
	}
	var nonce [16]byte
	if _, err := io.ReadFull(m.random, nonce[:]); err != nil {
		return Session{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.workers[workerID]
	if state == nil {
		state = &workerState{deliveries: map[string]deliveryState{}, cancels: map[string]cancelState{}}
		m.workers[workerID] = state
	}
	state.generation++
	state.sessionID = hex.EncodeToString(nonce[:])
	state.lastSeq = 0
	return Session{WorkerID: workerID, ID: state.sessionID, Generation: state.generation}, nil
}

func (m *SessionManager) AcceptHeartbeat(session Session, heartbeat Heartbeat) error {
	if heartbeat.WorkerID != session.WorkerID || heartbeat.SessionID != session.ID || heartbeat.Sequence == 0 {
		return ErrInvalidMessage
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.acceptLocked(session, heartbeat.Sequence)
	if err != nil {
		return err
	}
	_ = state
	return nil
}

func (m *SessionManager) RecordDelivery(session Session, sequence uint64, delivery LeaseDelivery) (bool, error) {
	if strings.TrimSpace(delivery.DeliveryID) == "" || !delivery.Identity.valid() || delivery.Identity.WorkerID != session.WorkerID {
		return false, ErrInvalidMessage
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.acceptLocked(session, sequence)
	if err != nil {
		return false, err
	}
	existing, ok := state.deliveries[delivery.DeliveryID]
	if ok {
		if existing.identity != delivery.Identity {
			return false, ErrIdentityMismatch
		}
		return false, nil
	}
	state.deliveries[delivery.DeliveryID] = deliveryState{identity: delivery.Identity}
	return true, nil
}

func (m *SessionManager) RecordAck(session Session, sequence uint64, ack LeaseAck) (bool, error) {
	if strings.TrimSpace(ack.DeliveryID) == "" || !ack.Identity.valid() || ack.Identity.WorkerID != session.WorkerID {
		return false, ErrInvalidMessage
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.acceptLocked(session, sequence)
	if err != nil {
		return false, err
	}
	delivery, ok := state.deliveries[ack.DeliveryID]
	if !ok {
		return false, ErrUnknownDelivery
	}
	if delivery.identity != ack.Identity {
		return false, ErrIdentityMismatch
	}
	if delivery.acked {
		return false, nil
	}
	delivery.acked = true
	state.deliveries[ack.DeliveryID] = delivery
	return true, nil
}

func (m *SessionManager) RecordCancel(session Session, sequence uint64, cancel CancelRequest) (bool, error) {
	if strings.TrimSpace(cancel.CancelID) == "" || !cancel.Identity.valid() || cancel.Identity.WorkerID != session.WorkerID {
		return false, ErrInvalidMessage
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.acceptLocked(session, sequence)
	if err != nil {
		return false, err
	}
	existing, ok := state.cancels[cancel.CancelID]
	if ok {
		if existing.identity != cancel.Identity {
			return false, ErrIdentityMismatch
		}
		return false, nil
	}
	state.cancels[cancel.CancelID] = cancelState{identity: cancel.Identity}
	return true, nil
}

func (m *SessionManager) RecordCancelAck(session Session, sequence uint64, ack CancelAck) (bool, error) {
	if strings.TrimSpace(ack.CancelID) == "" || !ack.Identity.valid() || ack.Identity.WorkerID != session.WorkerID {
		return false, ErrInvalidMessage
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.acceptLocked(session, sequence)
	if err != nil {
		return false, err
	}
	cancel, ok := state.cancels[ack.CancelID]
	if !ok {
		return false, ErrUnknownDelivery
	}
	if cancel.identity != ack.Identity {
		return false, ErrIdentityMismatch
	}
	if cancel.acked {
		return false, nil
	}
	cancel.acked = true
	state.cancels[ack.CancelID] = cancel
	return true, nil
}

const (
	MaxStatusMetadataEntries    = 16
	MaxStatusMetadataValueBytes = 256
)

func (m *SessionManager) RecordStatus(session Session, sequence uint64, status Status) error {
	if status.Identity.WorkerID != session.WorkerID || !status.Identity.valid() || !validStatus(status) {
		return ErrInvalidMessage
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.acceptLocked(session, sequence)
	return err
}

func validStatus(status Status) bool {
	state := strings.TrimSpace(status.State)
	if state == "" || len(state) > 64 || len(status.Metadata) > MaxStatusMetadataEntries {
		return false
	}
	for key, value := range status.Metadata {
		key = strings.TrimSpace(strings.ToLower(key))
		if key == "" || len(key) > 64 || len(value) > MaxStatusMetadataValueBytes || sensitiveMetadataKey(key) {
			return false
		}
	}
	return true
}

func sensitiveMetadataKey(key string) bool {
	replacer := strings.NewReplacer("-", "_", " ", "_")
	key = replacer.Replace(strings.ToLower(key))
	for _, fragment := range []string{"token", "secret", "password", "credential", "authorization", "cookie", "private_key", "raw_terminal"} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}

func (m *SessionManager) acceptLocked(session Session, sequence uint64) (*workerState, error) {
	state := m.workers[session.WorkerID]
	if state == nil || session.ID == "" || session.ID != state.sessionID || session.Generation != state.generation {
		return nil, ErrStaleSession
	}
	if sequence == 0 || sequence <= state.lastSeq {
		return nil, ErrReplay
	}
	state.lastSeq = sequence
	return state, nil
}
