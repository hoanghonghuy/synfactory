package terminal

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var (
	ErrDisabled       = errors.New("operator terminal is disabled")
	ErrTargetUnknown  = errors.New("terminal target is unknown")
	ErrCapacity       = errors.New("terminal session capacity exhausted")
	ErrTargetCapacity = errors.New("terminal target session capacity exhausted")
	ErrSessionUnknown = errors.New("terminal session is unknown")
)

type TargetKind string

const (
	TargetLocal TargetKind = "local"
	TargetSSH   TargetKind = "ssh"
)

type Target struct {
	ID             string     `json:"id"`
	Kind           TargetKind `json:"kind"`
	WorkDir        string     `json:"work_dir,omitempty"`
	Shell          string     `json:"shell,omitempty"`
	Host           string     `json:"host,omitempty"`
	User           string     `json:"user,omitempty"`
	Port           int        `json:"port,omitempty"`
	IdentityFile   string     `json:"identity_file,omitempty"`
	KnownHostsFile string     `json:"known_hosts_file,omitempty"`
}

type Size struct {
	Rows uint16
	Cols uint16
}

type Process interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Resize(Size) error
	Wait() error
	Close() error
}

type Backend interface {
	Start(context.Context, Target, Size) (Process, error)
}

type Config struct {
	Enabled              bool
	MaxSessions          int
	MaxSessionsPerTarget int
	IdleTimeout          time.Duration
	MaxLifetime          time.Duration
}

type Session struct {
	ID       string
	Operator string
	Target   Target
	OpenedAt time.Time
	LastIOAt time.Time
	Process  Process
}

type Manager struct {
	mu       sync.Mutex
	cfg      Config
	targets  map[string]Target
	backends map[TargetKind]Backend
	sessions map[string]*Session
	nextID   uint64
	now      func() time.Time
	audit    AuditSink
}

func NewManager(cfg Config, targets []Target, backends map[TargetKind]Backend) *Manager {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 2
	}
	if cfg.MaxSessionsPerTarget <= 0 {
		cfg.MaxSessionsPerTarget = 1
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 15 * time.Minute
	}
	if cfg.MaxLifetime <= 0 {
		cfg.MaxLifetime = 2 * time.Hour
	}
	index := make(map[string]Target, len(targets))
	for _, target := range targets {
		if target.ID != "" {
			index[target.ID] = target
		}
	}
	return &Manager{
		cfg:      cfg,
		targets:  index,
		backends: backends,
		sessions: map[string]*Session{},
		now:      time.Now,
	}
}

func (m *Manager) SetAuditSink(sink AuditSink) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audit = sink
}

func (m *Manager) Open(ctx context.Context, targetID string, size Size) (*Session, error) {
	return m.OpenAs(ctx, "operator", targetID, size)
}

func (m *Manager) OpenAs(ctx context.Context, operator, targetID string, size Size) (*Session, error) {
	m.mu.Lock()
	if !m.cfg.Enabled {
		m.mu.Unlock()
		return nil, ErrDisabled
	}
	target, ok := m.targets[targetID]
	if !ok {
		m.mu.Unlock()
		return nil, ErrTargetUnknown
	}
	if len(m.sessions) >= m.cfg.MaxSessions {
		m.mu.Unlock()
		return nil, ErrCapacity
	}
	var targetSessions int
	for _, session := range m.sessions {
		if session.Target.ID == targetID {
			targetSessions++
		}
	}
	if targetSessions >= m.cfg.MaxSessionsPerTarget {
		m.mu.Unlock()
		return nil, ErrTargetCapacity
	}
	backend := m.backends[target.Kind]
	if backend == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("terminal backend %q unavailable", target.Kind)
	}
	process, err := backend.Start(ctx, target, size)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	now := m.now().UTC()
	m.nextID++
	session := &Session{
		ID:       fmt.Sprintf("term-%d", m.nextID),
		Operator: operator,
		Target:   target,
		OpenedAt: now,
		LastIOAt: now,
		Process:  process,
	}
	m.sessions[session.ID] = session
	audit := m.audit
	m.mu.Unlock()
	if audit != nil {
		if err := audit.Record(AuditEvent{Event: "opened", SessionID: session.ID, Operator: session.Operator, TargetID: target.ID, TargetKind: target.Kind, StartedAt: now}); err != nil {
			_ = m.CloseWithReason(session.ID, "audit_open_failed")
			return nil, fmt.Errorf("audit terminal session open: %w", err)
		}
	}
	return session, nil
}

func (m *Manager) Touch(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[sessionID]
	if !ok {
		return ErrSessionUnknown
	}
	session.LastIOAt = m.now().UTC()
	return nil
}

func (m *Manager) Session(sessionID string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[sessionID]
	if !ok {
		return Session{}, ErrSessionUnknown
	}
	return *session, nil
}

func (m *Manager) Resize(sessionID string, size Size) error {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	if ok {
		session.LastIOAt = m.now().UTC()
	}
	m.mu.Unlock()
	if !ok {
		return ErrSessionUnknown
	}
	return session.Process.Resize(size)
}

func (m *Manager) Close(sessionID string) error {
	return m.CloseWithReason(sessionID, "explicit_close")
}

func (m *Manager) CloseWithReason(sessionID, reason string) error {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	audit := m.audit
	now := m.now().UTC()
	m.mu.Unlock()
	if !ok {
		return ErrSessionUnknown
	}
	closeErr := session.Process.Close()
	var auditErr error
	if audit != nil {
		auditErr = audit.Record(AuditEvent{Event: "closed", SessionID: session.ID, Operator: session.Operator, TargetID: session.Target.ID, TargetKind: session.Target.Kind, StartedAt: session.OpenedAt, EndedAt: &now, Reason: reason})
	}
	return errors.Join(closeErr, auditErr)
}

func (m *Manager) ReapExpired() []string {
	now := m.now().UTC()
	m.mu.Lock()
	type expiredSession struct {
		session *Session
		reason  string
	}
	var expired []expiredSession
	for id, session := range m.sessions {
		idleExpired := now.Sub(session.LastIOAt) >= m.cfg.IdleTimeout
		lifetimeExpired := now.Sub(session.OpenedAt) >= m.cfg.MaxLifetime
		if idleExpired || lifetimeExpired {
			reason := "idle_timeout"
			if lifetimeExpired {
				reason = "max_lifetime"
			}
			expired = append(expired, expiredSession{session: session, reason: reason})
			delete(m.sessions, id)
		}
	}
	audit := m.audit
	m.mu.Unlock()

	ids := make([]string, 0, len(expired))
	for _, item := range expired {
		_ = item.session.Process.Close()
		if audit != nil {
			_ = audit.Record(AuditEvent{Event: "closed", SessionID: item.session.ID, Operator: item.session.Operator, TargetID: item.session.Target.ID, TargetKind: item.session.Target.Kind, StartedAt: item.session.OpenedAt, EndedAt: &now, Reason: item.reason})
		}
		ids = append(ids, item.session.ID)
	}
	return ids
}

func (m *Manager) Shutdown() error {
	m.mu.Lock()
	closing := make([]*Session, 0, len(m.sessions))
	for id, session := range m.sessions {
		closing = append(closing, session)
		delete(m.sessions, id)
	}
	audit := m.audit
	now := m.now().UTC()
	m.mu.Unlock()

	var errs []error
	for _, session := range closing {
		if err := session.Process.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close terminal session %s: %w", session.ID, err))
		}
		if audit != nil {
			if err := audit.Record(AuditEvent{Event: "closed", SessionID: session.ID, Operator: session.Operator, TargetID: session.Target.ID, TargetKind: session.Target.Kind, StartedAt: session.OpenedAt, EndedAt: &now, Reason: "shutdown"}); err != nil {
				errs = append(errs, fmt.Errorf("audit terminal session %s shutdown: %w", session.ID, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) Active() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		items = append(items, *session)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (m *Manager) Targets() []Target {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Target, 0, len(m.targets))
	for _, target := range m.targets {
		items = append(items, target)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}
