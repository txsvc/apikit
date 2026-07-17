---
spec_id: '06'
spec_name: oauth_provider_registry
title: Oauth Provider Registry
status: draft
created_at: '2026-07-17T10:45:41.747157+00:00'
updated_at: '2026-07-17T10:46:44.122358+00:00'
owner: ''
source: interactive
schema_version: 1
---
# OAuth Provider Registry

## Source

This spec is derived from the [apikit master PRD](docs/PRD.md). It covers the
pluggable OAuth provider registry, the GitHub provider implementation, and the
two public OAuth endpoints (`GET /auth/providers` and `POST /auth/callback`).

## Intent

The OAuth Provider Registry provides the pluggable identity provider
abstraction through which users authenticate with apikit-based services. Each
provider implements a common interface for authorize URL construction,
authorization code exchange, and user info extraction. Providers are registered
from TOML configuration at startup and discovered at runtime by the two public
OAuth endpoints.

The first iteration ships with **GitHub** as the sole provider. GitHub's
well-known URLs are built-in defaults; `authorize_url`, `token_url`, and
`userinfo_url` in config are optional overrides for GitHub Enterprise or
custom deployments.

Adding a new provider requires only registering it in the registry with its
URLs and field mappings — no changes to auth middleware, handlers, or the
callback flow.

## Goals

- Define a common `Provider` interface for OAuth identity providers.
- Implement a provider registry that loads providers from TOML config at
  startup.
- Ship a GitHub provider with built-in well-known URLs and support for
  optional URL overrides.
- Expose `GET /auth/providers` (public) to list configured providers.
- Expose `POST /auth/callback` (public) to exchange an authorization code
  for a user record and API key.
- Validate `redirect_uri` against a configured allowlist derived from
  `external_url` (production) and `http://localhost:*` (development).
- Upsert users on OAuth callback: create new users, update existing users'
  username and email.
- Enforce email requirement: reject login when the provider returns a null
  or empty email.
- Respect blocked user status: blocked users are not re-activated on OAuth
  login.
- Auto-grant admin role on first login when the user's email matches the
  designated `admin_email` from first boot.
- Generate a new API key for the user on each login, revoking any previously
  active key.
- Return user object (including `role` field) and API key in the callback
  response.

## Non-Goals

- **Auth middleware and token validation.** Covered by a separate auth spec.
  The OAuth endpoints are public and do not require authentication.
- **CLI login flow.** The CLI's `akc login` command (browser open, local
  callback server, state parameter) is covered by the CLI spec. This spec
  covers only the server-side endpoints.
- **Additional OAuth providers beyond GitHub.** The registry is designed for
  extensibility, but only GitHub is implemented in this iteration.
- **PKCE (Proof Key for Code Exchange).** Not implemented in the first
  iteration; the authorization code flow uses the traditional client_secret.
- **Refresh tokens from the identity provider.** The server exchanges the
  code for an access token, retrieves user info, and discards the IdP token.
  No IdP refresh tokens are stored.
- **Rate limiting on OAuth endpoints.**
- **CORS on OAuth endpoints.**
- **Admin token management.** The admin bootstrap and admin token lifecycle
  are covered by a separate admin bootstrap spec. This spec reads
  `admin_email` from `admin_config` but does not write it. There is no
  build-time or import-time dependency on the `admin_bootstrap` spec — the
  dependency is a runtime data dependency that flows through the shared
  database, fully covered by the `database_layer` upstream dependency.
- **PAT creation or management.** PAT lifecycle is covered by a separate spec.
- **API key refresh or revocation endpoints.** This spec only creates keys
  during the OAuth callback flow. Key management endpoints are covered by a
  separate spec.
- **OpenAPI cross-reference.** The `GET /auth/providers` and
  `POST /auth/callback` endpoints will be reflected in `api/openapi.yaml`
  when they are implemented; the `openapi_specification` spec is maintained
  independently and is not a declared dependency of this spec.
- **Configurable OAuth scopes.** The GitHub provider hardcodes `user:email`
  as its scope (see [GitHub Provider](#github-provider)). Scope
  configurability is not needed in the first iteration.
- **Concurrent login serialization beyond SQLite's single-connection
  guarantee.** Concurrent callback requests for the same user are serialized
  by SQLite's single-connection model (see
  [Concurrent Callback Requests](#concurrent-callback-requests)).

## Dependencies

| Spec | Dependency | Relationship |
|------|-----------|--------------|
| `01_server_core` | Upstream | Registers OAuth handlers on the Echo group returned by `APIGroup()`. Uses `LoadConfig()` for configuration (including `external_url` and `[[oauth.providers]]` sections). Uses `APIError()` for error responses. Uses `CacheMiddleware(CachePublic)` for the `/auth/providers` endpoint. Uses `FormatUTC()` and `NowUTC()` for timestamp handling. |
| `02_database_layer` | Upstream | Queries and upserts users in the `users` table via `db.SqlDB`. Creates API keys in the `api_keys` table. Reads `admin_email` from the `admin_config` table (written by the `admin_bootstrap` spec at runtime; this spec only reads). Uses `db.WithTx()` for transactional user upsert + key creation. Uses `db.WrapError()` for error mapping. Uses `db.FormatTime()` for timestamp storage. |

> **Note on `admin_bootstrap` dependency:** This spec reads `admin_email` from
> the `admin_config` table. That table is part of the database schema owned by
> `database_layer` and is populated at runtime by the `admin_bootstrap` spec.
> Because the dependency is a pure runtime data dependency (no shared code or
> import), `admin_bootstrap` is **not** listed as a formal upstream dependency.
> The `database_layer` dependency is sufficient to cover table-level access.

---

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25+ |
| HTTP framework | Echo v4 (`github.com/labstack/echo/v4`) |
| HTTP client | Go stdlib `net/http` (for identity provider API calls) |
| Config format | TOML (`github.com/BurntSushi/toml`) |
| UUID generation | `github.com/google/uuid` |
| Token hashing | SHA-256 (stdlib `crypto/sha256`) |
| Random generation | `crypto/rand` (for API key secrets and key IDs) |

## Repository Layout

```
internal/
  oauth/                  OAuth provider registry and provider implementations
    registry.go           Registry type, provider interface, registration
    github.go             GitHub provider implementation
    callback.go           POST /auth/callback handler logic
    providers.go          GET /auth/providers handler logic
    redirect.go           Redirect URI validation
```

The package is `internal/oauth` and is not importable by consuming projects.
The public registration API (for consuming projects to add custom providers)
is re-exported through the root `apikit` package.

---

## Server Configuration (OAuth)

The OAuth provider configuration extends the `Config` struct loaded by
`LoadConfig()` (from spec `01_server_core`):

```toml
[[oauth.providers]]
name = "github"
client_id = "..."
client_secret = "..."
# Optional overrides (GitHub well-known URLs are built-in):
# authorize_url = "https://github.example.com/login/oauth/authorize"
# token_url = "https://github.example.com/login/oauth/access_token"
# userinfo_url = "https://github.example.com/api/v3/user"
```

### Config Struct Extension

The `Config` struct (defined in `internal/config` and re-exported as
`apikit.Config`) is extended with:

```go
type Config struct {
    Server   ServerConfig
    Database DatabaseConfig
    Logging  LoggingConfig
    OAuth    OAuthConfig `toml:"oauth"`
}

type OAuthConfig struct {
    Providers []ProviderConfig `toml:"providers"`
}

type ProviderConfig struct {
    Name         string `toml:"name"`
    ClientID     string `toml:"client_id"`
    ClientSecret string `toml:"client_secret"`
    AuthorizeURL string `toml:"authorize_url"`  // optional override
    TokenURL     string `toml:"token_url"`       // optional override
    UserinfoURL  string `toml:"userinfo_url"`    // optional override
}
```

### Config Validation Rules (OAuth-specific)

`LoadConfig()` validates OAuth provider configuration:

- Each provider must have a non-empty `name`, `client_id`, and
  `client_secret`. Missing any of these returns `(nil, error)`.
- Provider `name` values must be unique across the `[[oauth.providers]]`
  array. Duplicate names return `(nil, error)`.
- An empty `[[oauth.providers]]` array is valid — the server can start
  without any OAuth providers configured (useful for admin-token-only
  operation during setup). The `GET /auth/providers` endpoint returns an
  empty array in this case.
- `authorize_url`, `token_url`, and `userinfo_url` are optional. When
  absent or empty, the provider implementation supplies its own defaults
  (e.g. GitHub's well-known URLs).

---

## Functional Requirements

### Provider Interface

The provider interface defines the contract that every OAuth provider must
implement:

```go
// Provider defines the OAuth provider contract.
type Provider interface {
    // Name returns the provider's identifier (e.g. "github").
    Name() string

    // AuthorizeURL returns the full authorization URL that the client should
    // redirect the user to. The state parameter is included for CSRF
    // protection. The redirectURI is the callback URL the IdP should redirect
    // back to after authorization.
    AuthorizeURL(state, redirectURI string) string

    // Exchange trades an authorization code for an access token from the
    // identity provider. The redirectURI must match the one used in the
    // authorization request.
    Exchange(ctx context.Context, code, redirectURI string) (string, error)

    // UserInfo retrieves user information from the identity provider using
    // the access token obtained from Exchange. Returns a UserInfo struct
    // containing the user's identity fields.
    UserInfo(ctx context.Context, accessToken string) (*UserInfo, error)
}

// UserInfo holds the identity fields extracted from the OAuth provider.
type UserInfo struct {
    Username   string // Provider username / login
    Email      string // Email address (required; empty triggers login failure)
    ProviderID string // Provider-specific user identifier (e.g. GitHub numeric ID as string)
}
```

### Provider Registry

The registry manages the set of configured OAuth providers:

```go
// Registry holds all configured OAuth providers, keyed by name.
type Registry struct {
    providers map[string]Provider
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry

// Register adds a provider to the registry. Returns an error if a provider
// with the same name is already registered.
func (r *Registry) Register(p Provider) error

// Get returns the provider with the given name, or nil if not found.
func (r *Registry) Get(name string) Provider

// List returns all registered provider names in alphabetical order.
func (r *Registry) List() []string
```

### GitHub Provider

The GitHub provider implements the `Provider` interface with built-in
well-known URLs:

| URL | Default | Config Override |
|-----|---------|-----------------|
| Authorize URL | `https://github.com/login/oauth/authorize` | `authorize_url` |
| Token URL | `https://github.com/login/oauth/access_token` | `token_url` |
| User Info URL | `https://api.github.com/user` | `userinfo_url` |

When config override fields are empty, the defaults above are used. When
overrides are provided (e.g. for GitHub Enterprise), they replace the
defaults entirely.

#### GitHub OAuth Scope

The GitHub provider hardcodes the OAuth scope **`user:email`**. This scope
is sufficient to retrieve the user's email address from the `/user` endpoint.
The `login` and `id` fields are always available on the GitHub `/user`
endpoint without additional scopes. The scope is appended as a query
parameter when constructing the authorization URL:

```
https://github.com/login/oauth/authorize?client_id=<id>&scope=user:email
```

Making scope configurable adds complexity without value in the first
iteration. A future provider implementation owns its own `AuthorizeURL`
construction and may append any scopes it requires internally.

#### GitHub `Exchange` Implementation

1. POST to the token URL with `client_id`, `client_secret`, `code`, and
   `redirect_uri` as form-encoded body.
2. Set `Accept: application/json` header to receive JSON response.
3. Parse the JSON response to extract `access_token`.
4. Return an error if the response contains an `error` field or if
   `access_token` is empty.

#### GitHub `UserInfo` Implementation

1. GET the user info URL with `Authorization: Bearer <access_token>` header.
2. Parse the JSON response to extract:
   - `login` → `UserInfo.Username`
   - `email` → `UserInfo.Email`
   - `id` (numeric) → `UserInfo.ProviderID` (converted to string)
3. Return the populated `UserInfo`.

### Config-Driven Provider Registration

At server startup, the bootstrap code:

1. Reads `[[oauth.providers]]` from the loaded config.
2. Creates a `Registry`.
3. For each configured provider:
   - If `name == "github"`, creates a GitHub provider instance with the
     config's `client_id`, `client_secret`, and optional URL overrides.
   - For unknown provider names, returns a startup error (only known
     provider types can be instantiated; new types require code changes
     to the provider factory, but no changes to handlers or middleware).
4. Registers each provider in the registry.

### `GET /auth/providers` Endpoint

**Path:** `{mount_point}/auth/providers`
**Method:** GET
**Auth:** None (public endpoint)
**Cache-Control:** `public, max-age=300` (via `CacheMiddleware(CachePublic)`)

Returns the list of configured OAuth providers. No secrets are exposed.

**Response (HTTP 200):**
```json
[
  {
    "name": "github",
    "authorize_url": "https://github.com/login/oauth/authorize?client_id=...&scope=user:email"
  }
]
```

Each entry includes:
- `name` — the provider identifier.
- `authorize_url` — the authorization URL with `client_id` and scope
  pre-populated. For GitHub, the scope is always `user:email` (hardcoded).
  The `state` and `redirect_uri` parameters are **not** included — they are
  added by the CLI at request time (the CLI generates a cryptographic `state`
  parameter for CSRF protection and supplies its own local callback
  `redirect_uri`).

When no providers are configured, returns an empty JSON array `[]` with
HTTP 200.

### `POST /auth/callback` Endpoint

**Path:** `{mount_point}/auth/callback`
**Method:** POST
**Auth:** None (public endpoint)
**Cache-Control:** `no-store` (inherited from mount point group default)

Exchanges an authorization code for a user record and API key.

**Request body:**
```json
{
  "provider": "github",
  "code": "<authorization_code>",
  "redirect_uri": "http://localhost:54321/callback",
  "expires": 90
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | string | Yes | Provider name (must match a registered provider) |
| `code` | string | Yes | Authorization code from the identity provider |
| `redirect_uri` | string | Yes | The redirect URI used in the authorization request |
| `expires` | integer | No | API key expiry in days: `0`, `30`, `60`, or `90`; default `90` |

**Success Response (HTTP 200):**
```json
{
  "user": {
    "id": "<uuid>",
    "username": "<string>",
    "email": "<string>",
    "full_name": "<string>",
    "status": "active",
    "role": "user",
    "provider": "<string>",
    "provider_id": "<string>",
    "created_at": "<RFC 3339 timestamp>",
    "updated_at": "<RFC 3339 timestamp>"
  },
  "api_key": {
    "key": "<prefix>_<key_id>_<secret>",
    "key_id": "<key_id>",
    "expires_at": "<RFC 3339 timestamp or null>"
  }
}
```

#### Callback Processing Steps

1. **Parse and validate request body.** Reject with HTTP 400 if `provider`,
   `code`, or `redirect_uri` are missing or empty. Reject with HTTP 400 if
   `expires` is present but not one of `0`, `30`, `60`, `90`.

2. **Look up provider.** Retrieve the provider from the registry by name.
   Reject with HTTP 400 if the provider is not found:
   `{"error": {"code": 400, "message": "unknown provider: <name>"}}`.

3. **Validate redirect URI.** Check `redirect_uri` against the configured
   allowlist (see [Redirect URI Validation](#redirect-uri-validation)).
   Reject with HTTP 400 if the URI is not allowed.

4. **Exchange code for access token.** Call `provider.Exchange(ctx, code, redirectURI)`.
   If the exchange fails, return HTTP 401:
   `{"error": {"code": 401, "message": "authorization code exchange failed"}}`.

5. **Retrieve user info.** Call `provider.UserInfo(ctx, accessToken)`.
   If the call fails, return HTTP 502:
   `{"error": {"code": 502, "message": "failed to retrieve user info from provider"}}`.

6. **Validate email.** If `UserInfo.Email` is empty, return HTTP 400:
   `{"error": {"code": 400, "message": "provider returned empty email; email is required"}}`.

7. **Upsert user.** Within a database transaction (`db.WithTx`):
   a. Query for an existing user by `(provider, provider_id)`.
   b. If the user exists:
      - If the user's status is `blocked`, return HTTP 403:
        `{"error": {"code": 403, "message": "user is blocked"}}`.
        The transaction is rolled back. Blocked users are **not**
        re-activated on OAuth login.
      - Update the user's `username`, `email`, and `updated_at`.
   c. If the user does not exist:
      - Generate a new UUID for the user ID.
      - Check `admin_config` for the `admin_email` value. If the
        user's email matches `admin_email` and no admin user exists
        yet (no user with `role = 'admin'` in the database), set the
        new user's role to `admin`. Otherwise, set it to `user`.
      - Insert the new user with `status = 'active'`.
   d. Revoke any existing active API key for the user (set `revoked_at`
      to now).
   e. Generate a new API key:
      - Generate a random 8-character alphanumeric `key_id`.
      - Generate a random 32-character alphanumeric `secret`.
      - Compute the SHA-256 hash of `secret`.
      - Calculate `expires_at` from the `expires` parameter: `null` when
        `expires` is `0`, otherwise `now + (expires * 24h)`.
      - Insert the key record into `api_keys`.
   f. Commit the transaction.

8. **Return response.** Return HTTP 200 with the user object and the full
   API key (including plaintext secret). The plaintext secret is only
   available at this moment — it is not stored and cannot be retrieved later.

### Redirect URI Validation

The `redirect_uri` parameter in `POST /auth/callback` is validated against
an allowlist:

- **Development (localhost):** Any URI matching `http://localhost:<port>/*`
  is accepted, where `<port>` is any valid port number. This supports the
  CLI's local callback server which binds to a random available port.
- **Production:** When `external_url` is configured in `config.toml`, URIs
  that are rooted at the `external_url` are accepted (scheme + host must
  match; path may extend beyond the external URL's path).
- Both localhost and external_url URIs are accepted simultaneously when
  `external_url` is configured. When `external_url` is not configured
  (empty string), only localhost URIs are accepted.
- **Rejection:** URIs that match neither pattern are rejected with HTTP 400:
  `{"error": {"code": 400, "message": "redirect_uri is not allowed"}}`.

### Concurrent Callback Requests

Concurrent `POST /auth/callback` requests for the same user (e.g. two
simultaneous OAuth logins) are handled via **last-write-wins** through
SQLite's single-connection serialization.

Because `database_layer` configures SQLite with `SetMaxOpenConns(1)`, all
database transactions are serialized through a single connection. When two
concurrent callback requests race on the upsert + key revocation for the
same user, the second transaction observes the state left by the first
(including the newly created key) and proceeds normally — revoking the
first transaction's key and issuing yet another new one. The user ends up
with whichever key was issued last.

This behavior is acceptable for a single-user interactive login flow where
concurrent logins are not a realistic scenario. No application-level locking
or serializable isolation hints are required beyond what SQLite already
provides.

### Provider Removal Handling

If a provider is removed from the `[[oauth.providers]]` configuration,
existing users who authenticated through that provider retain their API keys
and PATs. Those credentials remain valid until they expire or are revoked.
The users cannot re-authenticate via OAuth through the removed provider, but
they can authenticate via any other configured provider if one is available.

No database cleanup is performed when a provider is removed from config.
The `users.provider` and `users.provider_id` columns retain their original
values for audit purposes.

### API Key Generation

API keys follow the format `<prefix>_<key_id>_<secret>` where:
- `<prefix>` is the build-time configurable `apikit.TokenPrefix` (default `ak`).
- `<key_id>` is a random 8-character alphanumeric string (`[a-zA-Z0-9]`).
- `<secret>` is a random 32-character alphanumeric string (`[a-zA-Z0-9]`).

Both `key_id` and `secret` are generated using `crypto/rand` for
cryptographic security. The `secret` is hashed with SHA-256 before storage;
only the hash is persisted in the `api_keys.secret_hash` column.

---

## Interfaces

### Public API (root module re-exports)

The following types and functions are re-exported from `internal/oauth`
through the root `apikit` package so that consuming projects can interact
with the provider registry:

```go
// Provider is the re-exported OAuth provider interface. Consuming projects
// implement this to add custom OAuth providers.
type Provider = oauth.Provider

// UserInfo is the re-exported struct containing identity fields from an
// OAuth provider.
type UserInfo = oauth.UserInfo

// RegisterOAuthHandlers registers the GET /auth/providers and
// POST /auth/callback handlers on the given Echo group. Called during
// server bootstrap after providers are loaded from config.
// This function is internal to apikit's bootstrap and is not part of the
// public API for consuming projects.
func RegisterOAuthHandlers(group *echo.Group, registry *oauth.Registry, database *db.DB, externalURL string)
```

### OAuth Handler Registration

The OAuth endpoints are registered during server bootstrap. The registration
function receives the Echo group (from `APIGroup()`), the provider registry,
the database handle, and the `external_url` from config. It registers:

- `GET /auth/providers` with `CacheMiddleware(CachePublic)`.
- `POST /auth/callback` (inherits `CacheNoStore` from group default).

### HTTP Client for Provider API Calls

Provider implementations use Go's `net/http` stdlib client for
communication with identity providers. A shared `*http.Client` with a
**30-second timeout** is created once and injected into provider instances
at construction time. Providers do not create their own HTTP clients.

The shared client uses `http.DefaultTransport` with no custom TLS
configuration (TLS is terminated by the upstream proxy, not by apikit). The
30-second timeout prevents indefinite hangs when an identity provider is
unresponsive.

---

## Error Handling

| Condition | HTTP Status | Error Message |
|-----------|-------------|---------------|
| Missing `provider` field | 400 | `"provider is required"` |
| Missing `code` field | 400 | `"code is required"` |
| Missing `redirect_uri` field | 400 | `"redirect_uri is required"` |
| Invalid `expires` value | 400 | `"expires must be 0, 30, 60, or 90"` |
| Unknown provider name | 400 | `"unknown provider: <name>"` |
| Redirect URI not in allowlist | 400 | `"redirect_uri is not allowed"` |
| Empty email from provider | 400 | `"provider returned empty email; email is required"` |
| Code exchange failure | 401 | `"authorization code exchange failed"` |
| User is blocked | 403 | `"user is blocked"` |
| Provider user info API failure | 502 | `"failed to retrieve user info from provider"` |
| Database error during upsert | 500 | `"internal server error"` |

All errors use the standard `APIError()` envelope from spec `01_server_core`.

---

## Testing Strategy

### Unit Tests

- **Provider interface:** verify that the GitHub provider correctly
  implements all three interface methods.
- **GitHub `AuthorizeURL`:** verify URL construction with default and
  override URLs, including `client_id` and hardcoded `scope=user:email`
  parameters.
- **GitHub `Exchange`:** verify correct HTTP request construction (method,
  URL, headers, body) using an `httptest.Server`. Verify error handling for
  failed exchanges and empty access tokens.
- **GitHub `UserInfo`:** verify correct HTTP request construction and
  response parsing using an `httptest.Server`. Verify extraction of
  `login`, `email`, and `id` fields.
- **Registry `Register`:** verify successful registration and duplicate-name
  rejection.
- **Registry `Get`:** verify lookup by name and nil return for unknown names.
- **Registry `List`:** verify alphabetical ordering of provider names.
- **Redirect URI validation:** verify acceptance of `http://localhost:*`
  URIs with various ports, acceptance of URIs rooted at `external_url`,
  rejection of unmatched URIs, and behavior when `external_url` is empty.
- **Request body validation:** verify rejection of missing `provider`,
  `code`, `redirect_uri`, and invalid `expires` values.
- **Config validation:** verify rejection of providers with missing `name`,
  `client_id`, or `client_secret`. Verify rejection of duplicate provider
  names. Verify that empty provider array is accepted.

### Integration Tests

- **Full callback flow (happy path):** configure a GitHub provider with an
  `httptest.Server` standing in for GitHub's token and user info endpoints.
  POST to `/auth/callback` with a valid code. Verify:
  - HTTP 200 response with correct user and API key structure.
  - User is created in the database with correct fields.
  - API key is created in the database with correct `secret_hash`.
  - The returned API key `key` field matches the format
    `<prefix>_<key_id>_<secret>`.
- **Existing user upsert:** create a user, then POST to `/auth/callback`
  again with the same `provider_id`. Verify username and email are updated
  and a new API key is issued (previous key revoked).
- **Blocked user rejection:** create a user, block them, then POST to
  `/auth/callback`. Verify HTTP 403.
- **Admin auto-grant:** set `admin_email` in `admin_config`, then POST to
  `/auth/callback` with a user whose email matches. Verify the user's role
  is `admin`.
- **Admin auto-grant only on first admin:** create an admin user, then POST
  to `/auth/callback` with the `admin_email`. Verify the new user gets role
  `user` (admin already exists).
- **Empty email rejection:** configure mock provider to return empty email.
  Verify HTTP 400.
- **Key expiry:** verify that `expires: 0` produces `null` `expires_at`,
  and `expires: 30` produces correct `expires_at` timestamp.
- **Previous key revocation:** verify that re-login revokes the previous
  API key (check `revoked_at` is set on old key).
- **Provider list endpoint:** verify `GET /auth/providers` returns provider
  names and authorize URLs (including `scope=user:email` for GitHub). Verify
  `Cache-Control: public, max-age=300` header. Verify no secrets in response.
- **Empty provider list:** verify `GET /auth/providers` returns `[]` when
  no providers are configured.
- **Redirect URI validation integration:** verify callback rejects URIs
  not matching the allowlist with HTTP 400.
- **Unknown provider:** verify callback returns HTTP 400 for an
  unregistered provider name.
- **Config-driven registration:** verify that providers from config are
  available in the registry and reachable via the endpoints.
- **Concurrent callback serialization:** verify that two sequential
  transactions for the same user each revoke the prior key and issue a new
  one, leaving the user with a single active key at the end.

---

## Design Decisions

- **Interface-based provider abstraction.** The `Provider` interface
  decouples the OAuth flow from any specific identity provider. Adding a
  new provider (e.g. GitLab, Google) requires implementing three methods
  and registering the instance — no handler or middleware changes.
- **Built-in GitHub well-known URLs with optional overrides.** GitHub's
  standard OAuth URLs are hardcoded as defaults. Config-level overrides
  exist for GitHub Enterprise deployments. This avoids requiring config
  for the common case while supporting custom deployments.
- **Hardcoded `user:email` scope for GitHub.** The scope is sufficient to
  retrieve all required identity fields (`login`, `email`, `id`). Making
  scope configurable adds complexity without value in the first iteration.
  Future provider implementations own their own `AuthorizeURL` construction
  and can append any scopes they require.
- **Registry keyed by name.** Provider names are unique identifiers that
  appear in both the config and the callback request. The registry enforces
  uniqueness at registration time.
- **Config validation at load time.** Missing `name`, `client_id`, or
  `client_secret` are caught during `LoadConfig()`, failing fast before
  the server starts serving traffic.
- **Redirect URI allowlist.** The allowlist approach (localhost + external_url)
  prevents open-redirect attacks. Localhost is always accepted for CLI
  development flows. Production URIs are restricted to the configured
  external_url.
- **Upsert by `(provider, provider_id)`.** Users are matched by their
  identity provider credentials, not by email. This prevents account
  collision when two providers return the same email and ensures the
  `UNIQUE(provider, provider_id)` constraint is leveraged.
- **Blocked users not re-activated.** OAuth login does not change a blocked
  user's status. This is a deliberate security decision — blocking is an
  administrative action that should only be reversed by an explicit unblock.
- **Admin auto-grant on first admin only.** The `admin_email` auto-promotion
  only fires when no admin user exists yet in the database. This prevents
  the email match from silently overriding a demotion or creating unexpected
  admin escalation on subsequent logins.
- **Shared HTTP client with 30-second timeout.** A single `*http.Client`
  instance is shared across provider implementations. The 30-second timeout
  prevents indefinite hangs when an identity provider is unresponsive.
  Provider implementations receive the client via injection, not via global
  state.
- **No PKCE in first iteration.** The authorization code flow uses the
  traditional `client_secret` exchange. PKCE can be added later without
  changing the provider interface (it only affects the `Exchange` method
  implementation).
- **Provider removal is a no-op for existing data.** Removing a provider
  from config does not delete users or credentials. Existing users retain
  access via their API keys and PATs. This follows the principle of least
  surprise and preserves audit trails.
- **API key generation in the callback transaction.** Key generation
  (including old key revocation) happens inside the same database
  transaction as the user upsert. This ensures atomicity — either the
  user is upserted and a new key is issued, or nothing changes.
- **`crypto/rand` for key material.** Both `key_id` and `secret` use
  `crypto/rand` for cryptographic security. `math/rand` is explicitly
  not used for security-sensitive random generation.
- **502 for provider API failures.** When the identity provider's API
  call fails (user info retrieval), the server returns HTTP 502 (Bad
  Gateway) to indicate the upstream dependency failed. This distinguishes
  provider failures from client errors (4xx) and server bugs (500).
- **Last-write-wins for concurrent logins via SQLite serialization.**
  SQLite's `SetMaxOpenConns(1)` serializes all transactions through a single
  connection. Concurrent callback requests for the same user are handled
  naturally — the second transaction revokes the first transaction's key and
  issues a new one. No application-level locking is required. Concurrent
  logins are not a realistic scenario for this interactive CLI login flow.
- **`admin_bootstrap` is not a formal dependency.** This spec reads
  `admin_email` from `admin_config`, which is populated at runtime by the
  `admin_bootstrap` spec. Because there is no shared code or import, the
  dependency is a runtime data dependency covered entirely by `database_layer`.
  Declaring `admin_bootstrap` as a formal upstream dependency would create
  an artificial ordering constraint with no implementation benefit.

---

## Glossary

| Term | Definition |
|------|------------|
| **Provider** | An OAuth identity provider (e.g. GitHub) that implements the `Provider` interface for authentication. |
| **Registry** | The `oauth.Registry` type that holds all configured providers, keyed by name. |
| **Provider interface** | The Go interface (`Provider`) defining `Name()`, `AuthorizeURL()`, `Exchange()`, and `UserInfo()` methods. |
| **UserInfo** | The struct returned by `Provider.UserInfo()` containing `Username`, `Email`, and `ProviderID` fields. |
| **authorize_url** | The URL the user's browser is redirected to for OAuth authorization. GitHub default: `https://github.com/login/oauth/authorize`. |
| **token_url** | The URL used server-to-server to exchange an authorization code for an access token. GitHub default: `https://github.com/login/oauth/access_token`. |
| **userinfo_url** | The URL used server-to-server to retrieve user identity information using the access token. GitHub default: `https://api.github.com/user`. |
| **redirect_uri** | The URL the identity provider redirects back to after authorization. Must match the configured allowlist. |
| **admin_email** | The email address stored in `admin_config` that designates which user becomes the first admin on initial OAuth login. Written by `admin_bootstrap`; read by this spec at runtime via the shared database. |
| **upsert** | A database operation that inserts a new row if no match exists or updates the existing row if a match is found. Used for user records matched by `(provider, provider_id)`. |
| **key_id** | The random 8-character alphanumeric identifier portion of an API key, stored in plaintext in the `api_keys` table. |
| **secret** | The random 32-character alphanumeric portion of an API key, stored only as a SHA-256 hash. The plaintext is returned once at creation. |
| **well-known URLs** | The standard OAuth endpoint URLs for a provider (authorize, token, userinfo) that are built into the provider implementation as defaults. |
| **external_url** | The public-facing URL of the apikit server, configured in `config.toml`, used to validate production redirect URIs. |
| **scope** | The OAuth permission scope(s) requested from the identity provider during authorization. The GitHub provider hardcodes `user:email`. |
| **last-write-wins** | The concurrency resolution strategy for simultaneous OAuth logins: SQLite's single-connection serialization ensures transactions execute sequentially; the final transaction's key is the one the user retains. |
