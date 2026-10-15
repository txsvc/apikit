---
spec_id: '03'
spec_name: openapi_specification
title: Openapi Specification
status: draft
created_at: '2026-07-17T09:50:36.453282+00:00'
updated_at: '2026-07-17T09:55:01.438747+00:00'
owner: ''
source: interactive
schema_version: 1
---
# OpenAPI Specification

## Intent

Create a complete OpenAPI 3.1 specification document (`api/openapi.yaml`) that
serves as the single source of truth for the apikit API contract. All SDKs,
documentation, and server implementations derive from this specification.

This is the foundational artifact of the apikit project — it defines every
built-in endpoint, request/response schema, authentication scheme, error
envelope, and operational behavior before any code is written.

## Background

apikit follows an OpenAPI-first design philosophy. The hand-authored OpenAPI
3.1 specification is the foundational artifact of the project — written before
any server code, SDK, or CLI. This approach ensures a single, unambiguous
source of truth for the API contract that all implementations derive from.

Code-first generation is explicitly rejected because it inverts the dependency:
the spec would reflect implementation details rather than defining the intended
contract. A hand-authored spec forces deliberate API design decisions upfront,
separating "what the API should do" from "how it is implemented."

The master PRD (`docs/PRD.md`) defines the complete functional requirements;
this spec extracts the API contract subset into a machine-readable format that
tooling can validate, and from which SDKs and documentation can be generated.
This matches the project's core design principle: *"OpenAPI-first — the OpenAPI
3.1 specification is the single source of truth for the API contract. The
server implements it; the SDKs and documentation derive from it."*

## Source Reference

This spec is derived from the master PRD at `docs/PRD.md` in the apikit
repository. All endpoint definitions, schema shapes, status codes, headers,
and behavioral constraints are extracted from that document.

## Dependencies

This spec has no upstream dependencies. It is a standalone deliverable — a
YAML file authored by hand (not generated from code).

All other specs depend on this spec as their source of truth:

- **server_core** depends on this spec: the server implements the API contract
  defined here. X-Request-ID middleware, error envelope format, Content-Type
  constraints, and caching headers are all behavioral contracts specified here
  that server_core must fulfill.
- **database_layer**, SDKs, CLI, and all future specs derive from this spec
  directly or transitively.

## Goals

- Produce a valid OpenAPI 3.1 specification at `api/openapi.yaml`.
- Define all built-in endpoints with complete request/response schemas.
- Document all three authentication schemes (admin token, API key, PAT).
- Document all error responses using the standard error envelope.
- Document caching headers, conditional request support, and operational
  headers per endpoint category.
- Document content-type constraints and timestamp format conventions.
- Use a configurable mount point (default `/api/v1`) — paths in the spec
  are relative to the mount point via the `servers` block.

## Non-Goals

- Code generation from the spec (that is a downstream concern).
- Implementing any server logic.
- Defining endpoints for consuming projects (only built-in endpoints).
- Rate limiting headers (not in first iteration per PRD).
- Pagination parameters (not in first iteration per PRD).
- CORS headers (not in first iteration per PRD).
- Specifying the ETag derivation algorithm (an implementation detail owned by
  server_core; this spec documents ETags as opaque strings only).

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Spec format | OpenAPI 3.1 (YAML) |
| Validation | `go run github.com/pb33f/libopenapi-validator/cmd/openapi-validator@latest api/openapi.yaml` or equivalent |
| Go validation test | `api/api_test.go` — loads and parses the YAML as OpenAPI 3.1 using `libopenapi`, asserts no structural errors. Part of `go test ./...`. |
| Build check | `make check` |

> Note: Go tooling is listed because the repository uses Go for all build and
> test infrastructure, including the spec validation test. The spec itself is a
> YAML artifact; Go code is only involved in the automated validation harness.

## Detailed Requirements

### File Location and Structure

The OpenAPI specification lives at `api/openapi.yaml` in the repository root.
It uses OpenAPI 3.1.0 format with YAML syntax.

The `servers` block defines the configurable mount point:
```yaml
servers:
  - url: /api/v1
    description: Default mount point (configurable per deployment)
```

Health probe endpoints (`/healthz`, `/readyz`, `/version`) are outside the
mount point. They are represented in the OpenAPI spec using a **path-level
`servers` override** on each health probe path:

```yaml
paths:
  /healthz:
    servers:
      - url: /
    get:
      ...
  /readyz:
    servers:
      - url: /
    get:
      ...
  /version:
    servers:
      - url: /
    get:
      ...
```

This keeps the top-level `servers` block exclusively for the `/api/v1` mount
point, while the path-level override indicates absolute paths for health
probes. SDK generators and validators will correctly resolve health probe
paths as absolute (e.g., `GET /healthz`) rather than relative to the mount
point.

### Authentication Schemes

Three authentication schemes, all using Bearer tokens in the `Authorization`
header:

1. **Admin Token**: Format `<prefix>_admin_<64 hex chars>`. Global scope,
   break-glass emergency access.
2. **API Key**: Format `<prefix>_<key_id>_<secret>`. User-scoped, full access
   to own resources. Admin users get global access.
3. **Personal Access Token (PAT)**: Format `<prefix>_pat_<token_id>_<secret>`.
   Fine-grained per-permission access.

The spec documents a single `bearerAuth` security scheme with a description
covering all three credential types. Individual endpoints document which
credential types are accepted and, where applicable, the specific PAT
permission required.

### PAT Permission Model

PAT permissions follow the pattern `<resource_type>:<action>`. The permission
model is extensible by design — consuming projects can register additional
resource types and actions via the apikit configuration.

The built-in permission strings are:

| Permission | Grants access to |
|-----------|-----------------|
| `users:read` | Read own user profile; also sufficient for self-profile updates (PATCH /user) |
| `orgs:read` | Read org records and membership lists |
| `keys:read` | List own API keys |
| `keys:manage` | Refresh or revoke own API keys |
| `tokens:read` | List own PATs |
| `tokens:manage` | Create or revoke own PATs |

Because the permission model is open to extension by consuming projects, the
OpenAPI schema for PAT `permissions` array items uses a pattern constraint
(`^[a-z_]+:[a-z_]+$`) rather than a closed enum. The built-in permissions
above are documented as examples in the schema description.

### Expiry Semantics (`expires` field)

The `expires` field appears on `POST /auth/callback` and `POST /user/tokens`.
It accepts the following integer values:

| Value | Meaning |
|-------|---------|
| `0` | No expiry — the token/key is permanent. `expires_at` will be `null` in the response. |
| `30` | Expires in 30 days from creation timestamp. |
| `60` | Expires in 60 days from creation timestamp. |
| `90` | Expires in 90 days (default if `expires` is omitted). |

Expiry is calculated as exactly `24h × N` from the creation timestamp.
The `expires_at` field in all response objects is nullable: it is `null` when
`expires` is `0`, and an RFC 3339 UTC timestamp otherwise.

### PATCH Endpoint Field Immutability

`PATCH /user` and `PATCH /users/{id}` accept only `full_name` in the request
body. The `username` and `email` fields are **immutable via PATCH** — they are
set at creation time (via admin `POST /users` or OAuth upsert on
authentication) and cannot be changed through the update endpoints. OAuth
re-authentication may update `username` and `email` as a side effect of the
login flow, but this is not exposed through any PATCH endpoint. The OpenAPI
request body schemas for both PATCH endpoints must define only `full_name` and
must not include `username` or `email`.

### Health Probe Endpoints (Public, Outside Mount Point)

Health probe paths use a path-level `servers: [{url: /}]` override (see File
Location and Structure above). All three endpoints are public — no
authentication is required and 401/403 are never returned.

#### GET /healthz
- **Description**: Liveness probe — always returns 200
- **Response 200**: `{"status": "ok"}`
- **Cache-Control**: `no-cache`
- **Headers**: `X-Request-ID`

#### GET /readyz
- **Description**: Readiness probe — pings database
- **Response 200**: `{"status": "ok"}`
- **Response 503**: `{"status": "unavailable"}`
- **Cache-Control**: `no-cache`
- **Headers**: `X-Request-ID`

#### GET /version
- **Description**: Server version and build info
- **Response 200**: Object with `version`, `build_time`, `commit`, `mount_point` fields
- **Cache-Control**: `no-cache`
- **Headers**: `X-Request-ID`

### OAuth Endpoints (Public)

#### GET /auth/providers
- **Description**: List configured OAuth providers. Always public — no
  authentication is required and 401 is never returned by this endpoint.
- **Response 200**: Array of objects with `name` (string) and `authorize_url` (string)
- **Cache-Control**: `public, max-age=300`
- **Headers**: `X-Request-ID`

#### POST /auth/callback
- **Description**: Exchange authorization code for user record and API key
- **Request body**:
  - `provider` (string, required)
  - `code` (string, required) — authorization code
  - `redirect_uri` (string, required) — must match configured allowlist
  - `expires` (integer, optional) — key expiry in days: `0` (no expiry, permanent), `30`, `60`, or `90`; default `90`
- **Response 200**: Object with `user` (User object) and `api_key` object containing `key` (full key string), `key_id`, and `expires_at` (nullable RFC 3339 timestamp; `null` when `expires=0`)
- **Response 400**: Invalid request (bad redirect_uri, missing fields, provider returns no email)
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

### Authenticated User Endpoints (API key or PAT)

All paths below are relative to the mount point.

#### GET /user
- **Description**: Get authenticated user's profile
- **Auth**: API key or PAT with `users:read`
- **Response 200**: User object
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: User is blocked
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`, `ETag` (opaque string)
- **Conditional**: Supports `If-None-Match` → 304 (with `X-Request-ID` and `ETag` headers, no body)

#### PATCH /user
- **Description**: Update authenticated user's profile (only `full_name` is updatable; `username` and `email` are immutable via PATCH)
- **Auth**: API key or PAT with `users:read`. The `users:read` scope is
  intentional: there is no `users:write` permission in the built-in model.
  Self-profile updates are scoped to the authenticated user's own data and
  require only basic user access. API keys grant this inherently.
- **Request body**: `full_name` (string, required). `username` and `email` are not accepted.
- **Response 200**: Updated User object
- **Response 400**: Invalid request
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions or user is blocked
- **Response 415**: Wrong Content-Type
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### GET /user/keys
- **Description**: List authenticated user's API keys
- **Auth**: API key or PAT with `keys:read`
- **Response 200**: Array of API key metadata objects (`key_id`, `created_at`, `expires_at`, `revoked_at`)
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions or user is blocked
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### POST /user/keys/{key_id}/refresh
- **Description**: Refresh API key (new secret, same key_id)
- **Auth**: API key or PAT with `keys:manage`
- **Response 200**: API key object with full key including new plaintext secret
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions or user is blocked
- **Response 404**: Key not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### DELETE /user/keys/{key_id}
- **Description**: Revoke API key
- **Auth**: API key or PAT with `keys:manage`
- **Response 204**: No content
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions or user is blocked
- **Response 404**: Key not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### GET /user/tokens
- **Description**: List authenticated user's PATs
- **Auth**: API key or PAT with `tokens:read`
- **Response 200**: Array of PAT metadata objects (`token_id`, `name`, `permissions`, `created_at`, `expires_at`, `revoked_at`)
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions or user is blocked
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### POST /user/tokens
- **Description**: Create a new PAT
- **Auth**: API key or PAT with `tokens:manage`
- **Request body**:
  - `name` (string, required) — human-readable label
  - `permissions` (array of strings matching `^[a-z_]+:[a-z_]+$`, required) — e.g. `["users:read", "orgs:read"]`
  - `expires` (integer, optional) — days: `0` (no expiry, permanent), `30`, `60`, or `90`; default `90`
- **Response 201**: PAT object including full token with plaintext secret (returned only at creation). `expires_at` is `null` when `expires=0`.
- **Response 400**: Invalid request
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions or user is blocked
- **Response 415**: Wrong Content-Type
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### GET /user/tokens/{token_id}
- **Description**: Get a specific PAT's metadata
- **Auth**: API key or PAT with `tokens:read`
- **Response 200**: PAT metadata object
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions or user is blocked
- **Response 404**: Token not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`, `ETag` (opaque string)
- **Conditional**: Supports `If-None-Match` → 304 (with `X-Request-ID` and `ETag` headers, no body)

#### DELETE /user/tokens/{token_id}
- **Description**: Revoke a PAT
- **Auth**: API key or PAT with `tokens:manage`
- **Response 204**: No content
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions or user is blocked
- **Response 404**: Token not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### GET /user/orgs
- **Description**: List authenticated user's organization memberships
- **Auth**: API key or PAT with `orgs:read`
- **Response 200**: Array of Organization objects
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions or user is blocked
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

### Admin User Endpoints (Admin Only)

All admin endpoints require admin credentials (an API key with admin role, or
an admin token). All admin endpoints return 401 on missing/invalid/expired
credentials and 403 on valid but non-admin credentials.

#### POST /users
- **Description**: Create a user
- **Auth**: Admin (API key with admin role, or admin token)
- **Request body**:
  - `username` (string, required)
  - `email` (string, required)
  - `provider` (string, required)
  - `provider_id` (string, required)
- **Response 201**: Created User object
- **Response 400**: Invalid request
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 409**: Duplicate username or (provider, provider_id)
- **Response 415**: Wrong Content-Type
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### GET /users
- **Description**: List all users. When `include_blocked=false` (default), blocked users are excluded from the listing even though their records exist. When `include_blocked=true`, users with `status=blocked` are included.
- **Auth**: Admin
- **Query params**: `include_blocked` (boolean, optional, default `false`)
- **Response 200**: Array of User objects
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### GET /users/{id}
- **Description**: Get a user by ID
- **Auth**: Admin
- **Response 200**: User object
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: User not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`, `ETag` (opaque string)
- **Conditional**: Supports `If-None-Match` → 304 (with `X-Request-ID` and `ETag` headers, no body)

#### PATCH /users/{id}
- **Description**: Update a user (only `full_name` is updatable; `username` and `email` are immutable via PATCH)
- **Auth**: Admin
- **Request body**: `full_name` (string, required). `username` and `email` are not accepted.
- **Response 200**: Updated User object
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: User not found
- **Response 415**: Wrong Content-Type
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### POST /users/{id}/promote
- **Description**: Grant admin role to a user
- **Auth**: Admin
- **Response 200**: Updated User object with role=admin
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: User not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### POST /users/{id}/demote
- **Description**: Revoke admin role from a user
- **Auth**: Admin
- **Response 200**: Updated User object with role=user
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: User not found
- **Response 409**: Cannot demote last admin
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### POST /users/{id}/block
- **Description**: Block a user
- **Auth**: Admin
- **Response 200**: Updated User object with status=blocked
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: User not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### POST /users/{id}/unblock
- **Description**: Unblock a user
- **Auth**: Admin
- **Response 200**: Updated User object with status=active
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: User not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### GET /users/{id}/keys
- **Description**: List a user's API keys
- **Auth**: Admin
- **Response 200**: Array of API key metadata objects
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: User not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### DELETE /users/{id}/keys/{key_id}
- **Description**: Revoke a user's API key
- **Auth**: Admin
- **Response 204**: No content
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: User or key not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### GET /users/{id}/tokens
- **Description**: List a user's PATs
- **Auth**: Admin
- **Response 200**: Array of PAT metadata objects
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: User not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### DELETE /users/{id}/tokens/{token_id}
- **Description**: Revoke a user's PAT
- **Auth**: Admin
- **Response 204**: No content
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: User or token not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

### Organization Endpoints

#### POST /orgs
- **Description**: Create an organization
- **Auth**: Admin
- **Request body**:
  - `name` (string, required)
  - `slug` (string, required)
  - `url` (string, optional)
- **Response 201**: Created Organization object
- **Response 400**: Invalid request
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 409**: Duplicate name or slug
- **Response 415**: Wrong Content-Type
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### GET /orgs
- **Description**: List all organizations. When `include_blocked=false` (default), organizations with `status=blocked` are excluded from the listing. When `include_blocked=true`, all organizations regardless of status are returned.
- **Auth**: Admin
- **Query params**: `include_blocked` (boolean, optional, default `false`)
- **Response 200**: Array of Organization objects
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### GET /orgs/{id}
- **Description**: Get an organization by ID
- **Auth**: Admin, or member with `orgs:read` PAT permission
- **Response 200**: Organization object
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions
- **Response 404**: Organization not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`, `ETag` (opaque string)
- **Conditional**: Supports `If-None-Match` → 304 (with `X-Request-ID` and `ETag` headers, no body)

#### PATCH /orgs/{id}
- **Description**: Update an organization
- **Auth**: Admin
- **Request body**: `name` (string, optional), `url` (string, optional)
- **Response 200**: Updated Organization object
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: Organization not found
- **Response 409**: Duplicate name
- **Response 415**: Wrong Content-Type
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### DELETE /orgs/{id}
- **Description**: Delete an organization (cascades memberships)
- **Auth**: Admin
- **Response 204**: No content
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: Organization not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### POST /orgs/{id}/block
- **Description**: Block an organization
- **Auth**: Admin
- **Response 200**: Updated Organization object with status=blocked
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: Organization not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### POST /orgs/{id}/unblock
- **Description**: Unblock an organization
- **Auth**: Admin
- **Response 200**: Updated Organization object with status=active
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: Organization not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### GET /orgs/{id}/members
- **Description**: List organization members
- **Auth**: Admin, or member with `orgs:read` PAT permission
- **Response 200**: Array of User objects (members)
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions
- **Response 404**: Organization not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### PUT /orgs/{id}/members/{user_id}
- **Description**: Add a member to an organization. No request body is required or expected — the user ID is conveyed entirely via the path parameter.
- **Auth**: Admin
- **Response 204**: No content (member added)
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: Organization or user not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

#### DELETE /orgs/{id}/members/{user_id}
- **Description**: Remove a member from an organization
- **Auth**: Admin
- **Response 204**: No content (member removed)
- **Response 401**: Missing/invalid/expired credential
- **Response 403**: Insufficient permissions (not admin)
- **Response 404**: Organization or user not found
- **Cache-Control**: `no-store`
- **Headers**: `X-Request-ID`

### Schema Definitions

#### User Object
```json
{
  "id": "<uuid>",
  "username": "<string>",
  "email": "<string>",
  "full_name": "<string or null>",
  "status": "active | blocked",
  "role": "admin | user",
  "provider": "<string>",
  "provider_id": "<string>",
  "created_at": "<RFC 3339 timestamp, UTC, Z suffix>",
  "updated_at": "<RFC 3339 timestamp, UTC, Z suffix>"
}
```

#### API Key Metadata Object (listings)
```json
{
  "key_id": "<string>",
  "created_at": "<RFC 3339 timestamp>",
  "expires_at": "<RFC 3339 timestamp or null>",
  "revoked_at": "<RFC 3339 timestamp or null>"
}
```

#### API Key Object (creation/refresh — includes secret)
```json
{
  "key": "<prefix>_<key_id>_<secret>",
  "key_id": "<string>",
  "expires_at": "<RFC 3339 timestamp or null>"
}
```

#### PAT Metadata Object (listings)
```json
{
  "token_id": "<string>",
  "name": "<string>",
  "permissions": ["<resource_type>:<action>", ...],
  "created_at": "<RFC 3339 timestamp>",
  "expires_at": "<RFC 3339 timestamp or null>",
  "revoked_at": "<RFC 3339 timestamp or null>"
}
```

#### PAT Object (creation — includes secret)
```json
{
  "token": "<prefix>_pat_<token_id>_<secret>",
  "token_id": "<string>",
  "name": "<string>",
  "permissions": ["<resource_type>:<action>", ...],
  "expires_at": "<RFC 3339 timestamp or null>"
}
```

#### Organization Object
```json
{
  "id": "<uuid>",
  "name": "<string>",
  "slug": "<string>",
  "url": "<string or null>",
  "status": "active | blocked",
  "created_at": "<RFC 3339 timestamp>",
  "updated_at": "<RFC 3339 timestamp>"
}
```

#### OAuth Provider Object
```json
{
  "name": "<string>",
  "authorize_url": "<string>"
}
```

#### Auth Callback Response Object
```json
{
  "user": { ... User object ... },
  "api_key": {
    "key": "<full key string>",
    "key_id": "<string>",
    "expires_at": "<RFC 3339 timestamp or null>"
  }
}
```

#### Error Envelope
```json
{
  "error": {
    "code": "<integer HTTP status code>",
    "message": "<human-readable description>"
  }
}
```

The `code` field is always equal to the HTTP response status code. It is not a
sub-code or internal error code — it mirrors the HTTP status exactly (e.g.,
`404`, `401`, `409`). Sub-codes or granular error classification are not part
of the built-in error envelope.

### Error Status Codes

All endpoints may return these error responses:

| Status | Meaning |
|--------|---------|
| 400 | Bad request — malformed JSON, missing required fields, validation failure |
| 401 | Unauthorized — missing, invalid, expired, or revoked credential |
| 403 | Forbidden — valid credential but insufficient permissions, or user is blocked |
| 404 | Not found — resource does not exist |
| 409 | Conflict — unique constraint violation (duplicate username, slug, etc.) |
| 413 | Payload too large — request body exceeds the server's configured size limit (threshold is a server_core configuration concern; the spec documents the error response only) |
| 415 | Unsupported media type — Content-Type is not application/json |
| 500 | Internal server error |

### Caching Headers

Per-endpoint category caching policy:

| Category | Cache-Control | ETag Support |
|----------|---------------|-------------|
| Mutable resources (/user, /users/*, /orgs/*, keys, tokens) | `no-store` | Yes, on single-resource GET |
| Health probes (/healthz, /readyz, /version) | `no-cache` | No |
| Static discovery (/auth/providers) | `public, max-age=300` | No |

### Conditional Responses (304 Not Modified)

Endpoints that return an `ETag` header support conditional GET via
`If-None-Match`. When the client's `If-None-Match` value matches the current
ETag, the server returns `304 Not Modified` with:

- **No response body**
- **`X-Request-ID` header present** (every response carries this header for
  tracing, including 304s)
- **`ETag` header present** (so the client can confirm the current version)
- **No `Cache-Control` header** (304 inherits the caching policy of the
  corresponding 200 response)

Endpoints supporting conditional GET: `GET /user`, `GET /users/{id}`,
`GET /user/tokens/{token_id}`, `GET /orgs/{id}`.

### Operational Headers

- **X-Request-ID**: UUID, present on **every** response including 304s. Same ID appears in server logs.
- **Content-Type**: Always `application/json; charset=utf-8` on responses with a body. Requests with body must use `application/json` or receive 415. 304 and 204 responses carry no body and no Content-Type.
- **ETag**: On single-resource GET endpoints. Treated as an opaque string by the OpenAPI spec. The derivation algorithm is an implementation detail owned by server_core.
- **If-None-Match**: Supported on GET endpoints with ETag. Returns 304 on match.

### Timestamp Format

All timestamps use RFC 3339 format, normalized to UTC with `Z` suffix.
Example: `2026-07-17T14:30:00Z`. Timezone offsets are never produced.

### Query Parameters

- `include_blocked` (boolean): Available on `GET /users` and `GET /orgs`. Default `false`. When `true`, includes resources with `status=blocked` in the listing. When `false`, only resources with `status=active` are returned.

## Implementation Notes

The OpenAPI spec is a hand-authored YAML file, not generated from code. It
must be valid OpenAPI 3.1.0 and parseable by standard tooling. The spec uses
`$ref` for reusable schema components to avoid duplication.

### Go Validation Test

The file `api/api_test.go` contains a Go test that validates the spec as part
of `go test ./...`. The test:

1. Reads `api/openapi.yaml` from disk.
2. Parses and validates it as an OpenAPI 3.1 document using the `libopenapi`
   library (`github.com/pb33f/libopenapi`).
3. Asserts that no structural or schema errors are returned by the parser.
4. Does **not** validate individual operations, request/response examples, or
   ETag derivation — that is the responsibility of the spec tooling
   (`openapi-validator`) and server_core tests respectively.

This test is a lightweight structural guard that catches YAML syntax errors,
missing `$ref` targets, and OpenAPI 3.1 schema violations early in the
development cycle.

The PAT `permissions` array items schema uses a pattern constraint
(`^[a-z_]+:[a-z_]+$`) with built-in permission strings documented as examples
in the description. A closed enum is deliberately avoided to preserve
extensibility for consuming projects.

ETag values are documented in the spec as opaque strings. The derivation
mechanism (e.g., hashing of `updated_at`) is left to server_core and must not
be asserted by the OpenAPI validation test.

## Acceptance Criteria

1. `api/openapi.yaml` exists and is valid OpenAPI 3.1.0.
2. All built-in endpoints from the PRD are defined with correct methods, paths,
   request bodies, query parameters, and response schemas.
3. All schema objects (User, APIKey, PAT, Organization, Error, etc.) are defined
   under `components/schemas` with proper types and constraints.
4. The `bearerAuth` security scheme is defined and applied to authenticated
   endpoints.
5. Caching headers are documented per endpoint category.
6. X-Request-ID header is documented on all responses, including 304 responses.
7. Content-Type constraints are documented.
8. Timestamp format (RFC 3339, UTC, Z suffix) is documented.
9. The `servers` block uses the default mount point `/api/v1`.
10. Health probe endpoints use path-level `servers: [{url: /}]` overrides to
    represent absolute paths outside the mount point.
11. Error responses use the standard error envelope schema. The `code` field
    mirrors the HTTP status code exactly (no sub-codes).
12. A Go validation test exists at `api/api_test.go` that loads `api/openapi.yaml`,
    parses it using `libopenapi`, and asserts no structural errors. It does not
    validate individual operations or ETag derivation.
13. The PAT `permissions` array items schema uses pattern `^[a-z_]+:[a-z_]+$`
    (not a closed enum) with built-in permission strings documented as examples.
14. ETag headers are typed as opaque strings in the spec; no derivation algorithm
    is asserted.
15. `GET /auth/providers` is documented as unconditionally public (no 401 response).
16. `GET /orgs/{id}` and `GET /orgs/{id}/members` document `orgs:read` as the
    PAT permission required for member (non-admin) access.
17. All authenticated endpoints document 401 and 403 error responses explicitly.
18. 304 responses on conditional GET endpoints document `X-Request-ID` and `ETag`
    headers and no response body.
19. `expires=0` is documented as meaning no expiry (permanent token/key, `expires_at=null`).
    Valid values (`0`, `30`, `60`, `90`) are documented as an enum on the schema.
20. PATCH request body schemas for `/user` and `/users/{id}` include only `full_name`
    and explicitly exclude `username` and `email` (immutable fields).
21. `PUT /orgs/{id}/members/{user_id}` is documented as requiring no request body.
22. `include_blocked` query parameter behavior is documented: `false` returns only
    `status=active` resources; `true` includes `status=blocked` resources.
