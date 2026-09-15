package provider

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/use-lock/client-go/oidc"
)

type tokenSource struct {
	mu      sync.Mutex
	client  *oidc.ClientWithResponses
	body    oidc.OAuthTokenRequest
	token   string
	expires time.Time
}

func newTokenSource(config *Config, httpClient *http.Client, audience, scopes string) (*tokenSource, error) {
	options := []oidc.ClientOption{oidc.WithHTTPClient(httpClient)}
	request := oidc.OAuthTokenRequest2{GrantType: "client_credentials", Scope: &scopes}
	var resource oidc.OAuthTokenRequest_2_Resource
	if err := resource.FromOAuthTokenRequest2Resource0(audience); err != nil {
		return nil, err
	}
	request.Resource = &resource
	if config.TokenEndpointAuthMethod == "client_secret_post" {
		request.ClientID, request.ClientSecret = &config.ClientID, &config.ClientSecret
	} else {
		id, secret := url.QueryEscape(config.ClientID), url.QueryEscape(config.ClientSecret)
		options = append(options, oidc.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.SetBasicAuth(id, secret)
			return nil
		}))
	}
	client, err := oidc.NewClientWithResponses(config.BaseURL, options...)
	if err != nil {
		return nil, err
	}
	source := &tokenSource{client: client}
	if err := source.body.FromOAuthTokenRequest2(request); err != nil {
		return nil, err
	}
	return source, nil
}

func (s *tokenSource) authorize(ctx context.Context, req *http.Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.token == "" || !time.Now().Before(s.expires) {
		started := time.Now()
		response, err := s.client.OidcTokenPostWithFormdataBodyWithResponse(ctx, s.body)
		if err != nil {
			return err
		}
		if response.JSON200 == nil || response.StatusCode() != http.StatusOK {
			return apiError("obtain OAuth token", response.StatusCode())
		}
		token := response.JSON200
		if token.AccessToken == "" || !strings.EqualFold(string(token.TokenType), "Bearer") || token.ExpiresIn <= 0 {
			return errors.New("lock: token endpoint returned an invalid bearer token response")
		}
		s.token = token.AccessToken
		s.expires = started.Add(time.Duration(token.ExpiresIn)*time.Second - 30*time.Second)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	return nil
}
