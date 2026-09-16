package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/use-lock/client-go/admin"
)

type Realm struct{}

type RealmArgs struct {
	Slug     string         `pulumi:"slug" provider:"replaceOnChanges"`
	Name     string         `pulumi:"name"`
	Domain   string         `pulumi:"domain"`
	Settings *RealmSettings `pulumi:"settings,optional"`
}

type RealmState struct {
	RealmArgs
	Issuer       string `pulumi:"issuer"`
	Host         string `pulumi:"host"`
	DomainStatus string `pulumi:"domainStatus"`
	Master       bool   `pulumi:"master"`
}

func (r *Realm) Annotate(a infer.Annotator) {
	a.SetToken("index", "Realm")
	a.Describe(&r, "A Lock realm. Import using its slug. Only explicitly supplied settings are managed; omitted settings retain their server values.")
}

func (r *RealmArgs) Annotate(a infer.Annotator) {
	a.Describe(&r.Slug, "Immutable realm identifier. Changing it replaces the realm and its contents.")
	a.Describe(&r.Domain, "Realm hostname, without a scheme or path. DNS verification status is exposed as domainStatus.")
	a.Describe(&r.Settings, "Realm policy overrides. Removing an override stops managing it and retains its server value; the API has no reset-to-default operation. Set an explicit value to change it.")
}

func (Realm) Create(ctx context.Context, req infer.CreateRequest[RealmArgs]) (infer.CreateResponse[RealmState], error) {
	if req.DryRun {
		return infer.CreateResponse[RealmState]{Output: RealmState{RealmArgs: req.Inputs}}, nil
	}
	// The generated client models use float32 for settings numbers. JSON bodies
	// preserve integer lifetimes above 2^24 without rounding.
	body, err := json.Marshal(struct {
		admin.CreateRealmData
		Settings *realmSettingsWire `json:"settings,omitempty"`
	}{
		CreateRealmData: admin.CreateRealmData{Slug: req.Inputs.Slug, Name: req.Inputs.Name, Domain: req.Inputs.Domain},
		Settings:        settingsToWire(req.Inputs.Settings),
	})
	if err != nil {
		return infer.CreateResponse[RealmState]{}, err
	}
	client := infer.GetConfig[Config](ctx).admin
	request, err := admin.NewCreateRealmRequestWithBody(client.Server, "application/json", bytes.NewReader(body))
	if err != nil {
		return infer.CreateResponse[RealmState]{}, err
	}
	response, err := requestRealm(ctx, client, request, http.StatusCreated)
	if err != nil {
		return infer.CreateResponse[RealmState]{}, requestError("create realm", err)
	}
	if response.Data.Slug != req.Inputs.Slug {
		return infer.CreateResponse[RealmState]{}, apiError("create realm", http.StatusCreated)
	}
	state := realmState(response.Data)
	state.Settings, err = settingsFromResponse(response.Body, req.Inputs.Settings)
	return infer.CreateResponse[RealmState]{ID: response.Data.Slug, Output: state}, err
}

func (Realm) Update(ctx context.Context, req infer.UpdateRequest[RealmArgs, RealmState]) (infer.UpdateResponse[RealmState], error) {
	if req.DryRun {
		state := req.State
		state.RealmArgs = req.Inputs
		return infer.UpdateResponse[RealmState]{Output: state}, nil
	}
	body, err := json.Marshal(struct {
		admin.UpdateRealmData
		Settings *realmSettingsWire `json:"settings,omitempty"`
	}{UpdateRealmData: admin.UpdateRealmData{Name: &req.Inputs.Name, Domain: &req.Inputs.Domain}, Settings: settingsToWire(req.Inputs.Settings)})
	if err != nil {
		return infer.UpdateResponse[RealmState]{}, err
	}
	client := infer.GetConfig[Config](ctx).admin
	request, err := admin.NewPatchRealmRequestWithBody(client.Server, req.ID, "application/json", bytes.NewReader(body))
	if err != nil {
		return infer.UpdateResponse[RealmState]{}, err
	}
	response, err := requestRealm(ctx, client, request, http.StatusOK)
	if err != nil {
		return infer.UpdateResponse[RealmState]{}, requestError("update realm", err)
	}
	if response.Data.Slug != req.ID {
		return infer.UpdateResponse[RealmState]{}, apiError("update realm", http.StatusOK)
	}
	state := realmState(response.Data)
	state.Settings, err = settingsFromResponse(response.Body, req.Inputs.Settings)
	return infer.UpdateResponse[RealmState]{Output: state}, err
}

func (Realm) Read(ctx context.Context, req infer.ReadRequest[RealmArgs, RealmState]) (infer.ReadResponse[RealmArgs, RealmState], error) {
	client := infer.GetConfig[Config](ctx).admin
	request, err := admin.NewGetRealmRequest(client.Server, req.ID)
	if err != nil {
		return infer.ReadResponse[RealmArgs, RealmState]{}, err
	}
	response, err := requestRealm(ctx, client, request, http.StatusOK)
	if isNotFound(err) {
		return infer.ReadResponse[RealmArgs, RealmState]{}, nil
	}
	if err != nil {
		return infer.ReadResponse[RealmArgs, RealmState]{}, requestError("read realm", err)
	}
	if response.Data.Slug != req.ID {
		return infer.ReadResponse[RealmArgs, RealmState]{}, apiError("read realm", http.StatusOK)
	}
	if response.Data.Master {
		return infer.ReadResponse[RealmArgs, RealmState]{}, errors.New("lock: the master realm cannot be managed as a Realm resource")
	}
	state := realmState(response.Data)
	state.Settings, err = settingsFromResponse(response.Body, req.Inputs.Settings)
	if err != nil {
		return infer.ReadResponse[RealmArgs, RealmState]{}, err
	}
	return infer.ReadResponse[RealmArgs, RealmState]{ID: req.ID, Inputs: state.RealmArgs, State: state}, nil
}

func (Realm) Delete(ctx context.Context, req infer.DeleteRequest[RealmState]) (infer.DeleteResponse, error) {
	if req.State.Master {
		return infer.DeleteResponse{}, errors.New("lock: the master realm cannot be deleted")
	}
	_, err := infer.GetConfig[Config](ctx).admin.DeleteRealm(ctx, req.ID)
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, requestError("delete realm", err)
	}
	return infer.DeleteResponse{}, nil
}

func realmState(data admin.RealmData) RealmState {
	domain, _ := data.Domain.Get()
	return RealmState{RealmArgs: RealmArgs{Slug: data.Slug, Name: data.Name, Domain: domain}, Issuer: data.Issuer, Host: data.Host, DomainStatus: string(data.DomainStatus), Master: data.Master}
}
