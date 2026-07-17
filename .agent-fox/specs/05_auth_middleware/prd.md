---
spec_id: '05'
spec_name: auth_middleware
title: Auth Middleware
status: draft
created_at: '2026-07-17T10:44:40.869274+00:00'
updated_at: '2026-07-17T10:44:40.869274+00:00'
owner: ''
source: interactive
schema_version: 1
---
# Auth Middleware

## Source

This spec is derived from the [apikit master PRD](docs/PRD.md). It covers the
**Auth Middleware** component — spec 05 of 15.

## Intent

Implement the authentication and authorization middleware for apikit. This
middleware intercepts every request to protected endpoints, extracts the Bearer
token from the `Authorization` header, identifies the credential type by its
format prefix, validates the credential against the database, enforces
role-based access control, and injects authenticated user context into the
request for downstream handlers.

The middleware is the single enforcement point for all authentication and
authorization decisions. Handlers never validate credentials themselves; they
read the authenticated context injected by this middleware.

## Goals

- Extract Bearer tokens from the `Authorization` header on every protected request.
- Detect credential type by format prefix using the build-time `TokenPrefix` variable.
- Validate admin tokens by SHA-256 hash comparison against the stored hash in the `admin_config` table.
- Validate API keys by `key_id` lookup in the `api_keys` table followed by SHA-256 hash verification of the secret.
- Validate PATs by `token_id` lookup in the `pats` table followed by SHA-256 hash verification of the secret, then permission check against the requested operation.
- Enforce role-based access control (admin token, admin user, regular user, PAT scoped access).
- Reject blocked users with HTTP 403 on every authenticated request regardless of credential validity.
- Reject expired credentials with HTTP 401.
- Reject revoked credentials with HTTP 401.
- Reject missing or malformed `Authorization` headers with HTTP 401.
- Provide a permission registration API for consuming projects to register their own resource types and actions.
- Inject authenticated user info and credential metadata into the request context for downstream handlers.

## Non-Goals

- **OAuth login flow.** Covered by a separate OAuth spec.
- **Credential creation or lifecycle management.** API key and PAT CRUD operations are covered by handler specs.
- **Admin token generation and rotation.** Covered by the admin bootstrap spec.
- **Rate limiting.** Explicitly deferred in the master PRD.
- **Session management or cookie-based auth.** apikit uses stateless Bearer token authentication only.
- **Permission inheritance or hierarchical roles.** The role model is flat: admin, user, and PAT-scoped.

## Dependencies

| Spec | Relationship |
|------|-------------|
| `01_server_core` | Registers as Echo middleware in the middleware chain; uses `APIError()` for error responses; reads `apikit.TokenPrefix` for credential type detection |
| `02_database_layer` | Queries `api_keys`, `pats`, `admin_config`, and `users` tables via `*sql.DB`; uses `db.ErrNotFound` sentinel; uses `db.FormatTime`/`db.ParseTime` for timestamp handling |

---

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25+ |
| HTTP framework | Echo v4 (`github.com/labstack/echo/v4`) |
| Database | SQLite via `internal/db` |
| Token hashing | SHA-256 (stdlib `crypto/sha256`) |
| Hex encoding | stdlib `encoding/hex` |

## Repository Layout

```
internal/
  auth/                     Auth middleware, token validation, permission registry
    auth.go                 Middleware constructor, token extraction, credential dispatch
    credentials.go          Admin token, API key, PAT validation logic
    permissions.go          Permission registry and PAT permission checking
    context.go              Request context keys, getters, and AuthInfo type
    auth_test.go            Unit and integration tests
```

---

## Functional Requirements

### Middleware Registration

The auth middleware is registered in the Echo middleware chain after the
Server Core middleware (panic recovery, request ID, body size limit,
content-type enforcement, security headers, logging). It runs before any
handler executes.

The middleware is created via a constructor that accepts the database handle
and the permission registry:

```go
// NewAuthMiddleware creates Echo middleware that authenticates requests
// using Bearer tokens from the Authorization header. It requires a
// database handle for credential lookups and a permission registry for
// PAT permission validation.
func NewAuthMiddleware(database *db.DB, registry *PermissionRegistry) echo.MiddlewareFunc
```

The middleware is applied to the `APIGroup()` Echo group so that all routes
under the mount point are protected. Health probes (`/healthz`, `/readyz`,
`/version`) and OAuth endpoints (`/auth/providers`, `/auth/callback`) are
outside the middleware's scope — they are registered at the server root or
on a separate unprotected group.

### Token Extraction

On every request to a protected endpoint, the middleware:

1. Reads the `Authorization` header.
2. If the header is missing or empty, returns HTTP 401 with error message
   `"missing authorization header"`.
3. If the header does not start with `Bearer ` (case-sensitive, with exactly
   one space), returns HTTP 401 with error message
   `"invalid authorization header format"`.
4. Extracts the token string after the `Bearer ` prefix.
5. If the extracted token is empty (header is `"Bearer "`), returns HTTP 401
   with error message `"missing token"`.

### Credential Type Detection

The middleware identifies the credential type by examining the token format
using the build-time `apikit.TokenPrefix` variable:

| Pattern | Credential Type |
|---------|----------------|
| `<prefix>_admin_<64 hex chars>` | Admin token |
| `<prefix>_pat_<token_id>_<secret>` | Personal access token (PAT) |
| `<prefix>_<key_id>_<secret>` | API key |

Detection order matters — the middleware must check for `<prefix>_admin_`
first, then `<prefix>_pat_`, and finally fall back to the API key pattern.
This prevents `<prefix>_pat_...` from being misidentified as an API key.

If the token does not match any recognized pattern, the middleware returns
HTTP 401 with error message `"unrecognized token format"`.

### Admin Token Validation

When an admin token is detected (`<prefix>_admin_<64 hex chars>`):

1. Validate that the hex portion is exactly 64 characters of valid hexadecimal.
2. Compute the SHA-256 hash of the **full token string** (including prefix).
3. Query the `admin_config` table for the row where `key = 'admin_token_hash'`.
4. Compare the computed hash (hex-encoded) against the stored `value`.
5. If the hash matches, authentication succeeds with admin-level access.
6. If the hash does not match or the `admin_token_hash` row is missing,
   return HTTP 401 with error message `"invalid credentials"`.

On success, inject into the request context:
- Credential type: `"admin_token"`
- User ID: empty (admin token is not associated with a user)
- Role: `"admin"`

### API Key Validation

When an API key is detected (`<prefix>_<key_id>_<secret>`):

1. Extract `key_id` and `secret` from the token.
2. Query the `api_keys` table for the row matching `key_id`.
3. If no row is found, return HTTP 401 with error message
   `"invalid credentials"`.
4. If `revoked_at` is not NULL, return HTTP 401 with error message
   `"credential revoked"`.
5. If `expires_at` is not NULL and the current time is past `expires_at`,
   return HTTP 401 with error message `"credential expired"`.
6. Compute the SHA-256 hash of `secret` and compare against `secret_hash`.
7. If the hash does not match, return HTTP 401 with error message
   `"invalid credentials"`.
8. Query the `users` table for the row matching `user_id` from the API key.
9. If the user's `status` is `"blocked"`, return HTTP 403 with error message
   `"user is blocked"`.
10. Authentication succeeds.

On success, inject into the request context:
- Credential type: `"api_key"`
- User ID: the user's UUID
- Role: the user's role (`"admin"` or `"user"`)
- Key ID: the `key_id`

### PAT Validation

When a PAT is detected (`<prefix>_pat_<token_id>_<secret>`):

1. Extract `token_id` and `secret` from the token.
2. Query the `pats` table for the row matching `token_id`.
3. If no row is found, return HTTP 401 with error message
   `"invalid credentials"`.
4. If `revoked_at` is not NULL, return HTTP 401 with error message
   `"credential revoked"`.
5. If `expires_at` is not NULL and the current time is past `expires_at`,
   return HTTP 401 with error message `"credential expired"`.
6. Compute the SHA-256 hash of `secret` and compare against `secret_hash`.
7. If the hash does not match, return HTTP 401 with error message
   `"invalid credentials"`.
8. Query the `users` table for the row matching `user_id` from the PAT.
9. If the user's `status` is `"blocked"`, return HTTP 403 with error message
   `"user is blocked"`.
10. Authentication succeeds, but access is subject to permission checks.

On success, inject into the request context:
- Credential type: `"pat"`
- User ID: the user's UUID
- Role: the user's role (for informational purposes; access is governed by PAT permissions)
- Token ID: the `token_id`
- Permissions: the JSON array of `resource_type:action` strings

### Role-Based Access Control

After authentication, the middleware enforces access control based on the
credential type and user role. Handlers use helper functions to check
authorization:

| Level | Credential | Access |
|-------|------------|--------|
| Break-glass | Admin token | Full access to all endpoints and all resources |
| Admin | API key (role=admin) | Full access to all endpoints and all resources |
| User | API key (role=user) | Full access to own resources only |
| Scoped | PAT | Access limited to granted `resource_type:action` permissions |

#### Authorization Helper Functions

Handlers call these functions to enforce authorization. They read the
authenticated context injected by the middleware:

```go
// RequireAdmin returns HTTP 403 if the authenticated credential does not
// have admin-level access (admin token or admin-role API key). Called by
// admin-only endpoints.
func RequireAdmin(c echo.Context) error

// RequireOwnerOrAdmin returns HTTP 403 if the authenticated user is neither
// the resource owner (userID matches the authenticated user) nor an admin.
// Called by user-scoped endpoints.
func RequireOwnerOrAdmin(c echo.Context, resourceOwnerID string) error

// RequirePermission returns HTTP 403 if the authenticated credential is a
// PAT that does not have the specified permission. Admin tokens and API keys
// (both admin and regular) bypass this check — they have implicit full
// permissions for their access level.
func RequirePermission(c echo.Context, resourceType, action string) error
```

### Blocked User Rejection

Blocked user checks are performed **after** credential validation but
**before** any access control decisions. A blocked user receives HTTP 403
with error message `"user is blocked"` regardless of credential type or
validity. This applies to:
- API key holders whose user status is `"blocked"`
- PAT holders whose user status is `"blocked"`

Admin tokens are not associated with a user and are not subject to blocked
user checks.

### Permission Registry

The permission registry allows consuming projects to register their own
resource types and actions. Built-in permissions are pre-registered:

```go
// PermissionRegistry tracks valid resource_type:action pairs.
type PermissionRegistry struct { ... }

// NewPermissionRegistry creates a registry pre-populated with the built-in
// permissions defined in the master PRD.
func NewPermissionRegistry() *PermissionRegistry

// Register adds a new resource_type:action permission. Returns an error if
// the permission is already registered (duplicate registration is a
// programming error). resource_type and action must be non-empty strings
// containing only lowercase letters, digits, and underscores.
func (r *PermissionRegistry) Register(resourceType, action string) error

// IsValid returns true if the given resource_type:action permission is
// registered.
func (r *PermissionRegistry) IsValid(resourceType, action string) bool

// List returns all registered permissions as a sorted slice of
// "resource_type:action" strings.
func (r *PermissionRegistry) List() []string
```

Built-in permissions (pre-registered):

| Resource Type | Action | Description |
|---------------|--------|-------------|
| `users` | `read` | View user profiles |
| `orgs` | `read` | View organizations and memberships |
| `keys` | `read` | View API keys |
| `keys` | `manage` | Manage API keys |
| `tokens` | `read` | View PATs |
| `tokens` | `manage` | Manage PATs |

### Request Context Injection

After successful authentication, the middleware injects an `AuthInfo` struct
into the Echo request context. Downstream handlers access it via helper
functions:

```go
// AuthInfo contains the authenticated identity and credential metadata
// extracted by the auth middleware.
type AuthInfo struct {
    CredentialType string   // "admin_token", "api_key", or "pat"
    UserID         string   // User UUID; empty for admin token
    Role           string   // "admin" or "user"; "admin" for admin token
    KeyID          string   // API key key_id; empty for other credential types
    TokenID        string   // PAT token_id; empty for other credential types
    Permissions    []string // PAT permissions; nil for other credential types
}

// GetAuthInfo retrieves the AuthInfo from the request context. Returns nil
// if the request was not authenticated (should not happen on protected routes).
func GetAuthInfo(c echo.Context) *AuthInfo

// GetUserID is a convenience function that returns the authenticated user's
// UUID from the request context. Returns an empty string for admin token
// requests.
func GetUserID(c echo.Context) string

// IsAdmin returns true if the authenticated credential has admin-level
// access (admin token or admin-role API key).
func IsAdmin(c echo.Context) bool
```

### Constant-Time Hash Comparison

All hash comparisons (admin token hash, API key secret hash, PAT secret hash)
must use `crypto/subtle.ConstantTimeCompare` to prevent timing side-channel
attacks. The middleware never compares hashes using `==` or `bytes.Equal`.

### Error Response Format

All authentication and authorization errors use the standard `APIError()`
helper from Server Core to produce the consistent JSON error envelope:

```json
{
  "error": {
    "code": 401,
    "message": "invalid credentials"
  }
}
```

Error messages are intentionally generic for security — they do not reveal
whether the credential type was recognized, the key_id was valid, or the
hash comparison failed. The specific error messages documented above
(`"invalid credentials"`, `"credential revoked"`, `"credential expired"`,
`"user is blocked"`) are the only messages used.

---

## Interfaces

### Auth Middleware Constructor

```go
// NewAuthMiddleware creates Echo middleware that authenticates requests.
// database is required for credential lookups (api_keys, pats, admin_config,
// users tables). registry is required for PAT permission validation.
// Both parameters must be non-nil; NewAuthMiddleware panics if either is nil.
func NewAuthMiddleware(database *db.DB, registry *PermissionRegistry) echo.MiddlewareFunc
```

### Token Parsing (internal)

```go
// parseToken extracts the credential type and components from a raw Bearer
// token string using apikit.TokenPrefix for format detection.
// Returns the credential type ("admin_token", "api_key", "pat") and the
// parsed components, or an error if the format is unrecognized.
func parseToken(token string) (credType string, components []string, err error)
```

### Hash Computation

```go
// hashToken computes the SHA-256 hash of the input string and returns it
// as a lowercase hex-encoded string. Used for admin token validation
// (hashes the full token) and for API key / PAT secret validation (hashes
// just the secret portion).
func hashToken(input string) string
```

### Context Keys

```go
// contextKey is an unexported type for context keys to avoid collisions.
type contextKey string

const authInfoKey contextKey = "auth_info"
```

---

## Testing Strategy

### Unit Tests

- `TestParseToken_AdminToken` — valid admin token is correctly identified.
- `TestParseToken_APIKey` — valid API key is correctly identified and components extracted.
- `TestParseToken_PAT` — valid PAT is correctly identified and components extracted.
- `TestParseToken_UnrecognizedFormat` — garbage input returns error.
- `TestParseToken_EmptyString` — empty input returns error.
- `TestParseToken_WrongPrefix` — token with wrong prefix returns error.
- `TestParseToken_AdminBeforePAT` — `<prefix>_admin_<hex>` is identified as admin, not API key.
- `TestParseToken_PATBeforeAPIKey` — `<prefix>_pat_<id>_<secret>` is identified as PAT, not API key.
- `TestHashToken` — known input produces expected SHA-256 hex output.
- `TestHashToken_ConstantTime` — verify constant-time comparison is used (code inspection or integration test).
- `TestPermissionRegistry_BuiltIns` — new registry contains all 6 built-in permissions.
- `TestPermissionRegistry_Register` — custom permission is added and queryable.
- `TestPermissionRegistry_DuplicateRegister` — duplicate registration returns error.
- `TestPermissionRegistry_InvalidFormat` — empty or invalid resource_type/action returns error.
- `TestPermissionRegistry_IsValid` — returns true for registered, false for unregistered.
- `TestPermissionRegistry_List` — returns sorted list of all permissions.
- `TestGetAuthInfo_NotSet` — returns nil when no auth info in context.
- `TestGetAuthInfo_Set` — returns correct AuthInfo after middleware runs.
- `TestIsAdmin_AdminToken` — returns true for admin token credential.
- `TestIsAdmin_AdminAPIKey` — returns true for admin-role API key.
- `TestIsAdmin_RegularAPIKey` — returns false for regular-user API key.
- `TestIsAdmin_PAT` — returns false for PAT.
- `TestRequireAdmin_Authorized` — no error for admin credential.
- `TestRequireAdmin_Forbidden` — HTTP 403 for non-admin credential.
- `TestRequireOwnerOrAdmin_Owner` — no error when user is resource owner.
- `TestRequireOwnerOrAdmin_Admin` — no error when user is admin.
- `TestRequireOwnerOrAdmin_Forbidden` — HTTP 403 when neither owner nor admin.
- `TestRequirePermission_APIKey` — bypassed for API key (implicit full permissions).
- `TestRequirePermission_AdminToken` — bypassed for admin token.
- `TestRequirePermission_PAT_Granted` — no error when PAT has the permission.
- `TestRequirePermission_PAT_Denied` — HTTP 403 when PAT lacks the permission.

### Integration Tests

- `TestMiddleware_MissingAuthHeader` — returns HTTP 401.
- `TestMiddleware_InvalidAuthFormat` — `Authorization: Basic ...` returns HTTP 401.
- `TestMiddleware_EmptyBearer` — `Authorization: Bearer ` returns HTTP 401.
- `TestMiddleware_ValidAdminToken` — admin token authenticates, context has admin access.
- `TestMiddleware_InvalidAdminToken` — wrong admin token returns HTTP 401.
- `TestMiddleware_ValidAPIKey` — API key authenticates, context has user info.
- `TestMiddleware_ExpiredAPIKey` — expired API key returns HTTP 401.
- `TestMiddleware_RevokedAPIKey` — revoked API key returns HTTP 401.
- `TestMiddleware_InvalidAPIKeySecret` — wrong secret returns HTTP 401.
- `TestMiddleware_ValidPAT` — PAT authenticates, context has permissions.
- `TestMiddleware_ExpiredPAT` — expired PAT returns HTTP 401.
- `TestMiddleware_RevokedPAT` — revoked PAT returns HTTP 401.
- `TestMiddleware_InvalidPATSecret` — wrong PAT secret returns HTTP 401.
- `TestMiddleware_BlockedUser_APIKey` — blocked user with valid API key returns HTTP 403.
- `TestMiddleware_BlockedUser_PAT` — blocked user with valid PAT returns HTTP 403.
- `TestMiddleware_UnrecognizedToken` — random string returns HTTP 401.
- `TestMiddleware_AdminToken_FullAccess` — admin token can access admin-only endpoints.
- `TestMiddleware_AdminAPIKey_FullAccess` — admin-role API key can access admin-only endpoints.
- `TestMiddleware_RegularAPIKey_OwnResources` — regular user can access own resources.
- `TestMiddleware_RegularAPIKey_OtherResources` — regular user cannot access other user's resources (HTTP 403).
- `TestMiddleware_PAT_GrantedPermission` — PAT with matching permission succeeds.
- `TestMiddleware_PAT_DeniedPermission` — PAT without matching permission returns HTTP 403.
- `TestMiddleware_ContextInjection` — downstream handler can read AuthInfo from context.

---

## Error Handling

| Condition | Status | Message |
|-----------|--------|---------|
| Missing `Authorization` header | 401 | `"missing authorization header"` |
| `Authorization` not `Bearer ...` format | 401 | `"invalid authorization header format"` |
| Token string empty after `Bearer ` | 401 | `"missing token"` |
| Token format unrecognized | 401 | `"unrecognized token format"` |
| Admin token hash mismatch | 401 | `"invalid credentials"` |
| API key `key_id` not found | 401 | `"invalid credentials"` |
| API key secret hash mismatch | 401 | `"invalid credentials"` |
| PAT `token_id` not found | 401 | `"invalid credentials"` |
| PAT secret hash mismatch | 401 | `"invalid credentials"` |
| Credential revoked (`revoked_at` not NULL) | 401 | `"credential revoked"` |
| Credential expired (`expires_at` passed) | 401 | `"credential expired"` |
| User status is `"blocked"` | 403 | `"user is blocked"` |
| Non-admin accessing admin-only endpoint | 403 | `"forbidden"` |
| User accessing another user's resources | 403 | `"forbidden"` |
| PAT lacks required permission | 403 | `"insufficient permissions"` |
| Database error during lookup | 500 | `"internal server error"` |

---

## Design Decisions

- **Single middleware enforcement point.** All authentication and authorization
  decisions happen in one middleware. Handlers never validate credentials;
  they read the injected context. This eliminates the risk of inconsistent
  auth checks across handlers.

- **Detection order: admin > PAT > API key.** The prefix check order prevents
  `<prefix>_pat_...` from matching the API key pattern `<prefix>_<key_id>_<secret>`.
  Admin tokens are checked first because `<prefix>_admin_` could also match
  the API key pattern.

- **Constant-time hash comparison.** Using `crypto/subtle.ConstantTimeCompare`
  for all hash comparisons prevents timing attacks that could reveal
  information about stored hashes.

- **Generic error messages.** Authentication errors use generic messages like
  `"invalid credentials"` rather than revealing whether the key_id was found,
  the hash matched, etc. This follows security best practices.

- **Blocked user check after credential validation.** Blocked status is
  checked after verifying the credential is valid. This ensures that blocked
  users cannot probe whether their credentials are still valid by observing
  different error responses.

- **Admin token hashes the full token.** The admin token hash is computed
  from the entire token string (including prefix), consistent with how the
  admin bootstrap spec generates and stores the hash.

- **API key and PAT hash only the secret.** For API keys and PATs, only the
  secret portion is hashed. The `key_id`/`token_id` is stored in plaintext
  and used for lookup.

- **Permission registry is extensible.** Consuming projects register their
  own `resource_type:action` pairs at startup. This allows the auth
  middleware to validate PAT permissions for domain-specific operations
  without modifying apikit itself.

- **AuthInfo struct in context.** A single struct carries all authenticated
  identity information, avoiding scattered context keys. Downstream handlers
  use typed getter functions for clean access.

- **No session management.** apikit uses stateless Bearer token authentication.
  Every request is authenticated independently by looking up the credential
  in the database. There are no sessions, cookies, or JWTs.

---

## Glossary

| Term | Definition |
|------|------------|
| **Admin token** | A break-glass infrastructure credential in the format `<prefix>_admin_<64 hex>` for emergency access. Validated by SHA-256 hash comparison against the `admin_config` table. |
| **API key** | A user-scoped credential in the format `<prefix>_<key_id>_<secret>`, issued via OAuth login. Validated by `key_id` lookup and secret hash comparison. |
| **PAT** | A user-created, fine-grained credential in the format `<prefix>_pat_<token_id>_<secret>`. Validated by `token_id` lookup, secret hash comparison, and permission check. |
| **AuthInfo** | The struct injected into the Echo request context by the auth middleware, containing credential type, user ID, role, and permissions. |
| **PermissionRegistry** | A thread-safe registry of valid `resource_type:action` pairs used for PAT permission validation. Pre-populated with built-in permissions; extensible by consuming projects. |
| **Bearer token** | The authentication scheme used by apikit. Tokens are passed in the `Authorization: Bearer <token>` header. |
| **Credential type** | One of three token formats: admin token, API key, or PAT. Detected by examining the token's format prefix. |
| **TokenPrefix** | The build-time configurable variable `apikit.TokenPrefix` (default `"ak"`) used to identify and parse all credential types. |
| **Constant-time comparison** | Using `crypto/subtle.ConstantTimeCompare` for hash comparisons to prevent timing side-channel attacks. |
| **`admin_config`** | Database table storing singleton server state including `admin_token_hash`. |
| **`api_keys`** | Database table storing API key metadata and secret hashes. |
| **`pats`** | Database table storing PAT metadata, secret hashes, and permissions. |
| **`users`** | Database table storing user profiles including role and status. |

