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

const serviceClient = new lock.Client("service-client", {
    realm: realm.slug,
    name: "Backend service",
    tokenEndpointAuthMethod: "client_secret_basic",
    grantTypes: ["client_credentials"],
    consentRequired: false,
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
export const googleCallbackUrl = google.callbackUrl;
