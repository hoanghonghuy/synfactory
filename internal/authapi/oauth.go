package authapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

const oauthStateCookie = "synfactory.oauth.state"

type OAuthIdentityStore interface {
	FindAuthUserByExternalIdentity(context.Context, string, string) (string, bool, error)
	UpsertAuthUser(context.Context, string, string, string, string) error
}

type OAuthHandler struct {
	Store        OAuthIdentityStore
	Provider     authz.IdentityProvider
	Issuer       authz.SessionIssuer
	ClientID     string
	AuthorizeURL string
	RedirectURI  string
	ReturnPath   string
	Random       io.Reader
	Now          func() time.Time
}

func (h OAuthHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/github/login", h.login)
	mux.HandleFunc("GET /api/v1/auth/github/callback", h.callback)
}

func (h OAuthHandler) login(w http.ResponseWriter, r *http.Request) {
	if !h.ready() {
		http.Error(w, "github_oauth_unavailable", http.StatusServiceUnavailable)
		return
	}
	state, err := h.randomState()
	if err != nil {
		http.Error(w, "oauth_state_failed", http.StatusInternalServerError)
		return
	}
	h.setStateCookie(w, state, 10*time.Minute)

	authorizeURL, err := url.Parse(h.AuthorizeURL)
	if err != nil {
		http.Error(w, "github_oauth_unavailable", http.StatusServiceUnavailable)
		return
	}
	query := authorizeURL.Query()
	query.Set("client_id", strings.TrimSpace(h.ClientID))
	query.Set("redirect_uri", strings.TrimSpace(h.RedirectURI))
	query.Set("state", state)
	query.Set("scope", "read:user")
	authorizeURL.RawQuery = query.Encode()
	http.Redirect(w, r, authorizeURL.String(), http.StatusFound)
}

func (h OAuthHandler) callback(w http.ResponseWriter, r *http.Request) {
	if !h.ready() {
		http.Error(w, "github_oauth_unavailable", http.StatusServiceUnavailable)
		return
	}
	cookie, err := r.Cookie(oauthStateCookie)
	if err != nil || !sameSecret(cookie.Value, r.URL.Query().Get("state")) {
		http.Error(w, "oauth_state_invalid", http.StatusBadRequest)
		return
	}
	h.setStateCookie(w, "", -time.Hour)
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Error(w, "oauth_code_required", http.StatusBadRequest)
		return
	}

	identity, err := h.Provider.Exchange(r.Context(), code, strings.TrimSpace(h.RedirectURI))
	if err != nil {
		http.Error(w, "oauth_exchange_failed", http.StatusUnauthorized)
		return
	}
	identity, err = authz.ValidateExternalIdentity(identity)
	if err != nil {
		http.Error(w, "oauth_identity_invalid", http.StatusUnauthorized)
		return
	}
	userID, found, err := h.Store.FindAuthUserByExternalIdentity(r.Context(), identity.Provider, identity.Subject)
	if err != nil {
		http.Error(w, "oauth_identity_lookup_failed", http.StatusServiceUnavailable)
		return
	}
	if !found {
		userID = externalUserID(identity.Provider, identity.Subject)
		if err := h.Store.UpsertAuthUser(r.Context(), userID, identity.Provider, identity.Subject, identity.Name); err != nil {
			http.Error(w, "oauth_identity_create_failed", http.StatusConflict)
			return
		}
	}
	issued, err := h.Issuer.Issue(r.Context(), userID)
	if err != nil {
		http.Error(w, "session_issue_failed", http.StatusServiceUnavailable)
		return
	}

	returnPath := strings.TrimSpace(h.ReturnPath)
	if returnPath == "" || !strings.HasPrefix(returnPath, "/") || strings.HasPrefix(returnPath, "//") {
		returnPath = "/"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	_ = oauthSuccessTemplate.Execute(w, map[string]string{
		"TokenJSON":      jsonString(issued.Token),
		"SessionIDJSON":  jsonString(issued.ID),
		"ExpiresAtJSON":  jsonString(issued.ExpiresAt.UTC().Format(time.RFC3339)),
		"ReturnPathJSON": jsonString(returnPath),
	})
}

func (h OAuthHandler) ready() bool {
	return h.Store != nil && h.Provider != nil && h.Issuer.Store != nil && strings.TrimSpace(h.ClientID) != "" && strings.TrimSpace(h.AuthorizeURL) != "" && strings.TrimSpace(h.RedirectURI) != ""
}

func (h OAuthHandler) randomState() (string, error) {
	random := h.Random
	if random == nil {
		random = rand.Reader
	}
	buf := make([]byte, 32)
	if _, err := io.ReadFull(random, buf); err != nil {
		return "", fmt.Errorf("generate oauth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (h OAuthHandler) setStateCookie(w http.ResponseWriter, value string, ttl time.Duration) {
	now := time.Now().UTC()
	if h.Now != nil {
		now = h.Now().UTC()
	}
	secure := false
	if redirect, err := url.Parse(h.RedirectURI); err == nil {
		secure = strings.EqualFold(redirect.Scheme, "https")
	}
	cookie := &http.Cookie{
		Name:     oauthStateCookie,
		Value:    value,
		Path:     "/api/v1/auth/github/callback",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	if ttl > 0 {
		cookie.MaxAge = int(ttl.Seconds())
		cookie.Expires = now.Add(ttl)
	} else {
		cookie.MaxAge = -1
		cookie.Expires = now.Add(-time.Hour)
	}
	http.SetCookie(w, cookie)
}

func sameSecret(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	return left != "" && len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func externalUserID(provider, subject string) string {
	return strings.TrimSpace(provider) + ":" + strings.TrimSpace(subject)
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

var oauthSuccessTemplate = template.Must(template.New("oauth-success").Parse(`<!doctype html><meta charset="utf-8"><title>SynFactory sign-in</title><script>sessionStorage.setItem("synfactory.operator.token",{{.TokenJSON}});sessionStorage.setItem("synfactory.session.id",{{.SessionIDJSON}});sessionStorage.setItem("synfactory.session.expires_at",{{.ExpiresAtJSON}});location.replace({{.ReturnPathJSON}});</script>`))
