package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

const protectedResourceID = "staging/1b43027c-7ef1-435f-b029-74409311bcca"

func protectedResourceInputs(scopes map[string]any) property.Map {
	values := map[string]property.Value{"realm": property.New("staging"), "identifier": property.New("/api"), "name": property.New("API")}
	if scopes != nil {
		result := map[string]property.Value{}
		for value, scope := range scopes {
			fields := map[string]property.Value{}
			if description, ok := scope.(string); ok {
				fields["description"] = property.New(description)
			}
			result[value] = property.New(property.NewMap(fields))
		}
		values["scopes"] = property.New(property.NewMap(result))
	}
	return property.NewMap(values)
}

func TestProtectedResourceLifecycleReconcilesActualScopes(t *testing.T) {
	calls := 0
	scopes := map[string]any{"read": "Read", "write": "Write", "edit": "Before"}
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing bearer token")
		}
		expectedPath := "/api/v1/realms/staging/resources"
		if r.Method != "POST" {
			expectedPath += "/1b43027c-7ef1-435f-b029-74409311bcca"
		}
		if r.URL.Path != expectedPath {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		if r.Method == "POST" || r.Method == "PATCH" {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body) != 3 || body["identifier"] != "/api" || body["name"] != "API" {
				t.Errorf("unexpected fields: %v", body)
			}
			changes, ok := body["scopes"].([]any)
			if !ok {
				t.Fatalf("missing scopes: %v", body)
			}
			for _, item := range changes {
				change := item.(map[string]any)
				value := change["value"].(string)
				if change["delete"] == true {
					if _, exists := scopes[value]; !exists {
						t.Errorf("deleted absent scope %s", value)
						w.WriteHeader(422)
						return
					}
					delete(scopes, value)
				} else {
					description, exists := change["description"]
					if !exists {
						t.Errorf("description must be explicit, including null: %v", change)
					}
					scopes[value] = description
				}
			}
			if r.Method == "POST" {
				w.WriteHeader(201)
			}
		}
		rows := []any{}
		for value, description := range scopes {
			rows = append(rows, map[string]any{"value": value, "description": description, "created_at": "2026-09-15T00:00:00Z"})
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "1b43027c-7ef1-435f-b029-74409311bcca", "realm": "staging", "identifier": "/api", "name": "API", "scopes": rows}}); err != nil {
			t.Error(err)
		}
	})
	inputs := protectedResourceInputs(scopes)
	checked, err := server.Check(p.CheckRequest{Urn: urn("ProtectedResource"), Inputs: inputs})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("check: %+v %v", checked, err)
	}
	inputs = checked.Inputs
	preview, err := server.Create(p.CreateRequest{Urn: urn("ProtectedResource"), Properties: inputs, DryRun: true})
	if err != nil || calls != 0 || preview.ID != "" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	created, err := server.Create(p.CreateRequest{Urn: urn("ProtectedResource"), Properties: inputs})
	if err != nil || created.ID != protectedResourceID {
		t.Fatalf("create: %+v %v", created, err)
	}
	read, err := server.Read(p.ReadRequest{Urn: urn("ProtectedResource"), ID: created.ID})
	if err != nil || !read.Inputs.Equals(inputs) {
		t.Fatalf("import: %+v %v", read, err)
	}
	diff, err := server.Diff(p.DiffRequest{Urn: urn("ProtectedResource"), ID: read.ID, State: read.Properties, Inputs: inputs, OldInputs: inputs})
	if err != nil || diff.HasChanges {
		t.Fatalf("ordering must not drift: %+v %v", diff, err)
	}
	replacement, err := server.Diff(p.DiffRequest{Urn: urn("ProtectedResource"), ID: read.ID, State: read.Properties, Inputs: inputs.Set("realm", property.New("other")), OldInputs: inputs})
	if err != nil || replacement.DetailedDiff["realm"].Kind != p.UpdateReplace {
		t.Fatalf("realm replacement: %+v %v", replacement, err)
	}
	delete(scopes, "write")
	scopes["remote"] = "Outside Pulumi"
	desired := map[string]any{"read": nil, "new": "New", "edit": "After"}
	changed := protectedResourceInputs(desired)
	before := calls
	if _, err := server.Update(p.UpdateRequest{Urn: urn("ProtectedResource"), ID: created.ID, State: read.Properties, Inputs: changed, DryRun: true}); err != nil || calls != before {
		t.Fatalf("update preview: %v", err)
	}
	updated, err := server.Update(p.UpdateRequest{Urn: urn("ProtectedResource"), ID: created.ID, State: read.Properties, Inputs: changed})
	if err != nil || !reflect.DeepEqual(scopes, desired) {
		t.Fatalf("reconcile scopes: %v %v", scopes, err)
	}
	empty, err := server.Check(p.CheckRequest{Urn: urn("ProtectedResource"), Inputs: protectedResourceInputs(nil)})
	if err != nil {
		t.Fatal(err)
	}
	updated, err = server.Update(p.UpdateRequest{Urn: urn("ProtectedResource"), ID: created.ID, State: updated.Properties, Inputs: empty.Inputs})
	if err != nil || len(scopes) != 0 {
		t.Fatalf("clear all scopes: %v %v", scopes, err)
	}
	diff, err = server.Diff(p.DiffRequest{Urn: urn("ProtectedResource"), ID: created.ID, State: updated.Properties, Inputs: empty.Inputs, OldInputs: empty.Inputs})
	if err != nil || diff.HasChanges {
		t.Fatalf("empty scopes drift: %+v %v", diff, err)
	}
	if err := server.Delete(p.DeleteRequest{Urn: urn("ProtectedResource"), ID: created.ID, Properties: updated.Properties}); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedResourceFailures(t *testing.T) {
	for _, status := range []int{200, 401, 403, 404, 422, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if _, err := fmt.Fprint(w, `{"message":"secret-must-not-leak"}`); err != nil {
					t.Error(err)
				}
			})
			inputs := protectedResourceInputs(nil)
			state := inputs
			read, err := server.Read(p.ReadRequest{Urn: urn("ProtectedResource"), ID: protectedResourceID})
			if status == 404 {
				if err != nil || read.ID != "" {
					t.Fatalf("read missing: %+v %v", read, err)
				}
			} else {
				assertProtectedResourceError(t, err, status)
			}
			err = server.Delete(p.DeleteRequest{Urn: urn("ProtectedResource"), ID: protectedResourceID, Properties: state})
			if status == 404 {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				assertProtectedResourceError(t, err, status)
			}
			_, err = server.Create(p.CreateRequest{Urn: urn("ProtectedResource"), Properties: inputs})
			assertProtectedResourceError(t, err, status)
			before := calls
			_, err = server.Update(p.UpdateRequest{Urn: urn("ProtectedResource"), ID: protectedResourceID, State: state, Inputs: inputs})
			assertProtectedResourceError(t, err, status)
			if calls != before+1 {
				t.Fatal("failed read must prevent update")
			}
		})
	}
}

func assertProtectedResourceError(t *testing.T, err error, status int) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), "secret-must-not-leak") {
		t.Fatalf("expected sanitized HTTP %d error, got %v", status, err)
	}
}

func TestProtectedResourceUpdateFailure(t *testing.T) {
	for _, status := range []int{401, 403, 404, 422, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "GET" {
					if _, err := fmt.Fprint(w, `{"data":{"id":"1b43027c-7ef1-435f-b029-74409311bcca","realm":"staging","identifier":"/api","name":"API","scopes":[]}}`); err != nil {
						t.Error(err)
					}
					return
				}
				if r.Method != "PATCH" {
					t.Errorf("unexpected method: %s", r.Method)
				}
				w.WriteHeader(status)
				if _, err := fmt.Fprint(w, `{"message":"secret-must-not-leak"}`); err != nil {
					t.Error(err)
				}
			})
			inputs := protectedResourceInputs(nil)
			_, err := server.Update(p.UpdateRequest{Urn: urn("ProtectedResource"), ID: protectedResourceID, State: inputs, Inputs: inputs})
			assertProtectedResourceError(t, err, status)
			if calls != 2 {
				t.Fatalf("expected read and patch, got %d calls", calls)
			}
		})
	}
}

func TestProtectedResourceRejectsMalformedImportIDs(t *testing.T) {
	calls := 0
	server := testProvider(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) })
	for _, id := range []string{"staging", "/resource", "staging/", "staging/resource/extra", "../resource", "staging/.."} {
		if _, err := server.Read(p.ReadRequest{Urn: urn("ProtectedResource"), ID: id}); err == nil {
			t.Errorf("accepted malformed ID %q", id)
		}
	}
	if calls != 0 {
		t.Fatal("malformed IDs must not send requests")
	}
}
