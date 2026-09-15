package provider

import (
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi-go-provider/infer"
)

type RealmSettings struct {
	AccessTokenLifetime       *int      `pulumi:"accessTokenLifetime,optional"`
	IDTokenLifetime           *int      `pulumi:"idTokenLifetime,optional"`
	ClientCredentialsLifetime *int      `pulumi:"clientCredentialsLifetime,optional"`
	RefreshTokenLifetime      *int      `pulumi:"refreshTokenLifetime,optional"`
	SessionAbsoluteLifetime   *int      `pulumi:"sessionAbsoluteLifetime,optional"`
	SessionTokenTTL           *int      `pulumi:"sessionTokenTtl,optional"`
	SessionTokenRefreshSkew   *int      `pulumi:"sessionTokenRefreshSkew,optional"`
	LoginMethods              *[]string `pulumi:"loginMethods,optional"`
	EmailVerificationRequired *bool     `pulumi:"emailVerificationRequired,optional"`
	LinkByVerifiedEmail       *bool     `pulumi:"linkByVerifiedEmail,optional"`
	AutoProvision             *bool     `pulumi:"autoProvision,optional"`
	MFARequirement            *string   `pulumi:"mfaRequirement,optional"`
	ChallengeProviders        *[]string `pulumi:"challengeProviders,optional"`
	TOTPSecretLength          *int      `pulumi:"totpSecretLength,optional"`
	TOTPWindow                *int      `pulumi:"totpWindow,optional"`
	RecoveryCodes             *int      `pulumi:"recoveryCodes,optional"`
	PasswordMinLength         *int      `pulumi:"passwordMinLength,optional"`
	PasswordMixedCase         *bool     `pulumi:"passwordMixedCase,optional"`
	PasswordNumbers           *bool     `pulumi:"passwordNumbers,optional"`
	PasswordSymbols           *bool     `pulumi:"passwordSymbols,optional"`
	PasswordUncompromised     *bool     `pulumi:"passwordUncompromised,optional"`
	PasswordHistory           *int      `pulumi:"passwordHistory,optional"`
	PasswordMaxAgeDays        *int      `pulumi:"passwordMaxAgeDays,optional"`
	DynamicRegistration       *bool     `pulumi:"dynamicRegistration,optional"`
	AllowedRedirectSchemes    *[]string `pulumi:"allowedRedirectSchemes,optional"`
	AllowedRedirectDomains    *[]string `pulumi:"allowedRedirectDomains,optional"`
	DefaultScopes             *[]string `pulumi:"defaultScopes,optional"`
	OptionalScopes            *[]string `pulumi:"optionalScopes,optional"`
	TokenExchange             *bool     `pulumi:"tokenExchange,optional"`
	FirstPartyTrusted         *bool     `pulumi:"firstPartyTrusted,optional"`
	TrustedClients            *[]string `pulumi:"trustedClients,optional"`
}

type realmSettingsWire struct {
	AccessTokenLifetime       *int      `json:"access_token_lifetime,omitempty"`
	IDTokenLifetime           *int      `json:"id_token_lifetime,omitempty"`
	ClientCredentialsLifetime *int      `json:"client_credentials_lifetime,omitempty"`
	RefreshTokenLifetime      *int      `json:"refresh_token_lifetime,omitempty"`
	SessionAbsoluteLifetime   *int      `json:"session_absolute_lifetime,omitempty"`
	SessionTokenTTL           *int      `json:"session_token_ttl,omitempty"`
	SessionTokenRefreshSkew   *int      `json:"session_token_refresh_skew,omitempty"`
	LoginMethods              *[]string `json:"login_methods,omitempty"`
	EmailVerificationRequired *bool     `json:"email_verification_required,omitempty"`
	LinkByVerifiedEmail       *bool     `json:"link_by_verified_email,omitempty"`
	AutoProvision             *bool     `json:"auto_provision,omitempty"`
	MFARequirement            *string   `json:"mfa_requirement,omitempty"`
	ChallengeProviders        *[]string `json:"challenge_providers,omitempty"`
	TOTPSecretLength          *int      `json:"totp_secret_length,omitempty"`
	TOTPWindow                *int      `json:"totp_window,omitempty"`
	RecoveryCodes             *int      `json:"recovery_codes,omitempty"`
	PasswordMinLength         *int      `json:"password_min_length,omitempty"`
	PasswordMixedCase         *bool     `json:"password_mixed_case,omitempty"`
	PasswordNumbers           *bool     `json:"password_numbers,omitempty"`
	PasswordSymbols           *bool     `json:"password_symbols,omitempty"`
	PasswordUncompromised     *bool     `json:"password_uncompromised,omitempty"`
	PasswordHistory           *int      `json:"password_history,omitempty"`
	PasswordMaxAgeDays        *int      `json:"password_max_age_days,omitempty"`
	DynamicRegistration       *bool     `json:"dynamic_registration,omitempty"`
	AllowedRedirectSchemes    *[]string `json:"allowed_redirect_schemes,omitempty"`
	AllowedRedirectDomains    *[]string `json:"allowed_redirect_domains,omitempty"`
	DefaultScopes             *[]string `json:"default_scopes,omitempty"`
	OptionalScopes            *[]string `json:"optional_scopes,omitempty"`
	TokenExchange             *bool     `json:"token_exchange,omitempty"`
	FirstPartyTrusted         *bool     `json:"first_party_trusted,omitempty"`
	TrustedClients            *[]string `json:"trusted_clients,omitempty"`
}

func (s *RealmSettings) Annotate(a infer.Annotator) {
	a.Describe(&s.AccessTokenLifetime, "Seconds an access token stays valid.")
	a.Describe(&s.IDTokenLifetime, "Seconds an ID token stays valid.")
	a.Describe(&s.ClientCredentialsLifetime, "Seconds a token issued to a machine client stays valid.")
	a.Describe(&s.RefreshTokenLifetime, "Seconds a refresh token stays valid.")
	a.Describe(&s.SessionAbsoluteLifetime, "Seconds a sign-in session lives at most, however often it is refreshed.")
	a.Describe(&s.SessionTokenTTL, "Seconds a session token stays valid before it is refreshed.")
	a.Describe(&s.SessionTokenRefreshSkew, "Seconds before expiry at which a session token is refreshed early.")
	a.Describe(&s.LoginMethods, "The ways an identity may sign in: `password`, `passkey`, `social`.")
	a.Describe(&s.EmailVerificationRequired, "Whether an unverified address blocks sign-in.")
	a.Describe(&s.LinkByVerifiedEmail, "Link an upstream identity to a local user with the same verified email address.")
	a.Describe(&s.AutoProvision, "Create a realm user on their first verified upstream sign-in.")
	a.Describe(&s.MFARequirement, "When a second factor is demanded: `never`, `if_enrolled`, `always`.")
	a.Describe(&s.ChallengeProviders, "The second factors on offer: `totp`, `webauthn`.")
	a.Describe(&s.TOTPSecretLength, "Bytes of a generated TOTP secret.")
	a.Describe(&s.TOTPWindow, "Time steps either side of now a TOTP code is accepted in.")
	a.Describe(&s.RecoveryCodes, "How many recovery codes an identity is issued.")
	a.Describe(&s.PasswordMinLength, "Shortest password accepted.")
	a.Describe(&s.PasswordMixedCase, "Whether a password must mix upper and lower case.")
	a.Describe(&s.PasswordNumbers, "Whether a password must carry a digit.")
	a.Describe(&s.PasswordSymbols, "Whether a password must carry a symbol.")
	a.Describe(&s.PasswordUncompromised, "Whether a password is checked against known breaches.")
	a.Describe(&s.PasswordHistory, "How many previous passwords may not be reused.")
	a.Describe(&s.PasswordMaxAgeDays, "Days before a password must be changed; `0` never expires.")
	a.Describe(&s.DynamicRegistration, "Whether a client may register itself through RFC 7591.")
	a.Describe(&s.AllowedRedirectSchemes, "URI schemes a redirect URI may use.")
	a.Describe(&s.AllowedRedirectDomains, "Hosts a redirect URI may point at; `*` allows any.")
	a.Describe(&s.DefaultScopes, "Scopes every client is granted without asking.")
	a.Describe(&s.OptionalScopes, "Scopes a client may request on top; `*` allows any of the catalog.")
	a.Describe(&s.TokenExchange, "Whether the RFC 8693 token exchange grant is served.")
	a.Describe(&s.FirstPartyTrusted, "Whether the first-party client skips the consent screen.")
	a.Describe(&s.TrustedClients, "Client ids that skip the consent screen.")
}

func settingsFromResponse(body []byte, managed *RealmSettings) (*RealmSettings, error) {
	if managed == nil {
		return nil, nil
	}
	var response struct {
		Data struct {
			Settings map[string]json.RawMessage `json:"settings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(settingsToWire(managed))
	if err != nil {
		return nil, err
	}
	var selected map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &selected); err != nil {
		return nil, err
	}
	for key := range selected {
		value, exists := response.Data.Settings[key]
		if !exists || string(value) == "null" {
			return nil, fmt.Errorf("lock: realm response is missing managed setting %q", key)
		}
		selected[key] = value
	}
	encoded, err = json.Marshal(selected)
	if err != nil {
		return nil, err
	}
	var settings realmSettingsWire
	if err := json.Unmarshal(encoded, &settings); err != nil {
		return nil, err
	}
	result := RealmSettings(settings)
	return &result, nil
}

func settingsToWire(settings *RealmSettings) *realmSettingsWire {
	if settings == nil {
		return nil
	}
	wire := realmSettingsWire(*settings)
	wire.LoginMethods = settingsList(wire.LoginMethods)
	wire.ChallengeProviders = settingsList(wire.ChallengeProviders)
	wire.AllowedRedirectSchemes = settingsList(wire.AllowedRedirectSchemes)
	wire.AllowedRedirectDomains = settingsList(wire.AllowedRedirectDomains)
	wire.DefaultScopes = settingsList(wire.DefaultScopes)
	wire.OptionalScopes = settingsList(wire.OptionalScopes)
	wire.TrustedClients = settingsList(wire.TrustedClients)
	return &wire
}

func settingsList(values *[]string) *[]string {
	if values == nil {
		return nil
	}
	result := nonNil(*values)
	return &result
}
