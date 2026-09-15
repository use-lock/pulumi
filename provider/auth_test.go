package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOAuthCredentialsAudienceAndTokenReuse(t *testing.T) {
	for _, method := range []string{"client_secret_basic", "client_secret_post"} {
		t.Run(method, func(t *testing.T) {
			var tokens atomic.Int32
			var issuer string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/oauth/token" {
					tokens.Add(1)
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.PostForm.Get("grant_type") != "client_credentials" {
						t.Error("wrong grant")
					}
					id, secret, basic := r.BasicAuth()
					if method == "client_secret_basic" {
						if !basic || id != url.QueryEscape("id: +") || secret != url.QueryEscape("secret: +&") || r.PostForm.Has("client_secret") {
							t.Error("Basic credentials were not encoded correctly or were duplicated in the form")
						}
					} else if basic || r.PostForm.Get("client_id") != "id: +" || r.PostForm.Get("client_secret") != "secret: +&" {
						t.Error("incorrect form authentication")
					}
					switch r.PostForm.Get("resource") {
					case issuer + "/admin-api":
						if r.PostForm.Get("scope") != "realms:read realms:write" {
							t.Error("incorrect admin scopes")
						}
						if _, err := fmt.Fprint(w, `{"access_token":"admin-token","token_type":"Bearer","expires_in":300}`); err != nil {
							t.Error(err)
						}
					case issuer + "/api":
						if r.PostForm.Get("scope") != "clients:read clients:write" {
							t.Error("incorrect management scopes")
						}
						if _, err := fmt.Fprint(w, `{"access_token":"management-token","token_type":"Bearer","expires_in":300}`); err != nil {
							t.Error(err)
						}
					default:
						t.Error("incorrect audience")
						w.WriteHeader(400)
					}
					return
				}
				want := "Bearer admin-token"
				if strings.Contains(r.URL.Path, "/clients/") {
					want = "Bearer management-token"
				}
				if r.Header.Get("Authorization") != want {
					t.Errorf("wrong bearer token for %s", r.URL.Path)
				}
				w.WriteHeader(404)
				if _, err := fmt.Fprint(w, `{"message":"not found"}`); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			issuer = server.URL
			config := Config{BaseURL: issuer, ClientID: "id: +", ClientSecret: "secret: +&", TokenEndpointAuthMethod: method}
			if err := config.Configure(t.Context()); err != nil {
				t.Fatal(err)
			}
			if tokens.Load() != 0 {
				t.Fatal("Configure made a network request")
			}
			var group sync.WaitGroup
			for range 8 {
				group.Go(func() {
					if _, err := config.admin.V1RealmsShowWithResponse(t.Context(), "staging"); err != nil {
						t.Error(err)
					}
					if _, err := config.management.V1RealmsClientsShowWithResponse(t.Context(), "staging", "client"); err != nil {
						t.Error(err)
					}
				})
			}
			group.Wait()
			if tokens.Load() != 2 {
				t.Fatalf("expected one token per audience, got %d", tokens.Load())
			}
		})
	}
}

func TestTokenRefreshAndFailure(t *testing.T) {
	var tokens int
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, err := fmt.Fprintf(w, `{"access_token":"token-%d","token_type":"Bearer","expires_in":300,"client_secret":"must-not-leak"}`, tokens); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	source, err := newTokenSource(&Config{BaseURL: server.URL, ClientID: "id", ClientSecret: "secret", TokenEndpointAuthMethod: "client_secret_basic"}, server.Client(), server.URL+"/api", "clients:read")
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest("GET", server.URL+"/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Bearer token-1", "Bearer token-2"} {
		if err := source.authorize(t.Context(), request); err != nil {
			t.Fatal(err)
		}
		if request.Header.Get("Authorization") != want {
			t.Fatal("token was not renewed")
		}
		source.expires = time.Now().Add(-time.Second)
	}
	status = 401
	err = source.authorize(t.Context(), request)
	if err == nil || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("unsafe token error: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := tokens
	if err := source.authorize(ctx, request); err == nil || tokens != before {
		t.Fatal("cancelled token request was sent")
	}
}

func TestConfigRejectsInvalidCredentials(t *testing.T) {
	for _, env := range []string{"LOCK_BASE_URL", "LOCK_ACCESS_TOKEN", "LOCK_ADMIN_ACCESS_TOKEN", "LOCK_CLIENT_ID", "LOCK_CLIENT_SECRET", "LOCK_TOKEN_ENDPOINT_AUTH_METHOD"} {
		t.Setenv(env, "")
	}
	for _, config := range []Config{
		{BaseURL: "https://lock.example"},
		{BaseURL: "https://lock.example", ClientID: "id"},
		{BaseURL: "https://lock.example", AccessToken: "token", ClientID: "id", ClientSecret: "secret"},
		{BaseURL: "file:///tmp/lock", AccessToken: "token"},
		{BaseURL: "https://user:password@lock.example", AccessToken: "token"},
		{BaseURL: "https://lock.example?token=secret", AccessToken: "token"},
		{BaseURL: "https://lock.example", ClientID: "id", ClientSecret: "secret", TokenEndpointAuthMethod: "none"},
	} {
		if err := config.Configure(t.Context()); err == nil {
			t.Fatal("invalid configuration was accepted")
		}
	}
}

func TestConfigEnvironmentAndSeparateAccessTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "Bearer admin"
		if strings.Contains(r.URL.Path, "/clients/") {
			want = "Bearer management"
		}
		if r.Header.Get("Authorization") != want {
			t.Error("wrong token")
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	t.Setenv("LOCK_BASE_URL", server.URL)
	t.Setenv("LOCK_ACCESS_TOKEN", "management")
	t.Setenv("LOCK_ADMIN_ACCESS_TOKEN", "admin")
	t.Setenv("LOCK_CLIENT_ID", "")
	t.Setenv("LOCK_CLIENT_SECRET", "")
	config := Config{}
	if err := config.Configure(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := config.admin.V1RealmsShowWithResponse(t.Context(), "staging"); err != nil {
		t.Fatal(err)
	}
	if _, err := config.management.V1RealmsClientsShowWithResponse(t.Context(), "staging", "client"); err != nil {
		t.Fatal(err)
	}
}
