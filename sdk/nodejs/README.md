# Lock Pulumi Provider

Manage [Lock](https://github.com/use-lock/lock) realms, OAuth clients, protected resources, and social login providers with Pulumi.

## Installation

Install [Pulumi](https://www.pulumi.com/docs/install/) and Node.js 22 or later, then add the TypeScript SDK to your Pulumi project:

```sh
npm install @use-lock/pulumi
```

The SDK downloads the matching provider plugin from GitHub Releases. Plugins are available for Linux, macOS, and Windows on amd64 and arm64.

## Quick Start

Configure an existing confidential OAuth client in Lock's master realm. It needs access to the Admin API and Management API, with the scopes listed below. The provider uses this client to obtain and renew tokens as needed.

```sh
pulumi config set baseUrl https://lock.example
pulumi config set clientId YOUR_CLIENT_ID
pulumi config set --secret clientSecret
pulumi config set realmDomain staging.example
```

Use this in your project's `index.ts`:

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as lock from "@use-lock/pulumi";

const config = new pulumi.Config();

const provider = new lock.Provider("lock", {
    baseUrl: config.require("baseUrl"),
    clientId: config.require("clientId"),
    clientSecret: config.requireSecret("clientSecret"),
});

const realm = new lock.Realm("staging", {
    slug: "staging",
    name: "Staging",
    domain: config.require("realmDomain"),
    settings: {
        accessTokenLifetime: 3600,
        passwordMinLength: 14,
    },
}, { provider });

const client = new lock.Client("backend", {
    realm: realm.slug,
    name: "Backend service",
    tokenEndpointAuthMethod: "client_secret_basic",
    grantTypes: ["client_credentials"],
    consentRequired: false,
}, { provider });

export const issuer = realm.issuer;
export const clientId = client.clientId;
export const clientSecret = client.clientSecret;
```

Run `pulumi preview` to inspect the changes, then `pulumi up` to apply them. Client secrets are marked as secret outputs.

## Authentication

`baseUrl` is the master realm issuer, such as `https://lock.example`, without `/api`.

| Provider option | Environment variable | Purpose |
| --- | --- | --- |
| `baseUrl` | `LOCK_BASE_URL` | Master realm issuer |
| `clientId` | `LOCK_CLIENT_ID` | OAuth client ID |
| `clientSecret` | `LOCK_CLIENT_SECRET` | OAuth client secret |
| `tokenEndpointAuthMethod` | `LOCK_TOKEN_ENDPOINT_AUTH_METHOD` | `client_secret_basic` (default) or `client_secret_post` |
| `accessToken` | `LOCK_ACCESS_TOKEN` | Fixed Management API token, as an alternative to client credentials |
| `adminAccessToken` | `LOCK_ADMIN_ACCESS_TOKEN` | Separate Admin API token when using fixed tokens |

Use either client credentials or fixed access tokens. Fixed tokens are not refreshed. If `adminAccessToken` is omitted, `accessToken` is also used for realm administration and must be valid for that audience.

The provider obtains separate scoped tokens for each resource type:

| Resource | Token audience | Required scopes |
| --- | --- | --- |
| `Realm` | `{issuer}/admin-api` | `realms:read realms:write` |
| `Client` | `{issuer}/api` | `clients:read clients:write` |
| `ProtectedResource` | `{issuer}/api` | `resources:read resources:write` |
| `SocialProvider` | `{issuer}/api` | `social-providers:read social-providers:write` |

Grant the scopes for the resource types your program manages. Tokens are requested lazily, so unused resource types do not require grants.

## Resources and Imports

| Resource | Import ID |
| --- | --- |
| `lock:index:Realm` | Realm slug, such as `staging` |
| `lock:index:Client` | `realm-slug/client-id` |
| `lock:index:ProtectedResource` | `realm-slug/resource-uuid` |
| `lock:index:SocialProvider` | `realm-slug/provider-uuid` |

To adopt an existing resource with an explicit provider, pass its ID through the `import` resource option:

```typescript
const existing = new lock.Realm("existing", {
    slug: "existing",
    name: "Existing realm",
    domain: "existing.example",
}, { provider, import: "existing" });
```

Supply the resource's current values, run `pulumi up`, then remove the `import` option after adoption.

Realm settings are managed only when explicitly supplied. Removing an override stops managing it and keeps the server value. The master realm cannot be managed as a `Realm` resource. Changing a realm's slug replaces the realm and its contents.

Client secrets are available at creation and preserved in state during refresh. Importing a client does not retrieve its existing secret. For social providers, supply credentials as Pulumi secrets; omitted or blank secret fields retain the current value.

## Examples

- [TypeScript: realms, clients, scopes, and Google login](https://github.com/use-lock/pulumi/tree/main/examples/typescript)
- [YAML: realm and client](https://github.com/use-lock/pulumi/tree/main/examples/basic)
- [YAML: realm policies, scopes, and social login](https://github.com/use-lock/pulumi/tree/main/examples/configuration)

The repository examples use a locally built plugin. To run the TypeScript example from a checkout:

```sh
make build
make build-sdk
cd examples/typescript
npm ci
pulumi stack init dev
pulumi config set baseUrl https://lock.example
pulumi config set clientId YOUR_CLIENT_ID
pulumi config set --secret clientSecret
pulumi config set realmDomain staging.example
pulumi config set googleClientId YOUR_GOOGLE_CLIENT_ID
pulumi config set --secret googleClientSecret
pulumi preview
```

## Development

Requires Go matching `go.mod`, Node.js 22 or later, and the Pulumi CLI. CI pins the tool versions used for releases.

```sh
make check      # Go vet, race tests, and formatting checks
make schema     # Build the plugin and regenerate schema.json
make gen-sdk    # Regenerate the TypeScript SDK, metadata, README, and license
make build-sdk  # Compile the npm package
```

Edit provider definitions and `patch-sdk.js` rather than generated SDK files. The root README and license are copied into the SDK and release packages.

## Releases

Release Please opens the version PR. Merging it creates the tag and starts the release workflow, which publishes plugin archives, checksums, the schema, and `@use-lock/pulumi` to npm.

Configure `RELEASE_PLEASE_TOKEN` for the release automation and `NPM_TOKEN` with permission to publish `@use-lock/pulumi`. The npm token must support non-interactive publishing under the organization's policy. The release checks npm authentication before building and fails if the token is missing or invalid.

When updating the Go client, publish its release first, then update the version in `go.mod` before releasing the provider.

## License

[MIT](https://github.com/use-lock/pulumi/blob/main/LICENSE).
