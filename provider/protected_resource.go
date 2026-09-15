package provider

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/oapi-codegen/nullable"
	"github.com/pulumi/pulumi-go-provider/infer"
	lock "github.com/use-lock/client-go"
)

type ProtectedResource struct{}

type ScopeArgs struct {
	Description *string `pulumi:"description,optional"`
}

type ProtectedResourceArgs struct {
	Realm      string               `pulumi:"realm" provider:"replaceOnChanges"`
	Identifier string               `pulumi:"identifier"`
	Name       string               `pulumi:"name"`
	Scopes     map[string]ScopeArgs `pulumi:"scopes,optional"`
}

type ProtectedResourceState struct {
	ProtectedResourceArgs
}

func (r *ProtectedResource) Annotate(a infer.Annotator) {
	a.SetToken("index", "ProtectedResource")
	a.Describe(&r, "A protected API resource and its authoritative set of scopes. Import using realm-slug/resource-uuid.")
}

func (r *ProtectedResourceArgs) Annotate(a infer.Annotator) {
	a.Describe(&r.Realm, "Realm slug. Changing it replaces the protected resource.")
	a.Describe(&r.Identifier, "A path relative to the realm issuer or an absolute URI identifying the API resource.")
	a.Describe(&r.Scopes, "Authoritative scope definitions keyed by scope value. Omitting scopes or using an empty map removes all scopes, including scopes added outside Pulumi.")
}

func (r *ScopeArgs) Annotate(a infer.Annotator) {
	a.Describe(&r.Description, "Human readable scope description. Omit to clear the existing description.")
}

func (ProtectedResource) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[ProtectedResourceArgs], error) {
	args, failures, err := infer.DefaultCheck[ProtectedResourceArgs](ctx, req.NewInputs)
	if args.Scopes == nil {
		args.Scopes = map[string]ScopeArgs{}
	}
	return infer.CheckResponse[ProtectedResourceArgs]{Inputs: args, Failures: failures}, err
}

func (ProtectedResource) Create(ctx context.Context, req infer.CreateRequest[ProtectedResourceArgs]) (infer.CreateResponse[ProtectedResourceState], error) {
	if req.DryRun {
		return infer.CreateResponse[ProtectedResourceState]{Output: ProtectedResourceState{ProtectedResourceArgs: req.Inputs}}, nil
	}
	args := req.Inputs
	scopes := make([]lock.CreateResourceScopeData, 0, len(args.Scopes))
	for _, value := range slices.Sorted(maps.Keys(args.Scopes)) {
		scopes = append(scopes, lock.CreateResourceScopeData{Value: value, Description: scopeDescription(args.Scopes[value].Description)})
	}
	response, err := infer.GetConfig[Config](ctx).resources.V1RealmsResourcesStoreWithResponse(ctx, args.Realm, lock.CreateResourceData{Identifier: args.Identifier, Name: args.Name, Scopes: &scopes})
	if err != nil {
		return infer.CreateResponse[ProtectedResourceState]{}, err
	}
	if response.JSON201 == nil || response.JSON201.Data.ID == "" || response.JSON201.Data.Realm != args.Realm {
		return infer.CreateResponse[ProtectedResourceState]{}, apiError("create protected resource", response.StatusCode())
	}
	data := response.JSON201.Data
	return infer.CreateResponse[ProtectedResourceState]{ID: args.Realm + "/" + data.ID, Output: protectedResourceState(data)}, nil
}

func (ProtectedResource) Update(ctx context.Context, req infer.UpdateRequest[ProtectedResourceArgs, ProtectedResourceState]) (infer.UpdateResponse[ProtectedResourceState], error) {
	if req.DryRun {
		return infer.UpdateResponse[ProtectedResourceState]{Output: ProtectedResourceState{ProtectedResourceArgs: req.Inputs}}, nil
	}
	realm, id, err := protectedResourceParts(req.ID)
	if err != nil {
		return infer.UpdateResponse[ProtectedResourceState]{}, err
	}
	client := infer.GetConfig[Config](ctx).resources
	current, err := client.V1RealmsResourcesShowWithResponse(ctx, realm, id)
	if err != nil {
		return infer.UpdateResponse[ProtectedResourceState]{}, err
	}
	if current.JSON200 == nil || current.JSON200.Data.ID != id || current.JSON200.Data.Realm != realm {
		return infer.UpdateResponse[ProtectedResourceState]{}, apiError("read protected resource before update", current.StatusCode())
	}
	args := req.Inputs
	scopes := make([]lock.ResourceScopeChangeData, 0, len(args.Scopes)+len(current.JSON200.Data.Scopes))
	for _, value := range slices.Sorted(maps.Keys(args.Scopes)) {
		scopes = append(scopes, lock.ResourceScopeChangeData{Value: value, Description: scopeDescription(args.Scopes[value].Description)})
	}
	for _, scope := range current.JSON200.Data.Scopes {
		if _, desired := args.Scopes[scope.Value]; !desired {
			remove := true
			scopes = append(scopes, lock.ResourceScopeChangeData{Value: scope.Value, Delete: &remove})
		}
	}
	response, err := client.APIV1RealmsResourcesUpdatePatchWithResponse(ctx, realm, id, lock.UpdateResourceData{Identifier: &args.Identifier, Name: &args.Name, Scopes: &scopes})
	if err != nil {
		return infer.UpdateResponse[ProtectedResourceState]{}, err
	}
	if response.JSON200 == nil || response.JSON200.Data.ID != id || response.JSON200.Data.Realm != realm {
		return infer.UpdateResponse[ProtectedResourceState]{}, apiError("update protected resource", response.StatusCode())
	}
	return infer.UpdateResponse[ProtectedResourceState]{Output: protectedResourceState(response.JSON200.Data)}, nil
}

func (ProtectedResource) Read(ctx context.Context, req infer.ReadRequest[ProtectedResourceArgs, ProtectedResourceState]) (infer.ReadResponse[ProtectedResourceArgs, ProtectedResourceState], error) {
	realm, id, err := protectedResourceParts(req.ID)
	if err != nil {
		return infer.ReadResponse[ProtectedResourceArgs, ProtectedResourceState]{}, err
	}
	response, err := infer.GetConfig[Config](ctx).resources.V1RealmsResourcesShowWithResponse(ctx, realm, id)
	if err != nil {
		return infer.ReadResponse[ProtectedResourceArgs, ProtectedResourceState]{}, err
	}
	if response.StatusCode() == http.StatusNotFound {
		return infer.ReadResponse[ProtectedResourceArgs, ProtectedResourceState]{}, nil
	}
	if response.JSON200 == nil || response.JSON200.Data.ID != id || response.JSON200.Data.Realm != realm {
		return infer.ReadResponse[ProtectedResourceArgs, ProtectedResourceState]{}, apiError("read protected resource", response.StatusCode())
	}
	state := protectedResourceState(response.JSON200.Data)
	return infer.ReadResponse[ProtectedResourceArgs, ProtectedResourceState]{ID: req.ID, Inputs: state.ProtectedResourceArgs, State: state}, nil
}

func (ProtectedResource) Delete(ctx context.Context, req infer.DeleteRequest[ProtectedResourceState]) (infer.DeleteResponse, error) {
	realm, id, err := protectedResourceParts(req.ID)
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	response, err := infer.GetConfig[Config](ctx).resources.V1RealmsResourcesDestroyWithResponse(ctx, realm, id)
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	if response.StatusCode() != http.StatusNoContent && response.StatusCode() != http.StatusNotFound {
		return infer.DeleteResponse{}, apiError("delete protected resource", response.StatusCode())
	}
	return infer.DeleteResponse{}, nil
}

func protectedResourceState(data lock.ResourceData) ProtectedResourceState {
	scopes := make(map[string]ScopeArgs, len(data.Scopes))
	for _, scope := range data.Scopes {
		args := ScopeArgs{}
		if description, err := scope.Description.Get(); err == nil {
			args.Description = &description
		}
		scopes[scope.Value] = args
	}
	return ProtectedResourceState{ProtectedResourceArgs: ProtectedResourceArgs{Realm: data.Realm, Identifier: data.Identifier, Name: data.Name, Scopes: scopes}}
}

func protectedResourceParts(id string) (string, string, error) {
	realm, resource, ok := strings.Cut(id, "/")
	if !ok || realm == "" || resource == "" || strings.Contains(resource, "/") || realm == "." || realm == ".." || resource == "." || resource == ".." {
		return "", "", fmt.Errorf("lock: protected resource ID must have the form realm-slug/resource-uuid")
	}
	return realm, resource, nil
}

func scopeDescription(description *string) nullable.Nullable[string] {
	if description == nil {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(*description)
}
