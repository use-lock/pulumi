package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/oapi-codegen/nullable"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	lock "github.com/use-lock/client-go"
)

type SocialProvider struct{}

type SocialProviderDriver string

func (SocialProviderDriver) Values() []infer.EnumValue[SocialProviderDriver] {
	return []infer.EnumValue[SocialProviderDriver]{
		{Name: "Google", Value: "google"}, {Name: "GitHub", Value: "github"},
		{Name: "Apple", Value: "apple"}, {Name: "OIDC", Value: "oidc"},
	}
}

type SocialProviderConfig struct {
	ClientID     string  `pulumi:"clientId"`
	ClientSecret *string `pulumi:"clientSecret,optional" provider:"secret"`
	Issuer       *string `pulumi:"issuer,optional"`
	TeamID       *string `pulumi:"teamId,optional"`
	KeyID        *string `pulumi:"keyId,optional"`
	PrivateKey   *string `pulumi:"privateKey,optional" provider:"secret"`
}

type SocialProviderArgs struct {
	Realm   string               `pulumi:"realm" provider:"replaceOnChanges"`
	Key     string               `pulumi:"key" provider:"replaceOnChanges"`
	Driver  SocialProviderDriver `pulumi:"driver" provider:"replaceOnChanges"`
	Enabled bool                 `pulumi:"enabled"`
	Config  SocialProviderConfig `pulumi:"config"`
}

type SocialProviderState struct {
	SocialProviderArgs
	ProviderID  string `pulumi:"providerId"`
	CallbackURL string `pulumi:"callbackUrl"`
}

func (r *SocialProvider) Annotate(a infer.Annotator) {
	a.SetToken("index", "SocialProvider")
	a.Describe(&r, "A realm social login provider. Import using realm-slug/provider-UUID. The API never returns credentials; imports cannot recover them and refresh preserves credentials already in state.")
}

func (r *SocialProviderArgs) Annotate(a infer.Annotator) {
	a.Describe(&r.Realm, "Realm slug. Changing it replaces the provider.")
	a.Describe(&r.Key, "Immutable key used in login and callback URLs. Changing it replaces the provider.")
	a.Describe(&r.Driver, "Upstream driver. Changing it replaces the provider; replacement deletes first when the realm and key are unchanged.")
	a.Describe(&r.Config, "Driver configuration. The entire output object is secret to protect retained write-only credentials. Omitted optional values retain their stored values on update.")
}

func (r *SocialProviderConfig) Annotate(a infer.Annotator) {
	a.Describe(&r.ClientID, "Upstream OAuth client ID, required for every driver.")
	a.Describe(&r.ClientSecret, "Write-only Google, GitHub or OIDC secret. Required on creation. Omitted, null or blank values retain the previous secret, including in state; credentials cannot be cleared through this API.")
	a.Describe(&r.PrivateKey, "Write-only Apple private key. Required for Apple on creation. Omitted, null or blank values retain the previous key, including in state; credentials cannot be cleared through this API.")
	a.Describe(&r.Issuer, "Required for OIDC. Omission on update retains the stored issuer.")
	a.Describe(&r.TeamID, "Required for Apple. Omission on update retains the stored team ID.")
	a.Describe(&r.KeyID, "Required for Apple. Omission on update retains the stored key ID.")
}

func (SocialProvider) WireDependencies(f infer.FieldSelector, _ *SocialProviderArgs, state *SocialProviderState) {
	f.OutputField(&state.Config).AlwaysSecret()
}

func socialProviderDiff(req p.DiffRequest) (p.DiffResponse, error) {
	diff := map[string]p.PropertyDiff{}
	replace := false
	for _, name := range []string{"realm", "key", "driver", "enabled"} {
		old, next := req.State.Get(name), req.Inputs.Get(name)
		if next.HasComputed() || !old.WithSecret(false).Equals(next.WithSecret(false)) {
			kind := p.Update
			if name != "enabled" {
				kind = p.UpdateReplace
				replace = true
			}
			diff[name] = p.PropertyDiff{Kind: kind, InputDiff: true}
		}
	}
	old, next := req.State.Get("config"), req.Inputs.Get("config")
	if next.IsComputed() {
		diff["config"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	} else if next.IsMap() {
		oldConfig := property.Map{}
		if old.IsMap() {
			oldConfig = old.AsMap()
		}
		for _, name := range []string{"clientId", "issuer", "teamId", "keyId", "clientSecret", "privateKey"} {
			previous, value := oldConfig.Get(name), next.AsMap().Get(name)
			if name != "clientId" && value.IsNull() {
				continue
			}
			if (name == "clientSecret" || name == "privateKey") && value.IsString() && strings.TrimSpace(value.AsString()) == "" {
				continue
			}
			if !value.HasComputed() && previous.WithSecret(false).Equals(value.WithSecret(false)) {
				continue
			}
			kind := p.Update
			if previous.IsNull() {
				kind = p.Add
			}
			if value.IsNull() {
				kind = p.Delete
			}
			diff["config."+name] = p.PropertyDiff{Kind: kind, InputDiff: true}
		}
	}
	sameRealm := req.Inputs.Get("realm").HasComputed() || req.State.Get("realm").WithSecret(false).Equals(req.Inputs.Get("realm").WithSecret(false))
	sameKey := req.Inputs.Get("key").HasComputed() || req.State.Get("key").WithSecret(false).Equals(req.Inputs.Get("key").WithSecret(false))
	return p.DiffResponse{HasChanges: len(diff) > 0, DetailedDiff: diff, DeleteBeforeReplace: replace && sameRealm && sameKey}, nil
}

func (SocialProvider) Create(ctx context.Context, req infer.CreateRequest[SocialProviderArgs]) (infer.CreateResponse[SocialProviderState], error) {
	if req.DryRun {
		return infer.CreateResponse[SocialProviderState]{Output: SocialProviderState{SocialProviderArgs: req.Inputs}}, nil
	}
	args := req.Inputs
	response, err := infer.GetConfig[Config](ctx).socialProviders.V1RealmsSocialProvidersStoreWithResponse(ctx, args.Realm, lock.CreateSocialProviderData{
		Key: args.Key, Driver: lock.SocialProviderDriver(args.Driver), Enabled: args.Enabled, Config: socialCredentials(args.Config),
	})
	if err != nil {
		return infer.CreateResponse[SocialProviderState]{}, err
	}
	if response.JSON201 == nil || response.JSON201.Data.ID == "" || response.JSON201.Data.Realm != args.Realm || response.JSON201.Data.Key != args.Key || string(response.JSON201.Data.Driver) != string(args.Driver) {
		return infer.CreateResponse[SocialProviderState]{}, apiError("create social provider", response.StatusCode())
	}
	state := socialProviderState(response.JSON201.Data, args.Config)
	return infer.CreateResponse[SocialProviderState]{ID: args.Realm + "/" + state.ProviderID, Output: state}, nil
}

func (SocialProvider) Update(ctx context.Context, req infer.UpdateRequest[SocialProviderArgs, SocialProviderState]) (infer.UpdateResponse[SocialProviderState], error) {
	config := retainSocialConfig(req.Inputs.Config, req.State.Config)
	if req.DryRun {
		state := req.State
		state.SocialProviderArgs = req.Inputs
		state.Config = config
		return infer.UpdateResponse[SocialProviderState]{Output: state}, nil
	}
	realm, id, err := socialProviderParts(req.ID)
	if err != nil {
		return infer.UpdateResponse[SocialProviderState]{}, err
	}
	credentials := socialCredentials(req.Inputs.Config)
	response, err := infer.GetConfig[Config](ctx).socialProviders.APIV1RealmsSocialProvidersUpdatePatchWithResponse(ctx, realm, id, lock.UpdateSocialProviderData{Enabled: &req.Inputs.Enabled, Config: &credentials})
	if err != nil {
		return infer.UpdateResponse[SocialProviderState]{}, err
	}
	if response.JSON200 == nil || response.JSON200.Data.ID != id || response.JSON200.Data.Realm != realm {
		return infer.UpdateResponse[SocialProviderState]{}, apiError("update social provider", response.StatusCode())
	}
	return infer.UpdateResponse[SocialProviderState]{Output: socialProviderState(response.JSON200.Data, config)}, nil
}

func (SocialProvider) Read(ctx context.Context, req infer.ReadRequest[SocialProviderArgs, SocialProviderState]) (infer.ReadResponse[SocialProviderArgs, SocialProviderState], error) {
	realm, id, err := socialProviderParts(req.ID)
	if err != nil {
		return infer.ReadResponse[SocialProviderArgs, SocialProviderState]{}, err
	}
	response, err := infer.GetConfig[Config](ctx).socialProviders.V1RealmsSocialProvidersShowWithResponse(ctx, realm, id)
	if err != nil {
		return infer.ReadResponse[SocialProviderArgs, SocialProviderState]{}, err
	}
	if response.StatusCode() == http.StatusNotFound {
		return infer.ReadResponse[SocialProviderArgs, SocialProviderState]{}, nil
	}
	if response.JSON200 == nil || response.JSON200.Data.ID != id || response.JSON200.Data.Realm != realm {
		return infer.ReadResponse[SocialProviderArgs, SocialProviderState]{}, apiError("read social provider", response.StatusCode())
	}
	state := socialProviderState(response.JSON200.Data, req.State.Config)
	return infer.ReadResponse[SocialProviderArgs, SocialProviderState]{ID: req.ID, Inputs: state.SocialProviderArgs, State: state}, nil
}

func (SocialProvider) Delete(ctx context.Context, req infer.DeleteRequest[SocialProviderState]) (infer.DeleteResponse, error) {
	realm, id, err := socialProviderParts(req.ID)
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	response, err := infer.GetConfig[Config](ctx).socialProviders.V1RealmsSocialProvidersDestroyWithResponse(ctx, realm, id)
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	if response.StatusCode() != http.StatusNoContent && response.StatusCode() != http.StatusNotFound {
		return infer.DeleteResponse{}, apiError("delete social provider", response.StatusCode())
	}
	return infer.DeleteResponse{}, nil
}

func socialProviderParts(id string) (string, string, error) {
	realm, provider, err := clientParts(id)
	if err != nil {
		return "", "", fmt.Errorf("lock: social provider ID must have the form realm-slug/provider-UUID")
	}
	return realm, provider, nil
}

func socialCredentials(config SocialProviderConfig) lock.SocialProviderCredentialsData {
	data := lock.SocialProviderCredentialsData{ClientID: &config.ClientID, Issuer: config.Issuer, TeamID: config.TeamID, KeyID: config.KeyID}
	if config.ClientSecret != nil && strings.TrimSpace(*config.ClientSecret) != "" {
		data.ClientSecret = nullable.NewNullableWithValue(*config.ClientSecret)
	}
	if config.PrivateKey != nil && strings.TrimSpace(*config.PrivateKey) != "" {
		data.PrivateKey = nullable.NewNullableWithValue(*config.PrivateKey)
	}
	return data
}

func retainSocialConfig(next, old SocialProviderConfig) SocialProviderConfig {
	if next.Issuer == nil {
		next.Issuer = old.Issuer
	}
	if next.TeamID == nil {
		next.TeamID = old.TeamID
	}
	if next.KeyID == nil {
		next.KeyID = old.KeyID
	}
	if next.ClientSecret == nil || strings.TrimSpace(*next.ClientSecret) == "" {
		next.ClientSecret = old.ClientSecret
	}
	if next.PrivateKey == nil || strings.TrimSpace(*next.PrivateKey) == "" {
		next.PrivateKey = old.PrivateKey
	}
	return next
}

func socialProviderState(data lock.SocialProviderData, known SocialProviderConfig) SocialProviderState {
	config := SocialProviderConfig{ClientID: data.Config.ClientID, Issuer: data.Config.Issuer, TeamID: data.Config.TeamID, KeyID: data.Config.KeyID}
	if data.Driver == "apple" {
		config.PrivateKey = known.PrivateKey
	} else {
		config.ClientSecret = known.ClientSecret
	}
	return SocialProviderState{SocialProviderArgs: SocialProviderArgs{Realm: data.Realm, Key: data.Key, Driver: SocialProviderDriver(data.Driver), Enabled: data.Enabled, Config: config}, ProviderID: data.ID, CallbackURL: data.CallbackURL}
}
