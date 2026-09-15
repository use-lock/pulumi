package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/blang/semver"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

func testProvider(t *testing.T, handler http.HandlerFunc) integration.Server {
	t.Helper()
	api := httptest.NewServer(handler)
	t.Cleanup(api.Close)
	provider, err := New()
	if err != nil {
		t.Fatal(err)
	}
	server, err := integration.NewServer(t.Context(), "lock", semver.MustParse("0.1.0"), integration.WithProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Configure(p.ConfigureRequest{Args: property.NewMap(map[string]property.Value{
		"baseUrl": property.New(api.URL), "accessToken": property.New("test-token"),
	})}); err != nil {
		t.Fatal(err)
	}
	return server
}

func TestRealmImportUpdateAndReplacement(t *testing.T) {
	calls := 0
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/realms/staging" {
			t.Errorf("wrong realm path: %s", r.URL.Path)
		}
		if r.Method == "PATCH" {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["name"] != "Renamed" || body["domain"] != "new.example" || len(body) != 2 {
				t.Errorf("unexpected update: %v", body)
			}
		}
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"data":{"slug":"staging","name":"Renamed","domain":"new.example","host":"new.example","issuer":"https://new.example","domain_status":"pending","master":false}}`); err != nil {
			t.Error(err)
		}
	})
	read, err := server.Read(p.ReadRequest{Urn: urn("Realm"), ID: "staging"})
	if err != nil || read.ID != "staging" || read.Inputs.Get("domain").AsString() != "new.example" {
		t.Fatalf("realm import failed: %+v %v", read, err)
	}
	before := calls
	if _, err := server.Update(p.UpdateRequest{Urn: urn("Realm"), ID: read.ID, State: read.Properties, Inputs: read.Inputs, DryRun: true}); err != nil || calls != before {
		t.Fatalf("update preview failed: %v", err)
	}
	updated, err := server.Update(p.UpdateRequest{Urn: urn("Realm"), ID: read.ID, State: read.Properties, Inputs: read.Inputs})
	if err != nil || updated.Properties.Get("issuer").AsString() != "https://new.example" {
		t.Fatalf("realm update failed: %+v %v", updated, err)
	}
	for _, item := range []struct {
		key, value string
		kind       p.DiffKind
	}{{"name", "Changed", p.Update}, {"domain", "other.example", p.Update}, {"slug", "other", p.UpdateReplace}} {
		diff, err := server.Diff(p.DiffRequest{Urn: urn("Realm"), ID: read.ID, State: read.Properties, Inputs: read.Inputs.Set(item.key, property.New(item.value)), OldInputs: read.Inputs})
		if err != nil || diff.DetailedDiff[item.key].Kind != item.kind {
			t.Fatalf("incorrect %s diff: %+v %v", item.key, diff, err)
		}
		if item.key == "slug" && !diff.DeleteBeforeReplace {
			t.Fatal("same-domain replacement must delete first")
		}
	}
	if err := server.Delete(p.DeleteRequest{Urn: urn("Realm"), ID: read.ID, Properties: read.Properties}); err != nil {
		t.Fatal(err)
	}
}

func TestMasterRealmCannotBeImported(t *testing.T) {
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"data":{"slug":"admin","name":"Admin","domain":null,"host":"lock.example","issuer":"https://lock.example","domain_status":"verified","master":true}}`); err != nil {
			t.Error(err)
		}
	})
	if _, err := server.Read(p.ReadRequest{Urn: urn("Realm"), ID: "admin"}); err == nil {
		t.Fatal("master realm import should fail")
	}
}

func urn(kind string) resource.URN {
	return resource.URN("urn:pulumi:test::test::lock:index:" + kind + "::test")
}

func TestRealmCreateAndPreview(t *testing.T) {
	calls := 0
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/v1/realms" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		if _, err := io.WriteString(w, `{"data":{"slug":"staging","name":"Staging","domain":"staging.example","host":"staging.example","issuer":"https://staging.example","domain_status":"pending","master":false}}`); err != nil {
			t.Error(err)
		}
	})
	inputs := property.NewMap(map[string]property.Value{
		"slug": property.New("staging"), "name": property.New("Staging"), "domain": property.New("staging.example"),
	})
	preview, err := server.Create(p.CreateRequest{Urn: urn("Realm"), Properties: inputs, DryRun: true})
	if err != nil || calls != 0 || preview.ID != "" {
		t.Fatalf("preview made a request or failed: %+v, %v, calls=%d", preview, err, calls)
	}
	created, err := server.Create(p.CreateRequest{Urn: urn("Realm"), Properties: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "staging" || created.Properties.Get("issuer").AsString() != "https://staging.example" || calls != 1 {
		t.Fatalf("unexpected realm: %+v", created)
	}
}
