package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/oapi-codegen/nullable"
	"github.com/pulumi/pulumi-go-provider/infer"
	lock "github.com/use-lock/client-go"
)

type Client struct{}

type AuthMethod string

func (AuthMethod) Values() []infer.EnumValue[AuthMethod] {
	return []infer.EnumValue[AuthMethod]{
		{Name: "ClientSecretBasic", Value: "client_secret_basic"},
		{Name: "ClientSecretPost", Value: "client_secret_post"},
		{Name: "None", Value: "none"},
	}
}

type GrantType string

func (GrantType) Values() []infer.EnumValue[GrantType] {
	return []infer.EnumValue[GrantType]{
		{Name: "AuthorizationCode", Value: "authorization_code"},
		{Name: "RefreshToken", Value: "refresh_token"},
		{Name: "ClientCredentials", Value: "client_credentials"},
	}
}

type ClientArgs struct {
	Realm                   string      `pulumi:"realm" provider:"replaceOnChanges"`
	Name                    string      `pulumi:"name"`
	TokenEndpointAuthMethod AuthMethod  `pulumi:"tokenEndpointAuthMethod" provider:"replaceOnChanges"`
	GrantTypes              []GrantType `pulumi:"grantTypes"`
	RedirectURIs            []string    `pulumi:"redirectUris,optional"`
	PostLogoutRedirectURIs  []string    `pulumi:"postLogoutRedirectUris,optional"`
	ConsentRequired         *bool       `pulumi:"consentRequired,optional"`
	BackchannelLogoutURI    string      `pulumi:"backchannelLogoutUri,optional"`
}

type ClientState struct {
	ClientArgs
	ClientID     string   `pulumi:"clientId"`
	ClientSecret *string  `pulumi:"clientSecret,optional" provider:"secret"`
	Confidential bool     `pulumi:"confidential"`
	Scopes       []string `pulumi:"scopes"`
	Revoked      bool     `pulumi:"revoked"`
}

func (Client) WireDependencies(f infer.FieldSelector, _ *ClientArgs, state *ClientState) {
	f.OutputField(&state.ClientSecret).AlwaysSecret()
}

func (r *Client) Annotate(a infer.Annotator) {
	a.SetToken("index", "Client")
	a.Describe(&r, "A realm's OAuth client. Import using realm-slug/client-id. The secret is available at creation and preserved during refresh; importing does not disclose an existing secret.")
}

func (r *ClientArgs) Annotate(a infer.Annotator) {
	a.Describe(&r.Realm, "Realm slug. Changing it replaces the client.")
	a.Describe(&r.TokenEndpointAuthMethod, "Client authentication method. Changing it replaces the client so its credentials remain consistent with Pulumi state.")
	a.Describe(&r.BackchannelLogoutURI, "Back-channel logout endpoint. Omit or set an empty string to clear it.")
	a.SetDefault(&r.ConsentRequired, true)
}

func (Client) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[ClientArgs], error) {
	args, failures, err := infer.DefaultCheck[ClientArgs](ctx, req.NewInputs)
	args.RedirectURIs = nonNil(args.RedirectURIs)
	args.PostLogoutRedirectURIs = nonNil(args.PostLogoutRedirectURIs)
	return infer.CheckResponse[ClientArgs]{Inputs: args, Failures: failures}, err
}

func (Client) Create(ctx context.Context, req infer.CreateRequest[ClientArgs]) (infer.CreateResponse[ClientState], error) {
	if req.DryRun {
		return infer.CreateResponse[ClientState]{Output: ClientState{ClientArgs: req.Inputs}}, nil
	}
	args := req.Inputs
	grants := make([]lock.CreateClientDataGrantTypes, len(args.GrantTypes))
	for i, grant := range args.GrantTypes {
		grants[i] = lock.CreateClientDataGrantTypes(grant)
	}
	redirects, logoutRedirects := nonNil(args.RedirectURIs), nonNil(args.PostLogoutRedirectURIs)
	response, err := infer.GetConfig[Config](ctx).management.V1RealmsClientsStoreWithResponse(ctx, args.Realm, lock.CreateClientData{
		Name: args.Name, TokenEndpointAuthMethod: lock.TokenEndpointAuthMethod(args.TokenEndpointAuthMethod), GrantTypes: grants,
		RedirectUris: &redirects, PostLogoutRedirectUris: &logoutRedirects, ConsentRequired: args.ConsentRequired,
		BackchannelLogoutURI: logoutURI(args.BackchannelLogoutURI),
	})
	if err != nil {
		return infer.CreateResponse[ClientState]{}, err
	}
	if response.JSON201 == nil || response.JSON201.Data.Client.ClientID == "" || response.JSON201.Data.Client.Realm != args.Realm {
		return infer.CreateResponse[ClientState]{}, apiError("create client", response.StatusCode())
	}
	data := response.JSON201.Data
	var secret *string
	if value, err := data.Credentials.Secret.Get(); err == nil {
		secret = &value
	}
	return infer.CreateResponse[ClientState]{ID: args.Realm + "/" + data.Client.ClientID, Output: clientState(data.Client, secret)}, nil
}

func (Client) Update(ctx context.Context, req infer.UpdateRequest[ClientArgs, ClientState]) (infer.UpdateResponse[ClientState], error) {
	if req.DryRun {
		state := req.State
		state.ClientArgs = req.Inputs
		return infer.UpdateResponse[ClientState]{Output: state}, nil
	}
	realm, id, err := clientParts(req.ID)
	if err != nil {
		return infer.UpdateResponse[ClientState]{}, err
	}
	args := req.Inputs
	grants := make([]lock.UpdateClientDataGrantTypes, len(args.GrantTypes))
	for i, grant := range args.GrantTypes {
		grants[i] = lock.UpdateClientDataGrantTypes(grant)
	}
	redirects, logoutRedirects := nonNil(args.RedirectURIs), nonNil(args.PostLogoutRedirectURIs)
	response, err := infer.GetConfig[Config](ctx).management.APIV1RealmsClientsUpdatePatchWithResponse(ctx, realm, id, lock.UpdateClientData{
		Name: &args.Name, GrantTypes: &grants, RedirectUris: &redirects, PostLogoutRedirectUris: &logoutRedirects,
		ConsentRequired: args.ConsentRequired, BackchannelLogoutURI: logoutURI(args.BackchannelLogoutURI),
	})
	if err != nil {
		return infer.UpdateResponse[ClientState]{}, err
	}
	if response.JSON200 == nil || response.JSON200.Data.ClientID != id || response.JSON200.Data.Realm != realm {
		return infer.UpdateResponse[ClientState]{}, apiError("update client", response.StatusCode())
	}
	return infer.UpdateResponse[ClientState]{Output: clientState(response.JSON200.Data, req.State.ClientSecret)}, nil
}

func (Client) Read(ctx context.Context, req infer.ReadRequest[ClientArgs, ClientState]) (infer.ReadResponse[ClientArgs, ClientState], error) {
	realm, id, err := clientParts(req.ID)
	if err != nil {
		return infer.ReadResponse[ClientArgs, ClientState]{}, err
	}
	response, err := infer.GetConfig[Config](ctx).management.V1RealmsClientsShowWithResponse(ctx, realm, id)
	if err != nil {
		return infer.ReadResponse[ClientArgs, ClientState]{}, err
	}
	if response.StatusCode() == http.StatusNotFound {
		return infer.ReadResponse[ClientArgs, ClientState]{}, nil
	}
	if response.JSON200 == nil || response.JSON200.Data.ClientID != id || response.JSON200.Data.Realm != realm {
		return infer.ReadResponse[ClientArgs, ClientState]{}, apiError("read client", response.StatusCode())
	}
	state := clientState(response.JSON200.Data, req.State.ClientSecret)
	return infer.ReadResponse[ClientArgs, ClientState]{ID: req.ID, Inputs: state.ClientArgs, State: state}, nil
}

func (Client) Delete(ctx context.Context, req infer.DeleteRequest[ClientState]) (infer.DeleteResponse, error) {
	realm, id, err := clientParts(req.ID)
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	response, err := infer.GetConfig[Config](ctx).management.V1RealmsClientsDestroyWithResponse(ctx, realm, id)
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	if response.StatusCode() != http.StatusNoContent && response.StatusCode() != http.StatusNotFound {
		return infer.DeleteResponse{}, apiError("delete client", response.StatusCode())
	}
	return infer.DeleteResponse{}, nil
}

func clientState(data lock.ClientData, secret *string) ClientState {
	grants := make([]GrantType, len(data.GrantTypes))
	for i, grant := range data.GrantTypes {
		grants[i] = GrantType(grant)
	}
	logout, _ := data.BackchannelLogoutURI.Get()
	if !data.Confidential {
		secret = nil
	}
	return ClientState{
		ClientArgs: ClientArgs{Realm: data.Realm, Name: data.Name, TokenEndpointAuthMethod: AuthMethod(data.TokenEndpointAuthMethod),
			GrantTypes: grants, RedirectURIs: data.RedirectUris, PostLogoutRedirectURIs: data.PostLogoutRedirectUris,
			ConsentRequired: &data.ConsentRequired, BackchannelLogoutURI: logout},
		ClientID: data.ClientID, ClientSecret: secret, Confidential: data.Confidential, Scopes: nonNil(data.Scopes), Revoked: data.Revoked,
	}
}

func clientParts(id string) (string, string, error) {
	realm, client, ok := strings.Cut(id, "/")
	if !ok || realm == "" || client == "" || strings.Contains(client, "/") || realm == "." || realm == ".." || client == "." || client == ".." {
		return "", "", fmt.Errorf("lock: client ID must have the form realm-slug/client-id")
	}
	return realm, client, nil
}

func logoutURI(value string) nullable.Nullable[string] {
	if value == "" {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(value)
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
