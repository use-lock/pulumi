package provider

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/use-lock/client-go/admin"
	"github.com/use-lock/client-go/management"
)

type Config struct {
	BaseURL                 string `pulumi:"baseUrl,optional"`
	AccessToken             string `pulumi:"accessToken,optional" provider:"secret"`
	AdminAccessToken        string `pulumi:"adminAccessToken,optional" provider:"secret"`
	ClientID                string `pulumi:"clientId,optional"`
	ClientSecret            string `pulumi:"clientSecret,optional" provider:"secret"`
	TokenEndpointAuthMethod string `pulumi:"tokenEndpointAuthMethod,optional"`

	admin           *admin.Client
	management      *management.Client
	resources       *management.Client
	socialProviders *management.Client
}

func (c *Config) Annotate(a infer.Annotator) {
	a.Describe(&c.BaseURL, "Master realm issuer URL, e.g. https://lock.example, without /api.")
	a.Describe(&c.AccessToken, "Management API access token; used for the Admin API too unless adminAccessToken is set. Mutually exclusive with client credentials.")
	a.Describe(&c.AdminAccessToken, "Optional separate token addressed to the Admin API with realms:read and realms:write.")
	a.Describe(&c.ClientID, "Master realm OAuth client ID. Tokens are obtained lazily for each API and renewed before expiry.")
	a.Describe(&c.ClientSecret, "OAuth client secret. Grant read/write scopes for the resource types being managed: realms, clients, resources and social-providers. Each resource type obtains its own scoped token lazily.")
	a.Describe(&c.TokenEndpointAuthMethod, "Authentication method for the provider's OAuth client: client_secret_basic (default) or client_secret_post.")
	a.SetDefault(&c.BaseURL, "", "LOCK_BASE_URL")
	a.SetDefault(&c.AccessToken, "", "LOCK_ACCESS_TOKEN")
	a.SetDefault(&c.AdminAccessToken, "", "LOCK_ADMIN_ACCESS_TOKEN")
	a.SetDefault(&c.ClientID, "", "LOCK_CLIENT_ID")
	a.SetDefault(&c.ClientSecret, "", "LOCK_CLIENT_SECRET")
	a.SetDefault(&c.TokenEndpointAuthMethod, "client_secret_basic", "LOCK_TOKEN_ENDPOINT_AUTH_METHOD")
}

func (c *Config) Configure(_ context.Context) error {
	c.admin, c.management, c.resources, c.socialProviders = nil, nil, nil, nil
	c.BaseURL = strings.TrimRight(configValue(c.BaseURL, "LOCK_BASE_URL"), "/")
	c.AccessToken = configValue(c.AccessToken, "LOCK_ACCESS_TOKEN")
	c.AdminAccessToken = configValue(c.AdminAccessToken, "LOCK_ADMIN_ACCESS_TOKEN")
	c.ClientID = configValue(c.ClientID, "LOCK_CLIENT_ID")
	c.ClientSecret = configValue(c.ClientSecret, "LOCK_CLIENT_SECRET")
	c.TokenEndpointAuthMethod = configValue(c.TokenEndpointAuthMethod, "LOCK_TOKEN_ENDPOINT_AUTH_METHOD")
	if c.TokenEndpointAuthMethod == "" {
		c.TokenEndpointAuthMethod = "client_secret_basic"
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("lock: baseUrl must be an HTTP(S) issuer URL without credentials, query or fragment")
	}
	if c.AccessToken != "" && (c.ClientID != "" || c.ClientSecret != "") {
		return errors.New("lock: configure either accessToken or clientId and clientSecret")
	}
	if c.AccessToken == "" && (c.ClientID == "" || c.ClientSecret == "") {
		return errors.New("lock: provide accessToken or both clientId and clientSecret")
	}
	if c.AdminAccessToken != "" && c.AccessToken == "" {
		return errors.New("lock: adminAccessToken requires accessToken")
	}
	if c.TokenEndpointAuthMethod != "client_secret_basic" && c.TokenEndpointAuthMethod != "client_secret_post" {
		return errors.New("lock: tokenEndpointAuthMethod must be client_secret_basic or client_secret_post")
	}
	httpClient := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	adminToken := c.AdminAccessToken
	if adminToken == "" {
		adminToken = c.AccessToken
	}
	adminEditor, err := c.requestEditor(httpClient, adminToken, c.BaseURL+"/admin-api", "realms:read realms:write")
	if err != nil {
		return err
	}
	c.admin, err = admin.NewClient(c.BaseURL+"/api", admin.WithHTTPClient(httpClient), admin.WithRequestEditorFn(adminEditor))
	if err != nil {
		return err
	}
	c.management, err = c.apiClient(httpClient, c.AccessToken, c.BaseURL+"/api", "clients:read clients:write")
	if err != nil {
		return err
	}
	c.resources, err = c.apiClient(httpClient, c.AccessToken, c.BaseURL+"/api", "resources:read resources:write")
	if err != nil {
		return err
	}
	c.socialProviders, err = c.apiClient(httpClient, c.AccessToken, c.BaseURL+"/api", "social-providers:read social-providers:write")
	return err
}

func (c *Config) apiClient(httpClient *http.Client, token, audience, scopes string) (*management.Client, error) {
	editor, err := c.requestEditor(httpClient, token, audience, scopes)
	if err != nil {
		return nil, err
	}
	return management.NewClient(c.BaseURL+"/api", management.WithHTTPClient(httpClient), management.WithRequestEditorFn(editor))
}

func (c *Config) requestEditor(httpClient *http.Client, token, audience, scopes string) (func(context.Context, *http.Request) error, error) {
	var editor func(context.Context, *http.Request) error
	if token != "" {
		editor = func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		}
	} else {
		source, err := newTokenSource(c, httpClient, audience, scopes)
		if err != nil {
			return nil, err
		}
		editor = source.authorize
	}
	return editor, nil
}

func configValue(value, env string) string {
	if value != "" {
		return value
	}
	return os.Getenv(env)
}
