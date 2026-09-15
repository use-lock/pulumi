package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

const clientJSON = `{"client_id":"client-123","realm":"staging","name":"Service","token_endpoint_auth_method":"client_secret_basic","grant_types":["client_credentials"],"redirect_uris":[],"post_logout_redirect_uris":[],"consent_required":false,"backchannel_logout_uri":null,"confidential":true,"scopes":["openid"],"revoked":false}`

func clientInputs() property.Map {
	return property.NewMap(map[string]property.Value{
		"realm": property.New("staging"), "name": property.New("Service"),
		"tokenEndpointAuthMethod": property.New("client_secret_basic"),
		"grantTypes":              property.New(property.NewArray([]property.Value{property.New("client_credentials")})),
		"consentRequired":         property.New(false),
	})
}

func TestClientLifecyclePreservesSecretAndClearsOptionalFields(t *testing.T) {
	requests := 0
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing bearer token")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/realms/staging/clients":
			w.WriteHeader(201)
			if _, err := fmt.Fprintf(w, `{"data":{"client":%s,"credentials":{"client_id":"client-123","secret":"created-secret"}}}`, clientJSON); err != nil {
				t.Error(err)
			}
		case "GET /api/v1/realms/staging/clients/client-123":
			if _, err := fmt.Fprintf(w, `{"data":%s}`, clientJSON); err != nil {
				t.Error(err)
			}
		case "PATCH /api/v1/realms/staging/clients/client-123":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if value, exists := body["backchannel_logout_uri"]; !exists || value != nil {
				t.Errorf("expected explicit null to clear logout URI: %v", body)
			}
			for _, field := range []string{"redirect_uris", "post_logout_redirect_uris"} {
				values, ok := body[field].([]any)
				if !ok || len(values) != 0 {
					t.Errorf("expected empty list for %s: %v", field, body)
				}
			}
			if body["consent_required"] != false {
				t.Errorf("false was not sent: %v", body)
			}
			if _, err := fmt.Fprintf(w, `{"data":%s}`, clientJSON); err != nil {
				t.Error(err)
			}
		case "DELETE /api/v1/realms/staging/clients/client-123":
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	})
	inputs := clientInputs()
	checked, err := server.Check(p.CheckRequest{Urn: urn("Client"), Inputs: inputs})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("check failed: %+v %v", checked, err)
	}
	inputs = checked.Inputs
	preview, err := server.Create(p.CreateRequest{Urn: urn("Client"), Properties: inputs, DryRun: true})
	if err != nil || requests != 0 || preview.ID != "" {
		t.Fatalf("preview failed: %+v %v", preview, err)
	}
	created, err := server.Create(p.CreateRequest{Urn: urn("Client"), Properties: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "staging/client-123" {
		t.Fatalf("unexpected ID: %s", created.ID)
	}
	assertClientSecret(t, created.Properties)
	read, err := server.Read(p.ReadRequest{Urn: urn("Client"), ID: created.ID, Properties: created.Properties, Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	assertClientSecret(t, read.Properties)
	diff, err := server.Diff(p.DiffRequest{Urn: urn("Client"), ID: created.ID, State: read.Properties, Inputs: inputs, OldInputs: inputs})
	if err != nil || diff.HasChanges {
		t.Fatalf("unchanged client has a diff after refresh: %+v, %v", diff, err)
	}
	beforePreview := requests
	if _, err := server.Update(p.UpdateRequest{Urn: urn("Client"), ID: created.ID, State: read.Properties, Inputs: inputs, DryRun: true}); err != nil || requests != beforePreview {
		t.Fatalf("update preview made a request or failed: %v", err)
	}
	updated, err := server.Update(p.UpdateRequest{Urn: urn("Client"), ID: created.ID, State: read.Properties, Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	assertClientSecret(t, updated.Properties)
	if err := server.Delete(p.DeleteRequest{Urn: urn("Client"), ID: created.ID, Properties: updated.Properties}); err != nil {
		t.Fatal(err)
	}
}

func assertClientSecret(t *testing.T, properties property.Map) {
	t.Helper()
	secret := properties.Get("clientSecret")
	if !secret.Secret() || secret.AsString() != "created-secret" {
		t.Fatal("client secret was lost or is not marked secret")
	}
}

func TestClientImportAndReplacement(t *testing.T) {
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/realms/staging/clients/client-123" {
			t.Errorf("unexpected import request: %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"data":%s}`, clientJSON); err != nil {
			t.Error(err)
		}
	})
	read, err := server.Read(p.ReadRequest{Urn: urn("Client"), ID: "staging/client-123"})
	if err != nil || read.ID != "staging/client-123" || read.Inputs.Get("realm").AsString() != "staging" {
		t.Fatalf("import failed: %+v %v", read, err)
	}
	if !read.Properties.Get("clientSecret").IsNull() {
		t.Fatal("import invented a client secret")
	}
	for key, value := range map[string]string{"realm": "production", "tokenEndpointAuthMethod": "none"} {
		inputs := read.Inputs.Set(key, property.New(value))
		diff, err := server.Diff(p.DiffRequest{Urn: urn("Client"), ID: read.ID, State: read.Properties, Inputs: inputs, OldInputs: read.Inputs})
		if err != nil || diff.DetailedDiff[key].Kind != p.UpdateReplace {
			t.Fatalf("%s should replace the client: %+v %v", key, diff, err)
		}
	}
}

func TestMissingResourcesAndAPIErrors(t *testing.T) {
	for _, status := range []int{200, 401, 403, 404, 422, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if _, err := io.WriteString(w, `{"message":"denied","error":"invalid_token"}`); err != nil {
					t.Error(err)
				}
			})
			for _, item := range []struct{ kind, id string }{{"Realm", "staging"}, {"Client", "staging/client-123"}} {
				read, err := server.Read(p.ReadRequest{Urn: urn(item.kind), ID: item.id})
				if status == 404 {
					if err != nil || read.ID != "" {
						t.Fatalf("missing %s should clear state: %+v %v", item.kind, read, err)
					}
				} else if err == nil {
					t.Fatalf("read %s swallowed HTTP %d", item.kind, status)
				}
				var state property.Map
				if item.kind == "Client" {
					state = clientInputs().
						Set("clientId", property.New("client-123")).
						Set("confidential", property.New(true)).
						Set("scopes", property.New(property.NewArray([]property.Value{}))).
						Set("revoked", property.New(false))
				} else {
					state = property.NewMap(map[string]property.Value{
						"slug": property.New("staging"), "name": property.New("Staging"),
						"domain": property.New("staging.example"), "issuer": property.New("https://staging.example"),
						"host": property.New("staging.example"), "domainStatus": property.New("pending"),
						"master": property.New(false),
					})
				}
				err = server.Delete(p.DeleteRequest{Urn: urn(item.kind), ID: item.id, Properties: state})
				if (status == 404) != (err == nil) {
					t.Fatalf("unexpected delete %s result for HTTP %d: %v", item.kind, status, err)
				}
			}
		})
	}
}
