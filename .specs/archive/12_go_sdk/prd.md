---
spec_id: '12'
spec_name: go_sdk
title: Go Sdk
status: draft
created_at: '2026-07-17T12:25:09.153887+00:00'
updated_at: '2026-07-17T12:39:02.178359+00:00'
owner: ''
source: interactive
schema_version: 1
---
# Go SDK

## Source Reference

This spec is derived from the [apikit master PRD](docs/PRD.md). It covers the
**Go SDK** component — spec 11 of 15. The master PRD sections on "Go SDK"
(under SDKs), the API Endpoints listing, the Error Handling envelope, and the
Credential Model are the primary sources. The OpenAPI 3.1 specification
(spec 03) is the canonical definition of all request/response schemas that the
SDK implements, including expected HTTP success status codes per endpoint.

## Intent

Implement a typed Go client library that wraps every built-in API endpoint with
a Go function. The SDK is part of the apikit Go module itself — consuming
projects that import apikit for the server framework also get the client library.
The CLI (specs 13-15) wraps this SDK exclusively; the SDK is the single
implementation of API client logic in Go.

The SDK provides a `Client` struct constructed via `apikit.NewClient(baseURL,
opts...)` using the functional options pattern. Each built-in endpoint has a
corresponding method on `Client` that accepts `context.Context` as its first
parameter and returns a typed response struct (or slice) plus an error. API
errors from the server are returned as typed `*APIError` values (not raw HTTP
errors), enabling callers to inspect status codes and messages
programmatically.

## Goals

- Provide a `Client` struct in the root `apikit` package with a constructor
  `NewClient(baseURL string, opts ...ClientOption)` using the functional options
  pattern. The `Client` struct is safe for concurrent use from multiple goroutines
  after construction.
- Implement `ClientOption` functions:
  - `WithAPIKey(key string)` — sets the Bearer token for the `Authorization`
    header on every request.
  - `WithHTTPClient(client *http.Client)` — overrides the default
    `http.Client` (enables custom timeouts, TLS config, transport).
  - `WithRequestID(id string)` — sets a static `X-Request-ID` header on every
    request. When not set, no `X-Request-ID` is sent by the client (the server
    generates its own).
  - `WithMountPoint(path string)` — overrides the default mount point
    (`/api/v1`). The mount point is normalized: a leading slash is added if
    missing, and a trailing slash is stripped. For example, `"api/v1"` becomes
    `"/api/v1"`.
- Define typed request/response structs matching the OpenAPI 3.1 schemas:
  - `User` — matches the User object schema.
  - `APIKeyMeta` — metadata-only key object returned in listings (`key_id`,
    `created_at`, `expires_at`, `revoked_at`).
  - `APIKeyFull` — full key object returned on creation/refresh (`key`,
    `key_id`, `expires_at`).
  - `PAT` — metadata-only token object returned in listings (`token_id`,
    `name`, `permissions`, `created_at`, `expires_at`, `revoked_at`).
  - `PATFull` — full token object returned on creation (`token`,
    `token_id`, `name`, `permissions`, `expires_at`).
  - `Organization` — matches the Organization object schema.
  - `OAuthProvider` — provider discovery object (`name`, `authorize_url`).
  - `AuthCallbackRequest` — request body for `POST /auth/callback` (`provider`,
    `code`, `redirect_uri`, `expires`).
  - `AuthCallbackResponse` — response from `POST /auth/callback` containing
    `User` and `APIKeyFull`.
  - `CreateUserRequest` — request body for `POST /users` (`username`, `email`,
    `provider`, `provider_id`).
  - `UpdateUserRequest` — request body for `PATCH /user` and `PATCH /users/:id`
    (`full_name`). The `FullName` field is a plain `string` (not a pointer);
    every PATCH request always sets `full_name`. This is intentional: `full_name`
    is the only patchable field today, and the server always expects it to be
    present.
  - `CreateTokenRequest` — request body for `POST /user/tokens` (`name`,
    `permissions`, `expires`).
  - `CreateOrgRequest` — request body for `POST /orgs` (`name`, `slug`, `url`).
  - `UpdateOrgRequest` — request body for `PATCH /orgs/:id` (`name`, `url`).
  - `HealthResponse` — health probe response (`status`).
  - `VersionResponse` — version endpoint response (`version`, `build_time`,
    `commit`, `mount_point`).
  - `RevokeKeyResponse` — response from `DELETE /user/keys/:key_id` (`key_id`,
    `revoked_at`).
  - `Error` — the API error envelope (`code`, `message`).
- Implement endpoint wrapper methods on `Client`. Every method takes
  `context.Context` as the first parameter. ETag-capable methods return
  `(*Response[T], error)`; other methods return `(*T, error)` or
  `([]*T, error)` for list endpoints:
  - **Authenticated user endpoints:**
    - `GetUser(ctx, ...RequestOption) (*Response[User], error)`
    - `UpdateUser(ctx, *UpdateUserRequest) (*User, error)`
    - `ListKeys(ctx) ([]*APIKeyMeta, error)`
    - `RefreshKey(ctx, keyID string) (*APIKeyFull, error)`
    - `RevokeKey(ctx, keyID string) (*RevokeKeyResponse, error)`
    - `ListTokens(ctx) ([]*PAT, error)`
    - `CreateToken(ctx, *CreateTokenRequest) (*PATFull, error)`
    - `GetToken(ctx, tokenID string, ...RequestOption) (*Response[PAT], error)`
    - `RevokeToken(ctx, tokenID string) error`
    - `ListUserOrgs(ctx) ([]*Organization, error)`
  - **Admin user endpoints:**
    - `ListUsers(ctx, *ListUsersOptions) ([]*User, error)` — options struct
      with `IncludeBlocked bool`.
    - `GetUserByID(ctx, id string, ...RequestOption) (*Response[User], error)`
    - `CreateUser(ctx, *CreateUserRequest) (*User, error)`
    - `UpdateUserByID(ctx, id string, *UpdateUserRequest) (*User, error)`
    - `PromoteUser(ctx, id string) (*User, error)`
    - `DemoteUser(ctx, id string) (*User, error)`
    - `BlockUser(ctx, id string) (*User, error)`
    - `UnblockUser(ctx, id string) (*User, error)`
    - `ListUserKeys(ctx, userID string) ([]*APIKeyMeta, error)`
    - `RevokeUserKey(ctx, userID string, keyID string) error`
    - `ListUserTokens(ctx, userID string) ([]*PAT, error)`
    - `RevokeUserToken(ctx, userID string, tokenID string) error`
  - **Admin organization endpoints:**
    - `CreateOrg(ctx, *CreateOrgRequest) (*Organization, error)`
    - `ListOrgs(ctx, *ListOrgsOptions) ([]*Organization, error)` — options
      struct with `IncludeBlocked bool`.
    - `GetOrg(ctx, id string, ...RequestOption) (*Response[Organization], error)`
    - `UpdateOrg(ctx, id string, *UpdateOrgRequest) (*Organization, error)`
    - `DeleteOrg(ctx, id string) error`
    - `BlockOrg(ctx, id string) (*Organization, error)`
    - `UnblockOrg(ctx, id string) (*Organization, error)`
    - `ListOrgMembers(ctx, orgID string) ([]*User, error)`
    - `AddOrgMember(ctx, orgID string, userID string) error`
    - `RemoveOrgMember(ctx, orgID string, userID string) error`
  - **Auth endpoints (public):**
    - `GetProviders(ctx) ([]*OAuthProvider, error)`
    - `ExchangeOAuthCode(ctx, *AuthCallbackRequest) (*AuthCallbackResponse, error)`
  - **Health/meta endpoints (public):**
    - `Healthz(ctx) (*HealthResponse, error)`
    - `Readyz(ctx) (*HealthResponse, error)`
    - `Version(ctx) (*VersionResponse, error)`
- Return API errors as typed `*APIError` values. The `APIError` struct contains
  `Code int` and `Message string`, matching the server's error envelope. The
  `APIError` type implements the `error` interface. The `Error()` method returns
  the string `"API error {Code}: {Message}"` (e.g., `"API error 404: User not found"`).
  Callers use `errors.As(err, &apiErr)` to inspect error details. Non-API errors
  (network failures, JSON decode errors, context cancellation) are returned as
  plain Go errors (not `*APIError`).
- Include `Authorization: Bearer <key>` on every request when an API key is
  configured via `WithAPIKey`.
- Include `X-Request-ID: <id>` on every request when set via `WithRequestID`.
- Support query parameters:
  - `ListUsers` and `ListOrgs` accept an options struct with
    `IncludeBlocked bool`. When `true`, `?include_blocked=true` is appended
    to the request URL. When `false` or the options struct is `nil`, no query
    parameter is sent (server default applies).
  - `ListUsersOptions` and `ListOrgsOptions` are designed to grow: additional
    filter fields (e.g., pagination, search, sort) will be added in future
    iterations. Query parameter construction uses `url.Values` so that new
    fields can be appended without restructuring the implementation.
- Support ETag/If-None-Match for conditional GET requests:
  - Methods that support conditional GET (`GetUser`, `GetUserByID`, `GetToken`,
    `GetOrg`) accept a variadic `...RequestOption` parameter.
  - `WithIfNoneMatch(etag string)` is a `RequestOption` that adds the
    `If-None-Match` header to the request.
  - When the server returns HTTP 304, the method returns `nil` for the
    `*Response[T]` value and the sentinel error `ErrNotModified`. The caller
    checks with `errors.Is(err, ErrNotModified)`. Response headers from a 304
    are not accessible via this API (the `*Response[T]` is nil).
  - The ETag value from the response is accessible via `Response.Header` on
    successful (200) responses. The SDK does not parse or cache ETags
    automatically. Methods that support conditional GET return a `*Response[T]`
    wrapper that includes both the typed body and the raw `*http.Response`
    headers for ETag capture.
- Set `Content-Type: application/json` on all requests that include a body.
- Set `Accept: application/json` on all requests.
- Use the configurable mount point in URL construction. The `baseURL` passed
  to `NewClient` is the server's root URL (e.g., `https://api.example.com`).
  The SDK appends `/api/v1` (the default mount point) for API endpoints. Health
  probe endpoints (`/healthz`, `/readyz`, `/version`) are appended directly to
  the base URL without the mount point. Auth endpoints (`/auth/providers`,
  `/auth/callback`) use the mount point.
  A `WithMountPoint(path string)` option overrides the default `/api/v1`.
  The mount point is normalized at construction time: a leading slash is added
  if missing, and a trailing slash is stripped (e.g., `"api/v1"` → `"/api/v1"`,
  `"/api/v1/"` → `"/api/v1"`).
- Decode list endpoint responses as bare JSON arrays (e.g., `[{...}, {...}]`),
  not as wrapped objects. The SDK decodes list responses directly into `[]T`.
  This matches the OpenAPI spec (spec 03) which defines list responses as bare
  arrays. The expected HTTP success status codes per endpoint are defined in the
  OpenAPI spec (spec 03) and the SDK's internal `do` method branches on those
  codes (200, 201, 204) accordingly.
- Accept the default Go `http.Client` redirect behavior: redirects are followed
  automatically up to 10 hops. The `Authorization` header may be stripped on
  cross-origin redirects per Go's default policy; callers who require custom
  redirect handling should inject a configured `*http.Client` via
  `WithHTTPClient`.
- **Path parameter encoding:** Path parameters (`keyID`, `userID`, `tokenID`,
  `orgID`, `id`) are assumed to be URL-safe (UUIDs or alphanumeric strings).
  The SDK does not apply `url.PathEscape` to path parameters. Callers are
  responsible for ensuring IDs are URL-safe before passing them to SDK methods.
- **Empty `baseURL`:** `NewClient` silently accepts an empty `baseURL` string
  without panicking or returning an error. The caller is responsible for
  providing a valid URL. Requests made with an empty base URL will fail at the
  HTTP transport layer with a plain error (not `*APIError`).

## Non-Goals

- **Code generation from OpenAPI.** The SDK is hand-written to match the
  OpenAPI spec. Automated code generation is not used.
- **Automatic retry logic.** The SDK does not retry failed requests. Callers
  implement their own retry strategy if needed.
- **Automatic ETag caching.** The SDK does not cache ETags or responses. The
  caller manages ETag values and passes them via `WithIfNoneMatch`.
- **Rate limiting or throttling.** Not implemented in the first iteration.
- **WebSocket or streaming support.** All endpoints are request-response.
- **OAuth login flow orchestration.** The SDK provides `GetProviders` and
  `ExchangeOAuthCode` as building blocks; the full browser-based OAuth flow
  (opening browser, starting callback server, state parameter management) is
  the CLI's responsibility (specs 13-15).
- **Automatic token refresh.** The SDK does not automatically refresh expired
  API keys. The caller is responsible for calling `RefreshKey` or re-authenticating.
- **Pagination.** Not implemented in the first iteration per the master PRD.
- **Server-side functionality.** The SDK is a client library only. Server
  construction, handler registration, and middleware are separate concerns.
- **Multipart or file upload support.** All endpoints use JSON bodies.
- **Response body validation against OpenAPI schemas.** The SDK trusts the
  server's response structure.
- **Admin token support.** The SDK authenticates with API keys and PATs via
  the same `WithAPIKey` option. Admin token authentication uses the same
  Bearer token mechanism and requires no special SDK support.
- **go.mod management.** The go.mod `go` directive and module versioning are
  out of scope for this spec. The minimum supported Go version is the latest
  stable release at implementation time (Go 1.18+ is the absolute floor due to
  generics support required by `Response[T]`).
- **Custom redirect handling.** The SDK accepts Go's default redirect behavior.
  Callers requiring custom redirect policies inject a configured `*http.Client`
  via `WithHTTPClient`.
- **`baseURL` validation.** `NewClient` does not validate the `baseURL` argument.
  Passing an empty or malformed URL is silently accepted; errors surface at
  request time as plain Go errors from the HTTP transport layer.
- **Path parameter URL-encoding.** IDs are assumed to be URL-safe; the SDK does
  not escape them. Callers providing non-URL-safe IDs will produce malformed
  request URLs.

## Dependencies

| Spec | From Group | To Group | Relationship |
|------|-----------|----------|--------------|
| `03_openapi_specification` | 1 | 1 | Request/response schemas, endpoint paths, and expected HTTP success status codes derive from the OpenAPI spec. The SDK's typed structs match the schemas defined in `components/schemas`. Endpoint method signatures (HTTP method, path, request body, response shape, success status) are determined by the OpenAPI paths. |

The Go SDK has no runtime dependency on other specs — it is a pure HTTP client
library that communicates with the server over the network. It depends on the
OpenAPI spec (spec 03) as the source of truth for the API contract it
implements.

The CLI (specs 13-15) depends on this SDK as its exclusive API client layer.

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go (latest stable release at implementation time; minimum Go 1.18 for generics) |
| HTTP client | `net/http` (stdlib) |
| JSON encoding | `encoding/json` (stdlib) |
| URL construction | `net/url` (stdlib) — `url.Values` used for query parameter construction to support forward-compatible extension |
| Testing | `go test`, `net/http/httptest` for mock server |
| Assertions | stdlib `testing` package |

The SDK uses only Go standard library packages. No third-party HTTP client
libraries are introduced.

## Repository Layout

```
sdk.go                  Client struct, NewClient, ClientOption, RequestOption
sdk_types.go            All typed request/response structs (canonical; shared with server handlers)
sdk_user.go             Authenticated user endpoint methods
sdk_admin.go            Admin user and org endpoint methods
sdk_auth.go             OAuth and auth endpoint methods
sdk_health.go           Health probe and version endpoint methods
sdk_errors.go           APIError type, ErrNotModified sentinel
sdk_test.go             Unit tests for client construction and option handling
sdk_integration_test.go Integration tests using httptest mock server
```

All SDK files live in the root `apikit` package (package `apikit`), consistent
with the module's public API surface. The SDK is not placed under `internal/`
because consuming projects must be able to import it.

---

## Functional Requirements

### Canonical Shared Types

The typed structs defined in `sdk_types.go` (e.g., `User`, `Organization`,
`APIKeyMeta`) are the **canonical shared types** for the entire `apikit`
package. Both the SDK client methods and the server-side HTTP handler
implementations use these same type definitions. There are no duplicate or
parallel type definitions for these domain objects elsewhere in the `apikit`
package.

This decision resolves the potential naming conflict that would arise if server
specs defined their own `User` or `Organization` structs in the same root
package. The SDK types are defined first (in `sdk_types.go`) and are treated
as the ground truth. Server handler code imports and uses these types directly
rather than defining internal equivalents. The `sdk_types.go` file is the
single source of struct layout in the `apikit` package for all domain objects.

Note: `OrgMember` is not a distinct type. Member listing endpoints return
`[]*User` directly; the term "OrgMember" in the master PRD is an informal alias
for `User` in that context.

### Client Construction

The `Client` struct is the entry point for all SDK operations. It holds the
base URL, API key, HTTP client, request ID, and mount point. After construction,
`Client` holds no mutable state — all fields are set once during `NewClient`
and never modified thereafter. **`Client` is safe for concurrent use from
multiple goroutines.**

```go
// Client is an API client for an apikit-based service.
// Client is safe for concurrent use by multiple goroutines after construction.
type Client struct {
    baseURL    string
    apiKey     string
    httpClient *http.Client
    requestID  string
    mountPoint string
}

// NewClient creates a new API client for the given base URL.
// The base URL is the server's root URL (e.g., "https://api.example.com").
// Options configure authentication, HTTP client, and other settings.
// The returned *Client is safe for concurrent use by multiple goroutines.
//
// An empty baseURL is accepted without error; requests will fail at the
// HTTP transport layer. The caller is responsible for supplying a valid URL.
func NewClient(baseURL string, opts ...ClientOption) *Client

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithAPIKey sets the Bearer token for authentication.
func WithAPIKey(key string) ClientOption

// WithHTTPClient overrides the default http.Client.
func WithHTTPClient(client *http.Client) ClientOption

// WithRequestID sets a static X-Request-ID header on every request.
func WithRequestID(id string) ClientOption

// WithMountPoint overrides the default mount point (/api/v1).
// The path is normalized: a leading slash is added if missing, and a
// trailing slash is stripped. For example, "api/v1" becomes "/api/v1".
func WithMountPoint(path string) ClientOption
```

**Default values:**
- `httpClient`: `http.DefaultClient` (the Go stdlib default).
- `mountPoint`: `/api/v1`.
- `apiKey`: empty string (no authentication; requests to authenticated
  endpoints will fail with 401).
- `requestID`: empty string (no `X-Request-ID` sent).

**Base URL normalization:** The constructor strips a trailing `/` from
`baseURL` if present, to avoid double slashes in URL construction.

**Empty `baseURL`:** `NewClient` silently accepts an empty string. No
validation is performed at construction time. Requests issued against a client
with an empty base URL will fail at the HTTP transport layer and return a plain
`error` (not `*APIError`). The caller is responsible for supplying a valid URL.

**Mount point normalization:** `WithMountPoint` normalizes the path at
construction time:
1. If the path does not start with `/`, a leading `/` is prepended.
2. If the path ends with `/`, the trailing `/` is stripped.

Examples: `"api/v1"` → `"/api/v1"`, `"/api/v1/"` → `"/api/v1"`,
`""` → `"/"` (empty string becomes `"/"`).

**Path parameters:** Path parameters (`keyID`, `userID`, `tokenID`, `orgID`,
`id`) are interpolated into URLs as-is. IDs are assumed to be URL-safe (UUIDs
or alphanumeric strings). The SDK does not call `url.PathEscape`. Callers
providing non-URL-safe values will produce malformed request URLs.

**Redirect behavior:** The SDK uses Go's default HTTP redirect policy
(follow up to 10 redirects automatically). The `Authorization` header may be
stripped on cross-origin redirects per Go's default behavior. Callers who
need custom redirect handling should supply a configured `*http.Client` via
`WithHTTPClient`.

**Concurrency:** `Client` is immutable after construction. No fields are
modified after `NewClient` returns, making the struct safe for concurrent use
without synchronization. Multiple goroutines may call any combination of
`Client` methods simultaneously. Each method call creates its own
`*http.Request` and uses its own `context.Context` — there is no shared
per-request state on the `Client` struct.

### Request Execution

All endpoint methods delegate to an internal `do` method that:

1. Constructs the full URL by joining `baseURL` + `mountPoint` + endpoint path
   (or `baseURL` + path for health probes).
2. Creates an `*http.Request` with the appropriate method, URL, and body.
3. Sets `Accept: application/json`.
4. Sets `Content-Type: application/json` if the request has a body.
5. Sets `Authorization: Bearer <apiKey>` if `apiKey` is non-empty.
6. Sets `X-Request-ID: <requestID>` if `requestID` is non-empty.
7. Applies any per-request options (e.g., `If-None-Match`).
8. Passes the `context.Context` to the request via `req.WithContext(ctx)`.
9. Executes the request via `httpClient.Do(req)`.
10. Checks the response status code:
    - **2xx (200, 201)**: Decodes the JSON body into the typed response struct
      and returns it. If the body is empty, not valid JSON, or any other
      non-JSON content (e.g., plain text, HTML, binary), a plain `error` is
      returned wrapping the JSON decode error. The original error is always
      wrapped so that `errors.Unwrap` works. The exact message format is left
      to the implementor, subject to the wrapping contract.
    - **204**: Returns `nil` body (for endpoints like DELETE that return no
      content).
    - **304**: Returns `nil` for the `*Response[T]` value and `ErrNotModified`.
      Response headers from a 304 are not accessible (the `*Response[T]` is nil).
    - **4xx/5xx**: Decodes the error envelope and returns an `*APIError`.
    - **Non-JSON error response**: If the response body cannot be decoded as
      the error envelope (e.g., HTML error page from a proxy, plain-text body
      from a proxy returning 413, binary data), returns an `*APIError` with
      the status code and a generic message including the raw status text.
      This applies to any non-2xx, non-304 response whose body is not a valid
      JSON error envelope, regardless of `Content-Type`.

**`StatusCode` on `Response[T]`:** For successful 2xx responses, the
`StatusCode` field on `*Response[T]` is set to the actual HTTP status code
returned by the server (e.g., `200` or `201`). This field reflects whatever
the server returned, including unexpected 2xx variants. The `Data` field is
always populated from JSON decoding on any 2xx response that reaches the decode
step.

### Per-Request Options

Methods that support conditional GET accept variadic `...RequestOption`:

```go
// RequestOption configures a single request.
type RequestOption func(*http.Request)

// WithIfNoneMatch adds an If-None-Match header for conditional GET.
func WithIfNoneMatch(etag string) RequestOption
```

Methods supporting conditional GET: `GetUser`, `GetUserByID`, `GetToken`,
`GetOrg`.

### Response Wrapper for ETag Access

Methods that support conditional GET return a `*Response[T]` wrapper:

```go
// Response wraps a typed API response with access to HTTP headers.
type Response[T any] struct {
    Data       *T
    StatusCode int
    Header     http.Header
}

// ETag returns the ETag header value from the response, or empty string.
func (r *Response[T]) ETag() string
```

On a successful (200) response, `*Response[T]` is non-nil and `Data`,
`StatusCode`, and `Header` are all populated. On a 304 response,
`*Response[T]` is `nil` and `ErrNotModified` is returned — response headers
from a 304 are not accessible via this API.

This allows callers to capture the ETag for subsequent conditional requests:

```go
resp, err := client.GetUser(ctx)
etag := resp.ETag()
// Later...
resp2, err := client.GetUser(ctx, apikit.WithIfNoneMatch(etag))
if errors.Is(err, apikit.ErrNotModified) {
    // Resource unchanged, use cached version
}
```

### Error Handling

```go
// APIError represents an error response from the API server.
// It matches the server's error envelope: {"error": {"code": N, "message": "..."}}
type APIError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

// Error implements the error interface.
// The format is: "API error {Code}: {Message}"
// Example: "API error 404: User not found"
func (e *APIError) Error() string {
    return fmt.Sprintf("API error %d: %s", e.Code, e.Message)
}

// ErrNotModified is returned when a conditional GET receives HTTP 304.
var ErrNotModified = errors.New("not modified")
```

The JSON decoding for error responses handles the nested envelope:

```json
{"error": {"code": 404, "message": "User not found"}}
```

The SDK decodes the outer `{"error": ...}` wrapper and returns the inner
object as `*APIError`.

**Error classification:**
- `*APIError` — server returned a JSON error envelope (4xx/5xx). Callers use
  `errors.As(err, &apiErr)` to inspect `Code` and `Message`.
- `*APIError` (generic) — server returned a non-JSON or non-decodable error
  body (e.g., HTML from a proxy, plain text, binary). The `Code` is set from
  the HTTP status code and `Message` is the HTTP status text.
- `ErrNotModified` — server returned HTTP 304 (conditional GET). Callers use
  `errors.Is(err, ErrNotModified)`. The `*Response[T]` is nil in this case.
- Plain `error` — network failure, context cancellation, JSON decode error
  (including empty or malformed body — any non-JSON content — on a 2xx
  response), or request body encode error. The original underlying error is
  always wrapped so that `errors.Unwrap` works on the returned error.

### Typed Structs

The types defined below in `sdk_types.go` are the **canonical shared types**
for the root `apikit` package. Server-side handlers use these same structs
directly; there are no parallel or internal-only equivalents for these domain
objects in the `apikit` package.

All structs use `json` struct tags matching the field names in the OpenAPI
schemas. Nullable timestamp fields use `*time.Time` with the format handled
by a custom JSON marshaler/unmarshaler that uses RFC 3339 format. Nullable
string fields use `*string`.

List endpoint responses are bare JSON arrays (e.g., `[{...}, {...}]`), not
wrapped objects. The SDK decodes them directly into `[]T`. This matches the
OpenAPI spec (spec 03).

Member listing endpoints (`ListOrgMembers`) return `[]*User` directly.
`OrgMember` is not a distinct Go type; it is `User`.

```go
// User represents a user profile.
type User struct {
    ID         string     `json:"id"`
    Username   string     `json:"username"`
    Email      string     `json:"email"`
    FullName   *string    `json:"full_name"`
    Status     string     `json:"status"`
    Role       string     `json:"role"`
    Provider   string     `json:"provider"`
    ProviderID string     `json:"provider_id"`
    CreatedAt  time.Time  `json:"created_at"`
    UpdatedAt  time.Time  `json:"updated_at"`
}

// APIKeyMeta is the metadata-only key object returned in listings.
type APIKeyMeta struct {
    KeyID     string     `json:"key_id"`
    CreatedAt time.Time  `json:"created_at"`
    ExpiresAt *time.Time `json:"expires_at"`
    RevokedAt *time.Time `json:"revoked_at"`
}

// APIKeyFull is the full key object returned on creation/refresh.
type APIKeyFull struct {
    Key       string     `json:"key"`
    KeyID     string     `json:"key_id"`
    ExpiresAt *time.Time `json:"expires_at"`
}

// PAT is the metadata-only personal access token returned in listings.
type PAT struct {
    TokenID     string     `json:"token_id"`
    Name        string     `json:"name"`
    Permissions []string   `json:"permissions"`
    CreatedAt   time.Time  `json:"created_at"`
    ExpiresAt   *time.Time `json:"expires_at"`
    RevokedAt   *time.Time `json:"revoked_at"`
}

// PATFull is the full token object returned on creation.
type PATFull struct {
    Token       string     `json:"token"`
    TokenID     string     `json:"token_id"`
    Name        string     `json:"name"`
    Permissions []string   `json:"permissions"`
    ExpiresAt   *time.Time `json:"expires_at"`
}

// Organization represents an organization.
type Organization struct {
    ID        string     `json:"id"`
    Name      string     `json:"name"`
    Slug      string     `json:"slug"`
    URL       *string    `json:"url"`
    Status    string     `json:"status"`
    CreatedAt time.Time  `json:"created_at"`
    UpdatedAt time.Time  `json:"updated_at"`
}

// OAuthProvider represents a configured OAuth provider.
type OAuthProvider struct {
    Name         string `json:"name"`
    AuthorizeURL string `json:"authorize_url"`
}

// AuthCallbackRequest is the request body for POST /auth/callback.
type AuthCallbackRequest struct {
    Provider    string `json:"provider"`
    Code        string `json:"code"`
    RedirectURI string `json:"redirect_uri"`
    Expires     *int   `json:"expires,omitempty"`
}

// AuthCallbackResponse is the response from POST /auth/callback.
type AuthCallbackResponse struct {
    User   User       `json:"user"`
    APIKey APIKeyFull `json:"api_key"`
}

// CreateUserRequest is the request body for POST /users.
type CreateUserRequest struct {
    Username   string `json:"username"`
    Email      string `json:"email"`
    Provider   string `json:"provider"`
    ProviderID string `json:"provider_id"`
}

// UpdateUserRequest is the request body for PATCH /user and PATCH /users/:id.
// FullName is a plain string (not a pointer): every PATCH request always sets
// full_name. This is intentional — full_name is the only patchable field today
// and the server always expects it to be present in the request body.
type UpdateUserRequest struct {
    FullName string `json:"full_name"`
}

// CreateTokenRequest is the request body for POST /user/tokens.
type CreateTokenRequest struct {
    Name        string   `json:"name"`
    Permissions []string `json:"permissions"`
    Expires     *int     `json:"expires,omitempty"`
}

// CreateOrgRequest is the request body for POST /orgs.
type CreateOrgRequest struct {
    Name string  `json:"name"`
    Slug string  `json:"slug"`
    URL  *string `json:"url,omitempty"`
}

// UpdateOrgRequest is the request body for PATCH /orgs/:id.
type UpdateOrgRequest struct {
    Name *string `json:"name,omitempty"`
    URL  *string `json:"url,omitempty"`
}

// HealthResponse is the response from health probe endpoints.
type HealthResponse struct {
    Status string `json:"status"`
}

// VersionResponse is the response from GET /version.
type VersionResponse struct {
    Version    string `json:"version"`
    BuildTime  string `json:"build_time"`
    Commit     string `json:"commit"`
    MountPoint string `json:"mount_point"`
}

// RevokeKeyResponse is the response from DELETE /user/keys/:key_id.
type RevokeKeyResponse struct {
    KeyID     string    `json:"key_id"`
    RevokedAt time.Time `json:"revoked_at"`
}

// ListUsersOptions configures the ListUsers request.
// This struct is expected to grow in future iterations (e.g., pagination,
// search, sort). Query parameter construction uses url.Values internally
// to accommodate additional fields without restructuring.
type ListUsersOptions struct {
    IncludeBlocked bool
}

// ListOrgsOptions configures the ListOrgs request.
// This struct is expected to grow in future iterations (e.g., pagination,
// search, sort). Query parameter construction uses url.Values internally
// to accommodate additional fields without restructuring.
type ListOrgsOptions struct {
    IncludeBlocked bool
}
```

### Endpoint Methods

All methods are defined on `*Client`. Methods that operate on a single
resource and support ETags return `*Response[T]` (with header access); other
methods return the typed struct directly. All methods use `context.Context`
for cancellation and deadline propagation.

#### Authenticated User Methods

```go
func (c *Client) GetUser(ctx context.Context, opts ...RequestOption) (*Response[User], error)
func (c *Client) UpdateUser(ctx context.Context, req *UpdateUserRequest) (*User, error)
func (c *Client) ListKeys(ctx context.Context) ([]*APIKeyMeta, error)
func (c *Client) RefreshKey(ctx context.Context, keyID string) (*APIKeyFull, error)
func (c *Client) RevokeKey(ctx context.Context, keyID string) (*RevokeKeyResponse, error)
func (c *Client) ListTokens(ctx context.Context) ([]*PAT, error)
func (c *Client) CreateToken(ctx context.Context, req *CreateTokenRequest) (*PATFull, error)
func (c *Client) GetToken(ctx context.Context, tokenID string, opts ...RequestOption) (*Response[PAT], error)
func (c *Client) RevokeToken(ctx context.Context, tokenID string) error
func (c *Client) ListUserOrgs(ctx context.Context) ([]*Organization, error)
```

#### Admin User Methods

```go
func (c *Client) ListUsers(ctx context.Context, opts *ListUsersOptions) ([]*User, error)
func (c *Client) GetUserByID(ctx context.Context, id string, opts ...RequestOption) (*Response[User], error)
func (c *Client) CreateUser(ctx context.Context, req *CreateUserRequest) (*User, error)
func (c *Client) UpdateUserByID(ctx context.Context, id string, req *UpdateUserRequest) (*User, error)
func (c *Client) PromoteUser(ctx context.Context, id string) (*User, error)
func (c *Client) DemoteUser(ctx context.Context, id string) (*User, error)
func (c *Client) BlockUser(ctx context.Context, id string) (*User, error)
func (c *Client) UnblockUser(ctx context.Context, id string) (*User, error)
func (c *Client) ListUserKeys(ctx context.Context, userID string) ([]*APIKeyMeta, error)
func (c *Client) RevokeUserKey(ctx context.Context, userID string, keyID string) error
func (c *Client) ListUserTokens(ctx context.Context, userID string) ([]*PAT, error)
func (c *Client) RevokeUserToken(ctx context.Context, userID string, tokenID string) error
```

#### Admin Organization Methods

```go
func (c *Client) CreateOrg(ctx context.Context, req *CreateOrgRequest) (*Organization, error)
func (c *Client) ListOrgs(ctx context.Context, opts *ListOrgsOptions) ([]*Organization, error)
func (c *Client) GetOrg(ctx context.Context, id string, opts ...RequestOption) (*Response[Organization], error)
func (c *Client) UpdateOrg(ctx context.Context, id string, req *UpdateOrgRequest) (*Organization, error)
func (c *Client) DeleteOrg(ctx context.Context, id string) error
func (c *Client) BlockOrg(ctx context.Context, id string) (*Organization, error)
func (c *Client) UnblockOrg(ctx context.Context, id string) (*Organization, error)
func (c *Client) ListOrgMembers(ctx context.Context, orgID string) ([]*User, error)
func (c *Client) AddOrgMember(ctx context.Context, orgID string, userID string) error
func (c *Client) RemoveOrgMember(ctx context.Context, orgID string, userID string) error
```

#### Auth Methods

```go
func (c *Client) GetProviders(ctx context.Context) ([]*OAuthProvider, error)
func (c *Client) ExchangeOAuthCode(ctx context.Context, req *AuthCallbackRequest) (*AuthCallbackResponse, error)
```

#### Health/Meta Methods

Health probe methods hit paths outside the mount point (directly on baseURL).

```go
func (c *Client) Healthz(ctx context.Context) (*HealthResponse, error)
func (c *Client) Readyz(ctx context.Context) (*HealthResponse, error)
func (c *Client) Version(ctx context.Context) (*VersionResponse, error)
```

### URL Construction

The internal `do` method constructs URLs based on endpoint category:

- **API endpoints** (under mount point): `baseURL + mountPoint + path`
  - Example: `https://api.example.com/api/v1/user`
- **Health probes** (outside mount point): `baseURL + path`
  - Example: `https://api.example.com/healthz`
- **Auth endpoints** (under mount point): `baseURL + mountPoint + path`
  - Example: `https://api.example.com/api/v1/auth/providers`

### Query Parameters

Query parameters are constructed using `url.Values` to support forward-compatible
extension of `ListUsersOptions` and `ListOrgsOptions` in future iterations.

For `ListUsers` and `ListOrgs`, when the options struct is non-nil and
`IncludeBlocked` is `true`, the SDK appends `?include_blocked=true` to the
request URL. When `false` or options is `nil`, no query parameter is
appended (the server's default behavior applies).

### HTTP Method Mapping

Expected HTTP success status codes per endpoint are defined in the OpenAPI
spec (spec 03). The SDK's internal `do` method uses those status codes to
determine whether to decode a response body (200, 201) or treat the response
as no-content (204). For reference, endpoints that return only `error` (no
typed response) correspond to HTTP 204 No Content responses (e.g., `RevokeToken`,
`RevokeUserToken`, `RevokeUserKey`, `DeleteOrg`, `RemoveOrgMember`). The
`AddOrgMember` PUT endpoint also returns 204 with no body. The OpenAPI spec
(spec 03) is the authoritative source for exact success status codes.

| SDK Method | HTTP Method | Path (relative to mount point) |
|-----------|-------------|-------------------------------|
| `GetUser` | GET | `/user` |
| `UpdateUser` | PATCH | `/user` |
| `ListKeys` | GET | `/user/keys` |
| `RefreshKey` | POST | `/user/keys/{key_id}/refresh` |
| `RevokeKey` | DELETE | `/user/keys/{key_id}` |
| `ListTokens` | GET | `/user/tokens` |
| `CreateToken` | POST | `/user/tokens` |
| `GetToken` | GET | `/user/tokens/{token_id}` |
| `RevokeToken` | DELETE | `/user/tokens/{token_id}` |
| `ListUserOrgs` | GET | `/user/orgs` |
| `ListUsers` | GET | `/users` |
| `GetUserByID` | GET | `/users/{id}` |
| `CreateUser` | POST | `/users` |
| `UpdateUserByID` | PATCH | `/users/{id}` |
| `PromoteUser` | POST | `/users/{id}/promote` |
| `DemoteUser` | POST | `/users/{id}/demote` |
| `BlockUser` | POST | `/users/{id}/block` |
| `UnblockUser` | POST | `/users/{id}/unblock` |
| `ListUserKeys` | GET | `/users/{id}/keys` |
| `RevokeUserKey` | DELETE | `/users/{id}/keys/{key_id}` |
| `ListUserTokens` | GET | `/users/{id}/tokens` |
| `RevokeUserToken` | DELETE | `/users/{id}/tokens/{token_id}` |
| `CreateOrg` | POST | `/orgs` |
| `ListOrgs` | GET | `/orgs` |
| `GetOrg` | GET | `/orgs/{id}` |
| `UpdateOrg` | PATCH | `/orgs/{id}` |
| `DeleteOrg` | DELETE | `/orgs/{id}` |
| `BlockOrg` | POST | `/orgs/{id}/block` |
| `UnblockOrg` | POST | `/orgs/{id}/unblock` |
| `ListOrgMembers` | GET | `/orgs/{id}/members` |
| `AddOrgMember` | PUT | `/orgs/{id}/members/{user_id}` |
| `RemoveOrgMember` | DELETE | `/orgs/{id}/members/{user_id}` |
| `GetProviders` | GET | `/auth/providers` |
| `ExchangeOAuthCode` | POST | `/auth/callback` |
| `Healthz` | GET | `/healthz` (no mount point) |
| `Readyz` | GET | `/readyz` (no mount point) |
| `Version` | GET | `/version` (no mount point) |

### JSON Serialization

- All JSON field names use `snake_case`, matching the OpenAPI schemas.
- `time.Time` fields are serialized/deserialized in RFC 3339 format. Go's
  `encoding/json` handles `time.Time` with RFC 3339 by default.
- Nullable `*time.Time` fields deserialize `null` JSON values as `nil`.
- Nullable `*string` fields deserialize `null` JSON values as `nil`.
- The `omitempty` tag is used on optional request fields (`Expires *int`,
  `URL *string`) so they are omitted from the JSON body when not set.
- List endpoints return `[]` (empty slice) from the server when no resources
  exist. The SDK deserializes this as an empty slice, not `nil`. List responses
  are bare JSON arrays (e.g., `[{...}]`), not wrapped objects — the SDK
  decodes them directly into `[]T`.

---

## Interfaces

### Public API Surface

All types and functions listed in this spec are exported from the root
`apikit` package. The SDK shares the package with the server-side public API
(e.g., `NewServer`, `GenerateAPIKey`). There is no naming conflict because
the SDK types (`Client`, `NewClient`, `ClientOption`) are distinct from
server-side types, and the domain object types (`User`, `Organization`, etc.)
defined in `sdk_types.go` are explicitly the canonical shared types used by
both sides of the package.

The complete public API surface added by this spec:

- `Client` struct (safe for concurrent use)
- `NewClient` constructor
- `ClientOption` type and option functions (`WithAPIKey`, `WithHTTPClient`,
  `WithRequestID`, `WithMountPoint`)
- `RequestOption` type and option functions (`WithIfNoneMatch`)
- `Response[T]` generic wrapper
- `APIError` struct and `ErrNotModified` sentinel
- All typed request/response structs listed above (canonical; shared with
  server handlers)
- All endpoint methods on `*Client`
- `ListUsersOptions` and `ListOrgsOptions` structs

---

## Error Handling

| Condition | Error Type | Details |
|-----------|------------|---------|
| Server returns 4xx/5xx with JSON error envelope | `*APIError` | `Code` and `Message` from envelope; `Error()` returns `"API error {Code}: {Message}"` |
| Server returns 4xx/5xx without valid JSON body (HTML, plain text, binary, etc.) | `*APIError` | `Code` from HTTP status; `Message` from HTTP status text |
| Server returns HTTP 304 | `ErrNotModified` | Sentinel error; use `errors.Is`. `*Response[T]` is nil. |
| Network failure | `error` | Wraps the underlying network error; `errors.Unwrap` works |
| Context cancelled/deadline exceeded | `error` | Wraps `context.Canceled` or `context.DeadlineExceeded`; `errors.Unwrap` works |
| JSON decode error on 2xx response (empty body, malformed JSON, non-JSON content including plain text or HTML) | `error` | Wraps the JSON error; `errors.Unwrap` works. Exact message format is left to the implementor. |
| Request body JSON encode error | `error` | Wraps the JSON marshal error; `errors.Unwrap` works |

---

## Testing Strategy

### Unit Tests

- **Client construction:** Verify `NewClient` sets defaults correctly (default
  HTTP client, default mount point `/api/v1`, no API key, no request ID).
- **Client options:** Verify each option (`WithAPIKey`, `WithHTTPClient`,
  `WithRequestID`, `WithMountPoint`) modifies the client correctly.
- **Base URL normalization:** Verify trailing slash is stripped.
- **Empty `baseURL`:** Verify `NewClient("")` does not panic and returns a
  non-nil `*Client`.
- **Mount point normalization:** Verify `WithMountPoint("api/v1")` produces
  `/api/v1`; `WithMountPoint("/api/v1/")` produces `/api/v1`; `WithMountPoint("")`
  produces `/`.
- **URL construction:** Verify that API endpoint URLs are constructed as
  `baseURL + mountPoint + path`. Verify health probe URLs use `baseURL + path`
  without mount point.
- **Custom mount point URL construction:** Verify URLs use custom mount point
  when `WithMountPoint` is used (e.g., `WithMountPoint("/v2")` produces
  `/v2/user` for `GetUser`).
- **Request headers:** Verify `Authorization`, `X-Request-ID`, `Accept`, and
  `Content-Type` headers are set correctly based on client configuration.
- **APIError.Error():** Verify the error message format is
  `"API error {Code}: {Message}"` (e.g., `"API error 404: User not found"`).
- **ErrNotModified:** Verify `errors.Is(ErrNotModified, ErrNotModified)`.
- **Query parameter construction:** Verify `include_blocked=true` is appended
  when `IncludeBlocked` is `true` and omitted when `false` or options is `nil`.

### Integration Tests (httptest mock server)

Integration tests use `net/http/httptest` to create a mock server that returns
canned JSON responses, verifying the SDK correctly constructs requests and
parses responses.

- **GetUser (happy path):** Mock server returns a User JSON. Verify the SDK
  parses it into a `*Response[User]` with `Data` fields correct and `ETag()`
  returning the ETag header value.
- **GetUser (conditional GET — 304):** Mock server returns HTTP 304 when
  `If-None-Match` matches. Verify `ErrNotModified` is returned and
  `*Response[User]` is nil.
- **GetUser (conditional GET — ETag capture):** Mock server returns a 200 with
  `ETag` header. Verify `Response.ETag()` returns the value.
- **GetUser (empty 200 body):** Mock server returns HTTP 200 with an empty
  body. Verify a plain `error` is returned (not `*APIError`) and that
  `errors.Unwrap` returns a non-nil underlying error.
- **GetUser (non-JSON 200 body — plain text):** Mock server returns HTTP 200
  with a plain-text body (e.g., `"ok"`). Verify a plain `error` is returned
  (not `*APIError`) and that `errors.Unwrap` returns a non-nil underlying error.
- **UpdateUser:** Verify the SDK sends a PATCH request with the correct JSON
  body (including `full_name` always present as a string).
- **ListKeys:** Mock server returns a bare JSON array. Verify the SDK parses
  it into `[]*APIKeyMeta`.
- **ListKeys (empty):** Mock server returns `[]`. Verify the SDK returns an
  empty slice (not nil).
- **RefreshKey:** Verify POST request to correct path with key_id in URL.
- **RevokeKey:** Verify DELETE request. Verify response parsing of
  `*RevokeKeyResponse`.
- **CreateToken:** Verify POST with JSON body. Verify `*PATFull` response.
- **RevokeToken:** Verify DELETE returns no error on 204.
- **ListUsers (with include_blocked):** Verify `?include_blocked=true` is in
  the request URL.
- **ListUsers (without include_blocked):** Verify no query parameter.
- **CreateUser:** Verify POST with JSON body. Verify 201 response parsed.
- **PromoteUser/DemoteUser/BlockUser/UnblockUser:** Verify POST to correct
  action paths.
- **CreateOrg:** Verify POST with JSON body.
- **ListOrgs (with include_blocked):** Verify query parameter.
- **DeleteOrg:** Verify DELETE returns no error on 204.
- **AddOrgMember:** Verify PUT with no body to correct path; expect 204
  response with no error.
- **RemoveOrgMember:** Verify DELETE to correct path; expect 204 response with
  no error.
- **GetProviders:** Verify GET to auth/providers path. Verify response decoded
  from bare JSON array.
- **ExchangeOAuthCode:** Verify POST with correct JSON body.
- **Healthz/Readyz:** Verify GET to paths without mount point.
- **Version:** Verify GET to /version without mount point.
- **API error (4xx):** Mock server returns 404 with JSON error envelope.
  Verify `errors.As` extracts `*APIError` with correct code and message, and
  `err.Error()` returns `"API error 404: User not found"`.
- **API error (5xx):** Mock server returns 500 with JSON error envelope.
  Verify `*APIError`.
- **Non-JSON error (HTML proxy):** Mock server returns HTML 502. Verify
  `*APIError` with `Code` 502 and generic message from status text.
- **Non-JSON error (plain text):** Mock server returns plain-text body with a
  4xx status. Verify `*APIError` with status code and generic message.
- **Network error:** Client pointed at non-existent server (e.g., a port that
  refuses connections on localhost; both DNS resolution failures and
  connection-refused failures are acceptable for this test in CI). Verify plain
  error is returned (not `*APIError`) and that `errors.As(err, &apiErr)` returns
  `false`.
- **Context cancellation:** Cancel context before request completes. Verify
  `context.Canceled` error (accessible via `errors.Unwrap`).
- **Authorization header:** Verify Bearer token is sent when API key is set.
  Verify no Authorization header when API key is not set.
- **X-Request-ID header:** Verify header is sent when request ID is set.
  Verify no X-Request-ID header when not set.
- **Custom mount point:** Verify URLs use custom mount point when
  `WithMountPoint` is used.
- **Mount point normalization (integration):** Verify `WithMountPoint("api/v2")`
  (no leading slash) produces correct request URLs.
- **Error wrapping:** Verify that plain errors from network failures, context
  cancellation, and JSON decode failures satisfy `errors.Unwrap` (i.e., the
  underlying cause is accessible).
- **Response[T] StatusCode:** Verify that `Response.StatusCode` reflects the
  actual HTTP status code returned by the server on 2xx responses (e.g., 201
  for creation endpoints).

---

## Design Decisions

- **SDK lives in root package, not `internal/`.** The SDK must be importable
  by consuming projects. Placing it in `internal/` would prevent external
  import. The root `apikit` package already exposes the server-side public API;
  the SDK coexists in the same package with distinct type names.
- **SDK types are the canonical shared types for the package.** The typed
  structs in `sdk_types.go` (e.g., `User`, `Organization`) are used by both
  the client methods and the server-side handlers. There is a single definition
  of each domain object type in the package. This eliminates the risk of
  compile-time conflicts and avoids redundant parallel definitions. Server
  specs reference these types directly.
- **`OrgMember` is not a distinct Go type.** Member listing endpoints return
  `[]*User` directly. The term "OrgMember" appears in the master PRD as an
  informal alias for `User` in the membership context, but no separate Go type
  is defined.
- **`Client` is safe for concurrent use.** All fields are set once in
  `NewClient` and never modified. No synchronization is needed. This is
  explicitly documented in the type and constructor doc comments so that
  consuming code can share a single `Client` instance across goroutines
  (e.g., in HTTP handlers or CLI commands) without defensive copying. Each
  method call creates its own `*http.Request` and receives its own
  `context.Context` — there is no shared per-request state on `Client`.
- **Functional options pattern for client construction.** `ClientOption` is a
  function type applied to `*Client`. This is idiomatic Go, extensible without
  breaking changes, and avoids a config struct that must be filled before use.
- **Standard library only — no third-party HTTP client.** The SDK uses
  `net/http` exclusively. This avoids adding dependencies to consuming projects
  and ensures compatibility with all Go environments. Users who need custom
  behavior inject a configured `*http.Client` via `WithHTTPClient`.
- **Default Go redirect behavior.** The SDK does not override `http.Client`'s
  redirect policy. Up to 10 redirects are followed automatically. The
  `Authorization` header may be stripped on cross-origin redirects per Go's
  default policy. This is intentional: adding a custom redirect policy would
  increase complexity for an edge case. Callers with specific redirect
  requirements supply their own `*http.Client` via `WithHTTPClient`.
- **`*APIError` for server errors, plain errors for client-side failures.**
  This distinction lets callers handle server responses (inspect status code,
  message) differently from infrastructure failures (network, timeout). The
  two-tier model matches Go error handling conventions (`errors.As` for typed
  errors, `errors.Is` for sentinels). A 2xx response with an empty, malformed,
  or non-JSON body (including plain text or HTML) is treated as a plain error
  (JSON decode failure), not an `*APIError`, because the server did not return
  an error envelope.
- **Plain errors always wrap the underlying cause.** Network failures, context
  errors, JSON decode errors, and encode errors are all wrapped so that
  `errors.Unwrap` works. The exact message format is left to the implementor
  for flexibility; only the wrapping contract is required.
- **`APIError.Error()` format is `"API error {Code}: {Message}"`.** This
  format is human-readable, unambiguous, and easy to test. It includes both
  the numeric code (for programmatic parsing of log output) and the message.
  The string `"API error"` prefix distinguishes it from plain Go errors in
  log output.
- **`ErrNotModified` as a sentinel, not `*APIError`.** HTTP 304 is not an
  error in the traditional sense — it means the resource is unchanged. Using
  a sentinel error lets callers handle it with `errors.Is` without polluting
  the `*APIError` type with a non-error status code.
- **`*Response[T]` is nil on 304.** When a conditional GET returns 304,
  the method returns `nil, ErrNotModified`. This keeps the API simple:
  callers check the error first. Making `*Response[T]` non-nil on 304 would
  require callers to handle a struct with a nil `Data` field, adding
  complexity with no benefit since the 304 body is always empty and headers
  from the 304 are rarely needed in practice.
- **`Response[T]` wrapper for ETag-capable endpoints.** Endpoints that support
  conditional GET need to return both the typed response and the ETag header.
  A generic wrapper avoids duplicating return types or requiring callers to
  access raw `*http.Response` for every call.
- **`Response[T].StatusCode` reflects the actual HTTP status code.** The field
  is populated with whatever 2xx code the server returned. This provides
  accurate information for callers who need to distinguish 200 from 201 (e.g.,
  idempotent creation endpoints) without inspecting raw HTTP responses.
- **No automatic ETag caching.** The SDK is stateless — it does not cache
  responses or ETags. The caller manages cache state. This keeps the SDK
  simple and predictable, and avoids hidden state that complicates testing
  and debugging.
- **`context.Context` as first parameter on every method.** This is idiomatic
  Go for operations that may block. It enables cancellation, deadlines, and
  tracing propagation.
- **Mount point as a client option, not per-request.** The mount point is a
  property of the server deployment, not of individual requests. Setting it
  once on the client avoids repetition and ensures consistency.
- **Mount point normalization in `WithMountPoint`.** Adding a leading slash if
  missing and stripping a trailing slash prevents silent URL construction bugs
  from incorrectly formatted mount points. The normalization happens once at
  construction time, not per request.
- **`NewClient` silently accepts an empty `baseURL`.** Validation at
  construction time would require `NewClient` to return an error, breaking the
  common one-liner initialization pattern (`client := apikit.NewClient(url,
  opts...)`). Since an empty URL will fail at the HTTP transport layer with a
  clear error on first use, the tradeoff favors ergonomic construction over
  eager validation. The caller is responsible for supplying a valid URL.
- **Path parameters are not URL-encoded.** IDs are UUIDs or alphanumeric
  strings that are inherently URL-safe. Adding `url.PathEscape` calls to every
  path parameter interpolation would add noise for no benefit in practice.
  This assumption is documented as a caller responsibility.
- **Health probes bypass mount point.** Per the OpenAPI spec and master PRD,
  `/healthz`, `/readyz`, and `/version` are outside the mount point. The SDK
  handles this by using `baseURL + path` directly for these three methods.
- **No pagination support.** The master PRD explicitly lists pagination as a
  non-goal for the first iteration. List methods return all results.
- **`ListUsersOptions` and `ListOrgsOptions` are designed to grow.** Both
  structs are expected to gain additional filter fields (e.g., pagination, search,
  sort) in future iterations. Query parameter construction uses `url.Values`
  internally so new fields can be appended without restructuring the
  implementation. Forward-compatibility comments are included in the struct
  doc comments.
- **`ListUsersOptions` and `ListOrgsOptions` as pointer parameters.** Passing
  `nil` means "use server defaults." This avoids a boolean parameter that
  would require the caller to explicitly pass `false` for the default case.
- **`AddOrgMember` sends no request body and expects 204.** Per the OpenAPI
  spec, `PUT /orgs/:id/members/:user_id` requires no body. The user ID is
  conveyed entirely via the path parameter. The success response is 204 No
  Content; the method returns `error` only.
- **Methods returning only `error` correspond to 204 responses.** `RevokeToken`,
  `RevokeUserToken`, `RevokeUserKey`, `DeleteOrg`, `RemoveOrgMember`, and
  `AddOrgMember` all return HTTP 204 with no body. The SDK's `do` method
  treats 204 as success with no body to decode. The OpenAPI spec (spec 03) is
  the authoritative source for these status codes.
- **Nullable timestamps as `*time.Time`.** Nullable `expires_at` and
  `revoked_at` fields use `*time.Time` rather than `sql.NullTime` or a custom
  type. This is idiomatic for client-side Go where `database/sql` types are
  not appropriate, and `encoding/json` handles `*time.Time` with `null`
  correctly.
- **`RevokeKey` returns `*RevokeKeyResponse`.** Unlike token revocation (204),
  key revocation returns a 200 with `key_id` and `revoked_at`. The SDK
  deserializes this response.
- **`UpdateUserRequest.FullName` is a plain `string`, not `*string`.** The
  PATCH /user endpoint has only one patchable field (`full_name`). Because
  there is nothing else to partially update, every PATCH request always sets
  `full_name`. Using a plain string (without `omitempty`) makes this explicit
  and avoids the nil-pointer ergonomics of `*string` for a field that is always
  required.
- **Non-JSON error bodies always produce `*APIError`.** Whether the error
  response body is HTML, plain text, or binary, the SDK returns an `*APIError`
  with the HTTP status code and status text as the message. This ensures
  callers always use `errors.As` to inspect errors from non-2xx responses,
  regardless of the proxy or server's response format.
- **List endpoint responses are bare JSON arrays.** Per the OpenAPI spec
  (spec 03), list endpoints return bare arrays (e.g., `[{...}]`), not wrapped
  objects (e.g., `{"keys": [...]}`). The SDK decodes them directly into `[]T`
  with no intermediate wrapper struct.
- **HTTP success status codes are authoritative in spec 03.** The SDK's
  `do` method branches on expected success status codes (200, 201, 204) per
  endpoint. Rather than duplicating this table here, the SDK implementation
  references spec 03 as the source of truth for which status code each
  endpoint returns on success.
- **Minimum Go version is the latest stable release at implementation time.**
  The absolute floor is Go 1.18 (for generics required by `Response[T]`).
  Rather than pinning a specific version that may become stale, implementors
  should use whatever stable release is current. The go.mod `go` directive is
  updated at implementation time accordingly.
- **`url.Values` for query parameter construction.** Using `url.Values` (from
  `net/url`) rather than manual string concatenation ensures correct encoding
  of query parameter values and provides a natural extension point for adding
  new filter fields to `ListUsersOptions` and `ListOrgsOptions` in future
  iterations.

---

## Glossary

| Term | Definition |
|------|------------|
| **Client** | The SDK's entry point — a struct that holds configuration (base URL, API key, HTTP client, mount point) and provides methods for each API endpoint. Safe for concurrent use after construction. |
| **ClientOption** | A function type `func(*Client)` used in the functional options pattern to configure a `Client` at construction time. |
| **RequestOption** | A function type `func(*http.Request)` used to configure per-request behavior (e.g., conditional GET headers). |
| **Response[T]** | A generic wrapper that pairs a typed API response with HTTP headers, enabling ETag access without losing type safety. Non-nil only on successful (2xx) responses; nil on 304. `StatusCode` reflects the actual HTTP status code returned by the server. |
| **APIError** | A typed error struct matching the server's error envelope (`code` + `message`). Implements the `error` interface. `Error()` returns `"API error {Code}: {Message}"`. Callers inspect it via `errors.As`. |
| **ErrNotModified** | A sentinel error returned when a conditional GET receives HTTP 304 (Not Modified). Callers check with `errors.Is`. The accompanying `*Response[T]` is nil. |
| **mount point** | The URL path prefix for API endpoints (default `/api/v1`). Configurable via `WithMountPoint`. Normalized at construction time: leading slash added if missing, trailing slash stripped. Health probes are outside the mount point. |
| **baseURL** | The server's root URL (e.g., `https://api.example.com`). Passed to `NewClient`. All endpoint URLs are constructed relative to this. Trailing slash is stripped at construction time. Empty string is silently accepted; errors surface at request time. |
| **conditional GET** | An HTTP GET request with an `If-None-Match` header containing a previously received ETag. The server returns 304 if the resource is unchanged. |
| **functional options** | A Go pattern where configuration is applied via variadic function arguments (`NewClient(url, opts...)`), enabling extensibility without breaking changes. |
| **canonical shared types** | The domain object structs (`User`, `Organization`, etc.) defined in `sdk_types.go`. These are the single authoritative definitions used by both the SDK client and server-side handlers in the `apikit` package. |
| **bare JSON array** | A JSON response body that is a top-level array (e.g., `[{...}]`), as opposed to a wrapped object (e.g., `{"items": [...]}`). All list endpoints in this API return bare arrays. |
| **OrgMember** | An informal alias for `User` used in the master PRD to describe users in the context of organization membership. Not a distinct Go type; member listing endpoints return `[]*User`. |
| **url.Values** | The `net/url` standard library type used for query parameter construction. Used in `ListUsers` and `ListOrgs` to allow forward-compatible extension of filter options without restructuring the implementation. |

---

## Owner

Michael Kuehl
