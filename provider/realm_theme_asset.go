package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi-go-provider/infer/types"
	lock "github.com/use-lock/client-go"
	"github.com/use-lock/client-go/admin"
)

type RealmThemeAsset struct{}

type RealmThemeAssetArgs struct {
	Realm  string               `pulumi:"realm" provider:"replaceOnChanges"`
	Name   string               `pulumi:"name" provider:"replaceOnChanges"`
	Source types.AssetOrArchive `pulumi:"source"`
}

type RealmThemeAssetState struct {
	RealmThemeAssetArgs
	URL      string `pulumi:"url"`
	MimeType string `pulumi:"mimeType"`
	Size     int    `pulumi:"size"`
	SHA256   string `pulumi:"sha256"`
}

type realmThemeAssetData struct {
	URL      string `json:"url"`
	MimeType string `json:"mime_type"`
	Size     int    `json:"size"`
	SHA256   string `json:"sha256"`
}

func (r *RealmThemeAsset) Annotate(a infer.Annotator) {
	a.SetToken("index", "RealmThemeAsset")
	a.Describe(&r, "A file a realm's theme serves itself, such as a logo or a font. A theme setting names it as `asset:<name>`. Import using `realm/name`; an import has no source until the program supplies one.")
}

func (r *RealmThemeAssetArgs) Annotate(a infer.Annotator) {
	a.Describe(&r.Realm, "Slug of the realm the file belongs to. Changing it replaces the file.")
	a.Describe(&r.Name, "Lowercase file name. It decides the type the file is served with: `.png`, `.jpg`, `.jpeg`, `.webp`, `.svg` or `.woff2`. Changing it replaces the file.")
	a.Describe(&r.Source, "The file's contents, up to 5 MB, as an asset such as `new pulumi.asset.FileAsset(path)`. Pulumi uploads it again when its contents change.")
}

func (r *RealmThemeAssetState) Annotate(a infer.Annotator) {
	a.Describe(&r.URL, "The address the file is served from on the realm's host.")
	a.Describe(&r.MimeType, "The type the file is served with.")
	a.Describe(&r.Size, "The file's size in bytes.")
	a.Describe(&r.SHA256, "The SHA-256 of the file's contents.")
}

func (RealmThemeAsset) Create(ctx context.Context, req infer.CreateRequest[RealmThemeAssetArgs]) (infer.CreateResponse[RealmThemeAssetState], error) {
	if req.DryRun {
		return infer.CreateResponse[RealmThemeAssetState]{Output: RealmThemeAssetState{RealmThemeAssetArgs: req.Inputs}}, nil
	}
	state, err := uploadThemeAsset(ctx, req.Inputs)
	if err != nil {
		return infer.CreateResponse[RealmThemeAssetState]{}, err
	}
	return infer.CreateResponse[RealmThemeAssetState]{ID: req.Inputs.Realm + "/" + req.Inputs.Name, Output: state}, nil
}

func (RealmThemeAsset) Update(ctx context.Context, req infer.UpdateRequest[RealmThemeAssetArgs, RealmThemeAssetState]) (infer.UpdateResponse[RealmThemeAssetState], error) {
	if req.DryRun {
		state := req.State
		state.RealmThemeAssetArgs = req.Inputs
		return infer.UpdateResponse[RealmThemeAssetState]{Output: state}, nil
	}
	state, err := uploadThemeAsset(ctx, req.Inputs)
	if err != nil {
		return infer.UpdateResponse[RealmThemeAssetState]{}, err
	}
	return infer.UpdateResponse[RealmThemeAssetState]{Output: state}, nil
}

func (RealmThemeAsset) Read(ctx context.Context, req infer.ReadRequest[RealmThemeAssetArgs, RealmThemeAssetState]) (infer.ReadResponse[RealmThemeAssetArgs, RealmThemeAssetState], error) {
	realm, name, found := strings.Cut(req.ID, "/")
	if !found || realm == "" || name == "" {
		return infer.ReadResponse[RealmThemeAssetArgs, RealmThemeAssetState]{}, fmt.Errorf("lock: theme asset ID must be realm/name, got %q", req.ID)
	}
	data, err := requestThemeAsset(ctx, infer.GetConfig[Config](ctx).admin, http.MethodGet, realm, name, nil, http.StatusOK)
	if isNotFound(err) {
		return infer.ReadResponse[RealmThemeAssetArgs, RealmThemeAssetState]{}, nil
	}
	if err != nil {
		return infer.ReadResponse[RealmThemeAssetArgs, RealmThemeAssetState]{}, requestError("read theme asset", err)
	}
	inputs := RealmThemeAssetArgs{Realm: realm, Name: name, Source: req.Inputs.Source}
	return infer.ReadResponse[RealmThemeAssetArgs, RealmThemeAssetState]{ID: req.ID, Inputs: inputs, State: themeAssetState(inputs, data)}, nil
}

func (RealmThemeAsset) Delete(ctx context.Context, req infer.DeleteRequest[RealmThemeAssetState]) (infer.DeleteResponse, error) {
	_, err := requestThemeAsset(ctx, infer.GetConfig[Config](ctx).admin, http.MethodDelete, req.State.Realm, req.State.Name, nil, http.StatusNoContent)
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, requestError("delete theme asset", err)
	}
	return infer.DeleteResponse{}, nil
}

func uploadThemeAsset(ctx context.Context, inputs RealmThemeAssetArgs) (RealmThemeAssetState, error) {
	if inputs.Source.Asset == nil {
		return RealmThemeAssetState{}, errors.New("lock: a theme asset's source must be an asset, not an archive")
	}
	contents, err := inputs.Source.Asset.Bytes()
	if err != nil {
		return RealmThemeAssetState{}, fmt.Errorf("lock: read theme asset source: %w", err)
	}
	data, err := requestThemeAsset(ctx, infer.GetConfig[Config](ctx).admin, http.MethodPut, inputs.Realm, inputs.Name, contents, http.StatusOK)
	if err != nil {
		return RealmThemeAssetState{}, requestError("upload theme asset", err)
	}
	return themeAssetState(inputs, data), nil
}

func themeAssetState(inputs RealmThemeAssetArgs, data realmThemeAssetData) RealmThemeAssetState {
	return RealmThemeAssetState{RealmThemeAssetArgs: inputs, URL: data.URL, MimeType: data.MimeType, Size: data.Size, SHA256: data.SHA256}
}

// The generated client has no operation for these routes, because the body is the file itself.
func requestThemeAsset(ctx context.Context, client *admin.Client, method, realm, name string, body []byte, status int) (realmThemeAssetData, error) {
	var data realmThemeAssetData
	target := strings.TrimRight(client.Server, "/") + "/v1/realms/" + url.PathEscape(realm) + "/theme-assets/" + url.PathEscape(name)
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return data, err
	}
	request.Header.Set("Accept", "application/json")
	if method == http.MethodPut {
		request.Header.Set("Content-Type", "application/octet-stream")
	}
	for _, editor := range client.RequestEditors {
		if err := editor(ctx, request); err != nil {
			return data, err
		}
	}
	response, err := client.Client.Do(request)
	if err != nil {
		return data, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != status {
		return data, &lock.APIError{Response: lock.Response{StatusCode: response.StatusCode}}
	}
	if status == http.StatusNoContent {
		return data, nil
	}
	encoded, err := io.ReadAll(response.Body)
	if err != nil {
		return data, err
	}
	var result struct {
		Data realmThemeAssetData `json:"data"`
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return data, apiError("decode theme asset", response.StatusCode)
	}
	return result.Data, nil
}
