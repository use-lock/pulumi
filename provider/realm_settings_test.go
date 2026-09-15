package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

func TestRealmSettingsRoundTripAndDrift(t *testing.T) {
	settings := map[string]any{
		"access_token_lifetime": 16777217,
		"auto_provision":        false,
		"trusted_clients":       []string{},
		"mfa_requirement":       "never",
		"password_min_length":   12,
	}
	requests := 0
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method == "POST" || r.Method == "PATCH" {
			var body struct {
				Settings map[string]json.RawMessage `json:"settings"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.Settings) != 4 || string(body.Settings["access_token_lifetime"]) != "16777217" || string(body.Settings["auto_provision"]) != "false" || string(body.Settings["trusted_clients"]) != "[]" || string(body.Settings["mfa_requirement"]) != `"never"` {
				t.Errorf("settings were omitted, rounded or expanded: %s", body.Settings)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			w.WriteHeader(201)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"slug": "staging", "name": "Staging", "domain": "staging.example", "host": "staging.example", "issuer": "https://staging.example", "domain_status": "pending", "master": false, "settings": settings,
		}}); err != nil {
			t.Error(err)
		}
	})
	inputSettings := property.NewMap(map[string]property.Value{
		"accessTokenLifetime": property.New(float64(16777217)), "autoProvision": property.New(false),
		"trustedClients": property.New(property.NewArray([]property.Value{})), "mfaRequirement": property.New("never"),
	})
	inputs := property.NewMap(map[string]property.Value{
		"slug": property.New("staging"), "name": property.New("Staging"), "domain": property.New("staging.example"), "settings": property.New(inputSettings),
	})
	preview, err := server.Create(p.CreateRequest{Urn: urn("Realm"), Properties: inputs, DryRun: true})
	if err != nil || requests != 0 || preview.ID != "" {
		t.Fatalf("invalid preview: %+v %v", preview, err)
	}
	created, err := server.Create(p.CreateRequest{Urn: urn("Realm"), Properties: inputs})
	if err != nil {
		t.Fatal(err)
	}
	assertRealmSettings := func(props property.Map) {
		t.Helper()
		actual := props.Get("settings").AsMap()
		if actual.Len() != 4 || actual.Get("accessTokenLifetime").AsNumber() != 16777217 || actual.Get("autoProvision").AsBool() || actual.Get("trustedClients").AsArray().Len() != 0 {
			t.Fatalf("incorrect managed settings: %+v", actual)
		}
	}
	assertRealmSettings(created.Properties)
	read, err := server.Read(p.ReadRequest{Urn: urn("Realm"), ID: created.ID, Properties: created.Properties, Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	assertRealmSettings(read.Properties)
	diff, err := server.Diff(p.DiffRequest{Urn: urn("Realm"), ID: created.ID, State: read.Properties, Inputs: inputs, OldInputs: inputs})
	if err != nil || diff.HasChanges {
		t.Fatalf("unmanaged defaults caused a diff: %+v %v", diff, err)
	}
	settings["auto_provision"] = true
	read, err = server.Read(p.ReadRequest{Urn: urn("Realm"), ID: created.ID, Properties: created.Properties, Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	diff, err = server.Diff(p.DiffRequest{Urn: urn("Realm"), ID: created.ID, State: read.Properties, Inputs: inputs, OldInputs: inputs})
	if err != nil || diff.DetailedDiff["settings.autoProvision"].Kind != p.Update {
		t.Fatalf("managed drift was not detected: %+v %v", diff, err)
	}
	settings["auto_provision"] = false
	updated, err := server.Update(p.UpdateRequest{Urn: urn("Realm"), ID: created.ID, State: read.Properties, Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	assertRealmSettings(updated.Properties)
}

func TestRemovingRealmSettingsRelinquishesManagement(t *testing.T) {
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if _, exists := body["settings"]; exists {
				t.Error("removing settings must not invent server defaults or reset policy")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"data":{"slug":"staging","name":"Staging","domain":"staging.example","host":"staging.example","issuer":"https://staging.example","domain_status":"pending","master":false,"settings":{"auto_provision":true}}}`); err != nil {
			t.Error(err)
		}
	})
	inputs := property.NewMap(map[string]property.Value{"slug": property.New("staging"), "name": property.New("Staging"), "domain": property.New("staging.example")})
	imported, err := server.Read(p.ReadRequest{Urn: urn("Realm"), ID: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	old := imported.Properties.Set("settings", property.New(property.NewMap(map[string]property.Value{"autoProvision": property.New(true)})))
	updated, err := server.Update(p.UpdateRequest{Urn: urn("Realm"), ID: "staging", State: old, Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := server.Read(p.ReadRequest{Urn: urn("Realm"), ID: "staging", Properties: updated.Properties, Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if !refreshed.Properties.Get("settings").IsNull() {
		t.Fatal("refresh re-adopted unmanaged settings")
	}
	diff, err := server.Diff(p.DiffRequest{Urn: urn("Realm"), ID: "staging", State: refreshed.Properties, Inputs: inputs, OldInputs: inputs})
	if err != nil || diff.HasChanges {
		t.Fatalf("settings removal caused a perpetual diff: %+v %v", diff, err)
	}
}

func TestMissingManagedSettingFailsRefresh(t *testing.T) {
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"data":{"slug":"staging","master":false,"settings":{}}}`); err != nil {
			t.Error(err)
		}
	})
	inputs := property.NewMap(map[string]property.Value{"slug": property.New("staging"), "name": property.New("Staging"), "domain": property.New("staging.example"), "settings": property.New(property.NewMap(map[string]property.Value{"autoProvision": property.New(false)}))})
	if _, err := server.Read(p.ReadRequest{Urn: urn("Realm"), ID: "staging", Inputs: inputs}); err == nil {
		t.Fatal("missing managed settings must not silently reset state")
	}
}
