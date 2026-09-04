package terminal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type AuditEvent struct {
	Event      string     `json:"event"`
	SessionID  string     `json:"session_id"`
	Operator   string     `json:"operator"`
	TargetID   string     `json:"target_id"`
	TargetKind TargetKind `json:"target_kind"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	Reason     string     `json:"reason,omitempty"`
}

type AuditSink interface {
	Record(AuditEvent) error
}

type FileAuditSink struct {
	mu   sync.Mutex
	path string
}

func NewFileAuditSink(path string) (*FileAuditSink, error) {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return nil, errors.New("terminal audit path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create terminal audit directory: %w", err)
	}
	return &FileAuditSink{path: path}, nil
}

func (s *FileAuditSink) Record(event AuditEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode terminal audit event: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open terminal audit log: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("write terminal audit event: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync terminal audit event: %w", err)
	}
	return nil
}
