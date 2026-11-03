package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/archive"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/asset"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

const themedRealmResponse = `{"data":{"slug":"staging","name":"Staging","domain":"staging.example","host":"staging.example","issuer":"https://staging.example","domain_status":"pending","master":false,"theme":{"appearance":"dark","values":{"--lt-primary":"#3366cc","logo-url":"asset:logo.svg","--lt-bg":"#101418"},"font_faces":[{"family":"Example","file":"asset:text.woff2","weight":400,"style":"normal"}]}}}`

func realmInputs(theme map[string]property.Value) property.Map {
	values := map[string]property.Value{"slug": property.New("staging"), "name": property.New("Staging"), "domain": property.New("staging.example")}
	if theme != nil {
		values["theme"] = property.New(property.NewMap(theme))
	}
	return property.NewMap(values)
}

func realmStateFor(inputs property.Map) property.Map {
	values := inputs.AsMap()
	values["issuer"] = property.New("https://staging.example")
	values["host"] = property.New("staging.example")
	values["domainStatus"] = property.New("pending")
	values["master"] = property.New(false)
	return property.NewMap(values)
}

func TestRealmThemeIsSentAndOnlyManagedValuesReachTheState(t *testing.T) {
	var sent map[string]any
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		if _, err := io.WriteString(w, themedRealmResponse); err != nil {
			t.Error(err)
		}
	})
	created, err := server.Create(p.CreateRequest{Urn: urn("Realm"), Properties: realmInputs(map[string]property.Value{
		"appearance": property.New("dark"),
		"values":     property.New(property.NewMap(map[string]property.Value{"--lt-primary": property.New("#3366cc"), "logo-url": property.New("asset:logo.svg")})),
		"fontFaces": property.New(property.NewArray([]property.Value{property.New(property.NewMap(map[string]property.Value{
			"family": property.New("Example"), "file": property.New("asset:text.woff2"), "weight": property.New(400.0), "style": property.New("normal"),
		}))})),
	})})
	if err != nil {
		t.Fatal(err)
	}
	wantBody := map[string]any{
		"appearance": "dark",
		"values":     map[string]any{"--lt-primary": "#3366cc", "logo-url": "asset:logo.svg"},
		"font_faces": []any{map[string]any{"family": "Example", "file": "asset:text.woff2", "weight": 400.0, "style": "normal"}},
	}
	if !reflect.DeepEqual(sent["theme"], wantBody) {
		t.Fatalf("unexpected theme payload: %#v", sent["theme"])
	}
	values := created.Properties.Get("theme").AsMap().Get("values").AsMap()
	if values.Get("--lt-primary").AsString() != "#3366cc" || values.Len() != 2 {
		t.Fatalf("a value the inputs do not manage reached the state: %+v", values)
	}
}

func TestRealmUpdateResetsWhatTheInputsNoLongerManage(t *testing.T) {
	var sent map[string]any
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, themedRealmResponse); err != nil {
			t.Error(err)
		}
	})
	previous := realmStateFor(realmInputs(map[string]property.Value{
		"appearance": property.New("dark"),
		"values":     property.New(property.NewMap(map[string]property.Value{"--lt-primary": property.New("#3366cc"), "logo-url": property.New("asset:logo.svg")})),
		"fontFaces":  property.New(property.NewArray(nil)),
	}))
	_, err := server.Update(p.UpdateRequest{Urn: urn("Realm"), ID: "staging", State: previous, Inputs: realmInputs(map[string]property.Value{
		"values": property.New(property.NewMap(map[string]property.Value{"--lt-primary": property.New("#3366cc")})),
	})})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"appearance": "system", "font_faces": []any{}, "values": map[string]any{"--lt-primary": "#3366cc", "logo-url": nil}}
	if !reflect.DeepEqual(sent["theme"], want) {
		t.Fatalf("unexpected reset payload: %#v", sent["theme"])
	}
}

func TestRealmUpdateSendsNoThemeWithoutOne(t *testing.T) {
	var sent map[string]any
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, themedRealmResponse); err != nil {
			t.Error(err)
		}
	})
	if _, err := server.Update(p.UpdateRequest{Urn: urn("Realm"), ID: "staging", State: realmStateFor(realmInputs(nil)), Inputs: realmInputs(nil)}); err != nil {
		t.Fatal(err)
	}
	if _, exists := sent["theme"]; exists {
		t.Fatalf("an unmanaged theme was sent: %#v", sent["theme"])
	}
}

func themeAssetInputs(t *testing.T, text string) property.Map {
	t.Helper()
	source, err := asset.FromText(text)
	if err != nil {
		t.Fatal(err)
	}
	return property.NewMap(map[string]property.Value{"realm": property.New("staging"), "name": property.New("logo.svg"), "source": property.New(source)})
}

func TestRealmThemeAssetLifecycle(t *testing.T) {
	calls, uploaded := 0, ""
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/realms/staging/theme-assets/logo.svg" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPut {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			uploaded = string(body)
			if r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Errorf("unexpected content type: %s", r.Header.Get("Content-Type"))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"data":{"name":"logo.svg","url":"https://staging.example/theme-assets/logo.svg?v=abc","mime_type":"image/svg+xml","size":6,"sha256":"abc"}}`); err != nil {
			t.Error(err)
		}
	})
	inputs := themeAssetInputs(t, "<svg/>")
	preview, err := server.Create(p.CreateRequest{Urn: urn("RealmThemeAsset"), Properties: inputs, DryRun: true})
	if err != nil || calls != 0 || preview.ID != "" {
		t.Fatalf("preview made a request or failed: %+v, %v, calls=%d", preview, err, calls)
	}
	created, err := server.Create(p.CreateRequest{Urn: urn("RealmThemeAsset"), Properties: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "staging/logo.svg" || uploaded != "<svg/>" || created.Properties.Get("sha256").AsString() != "abc" || created.Properties.Get("mimeType").AsString() != "image/svg+xml" {
		t.Fatalf("unexpected asset: %+v uploaded=%q", created, uploaded)
	}
	if _, err := server.Update(p.UpdateRequest{Urn: urn("RealmThemeAsset"), ID: created.ID, State: created.Properties, Inputs: themeAssetInputs(t, "<svg></svg>")}); err != nil {
		t.Fatal(err)
	}
	if uploaded != "<svg></svg>" {
		t.Fatalf("the changed contents were not uploaded: %q", uploaded)
	}
	read, err := server.Read(p.ReadRequest{Urn: urn("RealmThemeAsset"), ID: created.ID, Inputs: inputs, Properties: created.Properties})
	if err != nil || read.ID != created.ID || read.Properties.Get("url").AsString() != "https://staging.example/theme-assets/logo.svg?v=abc" {
		t.Fatalf("read failed: %+v %v", read, err)
	}
	if err := server.Delete(p.DeleteRequest{Urn: urn("RealmThemeAsset"), ID: created.ID, Properties: created.Properties}); err != nil {
		t.Fatal(err)
	}
}

func TestRealmThemeAssetChangedContentsAreADiff(t *testing.T) {
	server := testProvider(t, func(http.ResponseWriter, *http.Request) {})
	state := themeAssetInputs(t, "<svg/>")
	same, err := server.Diff(p.DiffRequest{Urn: urn("RealmThemeAsset"), ID: "staging/logo.svg", State: state, Inputs: themeAssetInputs(t, "<svg/>")})
	if err != nil || same.HasChanges {
		t.Fatalf("unchanged contents produced a diff: %+v %v", same, err)
	}
	changed, err := server.Diff(p.DiffRequest{Urn: urn("RealmThemeAsset"), ID: "staging/logo.svg", State: state, Inputs: themeAssetInputs(t, "<svg></svg>")})
	if err != nil || !changed.HasChanges {
		t.Fatalf("changed contents produced no diff: %+v %v", changed, err)
	}
}

func TestRealmThemeAssetGoneIsNotAnError(t *testing.T) {
	server := testProvider(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	inputs := themeAssetInputs(t, "<svg/>")
	read, err := server.Read(p.ReadRequest{Urn: urn("RealmThemeAsset"), ID: "staging/logo.svg", Inputs: inputs, Properties: inputs})
	if err != nil || read.ID != "" {
		t.Fatalf("a missing file should read as gone: %+v %v", read, err)
	}
	state := inputs.AsMap()
	state["url"], state["mimeType"], state["size"], state["sha256"] = property.New("https://staging.example/x"), property.New("image/svg+xml"), property.New(6.0), property.New("abc")
	if err := server.Delete(p.DeleteRequest{Urn: urn("RealmThemeAsset"), ID: "staging/logo.svg", Properties: property.NewMap(state)}); err != nil {
		t.Fatalf("a missing file should delete cleanly: %v", err)
	}
}

func TestRealmThemeAssetReportsARefusedFile(t *testing.T) {
	server := testProvider(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnprocessableEntity) })
	_, err := server.Create(p.CreateRequest{Urn: urn("RealmThemeAsset"), Properties: themeAssetInputs(t, "<svg/>")})
	if err == nil || !strings.Contains(err.Error(), "422") {
		t.Fatalf("expected the refusal to surface: %v", err)
	}
}

func TestRealmThemeAssetRefusesAnArchive(t *testing.T) {
	server := testProvider(t, func(http.ResponseWriter, *http.Request) { t.Error("an archive must not be uploaded") })
	source, err := archive.FromAssets(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.Create(p.CreateRequest{Urn: urn("RealmThemeAsset"), Properties: property.NewMap(map[string]property.Value{
		"realm": property.New("staging"), "name": property.New("logo.svg"), "source": property.New(source),
	})})
	if err == nil || !strings.Contains(err.Error(), "archive") {
		t.Fatalf("expected the archive to be refused: %v", err)
	}
}

func TestThemeAssetRejectsAMalformedID(t *testing.T) {
	server := testProvider(t, func(http.ResponseWriter, *http.Request) { t.Error("a malformed ID must not reach the API") })
	if _, err := server.Read(p.ReadRequest{Urn: urn("RealmThemeAsset"), ID: "no-separator"}); err == nil {
		t.Fatal("expected an error for an ID without a realm")
	}
}
