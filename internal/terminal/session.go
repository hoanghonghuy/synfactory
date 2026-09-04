package terminal

import (
	"context"
	"errors"
	"fmt"
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

func (m *Manager) Open(ctx context.Context, targetID string, size Size) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.cfg.Enabled {
		return nil, ErrDisabled
	}
	target, ok := m.targets[targetID]
	if !ok {
		return nil, ErrTargetUnknown
	}
	if len(m.sessions) >= m.cfg.MaxSessions {
		return nil, ErrCapacity
	}
	var targetSessions int
	for _, session := range m.sessions {
		if session.Target.ID == targetID {
			targetSessions++
		}
	}
	if targetSessions >= m.cfg.MaxSessionsPerTarget {
		return nil, ErrTargetCapacity
	}
	backend := m.backends[target.Kind]
	if backend == nil {
		return nil, fmt.Errorf("terminal backend %q unavailable", target.Kind)
	}
	process, err := backend.Start(ctx, target, size)
	if err != nil {
		return nil, err
	}
	now := m.now().UTC()
	m.nextID++
	session := &Session{
		ID:       fmt.Sprintf("term-%d", m.nextID),
		Target:   target,
		OpenedAt: now,
		LastIOAt: now,
		Process:  process,
	}
	m.sessions[session.ID] = session
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
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()
	if !ok {
		return ErrSessionUnknown
	}
	return session.Process.Close()
}

func (m *Manager) ReapExpired() []string {
	now := m.now().UTC()
	m.mu.Lock()
	var expired []*Session
	for id, session := range m.sessions {
		idleExpired := now.Sub(session.LastIOAt) >= m.cfg.IdleTimeout
		lifetimeExpired := now.Sub(session.OpenedAt) >= m.cfg.MaxLifetime
		if idleExpired || lifetimeExpired {
			expired = append(expired, session)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	ids := make([]string, 0, len(expired))
	for _, session := range expired {
		_ = session.Process.Close()
		ids = append(ids, session.ID)
	}
	return ids
}

func (m *Manager) Active() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		items = append(items, *session)
	}
	return items
}
