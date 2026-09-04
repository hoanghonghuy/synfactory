package terminal

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeProcess struct {
	mu      sync.Mutex
	closed  int
	resized []Size
}

func (p *fakeProcess) Read([]byte) (int, error)    { return 0, nil }
func (p *fakeProcess) Write(b []byte) (int, error) { return len(b), nil }
func (p *fakeProcess) Wait() error                 { return nil }
func (p *fakeProcess) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed++
	return nil
}
func (p *fakeProcess) Resize(size Size) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resized = append(p.resized, size)
	return nil
}

type fakeBackend struct {
	processes []*fakeProcess
}

func (b *fakeBackend) Start(context.Context, Target, Size) (Process, error) {
	p := &fakeProcess{}
	b.processes = append(b.processes, p)
	return p, nil
}

func TestManagerDisabledDoesNotStartProcess(t *testing.T) {
	backend := &fakeBackend{}
	manager := NewManager(Config{Enabled: false}, []Target{{ID: "local", Kind: TargetLocal}}, map[TargetKind]Backend{TargetLocal: backend})
	if _, err := manager.Open(context.Background(), "local", Size{Rows: 24, Cols: 80}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Open() error = %v, want ErrDisabled", err)
	}
	if len(backend.processes) != 0 {
		t.Fatal("disabled manager must not start a terminal process")
	}
}

func TestManagerEnforcesGlobalAndPerTargetCapacity(t *testing.T) {
	backend := &fakeBackend{}
	manager := NewManager(Config{Enabled: true, MaxSessions: 2, MaxSessionsPerTarget: 1}, []Target{
		{ID: "control", Kind: TargetLocal},
		{ID: "worker", Kind: TargetLocal},
	}, map[TargetKind]Backend{TargetLocal: backend})
	if _, err := manager.Open(context.Background(), "control", Size{}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(context.Background(), "control", Size{}); !errors.Is(err, ErrTargetCapacity) {
		t.Fatalf("second same-target Open() error = %v, want ErrTargetCapacity", err)
	}
	if _, err := manager.Open(context.Background(), "worker", Size{}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(context.Background(), "missing", Size{}); !errors.Is(err, ErrTargetUnknown) && !errors.Is(err, ErrCapacity) {
		t.Fatalf("unexpected capacity/target error: %v", err)
	}
}

func TestManagerResizeAndCloseReleaseCapacity(t *testing.T) {
	backend := &fakeBackend{}
	manager := NewManager(Config{Enabled: true, MaxSessions: 1, MaxSessionsPerTarget: 1}, []Target{{ID: "local", Kind: TargetLocal}}, map[TargetKind]Backend{TargetLocal: backend})
	session, err := manager.Open(context.Background(), "local", Size{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Resize(session.ID, Size{Rows: 40, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	if len(backend.processes[0].resized) != 1 || backend.processes[0].resized[0].Cols != 120 {
		t.Fatalf("resize was not forwarded: %+v", backend.processes[0].resized)
	}
	if err := manager.Close(session.ID); err != nil {
		t.Fatal(err)
	}
	if backend.processes[0].closed != 1 {
		t.Fatalf("process close count = %d, want 1", backend.processes[0].closed)
	}
	if _, err := manager.Open(context.Background(), "local", Size{}); err != nil {
		t.Fatalf("capacity was not released after close: %v", err)
	}
}

func TestManagerReapsIdleAndLifetimeExpiredSessions(t *testing.T) {
	backend := &fakeBackend{}
	manager := NewManager(Config{
		Enabled:              true,
		MaxSessions:          2,
		MaxSessionsPerTarget: 2,
		IdleTimeout:          5 * time.Minute,
		MaxLifetime:          30 * time.Minute,
	}, []Target{{ID: "local", Kind: TargetLocal}}, map[TargetKind]Backend{TargetLocal: backend})

	now := time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	first, err := manager.Open(context.Background(), "local", Size{})
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(4 * time.Minute)
	if err := manager.Touch(first.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Minute)
	if got := manager.ReapExpired(); len(got) != 0 {
		t.Fatalf("session reaped before idle timeout: %v", got)
	}
	now = now.Add(2 * time.Minute)
	got := manager.ReapExpired()
	if len(got) != 1 || got[0] != first.ID {
		t.Fatalf("ReapExpired() = %v, want %s", got, first.ID)
	}
	if backend.processes[0].closed != 1 {
		t.Fatalf("expired process close count = %d, want 1", backend.processes[0].closed)
	}
}

func TestManagerShutdownClosesAllSessionsAndReleasesCapacity(t *testing.T) {
	backend := &fakeBackend{}
	manager := NewManager(Config{Enabled: true, MaxSessions: 2, MaxSessionsPerTarget: 2}, []Target{{ID: "local", Kind: TargetLocal}}, map[TargetKind]Backend{TargetLocal: backend})
	if _, err := manager.Open(context.Background(), "local", Size{}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(context.Background(), "local", Size{}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if len(manager.Active()) != 0 {
		t.Fatalf("active sessions remain after shutdown: %+v", manager.Active())
	}
	for i, process := range backend.processes {
		if process.closed != 1 {
			t.Fatalf("process %d close count = %d, want 1", i, process.closed)
		}
	}
	if _, err := manager.Open(context.Background(), "local", Size{}); err != nil {
		t.Fatalf("capacity was not released after shutdown: %v", err)
	}
}
