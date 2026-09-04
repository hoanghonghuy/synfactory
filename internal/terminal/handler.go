package terminal

import (
	"bufio"
	"context"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	streamTicketTTL = 30 * time.Second
	maxWSFrame       = 64 << 10
)

type Handler struct {
	Manager *Manager
	Token   string

	mu      sync.Mutex
	tickets map[string]streamTicket
	next    uint64
	now     func() time.Time
}

type streamTicket struct {
	SessionID string
	ExpiresAt time.Time
}

type safeTarget struct {
	ID      string     `json:"id"`
	Kind    TargetKind `json:"kind"`
	Host    string     `json:"host,omitempty"`
	User    string     `json:"user,omitempty"`
	Port    int        `json:"port,omitempty"`
	WorkDir string     `json:"work_dir,omitempty"`
}

type safeSession struct {
	ID       string     `json:"id"`
	TargetID string     `json:"target_id"`
	Kind     TargetKind `json:"kind"`
	OpenedAt time.Time  `json:"opened_at"`
	LastIOAt time.Time  `json:"last_io_at"`
}

type openRequest struct {
	TargetID string `json:"target_id"`
	Rows     uint16 `json:"rows"`
	Cols     uint16 `json:"cols"`
}

type openResponse struct {
	Session safeSession `json:"session"`
	Ticket  string      `json:"stream_ticket"`
}

type controlMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
}

func (h *Handler) Register(mux *http.ServeMux) {
	if h.tickets == nil {
		h.tickets = make(map[string]streamTicket)
	}
	if h.now == nil {
		h.now = time.Now
	}
	mux.HandleFunc("GET /api/v1/terminal/targets", h.auth(h.listTargets))
	mux.HandleFunc("GET /api/v1/terminal/sessions", h.auth(h.listSessions))
	mux.HandleFunc("POST /api/v1/terminal/sessions", h.auth(h.openSession))
	mux.HandleFunc("DELETE /api/v1/terminal/sessions/{id}", h.auth(h.closeSession))
	mux.HandleFunc("GET /api/v1/terminal/sessions/{id}/stream", h.stream)
}

func (h *Handler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(h.Token) == "" {
			http.NotFound(w, r)
			return
		}
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, prefix) || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, prefix)), []byte(h.Token)) != 1 {
			writeTerminalJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (h *Handler) listTargets(w http.ResponseWriter, _ *http.Request) {
	if h.Manager == nil {
		writeTerminalJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "terminal unavailable"})
		return
	}
	targets := h.Manager.Targets()
	out := make([]safeTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, safeTarget{ID: target.ID, Kind: target.Kind, Host: target.Host, User: target.User, Port: target.Port, WorkDir: target.WorkDir})
	}
	writeTerminalJSON(w, http.StatusOK, map[string]any{"targets": out})
}

func (h *Handler) listSessions(w http.ResponseWriter, _ *http.Request) {
	if h.Manager == nil {
		writeTerminalJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "terminal unavailable"})
		return
	}
	sessions := h.Manager.Active()
	out := make([]safeSession, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, safeSessionFrom(session))
	}
	writeTerminalJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (h *Handler) openSession(w http.ResponseWriter, r *http.Request) {
	if h.Manager == nil {
		writeTerminalJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "terminal unavailable"})
		return
	}
	var request openRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeTerminalJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid terminal request"})
		return
	}
	session, err := h.Manager.Open(r.Context(), strings.TrimSpace(request.TargetID), Size{Rows: request.Rows, Cols: request.Cols})
	if err != nil {
		h.writeManagerError(w, err)
		return
	}
	ticket := h.issueTicket(session.ID)
	writeTerminalJSON(w, http.StatusCreated, openResponse{Session: safeSessionFrom(*session), Ticket: ticket})
}

func (h *Handler) closeSession(w http.ResponseWriter, r *http.Request) {
	if h.Manager == nil {
		writeTerminalJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "terminal unavailable"})
		return
	}
	if err := h.Manager.Close(r.PathValue("id")); err != nil {
		h.writeManagerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeManagerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrDisabled):
		writeTerminalJSON(w, http.StatusForbidden, map[string]string{"error": ErrDisabled.Error()})
	case errors.Is(err, ErrTargetUnknown), errors.Is(err, ErrSessionUnknown):
		writeTerminalJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrCapacity), errors.Is(err, ErrTargetCapacity):
		writeTerminalJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
	default:
		writeTerminalJSON(w, http.StatusBadGateway, map[string]string{"error": "terminal backend unavailable"})
	}
}

func safeSessionFrom(session Session) safeSession {
	return safeSession{ID: session.ID, TargetID: session.Target.ID, Kind: session.Target.Kind, OpenedAt: session.OpenedAt, LastIOAt: session.LastIOAt}
}

func (h *Handler) issueTicket(sessionID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	seed := fmt.Sprintf("%s:%d:%d", sessionID, h.next, h.now().UnixNano())
	sum := sha1.Sum([]byte(seed))
	ticket := base64.RawURLEncoding.EncodeToString(sum[:])
	h.tickets[ticket] = streamTicket{SessionID: sessionID, ExpiresAt: h.now().Add(streamTicketTTL)}
	return ticket
}

func (h *Handler) consumeTicket(ticket, sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for value, item := range h.tickets {
		if !item.ExpiresAt.After(now) {
			delete(h.tickets, value)
		}
	}
	item, ok := h.tickets[ticket]
	if !ok || item.SessionID != sessionID || !item.ExpiresAt.After(now) {
		return false
	}
	delete(h.tickets, ticket)
	return true
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
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
	defer h.Manager.Close(sessionID)

	session, err := h.Manager.Session(sessionID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	writeMu := &sync.Mutex{}
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
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
			<-outputDone
			return
		default:
		}
		opcode, payload, err := readWSFrame(rw.Reader)
		if err != nil {
			cancel()
			<-outputDone
			return
		}
		switch opcode {
		case 0x2:
			if len(payload) > 0 {
				_, _ = session.Process.Write(payload)
				_ = h.Manager.Touch(sessionID)
			}
		case 0x1:
			var message controlMessage
			if json.Unmarshal(payload, &message) != nil {
				continue
			}
			switch message.Type {
			case "input":
				_, _ = session.Process.Write([]byte(message.Data))
				_ = h.Manager.Touch(sessionID)
			case "resize":
				_ = h.Manager.Resize(sessionID, Size{Rows: message.Rows, Cols: message.Cols})
			}
		case 0x8:
			cancel()
			<-outputDone
			return
		case 0x9:
			writeMu.Lock()
			_ = writeWSFrame(rw, 0xA, payload)
			writeMu.Unlock()
		}
	}
}

func upgradeWebSocket(w http.ResponseWriter, r *http.Request) (net.Conn, *bufio.ReadWriter, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || !headerContainsToken(r.Header.Get("Connection"), "upgrade") || r.Header.Get("Sec-WebSocket-Version") != "13" {
		writeTerminalJSON(w, http.StatusBadRequest, map[string]string{"error": "websocket upgrade required"})
		return nil, nil, errors.New("websocket upgrade required")
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		if !sameOriginHost(origin, r.Host) {
			writeTerminalJSON(w, http.StatusForbidden, map[string]string{"error": "websocket origin rejected"})
			return nil, nil, errors.New("websocket origin rejected")
		}
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		writeTerminalJSON(w, http.StatusBadRequest, map[string]string{"error": "missing websocket key"})
		return nil, nil, errors.New("missing websocket key")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		writeTerminalJSON(w, http.StatusInternalServerError, map[string]string{"error": "websocket unsupported"})
		return nil, nil, errors.New("websocket unsupported")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	acceptSum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	accept := base64.StdEncoding.EncodeToString(acceptSum[:])
	_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n")
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, rw, nil
}

func sameOriginHost(origin, host string) bool {
	origin = strings.TrimSpace(origin)
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(strings.ToLower(origin), prefix) {
			value := origin[len(prefix):]
			if slash := strings.IndexByte(value, '/'); slash >= 0 {
				value = value[:slash]
			}
			return strings.EqualFold(value, host)
		}
	}
	return false
}

func headerContainsToken(value, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func readWSFrame(reader *bufio.Reader) (byte, []byte, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	if first&0x80 == 0 {
		return 0, nil, errors.New("fragmented websocket frames are unsupported")
	}
	opcode := first & 0x0f
	if second&0x80 == 0 {
		return 0, nil, errors.New("client websocket frame must be masked")
	}
	length := uint64(second & 0x7f)
	switch length {
	case 126:
		var raw [2]byte
		if _, err := io.ReadFull(reader, raw[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(raw[:]))
	case 127:
		var raw [8]byte
		if _, err := io.ReadFull(reader, raw[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(raw[:])
	}
	if length > maxWSFrame {
		return 0, nil, errors.New("websocket frame too large")
	}
	var mask [4]byte
	if _, err := io.ReadFull(reader, mask[:]); err != nil {
		return 0, nil, err
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return opcode, payload, nil
}

func writeWSFrame(rw *bufio.ReadWriter, opcode byte, payload []byte) error {
	if err := rw.WriteByte(0x80 | opcode); err != nil {
		return err
	}
	length := len(payload)
	switch {
	case length < 126:
		if err := rw.WriteByte(byte(length)); err != nil {
			return err
		}
	case length <= 65535:
		if err := rw.WriteByte(126); err != nil {
			return err
		}
		var raw [2]byte
		binary.BigEndian.PutUint16(raw[:], uint16(length))
		if _, err := rw.Write(raw[:]); err != nil {
			return err
		}
	default:
		if err := rw.WriteByte(127); err != nil {
			return err
		}
		var raw [8]byte
		binary.BigEndian.PutUint64(raw[:], uint64(length))
		if _, err := rw.Write(raw[:]); err != nil {
			return err
		}
	}
	if _, err := rw.Write(payload); err != nil {
		return err
	}
	return rw.Flush()
}

func writeTerminalJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
