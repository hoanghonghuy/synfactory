package terminal

import (
	"errors"
	"net/http"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

// AuthorizedHandler exposes the operator terminal through the shared Go-owned
// authorization boundary. The short-lived websocket ticket remains the only
// credential accepted by the stream endpoint after an authorized session is
// opened, so bearer credentials never need to be placed in websocket URLs.
type AuthorizedHandler struct {
	Handler    *Handler
	Authorizer authz.RequestAuthorizer
}

func (h AuthorizedHandler) Register(mux *http.ServeMux) {
	if h.Handler == nil {
		return
	}
	if h.Handler.tickets == nil {
		h.Handler.tickets = make(map[string]streamTicket)
	}
	if h.Handler.now == nil {
		h.Handler.now = time.Now
	}

	mux.HandleFunc("GET /api/v1/terminal/targets", h.authorize(h.Handler.listTargets))
	mux.HandleFunc("GET /api/v1/terminal/sessions", h.authorize(h.Handler.listSessions))
	mux.HandleFunc("POST /api/v1/terminal/sessions", h.authorize(h.Handler.openSession))
	mux.HandleFunc("DELETE /api/v1/terminal/sessions/{id}", h.authorize(h.Handler.closeSession))
	mux.HandleFunc("GET /api/v1/terminal/sessions/{id}/stream", h.Handler.stream)
}

func (h AuthorizedHandler) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.Authorizer == nil {
			writeTerminalJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "terminal authorization unavailable"})
			return
		}
		if _, err := h.Authorizer.Authorize(r, authz.PermissionTerminalAccess, ""); err != nil {
			switch {
			case errors.Is(err, authz.ErrUnauthenticated):
				writeTerminalJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			case errors.Is(err, authz.ErrForbidden):
				writeTerminalJSON(w, http.StatusForbidden, map[string]string{"error": "terminal permission denied"})
			default:
				writeTerminalJSON(w, http.StatusInternalServerError, map[string]string{"error": "terminal authorization failed"})
			}
			return
		}
		next(w, r)
	}
}
