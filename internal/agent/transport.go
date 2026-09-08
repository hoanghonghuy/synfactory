package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const maxTransportBody = 64 << 10

type TransportBackend interface {
	Heartbeat(context.Context, Session, Heartbeat) error
	NextLease(context.Context, Session) (LeaseDelivery, bool, error)
	LeaseAcked(context.Context, Session, LeaseAck, bool) error
	NextCancel(context.Context, Session) (CancelRequest, bool, error)
	CancelAcked(context.Context, Session, CancelAck, bool) error
	Status(context.Context, Session, Status) error
}

type Transport struct {
	Manager *SessionManager
	Backend TransportBackend
}

type sessionSequence struct {
	Session  Session `json:"session"`
	Sequence uint64  `json:"sequence"`
}

type heartbeatEnvelope struct {
	Session   Session   `json:"session"`
	Heartbeat Heartbeat `json:"heartbeat"`
}

type leaseAckEnvelope struct {
	Session  Session  `json:"session"`
	Sequence uint64   `json:"sequence"`
	Ack      LeaseAck `json:"ack"`
}

type cancelAckEnvelope struct {
	Session  Session   `json:"session"`
	Sequence uint64    `json:"sequence"`
	Ack      CancelAck `json:"ack"`
}

type statusEnvelope struct {
	Session  Session `json:"session"`
	Sequence uint64  `json:"sequence"`
	Status   Status  `json:"status"`
}

type leasePollResponse struct {
	Delivery *LeaseDelivery `json:"delivery,omitempty"`
}

type cancelPollResponse struct {
	Cancel *CancelRequest `json:"cancel,omitempty"`
}

func (t Transport) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /agent/connect", t.connect)
	mux.HandleFunc("POST /agent/heartbeat", t.heartbeat)
	mux.HandleFunc("POST /agent/lease/poll", t.pollLease)
	mux.HandleFunc("POST /agent/lease/ack", t.ackLease)
	mux.HandleFunc("POST /agent/cancel/poll", t.pollCancel)
	mux.HandleFunc("POST /agent/cancel/ack", t.ackCancel)
	mux.HandleFunc("POST /agent/status", t.status)
}

func (t Transport) connect(w http.ResponseWriter, r *http.Request) {
	if t.Manager == nil {
		http.Error(w, "agent transport unavailable", http.StatusServiceUnavailable)
		return
	}
	var req ConnectRequest
	if err := decodeTransport(w, r, &req); err != nil {
		writeTransportError(w, err)
		return
	}
	credential := bearerCredential(r.Header.Get("Authorization"))
	session, err := t.Manager.Connect(r.Context(), req, credential)
	if err != nil {
		writeTransportError(w, err)
		return
	}
	writeTransportJSON(w, http.StatusOK, session)
}

func (t Transport) heartbeat(w http.ResponseWriter, r *http.Request) {
	var env heartbeatEnvelope
	if err := decodeTransport(w, r, &env); err != nil {
		writeTransportError(w, err)
		return
	}
	if t.Manager == nil {
		writeTransportError(w, ErrStaleSession)
		return
	}
	if err := t.Manager.AcceptHeartbeat(env.Session, env.Heartbeat); err != nil {
		writeTransportError(w, err)
		return
	}
	if t.Backend != nil {
		if err := t.Backend.Heartbeat(r.Context(), env.Session, env.Heartbeat); err != nil {
			http.Error(w, "agent backend unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (t Transport) pollLease(w http.ResponseWriter, r *http.Request) {
	var env sessionSequence
	if err := decodeTransport(w, r, &env); err != nil {
		writeTransportError(w, err)
		return
	}
	if err := t.acceptSequence(env.Session, env.Sequence); err != nil {
		writeTransportError(w, err)
		return
	}
	if t.Backend == nil {
		writeTransportJSON(w, http.StatusOK, leasePollResponse{})
		return
	}
	delivery, ok, err := t.Backend.NextLease(r.Context(), env.Session)
	if err != nil {
		http.Error(w, "agent backend unavailable", http.StatusServiceUnavailable)
		return
	}
	if !ok {
		writeTransportJSON(w, http.StatusOK, leasePollResponse{})
		return
	}
	if _, err := t.trackDelivery(env.Session, delivery); err != nil {
		writeTransportError(w, err)
		return
	}
	writeTransportJSON(w, http.StatusOK, leasePollResponse{Delivery: &delivery})
}

func (t Transport) ackLease(w http.ResponseWriter, r *http.Request) {
	var env leaseAckEnvelope
	if err := decodeTransport(w, r, &env); err != nil {
		writeTransportError(w, err)
		return
	}
	if t.Manager == nil {
		writeTransportError(w, ErrStaleSession)
		return
	}
	first, err := t.Manager.RecordAck(env.Session, env.Sequence, env.Ack)
	if err != nil {
		writeTransportError(w, err)
		return
	}
	if t.Backend != nil {
		if err := t.Backend.LeaseAcked(r.Context(), env.Session, env.Ack, first); err != nil {
			http.Error(w, "agent backend unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (t Transport) pollCancel(w http.ResponseWriter, r *http.Request) {
	var env sessionSequence
	if err := decodeTransport(w, r, &env); err != nil {
		writeTransportError(w, err)
		return
	}
	if err := t.acceptSequence(env.Session, env.Sequence); err != nil {
		writeTransportError(w, err)
		return
	}
	if t.Backend == nil {
		writeTransportJSON(w, http.StatusOK, cancelPollResponse{})
		return
	}
	cancel, ok, err := t.Backend.NextCancel(r.Context(), env.Session)
	if err != nil {
		http.Error(w, "agent backend unavailable", http.StatusServiceUnavailable)
		return
	}
	if !ok {
		writeTransportJSON(w, http.StatusOK, cancelPollResponse{})
		return
	}
	if _, err := t.trackCancel(env.Session, cancel); err != nil {
		writeTransportError(w, err)
		return
	}
	writeTransportJSON(w, http.StatusOK, cancelPollResponse{Cancel: &cancel})
}

func (t Transport) ackCancel(w http.ResponseWriter, r *http.Request) {
	var env cancelAckEnvelope
	if err := decodeTransport(w, r, &env); err != nil {
		writeTransportError(w, err)
		return
	}
	if t.Manager == nil {
		writeTransportError(w, ErrStaleSession)
		return
	}
	first, err := t.Manager.RecordCancelAck(env.Session, env.Sequence, env.Ack)
	if err != nil {
		writeTransportError(w, err)
		return
	}
	if t.Backend != nil {
		if err := t.Backend.CancelAcked(r.Context(), env.Session, env.Ack, first); err != nil {
			http.Error(w, "agent backend unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (t Transport) status(w http.ResponseWriter, r *http.Request) {
	var env statusEnvelope
	if err := decodeTransport(w, r, &env); err != nil {
		writeTransportError(w, err)
		return
	}
	if t.Manager == nil {
		writeTransportError(w, ErrStaleSession)
		return
	}
	if err := t.Manager.RecordStatus(env.Session, env.Sequence, env.Status); err != nil {
		writeTransportError(w, err)
		return
	}
	if t.Backend != nil {
		if err := t.Backend.Status(r.Context(), env.Session, env.Status); err != nil {
			http.Error(w, "agent backend unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (t Transport) acceptSequence(session Session, sequence uint64) error {
	if t.Manager == nil {
		return ErrStaleSession
	}
	t.Manager.mu.Lock()
	defer t.Manager.mu.Unlock()
	_, err := t.Manager.acceptLocked(session, sequence)
	return err
}

func (t Transport) trackDelivery(session Session, delivery LeaseDelivery) (bool, error) {
	if t.Manager == nil || strings.TrimSpace(delivery.DeliveryID) == "" || !delivery.Identity.valid() || delivery.Identity.WorkerID != session.WorkerID {
		return false, ErrInvalidMessage
	}
	t.Manager.mu.Lock()
	defer t.Manager.mu.Unlock()
	state, err := currentTransportState(t.Manager, session)
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

func (t Transport) trackCancel(session Session, cancel CancelRequest) (bool, error) {
	if t.Manager == nil || strings.TrimSpace(cancel.CancelID) == "" || !cancel.Identity.valid() || cancel.Identity.WorkerID != session.WorkerID {
		return false, ErrInvalidMessage
	}
	t.Manager.mu.Lock()
	defer t.Manager.mu.Unlock()
	state, err := currentTransportState(t.Manager, session)
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

func currentTransportState(manager *SessionManager, session Session) (*workerState, error) {
	state := manager.workers[session.WorkerID]
	if state == nil || session.ID == "" || session.ID != state.sessionID || session.Generation != state.generation {
		return nil, ErrStaleSession
	}
	return state, nil
}

func decodeTransport(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxTransportBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return ErrInvalidMessage
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return ErrInvalidMessage
	}
	return nil
}

func bearerCredential(value string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return ""
	}
	credential := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	if strings.ContainsAny(credential, " \t\r\n") {
		return ""
	}
	return credential
}

func writeTransportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, ErrStaleSession), errors.Is(err, ErrReplay), errors.Is(err, ErrIdentityMismatch), errors.Is(err, ErrUnknownDelivery):
		http.Error(w, "conflict", http.StatusConflict)
	default:
		http.Error(w, "invalid agent message", http.StatusBadRequest)
	}
}

func writeTransportJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
