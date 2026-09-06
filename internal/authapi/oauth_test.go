package authapi

import (
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

type oauthProviderStub struct {
	identity authz.ExternalIdentity
}

func (p oauthProviderStub) Exchange(context.Context, string, string) (authz.ExternalIdentity, error) {
	return p.identity, nil
}

type oauthStoreStub struct {
	userID       string
	found        bool
	createdUser  string
	sessionUser  string
	sessionToken [sha256.Size]byte
}

func (s *oauthStoreStub) FindAuthUserByExternalIdentity(context.Context, string, string) (string, bool, error) {
	return s.userID, s.found, nil
}

func (s *oauthStoreStub) UpsertAuthUser(_ context.Context, id, _, _, _ string) error {
	s.createdUser = id
	s.userID = id
	return nil
}

func (s *oauthStoreStub) CreateAuthSession(_ context.Context, _, userID string, tokenHash [sha256.Size]byte, _, _ time.Time) error {
	s.sessionUser = userID
	s.sessionToken = tokenHash
	return nil
}

func TestOAuthHandlerLoginUsesStateCookieAndGitHubRedirect(t *testing.T) {
	store := &oauthStoreStub{}
	h := OAuthHandler{
		Store:        store,
		Provider:     oauthProviderStub{},
		Issuer:       authz.SessionIssuer{Store: store},
		ClientID:     "client-1",
		AuthorizeURL: "https://github.com/login/oauth/authorize",
		RedirectURI:  "https://factory.example/api/v1/auth/github/callback",
		Random:       strings.NewReader(strings.Repeat("s", 64)),
	}
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/github/login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d, want 302", rec.Code)
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Host != "github.com" || location.Query().Get("client_id") != "client-1" || location.Query().Get("state") == "" {
		t.Fatalf("unexpected redirect %s", location.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != oauthStateCookie || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("unexpected state cookie %#v", cookies)
	}
}

func TestOAuthHandlerCallbackRejectsStateMismatch(t *testing.T) {
	store := &oauthStoreStub{}
	h := OAuthHandler{
		Store:        store,
		Provider:     oauthProviderStub{},
		Issuer:       authz.SessionIssuer{Store: store},
		ClientID:     "client-1",
		AuthorizeURL: "https://github.com/login/oauth/authorize",
		RedirectURI:  "https://factory.example/api/v1/auth/github/callback",
	}
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/github/callback?state=wrong&code=code", nil)
	req.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: "expected"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
	if store.sessionUser != "" {
		t.Fatal("session issued for invalid OAuth state")
	}
}

func TestOAuthHandlerCallbackCreatesUnprivilegedIdentityAndBrowserSession(t *testing.T) {
	store := &oauthStoreStub{}
	random := strings.NewReader(strings.Repeat("x", 256))
	h := OAuthHandler{
		Store: store,
		Provider: oauthProviderStub{identity: authz.ExternalIdentity{
			Provider: "github", Subject: "12345", Name: "Huy",
		}},
		Issuer:       authz.SessionIssuer{Store: store, Random: random, Now: func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }},
		ClientID:     "client-1",
		AuthorizeURL: "https://github.com/login/oauth/authorize",
		RedirectURI:  "https://factory.example/api/v1/auth/github/callback",
		ReturnPath:   "/",
	}
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/github/callback?state=good&code=code", nil)
	req.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: "good"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if store.createdUser != "github:12345" || store.sessionUser != "github:12345" {
		t.Fatalf("created=%q session=%q", store.createdUser, store.sessionUser)
	}
	body, _ := io.ReadAll(rec.Result().Body)
	if !strings.Contains(string(body), "sessionStorage.setItem") || strings.Contains(string(body), "access_token") {
		t.Fatalf("unexpected callback body %s", body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("OAuth callback containing session bootstrap must not be cacheable")
	}
}

func TestOAuthHandlerCallbackReusesProvisionedIdentity(t *testing.T) {
	store := &oauthStoreStub{userID: "operator-huy", found: true}
	h := OAuthHandler{
		Store: store,
		Provider: oauthProviderStub{identity: authz.ExternalIdentity{
			Provider: "github", Subject: "12345", Name: "Huy",
		}},
		Issuer:       authz.SessionIssuer{Store: store, Random: strings.NewReader(strings.Repeat("y", 128))},
		ClientID:     "client-1",
		AuthorizeURL: "https://github.com/login/oauth/authorize",
		RedirectURI:  "http://localhost:8080/api/v1/auth/github/callback",
	}
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/github/callback?state=good&code=code", nil)
	req.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: "good"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if store.createdUser != "" || store.sessionUser != "operator-huy" {
		t.Fatalf("provisioned identity not reused: created=%q session=%q", store.createdUser, store.sessionUser)
	}
}
