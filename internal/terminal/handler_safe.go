package terminal

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// RegisterSafe exposes the terminal API using disconnect-safe WebSocket lifecycle.
// It supersedes Register for production wiring while preserving the same REST contract.
func (h *Handler) RegisterSafe(mux *http.ServeMux) {
	if h.tickets == nil {
		h.tickets = make(map[string]streamTicket)
	}
	if h.now == nil {
		h.now = timeNow
	}
	mux.HandleFunc("GET /api/v1/terminal/targets", h.auth(h.listTargets))
	mux.HandleFunc("GET /api/v1/terminal/sessions", h.auth(h.listSessions))
	mux.HandleFunc("POST /api/v1/terminal/sessions", h.auth(h.openSession))
	mux.HandleFunc("DELETE /api/v1/terminal/sessions/{id}", h.auth(h.closeSession))
	mux.HandleFunc("GET /api/v1/terminal/sessions/{id}/stream", h.streamSafe)
}

var timeNow = func() time.Time { return time.Now() }

func (h *Handler) streamSafe(w http.ResponseWriter, r *http.Request) {
	if h.Manager == nil || strings.TrimSpace(h.Token) == "" {
		http.NotFound(w, r)
		return
	}
	sessionID := r.PathValue("id")
	if _, err := h.Manager.Session(sessionID); err != nil {
		http.NotFound(w, r)
		return
	}
	if !h.consumeTicket(r.URL.Query().Get("ticket"), sessionID) {
		writeTerminalJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired stream ticket"})
		return
	}
	conn, rw, err := upgradeWebSocket(w, r)
	if err != nil {
		return
	}
	defer conn.Close()
	defer func() { _ = h.Manager.Close(sessionID) }()

	session, err := h.Manager.Session(sessionID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	writeMu := &sync.Mutex{}
	go func() {
		buffer := make([]byte, 32<<10)
		for {
			n, readErr := session.Process.Read(buffer)
			if n > 0 {
				_ = h.Manager.Touch(sessionID)
				writeMu.Lock()
				err := writeWSFrame(rw, 0x2, buffer[:n])
				writeMu.Unlock()
				if err != nil {
					cancel()
					return
				}
			}
			if readErr != nil {
				cancel()
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		opcode, payload, err := readWSFrame(rw.Reader)
		if err != nil {
			return
		}
		switch opcode {
		case 0x2:
			if len(payload) > 0 {
				if _, err := session.Process.Write(payload); err != nil {
					return
				}
				_ = h.Manager.Touch(sessionID)
			}
		case 0x1:
			var message controlMessage
			if json.Unmarshal(payload, &message) != nil {
				continue
			}
			switch message.Type {
			case "input":
				if _, err := session.Process.Write([]byte(message.Data)); err != nil {
					return
				}
				_ = h.Manager.Touch(sessionID)
			case "resize":
				if err := h.Manager.Resize(sessionID, Size{Rows: message.Rows, Cols: message.Cols}); err != nil {
					return
				}
			}
		case 0x8:
			return
		case 0x9:
			writeMu.Lock()
			err := writeWSFrame(rw, 0xA, payload)
			writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}
