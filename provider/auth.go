package provider

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/use-lock/client-go/auth"
)

type tokenSource struct {
	mu      sync.Mutex
	client  *auth.Client
	body    auth.OAuthTokenRequest
	token   string
	expires time.Time
}

func newTokenSource(config *Config, httpClient *http.Client, audience, scopes string) (*tokenSource, error) {
	options := []auth.ClientOption{auth.WithHTTPClient(httpClient)}
	request := auth.ClientCredentialsRequest{GrantType: "client_credentials", Scope: &scopes}
	var resource auth.ClientCredentialsRequest_Resource
	if err := resource.FromClientCredentialsRequestResource0(audience); err != nil {
		return nil, err
	}
	request.Resource = &resource
	if config.TokenEndpointAuthMethod == "client_secret_post" {
		request.ClientID, request.ClientSecret = &config.ClientID, &config.ClientSecret
	} else {
		id, secret := url.QueryEscape(config.ClientID), url.QueryEscape(config.ClientSecret)
		options = append(options, auth.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.SetBasicAuth(id, secret)
			return nil
		}))
	}
	client, err := auth.NewClient(config.BaseURL, options...)
	if err != nil {
		return nil, err
	}
	source := &tokenSource{client: client}
	if err := source.body.FromClientCredentialsRequest(request); err != nil {
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
		token, err := s.client.IssueToken(ctx, s.body)
		if err != nil {
			return requestError("obtain OAuth token", err)
		}
		if token.AccessToken == "" || !strings.EqualFold(string(token.TokenType), "Bearer") || token.ExpiresIn <= 0 {
			return errors.New("lock: token endpoint returned an invalid bearer token response")
		}
		s.token = token.AccessToken
		s.expires = started.Add(time.Duration(token.ExpiresIn)*time.Second - 30*time.Second)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	return nil
}
