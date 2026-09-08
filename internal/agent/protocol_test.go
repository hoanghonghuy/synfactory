package agent

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

type verifierFunc func(context.Context, string, string) bool

func (f verifierFunc) VerifyAgentCredential(ctx context.Context, workerID, credential string) bool {
	return f(ctx, workerID, credential)
}

func manager() *SessionManager {
	return newSessionManagerWithRandom(verifierFunc(func(_ context.Context, workerID, credential string) bool {
		return workerID == "worker-1" && credential == "valid"
	}), bytes.NewReader(make([]byte, 64)))
}

func TestConnectFailsClosedAndDoesNotExposeCredential(t *testing.T) {
	m := manager()
	if _, err := m.Connect(context.Background(), ConnectRequest{WorkerID: "worker-1"}, "bad"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("bad credential error = %v", err)
	}
	session, err := m.Connect(context.Background(), ConnectRequest{WorkerID: "worker-1"}, "valid")
	if err != nil {
		t.Fatal(err)
	}
	if session.WorkerID != "worker-1" || session.ID == "" || session.Generation != 1 {
		t.Fatalf("unexpected session: %#v", session)
	}
}

func TestReconnectSupersedesSessionAndRejectsReplay(t *testing.T) {
	m := manager()
	first, err := m.Connect(context.Background(), ConnectRequest{WorkerID: "worker-1"}, "valid")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.AcceptHeartbeat(first, Heartbeat{WorkerID: "worker-1", SessionID: first.ID, Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	if err := m.AcceptHeartbeat(first, Heartbeat{WorkerID: "worker-1", SessionID: first.ID, Sequence: 1}); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay error = %v", err)
	}
	second, err := m.Connect(context.Background(), ConnectRequest{WorkerID: "worker-1"}, "valid")
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation != 2 {
		t.Fatalf("generation = %d", second.Generation)
	}
	if err := m.AcceptHeartbeat(first, Heartbeat{WorkerID: "worker-1", SessionID: first.ID, Sequence: 2}); !errors.Is(err, ErrStaleSession) {
		t.Fatalf("stale session error = %v", err)
	}
}

func TestDeliveryAndAckAreIdempotentForExactIdentity(t *testing.T) {
	m := manager()
	s, err := m.Connect(context.Background(), ConnectRequest{WorkerID: "worker-1"}, "valid")
	if err != nil {
		t.Fatal(err)
	}
	identity := LeaseIdentity{WorkerID: "worker-1", JobID: "job-1", RunID: "run-1", Revision: "abc123"}
	delivery := LeaseDelivery{DeliveryID: "delivery-1", Identity: identity}
	if first, err := m.RecordDelivery(s, 1, delivery); err != nil || !first {
		t.Fatalf("first delivery: first=%v err=%v", first, err)
	}
	if first, err := m.RecordDelivery(s, 2, delivery); err != nil || first {
		t.Fatalf("duplicate delivery: first=%v err=%v", first, err)
	}
	ack := LeaseAck{DeliveryID: delivery.DeliveryID, Identity: identity}
	if first, err := m.RecordAck(s, 3, ack); err != nil || !first {
		t.Fatalf("first ack: first=%v err=%v", first, err)
	}
	if first, err := m.RecordAck(s, 4, ack); err != nil || first {
		t.Fatalf("duplicate ack: first=%v err=%v", first, err)
	}
	wrong := ack
	wrong.Identity.Revision = "different"
	if _, err := m.RecordAck(s, 5, wrong); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("identity mismatch error = %v", err)
	}
}

func TestCancelIsTransportFactAndIdentityBound(t *testing.T) {
	m := manager()
	s, err := m.Connect(context.Background(), ConnectRequest{WorkerID: "worker-1"}, "valid")
	if err != nil {
		t.Fatal(err)
	}
	identity := LeaseIdentity{WorkerID: "worker-1", JobID: "job-1", RunID: "run-1", Revision: "abc123"}
	cancel := CancelRequest{CancelID: "cancel-1", Identity: identity}
	if first, err := m.RecordCancel(s, 1, cancel); err != nil || !first {
		t.Fatalf("first cancel: first=%v err=%v", first, err)
	}
	if first, err := m.RecordCancel(s, 2, cancel); err != nil || first {
		t.Fatalf("duplicate cancel: first=%v err=%v", first, err)
	}
	ack := CancelAck{CancelID: cancel.CancelID, Identity: identity}
	if first, err := m.RecordCancelAck(s, 3, ack); err != nil || !first {
		t.Fatalf("first cancel ack: first=%v err=%v", first, err)
	}
	if first, err := m.RecordCancelAck(s, 4, ack); err != nil || first {
		t.Fatalf("duplicate cancel ack: first=%v err=%v", first, err)
	}
	cancel.Identity.JobID = "other"
	if _, err := m.RecordCancel(s, 5, cancel); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("identity mismatch error = %v", err)
	}
}

func TestStatusIsBoundedAndRejectsSensitiveMetadata(t *testing.T) {
	m := manager()
	s, err := m.Connect(context.Background(), ConnectRequest{WorkerID: "worker-1"}, "valid")
	if err != nil {
		t.Fatal(err)
	}
	identity := LeaseIdentity{WorkerID: "worker-1", JobID: "job-1", RunID: "run-1", Revision: "abc123"}
	if err := m.RecordStatus(s, 1, Status{Identity: identity, State: "running", Metadata: map[string]string{"phase": "verify"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordStatus(s, 2, Status{Identity: identity, State: "running", Metadata: map[string]string{"access_token": "do-not-persist"}}); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("sensitive metadata error = %v", err)
	}
	tooLarge := make(map[string]string, MaxStatusMetadataEntries+1)
	for i := 0; i < MaxStatusMetadataEntries+1; i++ {
		tooLarge[string(rune('a'+i))] = "value"
	}
	if err := m.RecordStatus(s, 3, Status{Identity: identity, State: "running", Metadata: tooLarge}); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("oversized metadata error = %v", err)
	}
}
