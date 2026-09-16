package provider

import (
	"context"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
)

func New() (p.Provider, error) {
	provider, err := infer.NewProviderBuilder().
		WithConfig(infer.Config(&Config{})).
		WithModuleMap(map[tokens.ModuleName]tokens.ModuleName{"provider": "index"}).
		WithResources(infer.Resource(Realm{}), infer.Resource(Client{}), infer.Resource(ProtectedResource{}), infer.Resource(SocialProvider{})).
		WithDisplayName("Lock").
		WithDescription("Manage Lock realms, realm policies, OAuth clients, protected APIs with scopes, and social login providers.").
		WithPublisher("use-lock").
		WithLicense("MIT").
		WithRepository("https://github.com/use-lock/pulumi").
		WithPluginDownloadURL("https://github.com/use-lock/pulumi/releases/download/v$%7BVERSION%7D").
		Build()
	if err != nil {
		return p.Provider{}, err
	}
	diff := provider.Diff
	provider.Diff = func(ctx context.Context, req p.DiffRequest) (p.DiffResponse, error) {
		if string(req.Urn.Type()) == "lock:index:SocialProvider" {
			return socialProviderDiff(req)
		}
		result, err := diff(ctx, req)
		if err == nil && string(req.Urn.Type()) == "lock:index:Realm" {
			kind := result.DetailedDiff["slug"].Kind
			if kind == p.UpdateReplace || kind == p.AddReplace || kind == p.DeleteReplace {
				oldDomain, newDomain := req.State.Get("domain"), req.Inputs.Get("domain")
				result.DeleteBeforeReplace = newDomain.IsComputed() || oldDomain.Equals(newDomain)
			}
		}
		return result, err
	}
	read := provider.Read
	provider.Read = func(ctx context.Context, req p.ReadRequest) (p.ReadResponse, error) {
		result, err := read(ctx, req)
		// infer applies WireDependencies to Create/Update, but Read only encodes state.
		if err == nil && string(req.Urn.Type()) == "lock:index:SocialProvider" {
			if value := result.Properties.Get("config"); !value.IsNull() {
				result.Properties = result.Properties.Set("config", value.WithSecret(true))
			}
			if value := result.Inputs.Get("config"); !value.IsNull() {
				result.Inputs = result.Inputs.Set("config", value.WithSecret(true))
			}
		}
		return result, err
	}
	return provider, nil
}
