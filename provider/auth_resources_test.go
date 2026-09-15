package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdditionalResourceTypesUseTheirOwnScopedTokens(t *testing.T) {
	tokens := map[string]int{}
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/oauth/token" {
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			scope := r.PostForm.Get("scope")
			if scope != "resources:read resources:write" && scope != "social-providers:read social-providers:write" {
				t.Errorf("unexpected scopes: %s", scope)
			}
			if r.PostForm.Get("resource") != baseURL+"/api" {
				t.Error("wrong audience")
			}
			tokens[scope]++
			if _, err := fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":300}`, scope); err != nil {
				t.Error(err)
			}
			return
		}
		want := "Bearer resources:read resources:write"
		if r.URL.Path == "/api/v1/realms/staging/social-providers/provider-id" {
			want = "Bearer social-providers:read social-providers:write"
		}
		if r.Header.Get("Authorization") != want {
			t.Errorf("wrong token for %s", r.URL.Path)
		}
		w.WriteHeader(404)
		if _, err := fmt.Fprint(w, `{"message":"missing"}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	config := Config{BaseURL: baseURL, ClientID: "id", ClientSecret: "secret"}
	if err := config.Configure(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Fatal("configuration eagerly fetched tokens")
	}
	for range 2 {
		if _, err := config.resources.V1RealmsResourcesShowWithResponse(t.Context(), "staging", "resource-id"); err != nil {
			t.Fatal(err)
		}
		if _, err := config.socialProviders.V1RealmsSocialProvidersShowWithResponse(t.Context(), "staging", "provider-id"); err != nil {
			t.Fatal(err)
		}
	}
	if len(tokens) != 2 || tokens["resources:read resources:write"] != 1 || tokens["social-providers:read social-providers:write"] != 1 {
		t.Fatalf("scoped tokens were not cached independently: %v", tokens)
	}
}
