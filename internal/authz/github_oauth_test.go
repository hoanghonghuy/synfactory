package authz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGitHubOAuthProviderExchange(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.Method != http.MethodPost {
				t.Fatalf("token method = %s", r.Method)
			}
			if got := r.Header.Get("Accept"); got != "application/json" {
				t.Fatalf("token accept = %q", got)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			want := url.Values{
				"client_id":     {"client"},
				"client_secret": {"secret"},
				"code":          {"code-123"},
				"redirect_uri":  {"https://synfactory.example/auth/callback"},
			}
			if r.Form.Encode() != want.Encode() {
				t.Fatalf("token form = %q, want %q", r.Form.Encode(), want.Encode())
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"oauth-token"}`))
		case "/user":
			if r.Method != http.MethodGet {
				t.Fatalf("user method = %s", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer oauth-token" {
				t.Fatalf("authorization = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":12345,"login":"octocat","name":"The Octocat","email":"octocat@example.com"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	identity, err := (GitHubOAuthProvider{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL + "/token",
		UserURL:      server.URL + "/user",
		Client:       server.Client(),
	}).Exchange(context.Background(), "code-123", "https://synfactory.example/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Provider != "github" || identity.Subject != "12345" || identity.Name != "The Octocat" || identity.Email != "octocat@example.com" {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestGitHubOAuthProviderExchangeFallsBackToLoginName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/token") {
			_, _ = w.Write([]byte(`{"access_token":"oauth-token"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":42,"login":"octocat","name":""}`))
	}))
	defer server.Close()

	identity, err := (GitHubOAuthProvider{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL + "/token",
		UserURL:      server.URL + "/user",
		Client:       server.Client(),
	}).Exchange(context.Background(), "code", "")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Name != "octocat" {
		t.Fatalf("name = %q", identity.Name)
	}
}

func TestGitHubOAuthProviderExchangeFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "provider error", body: `{"error":"bad_verification_code"}`},
		{name: "missing token", body: `{}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, err := (GitHubOAuthProvider{
				ClientID:     "client",
				ClientSecret: "secret",
				TokenURL:     server.URL,
				Client:       server.Client(),
			}).Exchange(context.Background(), "code", "")
			if err == nil {
				t.Fatal("expected exchange error")
			}
		})
	}
}

func TestGitHubOAuthProviderRejectsMissingInputs(t *testing.T) {
	if _, err := (GitHubOAuthProvider{}).Exchange(context.Background(), "", ""); err == nil {
		t.Fatal("expected missing-code error")
	}
	if _, err := (GitHubOAuthProvider{}).Exchange(context.Background(), "code", ""); err == nil {
		t.Fatal("expected missing-client-credentials error")
	}
}
