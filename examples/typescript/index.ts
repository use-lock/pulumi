import * as pulumi from "@pulumi/pulumi";
import * as lock from "@use-lock/pulumi";

const config = new pulumi.Config();

const provider = new lock.Provider("lock", {
    baseUrl: config.require("baseUrl"),
    clientId: config.require("clientId"),
    clientSecret: config.requireSecret("clientSecret"),
});

const realm = new lock.Realm("realm", {
    slug: "staging",
    name: "Staging",
    domain: config.require("realmDomain"),
    settings: {
        accessTokenLifetime: 3600,
        passwordMinLength: 14,
        emailVerificationRequired: true,
        loginMethods: ["password", "social"],
    },
}, { provider });

const invoicesApi = new lock.ProtectedResource("invoices-api", {
    realm: realm.slug,
    identifier: "/invoices-api",
    name: "Invoices API",
    scopes: {
        "invoices:read": { description: "Read invoices" },
        "invoices:write": { description: "Create and update invoices" },
    },
}, { provider });

// A path-relative identifier is addressed under the realm issuer; the client
// sends this exact value as `resource` and its token carries it as `aud`.
const invoicesApiAudience = pulumi.interpolate`${realm.issuer}${invoicesApi.identifier}`;

const serviceClient = new lock.Client("service-client", {
    realm: realm.slug,
    name: "Backend service",
    tokenEndpointAuthMethod: "client_secret_basic",
    grantTypes: ["client_credentials"],
    consentRequired: false,
    allowedAudiences: [invoicesApiAudience],
}, { provider });

const google = new lock.SocialProvider("google", {
    realm: realm.slug,
    key: "google",
    driver: "google",
    enabled: true,
    config: {
        clientId: config.require("googleClientId"),
        clientSecret: config.requireSecret("googleClientSecret"),
    },
}, { provider });

export const issuer = realm.issuer;
export const clientId = serviceClient.clientId;
export const clientSecret = serviceClient.clientSecret;
export const apiIdentifier = invoicesApi.identifier;
export const apiAudience = invoicesApiAudience;
export const googleCallbackUrl = google.callbackUrl;
