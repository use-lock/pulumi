package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

const socialProviderJSON = `{"id":"provider-123","realm":"staging","key":"login","driver":"oidc","enabled":true,"config":{"client_id":"upstream","issuer":"https://issuer.example"},"callback_url":"https://staging.example/social/login/callback"}`

func socialProviderInputs() property.Map {
	return property.NewMap(map[string]property.Value{
		"realm": property.New("staging"), "key": property.New("login"), "driver": property.New("oidc"), "enabled": property.New(true),
		"config": property.New(property.NewMap(map[string]property.Value{
			"clientId": property.New("upstream"), "issuer": property.New("https://issuer.example"), "clientSecret": property.New("old-secret"),
		})),
	})
}

func assertSocialSecret(t *testing.T, state property.Map, field, expected string) {
	t.Helper()
	config := state.Get("config")
	if !config.Secret() || config.AsMap().Get(field).AsString() != expected {
		t.Fatal("social provider credential was lost or not protected as secret")
	}
}

func TestSocialProviderLifecycle(t *testing.T) {
	calls, patches := 0, 0
	current := socialProviderJSON
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing bearer token")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/realms/staging/social-providers":
			var body struct {
				Config      map[string]any
				Driver, Key string
				Enabled     bool
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Config["client_secret"] != "old-secret" || body.Config["client_id"] != "upstream" || body.Config["issuer"] != "https://issuer.example" || body.Driver != "oidc" || body.Key != "login" || !body.Enabled {
				t.Error("incorrect create payload")
			}
			w.WriteHeader(201)
		case "GET /api/v1/realms/staging/social-providers/provider-123":
		case "PATCH /api/v1/realms/staging/social-providers/provider-123":
			patches++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			config := body["config"].(map[string]any)
			if _, exists := body["key"]; exists {
				t.Error("sent immutable key")
			}
			if _, exists := config["issuer"]; exists {
				t.Error("omitted issuer must remain omitted")
			}
			if patches == 1 {
				if _, exists := config["client_secret"]; exists {
					t.Error("omitted secret must remain omitted")
				}
				if config["client_id"] != "changed" || body["enabled"] != false {
					t.Error("public config or enabled update missing")
				}
				current = `{"id":"provider-123","realm":"staging","key":"login","driver":"oidc","enabled":false,"config":{"client_id":"changed","issuer":"https://issuer.example"},"callback_url":"https://staging.example/social/login/callback"}`
			} else if config["client_secret"] != "new-secret" {
				t.Error("replacement secret missing")
			}
		case "DELETE /api/v1/realms/staging/social-providers/provider-123":
			w.WriteHeader(204)
			return
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(500)
			return
		}
		if _, err := fmt.Fprintf(w, `{"data":%s}`, current); err != nil {
			t.Error(err)
		}
	})
	inputs := socialProviderInputs()
	checked, err := server.Check(p.CheckRequest{Urn: urn("SocialProvider"), Inputs: inputs})
	if err != nil || len(checked.Failures) != 0 {
		t.Fatalf("check: %v %+v", err, checked.Failures)
	}
	inputs = checked.Inputs
	preview, err := server.Create(p.CreateRequest{Urn: urn("SocialProvider"), Properties: inputs, DryRun: true})
	if err != nil || calls != 0 || preview.ID != "" {
		t.Fatalf("preview: %v", err)
	}
	created, err := server.Create(p.CreateRequest{Urn: urn("SocialProvider"), Properties: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "staging/provider-123" || created.Properties.Get("providerId").AsString() != "provider-123" {
		t.Fatal("incorrect resource identity")
	}
	assertSocialSecret(t, created.Properties, "clientSecret", "old-secret")
	unmarked := created.Properties.Set("config", property.New(property.NewMap(map[string]property.Value{
		"clientId": property.New("upstream"), "issuer": property.New("https://issuer.example"), "clientSecret": property.New("old-secret"),
	})))
	read, err := server.Read(p.ReadRequest{Urn: urn("SocialProvider"), ID: created.ID, Properties: unmarked, Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	assertSocialSecret(t, read.Properties, "clientSecret", "old-secret")
	assertSocialSecret(t, read.Inputs, "clientSecret", "old-secret")
	for _, config := range []property.Map{
		property.NewMap(map[string]property.Value{"clientId": property.New("upstream")}),
		property.NewMap(map[string]property.Value{"clientId": property.New("upstream"), "clientSecret": property.New("")}),
		property.NewMap(map[string]property.Value{"clientId": property.New("upstream"), "clientSecret": {}}),
	} {
		diff, err := server.Diff(p.DiffRequest{Urn: urn("SocialProvider"), ID: read.ID, State: read.Properties, Inputs: inputs.Set("config", property.New(config)), OldInputs: inputs})
		if err != nil || diff.HasChanges {
			t.Fatalf("omitted public fields/blank secrets must retain values without diff: %+v %v", diff, err)
		}
	}
	updatedInputs := inputs.Set("enabled", property.New(false)).Set("config", property.New(property.NewMap(map[string]property.Value{"clientId": property.New("changed")})))
	before := calls
	if _, err := server.Update(p.UpdateRequest{Urn: urn("SocialProvider"), ID: read.ID, State: read.Properties, Inputs: updatedInputs, DryRun: true}); err != nil || calls != before {
		t.Fatalf("update preview: %v", err)
	}
	updated, err := server.Update(p.UpdateRequest{Urn: urn("SocialProvider"), ID: read.ID, State: read.Properties, Inputs: updatedInputs})
	if err != nil {
		t.Fatal(err)
	}
	assertSocialSecret(t, updated.Properties, "clientSecret", "old-secret")
	if updated.Properties.Get("config").AsMap().Get("issuer").AsString() != "https://issuer.example" {
		t.Fatal("omitted issuer was lost")
	}
	updatedInputs = updatedInputs.Set("config", property.New(updatedInputs.Get("config").AsMap().Set("clientSecret", property.New("new-secret"))))
	updated, err = server.Update(p.UpdateRequest{Urn: urn("SocialProvider"), ID: read.ID, State: updated.Properties, Inputs: updatedInputs})
	if err != nil {
		t.Fatal(err)
	}
	assertSocialSecret(t, updated.Properties, "clientSecret", "new-secret")
	read, err = server.Read(p.ReadRequest{Urn: urn("SocialProvider"), ID: read.ID, Properties: updated.Properties, Inputs: updatedInputs})
	if err != nil {
		t.Fatal(err)
	}
	assertSocialSecret(t, read.Properties, "clientSecret", "new-secret")
	diff, err := server.Diff(p.DiffRequest{Urn: urn("SocialProvider"), ID: read.ID, State: read.Properties, Inputs: updatedInputs, OldInputs: updatedInputs})
	if err != nil || diff.HasChanges {
		t.Fatalf("unexpected diff after refresh: %+v %v", diff, err)
	}
	if err := server.Delete(p.DeleteRequest{Urn: urn("SocialProvider"), ID: read.ID, Properties: read.Properties}); err != nil {
		t.Fatal(err)
	}
}

func TestSocialProviderImportReplacementAndErrors(t *testing.T) {
	status := 200
	calls := 0
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == 200 {
			if _, err := fmt.Fprintf(w, `{"data":%s}`, socialProviderJSON); err != nil {
				t.Error(err)
			}
		} else {
			if _, err := io.WriteString(w, `{"message":"credential-do-not-disclose"}`); err != nil {
				t.Error(err)
			}
		}
	})
	read, err := server.Read(p.ReadRequest{Urn: urn("SocialProvider"), ID: "staging/provider-123"})
	if err != nil {
		t.Fatal(err)
	}
	if !read.Properties.Get("config").AsMap().Get("clientSecret").IsNull() || read.Inputs.Get("realm").AsString() != "staging" {
		t.Fatal("import invented credentials or lost realm")
	}
	for _, test := range []struct {
		key, value  string
		replace     bool
		deleteFirst bool
	}{
		{"realm", "production", true, false}, {"key", "new-login", true, false}, {"driver", "google", true, true},
	} {
		diff, err := server.Diff(p.DiffRequest{Urn: urn("SocialProvider"), ID: read.ID, State: read.Properties, Inputs: read.Inputs.Set(test.key, property.New(test.value)), OldInputs: read.Inputs})
		if err != nil || diff.DetailedDiff[test.key].Kind != p.UpdateReplace || diff.DeleteBeforeReplace != test.deleteFirst {
			t.Fatalf("incorrect %s replacement: %+v %v", test.key, diff, err)
		}
	}
	before := calls
	if _, err := server.Read(p.ReadRequest{Urn: urn("SocialProvider"), ID: "invalid"}); err == nil || calls != before {
		t.Fatal("invalid import ID should fail without HTTP")
	}
	for _, code := range []int{401, 403, 404, 422, 500} {
		status = code
		missing, err := server.Read(p.ReadRequest{Urn: urn("SocialProvider"), ID: read.ID})
		if code == 404 {
			if err != nil || missing.ID != "" {
				t.Fatal("404 should remove resource from state")
			}
		} else if err == nil {
			t.Fatalf("read swallowed HTTP %d", code)
		} else if strings.Contains(err.Error(), "credential-do-not-disclose") {
			t.Fatal("API error disclosed response body")
		}
		err = server.Delete(p.DeleteRequest{Urn: urn("SocialProvider"), ID: read.ID, Properties: read.Properties})
		if (code == 404) != (err == nil) {
			t.Fatalf("unexpected delete result for HTTP %d: %v", code, err)
		}
		if _, err := server.Create(p.CreateRequest{Urn: urn("SocialProvider"), Properties: socialProviderInputs()}); err == nil {
			t.Fatalf("create swallowed HTTP %d", code)
		}
		if _, err := server.Update(p.UpdateRequest{Urn: urn("SocialProvider"), ID: read.ID, State: read.Properties, Inputs: socialProviderInputs()}); err == nil {
			t.Fatalf("update swallowed HTTP %d", code)
		}
	}
}

func TestSocialProviderAppleCredentialsAndUnknownPreview(t *testing.T) {
	calls := 0
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct{ Config map[string]any }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Config["private_key"] != "apple-private-key" || body.Config["team_id"] != "team" || body.Config["key_id"] != "key" {
			t.Error("missing Apple credentials")
		}
		if _, exists := body.Config["client_secret"]; exists {
			t.Error("Apple payload contains client secret")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		if _, err := io.WriteString(w, `{"data":{"id":"apple-id","realm":"staging","key":"login","driver":"apple","enabled":true,"config":{"client_id":"upstream","team_id":"team","key_id":"key"},"callback_url":"https://staging.example/callback"}}`); err != nil {
			t.Error(err)
		}
	})
	config := property.NewMap(map[string]property.Value{"clientId": property.New("upstream"), "teamId": property.New("team"), "keyId": property.New("key"), "privateKey": property.New("apple-private-key")})
	inputs := socialProviderInputs().Set("driver", property.New("apple")).Set("config", property.New(config))
	unknown := inputs.Set("config", property.New(config.Set("privateKey", property.New(property.Computed).WithSecret(true))))
	preview, err := server.Create(p.CreateRequest{Urn: urn("SocialProvider"), Properties: unknown, DryRun: true})
	if err != nil || calls != 0 {
		t.Fatalf("unknown preview failed: %v", err)
	}
	if !preview.Properties.Get("config").Secret() || !preview.Properties.Get("config").HasComputed() {
		t.Fatal("unknown credential preview lost secrecy or computed value")
	}
	created, err := server.Create(p.CreateRequest{Urn: urn("SocialProvider"), Properties: inputs})
	if err != nil {
		t.Fatal(err)
	}
	assertSocialSecret(t, created.Properties, "privateKey", "apple-private-key")
	diff, err := server.Diff(p.DiffRequest{Urn: urn("SocialProvider"), ID: created.ID, State: created.Properties, Inputs: unknown, OldInputs: inputs})
	if err != nil || !diff.HasChanges {
		t.Fatalf("unknown credential must plan a change: %+v %v", diff, err)
	}
	previewUpdate, err := server.Update(p.UpdateRequest{Urn: urn("SocialProvider"), ID: created.ID, State: created.Properties, Inputs: unknown, DryRun: true})
	if err != nil || calls != 1 {
		t.Fatalf("unknown update preview failed: %v", err)
	}
	if !previewUpdate.Properties.Get("config").Secret() || !previewUpdate.Properties.Get("config").HasComputed() {
		t.Fatal("update preview lost unknown secret")
	}
}

func TestSocialProviderBlankAndNullCredentialsRetainState(t *testing.T) {
	for _, driver := range []string{"oidc", "apple"} {
		for _, form := range []string{"omitted", "blank", "null"} {
			t.Run(driver+"/"+form, func(t *testing.T) {
				field, wireField := "clientSecret", "client_secret"
				data := socialProviderJSON
				if driver == "apple" {
					field, wireField = "privateKey", "private_key"
					data = `{"id":"provider-123","realm":"staging","key":"login","driver":"apple","enabled":true,"config":{"client_id":"upstream","team_id":"team","key_id":"key"},"callback_url":"https://staging.example/callback"}`
				}
				server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "PATCH" {
						var body struct{ Config map[string]any }
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Fatal(err)
						}
						if _, exists := body.Config[wireField]; exists {
							t.Error("blank or omitted credentials should not be sent")
						}
					}
					w.Header().Set("Content-Type", "application/json")
					if _, err := fmt.Fprintf(w, `{"data":%s}`, data); err != nil {
						t.Error(err)
					}
				})
				imported, err := server.Read(p.ReadRequest{Urn: urn("SocialProvider"), ID: "staging/provider-123"})
				if err != nil {
					t.Fatal(err)
				}
				state := imported.Properties.Set("config", property.New(imported.Properties.Get("config").AsMap().Set(field, property.New("known-secret"))))
				config := imported.Inputs.Get("config").AsMap().Delete(field)
				if form == "blank" {
					config = config.Set(field, property.New(""))
				}
				if form == "null" {
					config = config.Set(field, property.Value{})
				}
				inputs := imported.Inputs.Set("config", property.New(config))
				updated, err := server.Update(p.UpdateRequest{Urn: urn("SocialProvider"), ID: imported.ID, State: state, Inputs: inputs})
				if err != nil {
					t.Fatal(err)
				}
				assertSocialSecret(t, updated.Properties, field, "known-secret")
				refreshed, err := server.Read(p.ReadRequest{Urn: urn("SocialProvider"), ID: imported.ID, Properties: updated.Properties, Inputs: inputs})
				if err != nil {
					t.Fatal(err)
				}
				assertSocialSecret(t, refreshed.Properties, field, "known-secret")
				diff, err := server.Diff(p.DiffRequest{Urn: urn("SocialProvider"), ID: imported.ID, State: refreshed.Properties, Inputs: inputs, OldInputs: inputs})
				if err != nil || diff.HasChanges {
					t.Fatalf("retained credential causes perpetual diff: %+v %v", diff, err)
				}
			})
		}
	}
}

func TestSocialProviderDiffConfigAndComputedValues(t *testing.T) {
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("diff must not call API") })
	inputs := socialProviderInputs()
	state := inputs.Set("providerId", property.New("provider-123")).Set("callbackUrl", property.New("https://callback.example"))
	for _, name := range []string{"clientId", "issuer", "clientSecret"} {
		next := inputs.Set("config", property.New(inputs.Get("config").AsMap().Set(name, property.New("changed"))))
		diff, err := server.Diff(p.DiffRequest{Urn: urn("SocialProvider"), ID: "staging/provider-123", State: state, Inputs: next, OldInputs: inputs})
		if err != nil || diff.DetailedDiff["config."+name].Kind != p.Update || diff.DeleteBeforeReplace {
			t.Fatalf("incorrect %s update: %+v %v", name, diff, err)
		}
	}
	diff, err := server.Diff(p.DiffRequest{Urn: urn("SocialProvider"), ID: "staging/provider-123", State: state, Inputs: inputs, OldInputs: inputs, IgnoreChanges: []string{"config.clientSecret"}})
	if err != nil || diff.HasChanges {
		t.Fatalf("engine-restored ignored input changed: %+v %v", diff, err)
	}
	for _, name := range []string{"realm", "key", "driver"} {
		diff, err := server.Diff(p.DiffRequest{Urn: urn("SocialProvider"), ID: "staging/provider-123", State: state, Inputs: inputs.Set(name, property.New(property.Computed)), OldInputs: inputs})
		if err != nil || diff.DetailedDiff[name].Kind != p.UpdateReplace || !diff.DeleteBeforeReplace {
			t.Fatalf("unknown identity could conflict and must replace: %+v %v", diff, err)
		}
	}
	diff, err = server.Diff(p.DiffRequest{Urn: urn("SocialProvider"), ID: "staging/provider-123", State: state, Inputs: inputs.Set("config", property.New(property.Computed)), OldInputs: inputs})
	if err != nil || diff.DetailedDiff["config"].Kind != p.Update {
		t.Fatalf("unknown config must update: %+v %v", diff, err)
	}
}

func TestSocialProviderRejectsMalformedSuccess(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":{"id":"different","realm":"elsewhere"}}`} {
		server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == "POST" {
				w.WriteHeader(201)
			}
			if _, err := io.WriteString(w, body); err != nil {
				t.Error(err)
			}
		})
		inputs := socialProviderInputs()
		state := inputs.Set("providerId", property.New("provider-123")).Set("callbackUrl", property.New("https://callback.example"))
		if _, err := server.Read(p.ReadRequest{Urn: urn("SocialProvider"), ID: "staging/provider-123"}); err == nil {
			t.Fatal("read accepted malformed success")
		}
		if _, err := server.Create(p.CreateRequest{Urn: urn("SocialProvider"), Properties: inputs}); err == nil {
			t.Fatal("create accepted malformed success")
		}
		if _, err := server.Update(p.UpdateRequest{Urn: urn("SocialProvider"), ID: "staging/provider-123", State: state, Inputs: inputs}); err == nil {
			t.Fatal("update accepted malformed success")
		}
	}
}
