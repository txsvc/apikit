# API Reference

Complete HTTP API reference for apikit. All endpoints return JSON. All timestamps
use RFC 3339 format in UTC with a `Z` suffix.

---

## Base URL

All API endpoints are served under a configurable mount point. The default is:

```
/api/v1
```

The mount point is set via `server.mount_point` in `config.toml`. Health probes
and version info are served at the server root, outside the mount point.

Throughout this document, paths are shown relative to the mount point unless
noted otherwise. For example, `GET /user` means `GET /api/v1/user` with the
default configuration.

---

## Authentication

apikit uses Bearer token authentication via the `Authorization` header:

```
Authorization: Bearer <token>
```

Three credential types are supported:

| Credential | Format | Scope |
|---|---|---|
| Admin Token | `ak_admin_<64 hex chars>` | Full administrative access; not tied to a user account |
| API Key | `ak_<key_id>_<secret>` | Full access scoped to the owning user |
| Personal Access Token (PAT) | `ak_pat_<token_id>_<secret>` | Permission-scoped access for the owning user |

The `ak` prefix is the default token namespace and is configurable at build time.

### Credential detection priority

Tokens are classified by prefix in this order:

1. `ak_admin_` -- Admin Token
2. `ak_pat_` -- Personal Access Token
3. `ak_` -- API Key

### Permission model

Admin Tokens and API Keys carry implicit full permissions for their access level.
PATs are restricted to their declared permission set. A PAT belonging to a user
with the `admin` role is never treated as admin-level -- PATs always operate
within their declared permissions.

Built-in PAT permissions:

| Permission | Description |
|---|---|
| `users:read` | Read own user profile |
| `orgs:read` | Read own organization memberships |
| `keys:read` | List own API keys |
| `keys:manage` | Refresh and revoke own API keys |
| `tokens:read` | List and view own PATs |
| `tokens:manage` | Create and revoke own PATs |

Permission identifiers follow the pattern `resource_type:action` where both
parts contain only lowercase letters, digits, and underscores.

---

## Common Headers

### Request headers

| Header | Description |
|---|---|
| `Authorization` | `Bearer <token>` -- required for authenticated endpoints |
| `Content-Type` | Must be `application/json` for POST, PUT, and PATCH requests. Other values return 415. |
| `X-Request-ID` | Optional UUID v4. If a valid UUID v4 is provided, it is echoed back; otherwise the server generates one. |
| `If-None-Match` | ETag value for conditional GET requests. Supported on select endpoints. |

### Response headers

| Header | Value | Description |
|---|---|---|
| `X-Request-ID` | UUID v4 | Present on every response. Echoes the request ID or a server-generated one. |
| `X-Content-Type-Options` | `nosniff` | Prevents MIME-type sniffing |
| `X-Frame-Options` | `DENY` | Prevents clickjacking |
| `Referrer-Policy` | `no-referrer` | Suppresses Referer header |
| `Cache-Control` | Varies by endpoint | See individual endpoints |
| `ETag` | Weak ETag (`W/"<timestamp>"`) | Set on endpoints that support conditional GET |
| `Content-Type` | `application/json; charset=utf-8` | All JSON responses |

### Cache-Control policies

| Policy | Header value | Applied to |
|---|---|---|
| No store | `no-store` | All API endpoints under the mount point (default) |
| No cache | `no-cache` | Health probes (`/healthz`, `/readyz`) |
| Public | `public, max-age=300` | `/version`, `/auth/providers` |

---

## Error Format

All error responses use a consistent JSON envelope:

```json
{
  "error": {
    "code": 401,
    "message": "missing authorization header"
  }
}
```

The `code` field mirrors the HTTP status code exactly.

### Standard error codes

| Status | Meaning |
|---|---|
| 400 | Invalid or malformed request body |
| 401 | Missing, invalid, expired, or revoked credentials |
| 403 | Valid credentials but insufficient permissions, or user is blocked |
| 404 | Requested resource does not exist |
| 409 | Uniqueness constraint violated |
| 413 | Request body exceeds the configured size limit |
| 415 | Content-Type is not `application/json` |
| 500 | Internal server error |
| 502 | Upstream provider error (OAuth only) |
| 503 | Service not ready |

---

## Health Probes

Health and diagnostic endpoints are served at the server root (not under the
mount point). No authentication is required.

### GET /healthz

Liveness probe. Always succeeds if the server is running.

**Auth:** None

**Response:**

| Status | Body |
|---|---|
| 200 | `{"status": "ok"}` |

**Headers:** `Cache-Control: no-cache`

---

### GET /readyz

Readiness probe. Calls the configured health checker (typically a database ping).

**Auth:** None

**Response:**

| Status | Body |
|---|---|
| 200 | `{"status": "ready"}` |
| 503 | `{"status": "not ready"}` |

**Headers:** `Cache-Control: no-cache`

---

### GET /version

Returns server version and build information.

**Auth:** None

**Response:**

| Status | Body |
|---|---|
| 200 | See below |

```json
{
  "version": "0.1.0",
  "build": "7fb5b55",
  "mount_point": "/api/v1"
}
```

**Headers:** `Cache-Control: public, max-age=300`

---

## OAuth Endpoints

These endpoints handle OAuth authentication. No Bearer token is required.

### GET /auth/providers

Returns the list of configured OAuth providers with their authorization URLs.
Secrets (`client_secret`, `token_url`, `userinfo_url`) are never exposed.

**Auth:** None

**Response:**

| Status | Body |
|---|---|
| 200 | Array of provider objects |

```json
[
  {
    "name": "github",
    "authorize_url": "https://github.com/login/oauth/authorize?client_id=...&scope=user%3Aemail"
  }
]
```

**Headers:** `Cache-Control: public, max-age=300`

---

### POST /auth/callback

Exchanges an OAuth authorization code for user credentials. Creates or updates
the user account and generates a new API key. All previously active API keys for
the user are revoked.

**Auth:** None

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `provider` | string | Yes | OAuth provider name (e.g. `"github"`) |
| `code` | string | Yes | Authorization code from the OAuth provider |
| `redirect_uri` | string | Yes | The redirect URI used in the authorization request |
| `expires` | integer | No | API key expiry in days. Allowed values: `0` (permanent), `30`, `60`, `90`. Default: `90`. |

**Redirect URI rules:**

- `http://localhost:<any-port>/<any-path>` is accepted (HTTPS on localhost is rejected)
- URIs matching the server's configured `external_url` scheme and host are accepted
- All other origins are rejected

**Response:**

| Status | Body |
|---|---|
| 200 | User object and API key |

```json
{
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "username": "octocat",
    "email": "octocat@example.com",
    "full_name": null,
    "status": "active",
    "role": "user",
    "provider": "github",
    "provider_id": "12345",
    "created_at": "2026-07-18T10:00:00Z",
    "updated_at": "2026-07-18T10:00:00Z"
  },
  "api_key": {
    "key": "ak_aBcDeFgH_abcdefghijklmnopqrstuvwxyz012345",
    "key_id": "aBcDeFgH",
    "expires_at": "2026-10-16T10:00:00Z"
  }
}
```

The `api_key.key` value contains the plaintext secret and is only returned once.

**Errors:**

| Status | Condition |
|---|---|
| 400 | Missing required field, invalid `expires` value, unknown provider, disallowed redirect URI, empty email from provider |
| 401 | Authorization code exchange failed |
| 403 | User account is blocked |
| 502 | Failed to retrieve user info from the OAuth provider |

---

## User Endpoints

Authenticated endpoints for managing the current user's own profile and
credentials. All paths are relative to the mount point.

### GET /user

Returns the authenticated user's profile.

**Auth:** Bearer (Admin Token, API Key, or PAT with `users:read`)

**Conditional request:** Supports `If-None-Match` header. Returns `304 Not Modified`
when the ETag matches.

**Response:**

| Status | Body |
|---|---|
| 200 | User object |
| 304 | No body (cache is current) |

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "username": "octocat",
  "email": "octocat@example.com",
  "full_name": "The Octocat",
  "status": "active",
  "role": "user",
  "provider": "github",
  "provider_id": "12345",
  "created_at": "2026-07-18T10:00:00Z",
  "updated_at": "2026-07-18T12:00:00Z"
}
```

**Headers:** `ETag: W/"2026-07-18T12:00:00Z"`, `Cache-Control: no-store`

**Errors:** 401, 403

---

### PATCH /user

Updates the authenticated user's profile. Only `full_name` can be changed.

**Auth:** Bearer (Admin Token, API Key, or PAT with `users:read`)

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `full_name` | string | Yes | Display name. Empty string clears the field. |

**Response:**

| Status | Body |
|---|---|
| 200 | Updated User object |

**Errors:** 400, 401, 403

---

### GET /user/keys

Lists all API keys for the authenticated user (metadata only, no secrets).

**Auth:** Bearer (Admin Token, API Key, or PAT with `keys:read`)

**Conditional request:** Supports `If-None-Match` header.

**Response:**

| Status | Body |
|---|---|
| 200 | Array of API key metadata |
| 304 | No body (cache is current) |

```json
[
  {
    "key_id": "aBcDeFgH",
    "created_at": "2026-07-18T10:00:00Z",
    "expires_at": "2026-10-16T10:00:00Z",
    "revoked_at": null
  }
]
```

Returns `[]` (empty array, never `null`) when no keys exist.

**Errors:** 401, 403

---

### POST /user/keys/:key_id/refresh

Generates a new secret for an existing API key. The key ID is preserved but the
secret, secret hash, and expiry window are reset. Requires API key
authentication -- PAT authentication is rejected with 401.

**Auth:** Bearer (Admin Token or API Key only; PATs return 401)

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `key_id` | string | The API key identifier |

**Response:**

| Status | Body |
|---|---|
| 200 | New API key with plaintext secret |

```json
{
  "key": "ak_aBcDeFgH_newSecretValueHere0123456789ab",
  "key_id": "aBcDeFgH",
  "expires_at": "2026-10-16T10:00:00Z"
}
```

**Errors:**

| Status | Condition |
|---|---|
| 400 | Key is revoked or expired |
| 401 | PAT authentication used (API key required) |
| 404 | Key not found or belongs to another user |

---

### DELETE /user/keys/:key_id

Revokes an API key. The key remains in the database for audit purposes but can
no longer be used for authentication.

**Auth:** Bearer (Admin Token, API Key, or PAT with `keys:manage`)

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `key_id` | string | The API key identifier |

**Response:**

| Status | Body |
|---|---|
| 200 | Revocation confirmation |

```json
{
  "key_id": "aBcDeFgH",
  "revoked_at": "2026-07-18T14:00:00Z"
}
```

**Errors:**

| Status | Condition |
|---|---|
| 400 | Key is already revoked |
| 404 | Key not found or belongs to another user |

---

### GET /user/tokens

Lists all Personal Access Tokens for the authenticated user.

**Auth:** Bearer (Admin Token, API Key, or PAT with `tokens:read`)

**Response:**

| Status | Body |
|---|---|
| 200 | Array of PAT metadata |

```json
[
  {
    "token_id": "abcd1234",
    "name": "CI pipeline",
    "permissions": ["users:read", "orgs:read"],
    "created_at": "2026-07-18T10:00:00Z",
    "expires_at": "2026-10-16T10:00:00Z",
    "revoked_at": null
  }
]
```

Returns `[]` (empty array) when no tokens exist. Ordered by `created_at` descending.

**Errors:** 401, 403

---

### POST /user/tokens

Creates a new Personal Access Token. The plaintext token is returned once and
cannot be retrieved afterward.

**Auth:** Bearer (Admin Token, API Key, or PAT with `tokens:manage`)

**Privilege escalation guard:** When the caller authenticates via PAT, the new
token's permissions cannot exceed the caller's permissions.

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Human-readable label (max 255 characters) |
| `permissions` | array of strings | Yes | Non-empty list of permission strings (e.g. `["users:read", "orgs:read"]`). Each must be registered in the permission registry. |
| `expires` | integer | No | Expiry in days. Allowed values: `0` (permanent), `30`, `60`, `90`. Default: `90`. |

**Response:**

| Status | Body |
|---|---|
| 201 | PAT with plaintext token |

```json
{
  "token_id": "abcd1234",
  "name": "CI pipeline",
  "token": "ak_pat_abcd1234_secretvalue0123456789abcdef",
  "permissions": ["users:read", "orgs:read"],
  "expires_at": "2026-10-16T10:00:00Z",
  "created_at": "2026-07-18T10:00:00Z"
}
```

**Errors:**

| Status | Condition |
|---|---|
| 400 | Missing or invalid field, unknown permission, invalid `expires` value |
| 403 | PAT caller attempting to grant permissions it does not hold |

---

### GET /user/tokens/:token_id

Returns metadata for a specific PAT owned by the authenticated user.

**Auth:** Bearer (Admin Token, API Key, or PAT with `tokens:read`)

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `token_id` | string | The token identifier |

**Conditional request:** Supports `If-None-Match` header.

**Response:**

| Status | Body |
|---|---|
| 200 | PAT metadata |
| 304 | No body (cache is current) |

```json
{
  "token_id": "abcd1234",
  "name": "CI pipeline",
  "permissions": ["users:read", "orgs:read"],
  "created_at": "2026-07-18T10:00:00Z",
  "expires_at": "2026-10-16T10:00:00Z",
  "revoked_at": null
}
```

**Headers:** `ETag` header set on 200 responses.

**Errors:** 401, 403, 404

---

### DELETE /user/tokens/:token_id

Revokes a PAT. The token remains in the database for audit purposes.

**Auth:** Bearer (Admin Token, API Key, or PAT with `tokens:manage`)

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `token_id` | string | The token identifier |

**Response:**

| Status | Body |
|---|---|
| 200 | Revoked PAT metadata (includes `revoked_at`) |

**Errors:**

| Status | Condition |
|---|---|
| 400 | Token is already revoked |
| 404 | Token not found or belongs to another user |

---

### GET /user/orgs

Lists organizations the authenticated user is a member of. Only active (not
blocked) organizations are returned.

**Auth:** Bearer (Admin Token, API Key, or PAT with `orgs:read`)

**Response:**

| Status | Body |
|---|---|
| 200 | Array of Organization objects |

```json
[
  {
    "id": "660e8400-e29b-41d4-a716-446655440000",
    "name": "Acme Corp",
    "slug": "acme-corp",
    "url": "https://acme.example.com",
    "status": "active",
    "created_at": "2026-07-01T09:00:00Z",
    "updated_at": "2026-07-15T11:00:00Z"
  }
]
```

**Errors:** 401, 403

---

## Admin User Endpoints

Administrative endpoints for managing all user accounts. Requires admin
credentials (Admin Token, or API Key belonging to a user with the `admin` role).
PATs are never treated as admin-level regardless of the user's role.

### GET /users

Lists all user accounts.

**Auth:** Admin required

**Query parameters:**

| Parameter | Type | Default | Description |
|---|---|---|---|
| `include_blocked` | boolean | `false` | When `true`, includes users with `status=blocked` |

**Response:**

| Status | Body |
|---|---|
| 200 | Array of User objects |

Returns `[]` (empty array) when no users match.

**Errors:** 401, 403

---

### POST /users

Creates a new user account.

**Auth:** Admin required

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `username` | string | Yes | Unique username |
| `email` | string | Yes | Email address |
| `provider` | string | Yes | OAuth provider name |
| `provider_id` | string | Yes | Provider-specific user identifier |

The new user is created with `role=user`, `status=active`, and an empty `full_name`.

**Response:**

| Status | Body |
|---|---|
| 201 | Created User object |

**Errors:**

| Status | Condition |
|---|---|
| 400 | Missing required field |
| 409 | Username already exists, or provider identity already exists |

---

### GET /users/:id

Returns a user by ID.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | User identifier |

**Conditional request:** Supports `If-None-Match` header.

**Response:**

| Status | Body |
|---|---|
| 200 | User object |
| 304 | No body (cache is current) |

**Headers:** `ETag` header set on 200 responses.

**Errors:** 401, 403, 404

---

### PATCH /users/:id

Updates a user's profile by ID. Only `full_name` can be changed.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | User identifier |

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `full_name` | string | Yes | Display name. Empty string clears the field. |

**Response:**

| Status | Body |
|---|---|
| 200 | Updated User object |

**Errors:** 400, 401, 403, 404

---

### POST /users/:id/promote

Promotes a user to the admin role. Idempotent: if the user already has the admin
role, returns 200 with no changes.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | User identifier |

**Response:**

| Status | Body |
|---|---|
| 200 | User object with `role=admin` |

**Errors:** 401, 403, 404

---

### POST /users/:id/demote

Revokes the admin role from a user, setting their role to `user`. Idempotent: if
the user already has the `user` role, returns 200 with no changes.

A last-admin safeguard prevents demoting the only remaining admin.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | User identifier |

**Response:**

| Status | Body |
|---|---|
| 200 | User object with `role=user` |

**Errors:**

| Status | Condition |
|---|---|
| 404 | User not found |
| 409 | Cannot demote the last remaining admin |

---

### POST /users/:id/block

Blocks a user account. Blocked users cannot authenticate. Idempotent.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | User identifier |

**Response:**

| Status | Body |
|---|---|
| 200 | User object with `status=blocked` |

**Errors:** 401, 403, 404

---

### POST /users/:id/unblock

Unblocks a user account, restoring it to active status. Idempotent.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | User identifier |

**Response:**

| Status | Body |
|---|---|
| 200 | User object with `status=active` |

**Errors:** 401, 403, 404

---

### GET /users/:id/keys

Lists all API keys for a specific user (metadata only).

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | User identifier |

**Response:**

| Status | Body |
|---|---|
| 200 | Array of API key metadata objects |

**Errors:** 401, 403, 404

---

### DELETE /users/:id/keys/:key_id

Revokes a specific API key belonging to a user. Idempotent: revoking an
already-revoked key returns 204.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | User identifier |
| `key_id` | string | API key identifier |

**Response:**

| Status | Body |
|---|---|
| 204 | No body |

**Errors:** 401, 403, 404

---

### GET /users/:id/tokens

Lists all Personal Access Tokens for a specific user (metadata only).

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | User identifier |

**Response:**

| Status | Body |
|---|---|
| 200 | Array of PAT metadata objects |

**Errors:** 401, 403, 404

---

### DELETE /users/:id/tokens/:token_id

Revokes a specific PAT belonging to a user. Idempotent: revoking an
already-revoked token returns 204.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | User identifier |
| `token_id` | string | Token identifier |

**Response:**

| Status | Body |
|---|---|
| 204 | No body |

**Errors:** 401, 403, 404

---

## Admin Organization Endpoints

Administrative endpoints for managing organizations. Requires admin credentials.

### POST /orgs

Creates a new organization.

**Auth:** Admin required

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Display name (whitespace-trimmed; must be non-empty after trimming) |
| `slug` | string | Yes | URL-safe identifier. Must be 1-128 characters, lowercase alphanumeric plus hyphens and underscores, must not start or end with a hyphen or underscore. |
| `url` | string | No | Organization URL |

The organization is created with `status=active`.

**Response:**

| Status | Body |
|---|---|
| 201 | Created Organization object |

```json
{
  "id": "660e8400-e29b-41d4-a716-446655440000",
  "name": "Acme Corp",
  "slug": "acme-corp",
  "url": "https://acme.example.com",
  "status": "active",
  "created_at": "2026-07-18T10:00:00Z",
  "updated_at": "2026-07-18T10:00:00Z"
}
```

**Errors:**

| Status | Condition |
|---|---|
| 400 | Missing required field, invalid slug format |
| 409 | Organization name or slug already exists |

---

### GET /orgs

Lists all organizations.

**Auth:** Admin required

**Query parameters:**

| Parameter | Type | Default | Description |
|---|---|---|---|
| `include_blocked` | boolean | `false` | When `true`, includes organizations with `status=blocked` |

Results are ordered by `name` ascending.

**Response:**

| Status | Body |
|---|---|
| 200 | Array of Organization objects |

**Errors:** 401, 403

---

### GET /orgs/:id

Returns an organization by ID. Admins can view any organization. Non-admin users
can only view organizations they are a member of.

**Auth:** Bearer (Admin, or authenticated member of the organization)

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | Organization identifier |

**Conditional request:** Supports `If-None-Match` header.

**Response:**

| Status | Body |
|---|---|
| 200 | Organization object |
| 304 | No body (cache is current) |

**Headers:** `ETag` header set on 200 responses.

**Errors:**

| Status | Condition |
|---|---|
| 400 | Invalid organization ID format |
| 403 | Not an admin and not a member of the organization |
| 404 | Organization not found |

---

### PATCH /orgs/:id

Updates an organization. The `slug` field is immutable and cannot be changed.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | Organization identifier |

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | No | New display name (trimmed; must be non-empty after trimming) |
| `url` | string | No | New URL |

At least one field must be provided; otherwise returns 400.

**Response:**

| Status | Body |
|---|---|
| 200 | Updated Organization object |

**Errors:**

| Status | Condition |
|---|---|
| 400 | No fields to update, or empty name after trimming |
| 404 | Organization not found |
| 409 | Organization name already exists |

---

### DELETE /orgs/:id

Deletes an organization permanently. All membership records are cascade-deleted.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | Organization identifier |

**Response:**

| Status | Body |
|---|---|
| 204 | No body |

**Errors:** 401, 403, 404

---

### POST /orgs/:id/block

Blocks an organization. Idempotent.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | Organization identifier |

**Response:**

| Status | Body |
|---|---|
| 200 | Organization object with `status=blocked` |

**Errors:** 401, 403, 404

---

### POST /orgs/:id/unblock

Unblocks an organization, restoring it to active status. Idempotent.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | Organization identifier |

**Response:**

| Status | Body |
|---|---|
| 200 | Organization object with `status=active` |

**Errors:** 401, 403, 404

---

### GET /orgs/:id/members

Lists members of an organization. Admins can list any organization's members.
Non-admin users can only list members of organizations they belong to.

**Auth:** Bearer (Admin, or authenticated member of the organization)

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | Organization identifier |

Results are ordered by `username` ascending.

**Response:**

| Status | Body |
|---|---|
| 200 | Array of member objects |

```json
[
  {
    "org_id": "660e8400-e29b-41d4-a716-446655440000",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "username": "octocat",
    "email": "octocat@example.com",
    "role": "user",
    "created_at": "2026-07-18T10:00:00Z"
  }
]
```

**Errors:**

| Status | Condition |
|---|---|
| 403 | Not an admin and not a member of the organization |
| 404 | Organization not found |

---

### PUT /orgs/:id/members/:user_id

Adds a user to an organization. Idempotent: adding a user who is already a
member returns 204 with no changes.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | Organization identifier |
| `user_id` | string (UUID) | User identifier |

**Request body:** None

**Response:**

| Status | Body |
|---|---|
| 204 | No body |

**Errors:**

| Status | Condition |
|---|---|
| 404 | Organization or user not found |

---

### DELETE /orgs/:id/members/:user_id

Removes a user from an organization. Does not affect the user's account.

**Auth:** Admin required

**Path parameters:**

| Parameter | Type | Description |
|---|---|---|
| `id` | string (UUID) | Organization identifier |
| `user_id` | string (UUID) | User identifier |

**Response:**

| Status | Body |
|---|---|
| 204 | No body |

**Errors:**

| Status | Condition |
|---|---|
| 404 | Membership not found |

---

## Endpoint Summary

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | /healthz | None | Liveness probe |
| GET | /readyz | None | Readiness probe |
| GET | /version | None | Server version info |
| GET | /auth/providers | None | List OAuth providers |
| POST | /auth/callback | None | OAuth code exchange |
| GET | /user | Bearer | Get own profile |
| PATCH | /user | Bearer | Update own profile |
| GET | /user/keys | Bearer | List own API keys |
| POST | /user/keys/:key_id/refresh | Bearer (API key only) | Refresh own API key |
| DELETE | /user/keys/:key_id | Bearer | Revoke own API key |
| GET | /user/tokens | Bearer | List own PATs |
| POST | /user/tokens | Bearer | Create a PAT |
| GET | /user/tokens/:token_id | Bearer | Get a specific PAT |
| DELETE | /user/tokens/:token_id | Bearer | Revoke a PAT |
| GET | /user/orgs | Bearer | List own organizations |
| GET | /users | Admin | List all users |
| POST | /users | Admin | Create a user |
| GET | /users/:id | Admin | Get user by ID |
| PATCH | /users/:id | Admin | Update user by ID |
| POST | /users/:id/promote | Admin | Promote to admin |
| POST | /users/:id/demote | Admin | Demote from admin |
| POST | /users/:id/block | Admin | Block a user |
| POST | /users/:id/unblock | Admin | Unblock a user |
| GET | /users/:id/keys | Admin | List user's API keys |
| DELETE | /users/:id/keys/:key_id | Admin | Revoke user's API key |
| GET | /users/:id/tokens | Admin | List user's PATs |
| DELETE | /users/:id/tokens/:token_id | Admin | Revoke user's PAT |
| POST | /orgs | Admin | Create organization |
| GET | /orgs | Admin | List all organizations |
| GET | /orgs/:id | Admin or member | Get organization |
| PATCH | /orgs/:id | Admin | Update organization |
| DELETE | /orgs/:id | Admin | Delete organization |
| POST | /orgs/:id/block | Admin | Block organization |
| POST | /orgs/:id/unblock | Admin | Unblock organization |
| GET | /orgs/:id/members | Admin or member | List org members |
| PUT | /orgs/:id/members/:user_id | Admin | Add org member |
| DELETE | /orgs/:id/members/:user_id | Admin | Remove org member |