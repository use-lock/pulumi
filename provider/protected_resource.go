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
	"github.com/use-lock/client-go/management"
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
	scopes := make([]management.CreateResourceScopeData, 0, len(args.Scopes))
	for _, value := range slices.Sorted(maps.Keys(args.Scopes)) {
		scopes = append(scopes, management.CreateResourceScopeData{Value: value, Description: scopeDescription(args.Scopes[value].Description)})
	}
	response, err := infer.GetConfig[Config](ctx).resources.CreateResource(ctx, args.Realm, management.CreateResourceData{Identifier: args.Identifier, Name: args.Name, Scopes: &scopes})
	if err != nil {
		return infer.CreateResponse[ProtectedResourceState]{}, requestError("create protected resource", err)
	}
	if response.Data.ID == "" || response.Data.Realm != args.Realm {
		return infer.CreateResponse[ProtectedResourceState]{}, apiError("create protected resource", http.StatusCreated)
	}
	data := response.Data
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
	current, err := client.GetResource(ctx, realm, id)
	if err != nil {
		return infer.UpdateResponse[ProtectedResourceState]{}, requestError("update protected resource", err)
	}
	if current.Data.ID != id || current.Data.Realm != realm {
		return infer.UpdateResponse[ProtectedResourceState]{}, apiError("read protected resource before update", http.StatusOK)
	}
	args := req.Inputs
	scopes := make([]management.ResourceScopeChangeData, 0, len(args.Scopes)+len(current.Data.Scopes))
	for _, value := range slices.Sorted(maps.Keys(args.Scopes)) {
		scopes = append(scopes, management.ResourceScopeChangeData{Value: value, Description: scopeDescription(args.Scopes[value].Description)})
	}
	for _, scope := range current.Data.Scopes {
		if _, desired := args.Scopes[scope.Value]; !desired {
			remove := true
			scopes = append(scopes, management.ResourceScopeChangeData{Value: scope.Value, Delete: &remove})
		}
	}
	response, err := client.PatchResource(ctx, realm, id, management.UpdateResourceData{Identifier: &args.Identifier, Name: &args.Name, Scopes: &scopes})
	if err != nil {
		return infer.UpdateResponse[ProtectedResourceState]{}, requestError("update protected resource", err)
	}
	if response.Data.ID != id || response.Data.Realm != realm {
		return infer.UpdateResponse[ProtectedResourceState]{}, apiError("update protected resource", http.StatusOK)
	}
	return infer.UpdateResponse[ProtectedResourceState]{Output: protectedResourceState(response.Data)}, nil
}

func (ProtectedResource) Read(ctx context.Context, req infer.ReadRequest[ProtectedResourceArgs, ProtectedResourceState]) (infer.ReadResponse[ProtectedResourceArgs, ProtectedResourceState], error) {
	realm, id, err := protectedResourceParts(req.ID)
	if err != nil {
		return infer.ReadResponse[ProtectedResourceArgs, ProtectedResourceState]{}, err
	}
	response, err := infer.GetConfig[Config](ctx).resources.GetResource(ctx, realm, id)
	if isNotFound(err) {
		return infer.ReadResponse[ProtectedResourceArgs, ProtectedResourceState]{}, nil
	}
	if err != nil {
		return infer.ReadResponse[ProtectedResourceArgs, ProtectedResourceState]{}, requestError("read protected resource", err)
	}
	if response.Data.ID != id || response.Data.Realm != realm {
		return infer.ReadResponse[ProtectedResourceArgs, ProtectedResourceState]{}, apiError("read protected resource", http.StatusOK)
	}
	state := protectedResourceState(response.Data)
	return infer.ReadResponse[ProtectedResourceArgs, ProtectedResourceState]{ID: req.ID, Inputs: state.ProtectedResourceArgs, State: state}, nil
}

func (ProtectedResource) Delete(ctx context.Context, req infer.DeleteRequest[ProtectedResourceState]) (infer.DeleteResponse, error) {
	realm, id, err := protectedResourceParts(req.ID)
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	_, err = infer.GetConfig[Config](ctx).resources.DeleteResource(ctx, realm, id)
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, requestError("delete protected resource", err)
	}
	return infer.DeleteResponse{}, nil
}

func protectedResourceState(data management.ResourceData) ProtectedResourceState {
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
