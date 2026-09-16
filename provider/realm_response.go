package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	lock "github.com/use-lock/client-go"
	"github.com/use-lock/client-go/admin"
)

type realmResponse struct {
	Data admin.RealmData `json:"data"`
	Body []byte          `json:"-"`
}

// Keep the original JSON for settings: the client's float32 models round large integer lifetimes.
func requestRealm(ctx context.Context, client *admin.Client, request *http.Request, status int) (*realmResponse, error) {
	request = request.WithContext(ctx)
	for _, editor := range client.RequestEditors {
		if err := editor(ctx, request); err != nil {
			return nil, err
		}
	}
	response, err := client.Client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != status {
		return nil, &lock.APIError{Response: lock.Response{StatusCode: response.StatusCode}}
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var result realmResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, apiError("decode realm", response.StatusCode)
	}
	result.Body = body
	return &result, nil
}
