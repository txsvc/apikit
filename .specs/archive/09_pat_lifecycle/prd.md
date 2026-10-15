---
spec_id: 09
spec_name: pat_lifecycle
title: Pat Lifecycle
status: draft
created_at: '2026-07-17T11:29:14.393959+00:00'
updated_at: '2026-07-17T11:34:08.274818+00:00'
owner: ''
source: interactive
schema_version: 1
---
# PAT Lifecycle

## Source

This spec is derived from the [apikit master PRD](docs/PRD.md). It covers the
**PAT Lifecycle** component -- spec 10 of 15.

## Intent

Implement the Personal Access Token (PAT) lifecycle handlers for apikit. PATs
are user-created, fine-grained credentials that grant scoped access to API
resources via `resource_type:action` permission pairs. This spec covers the
four CRUD endpoints for a user's own PATs: create, list, get, and revoke.

Unlike API keys (one per user, auto-issued on OAuth login), PATs are
explicitly created by the user, can exist in multiples, carry fine-grained
permissions, and are not refreshable. The full token (including plaintext
secret) is returned only at creation and cannot be retrieved later.

## Goals

- Implement `POST /user/tokens` -- create a new PAT with a name, permissions,
  and optional expiry. Return the full token including the plaintext secret.
  The secret is NOT retrievable after creation.
- Implement `GET /user/tokens` -- list the authenticated user's PATs.
  Return metadata only (token_id, name, permissions, created_at, expires_at,
  revoked_at). Never return the plaintext secret.
- Implement `GET /user/tokens/:token_id` -- get a specific PAT's metadata.
  Same metadata fields as the list endpoint.
- Implement `DELETE /user/tokens/:token_id` -- revoke a PAT permanently.
  The token_id remains in the database for audit purposes.
- Generate PATs in the format `<prefix>_pat_<token_id>_<secret>` using the
  build-time `apikit.TokenPrefix` variable.
- Store the PAT secret as a SHA-256 hash only; never persist the plaintext.
- Validate that requested permissions are registered in the
  `PermissionRegistry` from the auth middleware.
- Enforce no privilege escalation: when a PAT creates another PAT, the new
  PAT's permissions must be a subset of the creating PAT's own permissions.
  API keys (admin or regular) can grant any registered permission.
- Support expiry options: 0 (no expiry), 30, 60, or 90 days. Default is 90.
  Expiry is calculated as exactly `24h x N` from the creation timestamp.
  `expires_at` is nullable (NULL when expires is 0).
- Expired PATs cannot authenticate but remain visible in listings.
- Revoked PATs cannot authenticate but remain visible in listings for audit.
- No refresh capability -- unlike API keys, PATs are not refreshable. Users
  create a new PAT when the old one expires or is compromised.
- PATs created by blocked users are inert while the user is blocked; they
  resume working if the user is unblocked.
- Set `Cache-Control: no-store` on all PAT endpoints (mutable resources).
- PAT creation is unbounded -- there is no per-user cap on the number of
  active PATs. Rate limiting is deferred to a future iteration per the master
  PRD.

## Non-Goals

- **Admin PAT management.** Admin endpoints for viewing and revoking any
  user's PATs (`GET /users/:id/tokens`, `DELETE /users/:id/tokens/:token_id`)
  are covered by the user_management spec. Cross-spec coordination for those
  endpoints is handled at the campaign level.
- **PAT authentication and validation.** Token parsing, hash verification,
  permission checking, and blocked-user enforcement are handled by the auth
  middleware (spec 05).
- **Permission registry implementation.** The `PermissionRegistry` type and
  its built-in permissions are defined in the auth middleware spec. This spec
  consumes the registry for validation.
- **Database schema.** The `pats` table is created by the database layer
  (spec 02). This spec queries the table.
- **CLI commands for PATs.** CLI commands (`akc tokens list`, `akc tokens
  create`, etc.) are covered by a separate CLI spec.
- **Rate limiting / PAT creation cap.** No per-user limit on PAT count is
  enforced in this iteration. This is an explicit deferral per the master PRD.

## Dependencies

| Spec | Relationship |
|------|-------------|
| `01_server_core` | Echo handler registration via `APIGroup()`; `APIError()` for error responses; `apikit.TokenPrefix` for token format; `Cache-Control: no-store` header via middleware |
| `02_database_layer` | Queries the `pats` table; uses `db.WithTx` for transactional inserts; uses `db.WrapError` for constraint violations; uses `db.ErrNotFound` and `db.ErrConflict` sentinels; uses `db.FormatTime`/`db.ParseTime` for timestamp handling |
| `05_auth_middleware` | Uses `auth.GetAuthInfo()` and `auth.GetUserID()` to identify the authenticated user; uses `auth.RequirePermission()` for PAT-scoped access control; consumes `auth.PermissionRegistry` for permission validation on create |

---

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25+ |
| HTTP framework | Echo v4 (`github.com/labstack/echo/v4`) |
| Database | SQLite via `internal/db` |
| Token hashing | SHA-256 (stdlib `crypto/sha256`) |
| Random generation | `crypto/rand` for token_id and secret |
| Hex encoding | stdlib `encoding/hex` |
| JSON marshaling | stdlib `encoding/json` for permissions serialization |
| UUID generation | `github.com/google/uuid` (not used for token_id; token_id is a random alphanumeric string) |

## Repository Layout

```
internal/
  handlers/
    pat.go                PAT lifecycle handlers (create, list, get, revoke)
    pat_test.go           Unit and integration tests for PAT handlers
```

---

## Functional Requirements

### Route Registration

The PAT lifecycle handlers are registered on the authenticated API group
returned by `server.APIGroup()`:

```
POST   /user/tokens            → CreatePAT
GET    /user/tokens             → ListPATs
GET    /user/tokens/:token_id   → GetPAT
DELETE /user/tokens/:token_id   → RevokePAT
```

All four endpoints require authentication (API key or PAT with appropriate
permissions). The auth middleware runs before these handlers and injects the
authenticated user context.

### Handler Constructor

```go
// PATHandler holds dependencies for PAT lifecycle operations.
type PATHandler struct {
    db       *db.DB
    registry *auth.PermissionRegistry
}

// NewPATHandler creates a PAT handler with the given database and permission
// registry. Both parameters must be non-nil.
func NewPATHandler(database *db.DB, registry *auth.PermissionRegistry) *PATHandler

// RegisterRoutes registers PAT lifecycle routes on the given Echo group.
func (h *PATHandler) RegisterRoutes(g *echo.Group)
```

### PAT Format

PATs follow the format: `<prefix>_pat_<token_id>_<secret>`

- `<prefix>` is the build-time `apikit.TokenPrefix` variable (default `"ak"`)
- `_pat_` is the literal PAT type marker
- `<token_id>` is a random 8-character alphanumeric string
- `<secret>` is a random 32-character alphanumeric string

**Character alphabet:** Both `token_id` and `secret` use strictly lowercase
letters and digits: `abcdefghijklmnopqrstuvwxyz0123456789` (36-character
alphabet). No uppercase letters are used. This matches the API key `key_id`
format described in the master PRD.

Both `token_id` and `secret` are generated using `crypto/rand` for
cryptographic security.

### POST /user/tokens -- Create PAT

**Permission check:** Requires `tokens:manage` permission (enforced via
`auth.RequirePermission(c, "tokens", "manage")`). API keys (both admin and
regular) bypass this check.

**Request body:**

```json
{
  "name": "ci-deploy",
  "permissions": ["users:read", "orgs:read"],
  "expires": 90
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | yes | -- | Human-readable label for identification. Max 255 characters. |
| `permissions` | array of strings | yes | -- | List of `resource_type:action` pairs. Preserved in insertion order. |
| `expires` | integer | no | 90 | Expiry in days: 0 (no expiry), 30, 60, or 90 |

**Validation rules:**

1. `name` must be a non-empty string. If missing or empty, return HTTP 400
   with message `"name is required"`.
2. `name` must be 255 characters or fewer. If exceeded, return HTTP 400 with
   message `"name must be 255 characters or fewer"`.
3. `permissions` must be a non-empty array. If missing or empty, return
   HTTP 400 with message `"permissions are required"`.
4. Each permission string must be in `resource_type:action` format (exactly
   one colon). If malformed, return HTTP 400 with message
   `"invalid permission format: <permission>"`.
5. Each permission must be registered in the `PermissionRegistry`. If
   unregistered, return HTTP 400 with message
   `"unknown permission: <permission>"`.
6. `expires` must be one of 0, 30, 60, or 90. If any other value, return
   HTTP 400 with message `"expires must be 0, 30, 60, or 90"`.
7. **No privilege escalation:** The privilege escalation rule applies only
   when the authenticated credential is itself a PAT. In that case, each
   requested permission must be present in the creating PAT's own permissions
   list. If the authenticated credential is an API key (admin or regular),
   any registered permission may be granted -- admin enforcement for
   admin-only endpoints is handled separately by the auth middleware's
   `RequireAdmin` check, not by the permission model.

   The built-in registered permissions (`users:read`, `orgs:read`,
   `keys:read`, `keys:manage`, `tokens:read`, `tokens:manage`) carry no
   inherent admin/user distinction in the registry. A regular user's PAT
   with `tokens:manage` can only manage that user's own tokens; the
   resource-scoping is enforced by the handler logic (user_id filter), not
   by the permission string itself.

   If a requested permission is not grantable by the creating PAT, return
   HTTP 403 with message `"cannot grant permission: <permission>"`.

**Name uniqueness:** PAT names are **non-unique** per user. A user may create
multiple PATs with the same name. Names are labels for human identification
only, not identifiers. No uniqueness constraint exists at the database or
handler level. This matches GitHub's PAT behavior.

**Processing:**

1. Parse and validate the request body (name, name length, permissions,
   expires).
2. Check permission to manage tokens.
3. Validate all requested permissions against the registry and privilege
   escalation rules.
4. Generate a cryptographically random `token_id` (8 chars, alphabet:
   `[a-z0-9]`).
5. Generate a cryptographically random `secret` (32 chars, alphabet:
   `[a-z0-9]`).
6. Compute the SHA-256 hash of `secret` (hex-encoded).
7. Construct the full token: `<prefix>_pat_<token_id>_<secret>`.
8. Calculate `expires_at`:
   - If `expires` is 0: `expires_at` is NULL.
   - Otherwise: `expires_at` = `created_at` + (`expires` * 24 hours).
9. Insert into the `pats` table within a `db.WithTx` transaction:
   - `token_id`, `user_id` (from auth context), `name`, `secret_hash`,
     `permissions` (JSON-serialized array, insertion order preserved),
     `expires_days`, `expires_at`, `created_at`.
   - If the transaction or INSERT fails for any reason (e.g., database
     error), no partial state is persisted (the transaction is rolled back
     automatically by `db.WithTx`) and the handler returns HTTP 500 with
     message `"internal server error"`.
10. Return HTTP 201 with the full PAT response including the plaintext token.

**Response (HTTP 201):**

```json
{
  "token_id": "a1b2c3d4",
  "name": "ci-deploy",
  "token": "ak_pat_a1b2c3d4_deadbeefdeadbeefdeadbeefdeadbeef",
  "permissions": ["users:read", "orgs:read"],
  "expires_at": "2026-10-15T14:30:00Z",
  "created_at": "2026-07-17T14:30:00Z"
}
```

The `token` field contains the full plaintext token including the secret.
This is the only time the plaintext secret is available. The response does
NOT include `revoked_at` (a newly created token is never revoked). The
`expires_days` field is **not** included in any response (stored in the DB
for internal/audit use only; clients can derive the original window from
`created_at` and `expires_at` if needed). The `permissions` array is
returned in the same order the client submitted it (insertion order).

### GET /user/tokens -- List PATs

**Permission check:** Requires `tokens:read` permission (enforced via
`auth.RequirePermission(c, "tokens", "read")`). API keys bypass this check.

**Processing:**

1. Get the authenticated user's ID from the request context.
2. Query the `pats` table for all rows matching `user_id`, ordered by
   `created_at DESC`.
3. Return the list as a JSON array. Includes all PATs regardless of
   status (active, expired, revoked).

**Response (HTTP 200):**

```json
[
  {
    "token_id": "a1b2c3d4",
    "name": "ci-deploy",
    "permissions": ["users:read", "orgs:read"],
    "created_at": "2026-07-17T14:30:00Z",
    "expires_at": "2026-10-15T14:30:00Z",
    "revoked_at": null
  },
  {
    "token_id": "e5f6g7h8",
    "name": "old-token",
    "permissions": ["users:read"],
    "created_at": "2026-01-01T00:00:00Z",
    "expires_at": "2026-04-01T00:00:00Z",
    "revoked_at": "2026-02-15T10:00:00Z"
  }
]
```

**No secrets are included.** The response contains metadata only. An empty
array is returned if the user has no PATs. The `permissions` array for each
PAT is returned in insertion order (the order the client specified at
creation time).

### GET /user/tokens/:token_id -- Get PAT

**Permission check:** Requires `tokens:read` permission.

**Processing:**

1. Extract `token_id` from the URL path parameter.
2. Query the `pats` table for the row matching both `token_id` AND `user_id`
   (from auth context). This ensures a user can only view their own PATs.
3. If not found, return HTTP 404 with message `"token not found"`.
4. Return the PAT metadata.

**Response (HTTP 200):**

```json
{
  "token_id": "a1b2c3d4",
  "name": "ci-deploy",
  "permissions": ["users:read", "orgs:read"],
  "created_at": "2026-07-17T14:30:00Z",
  "expires_at": "2026-10-15T14:30:00Z",
  "revoked_at": null
}
```

### DELETE /user/tokens/:token_id -- Revoke PAT

**Permission check:** Requires `tokens:manage` permission.

**Processing:**

1. Extract `token_id` from the URL path parameter.
2. Issue a conditional `UPDATE` on the `pats` table:
   `SET revoked_at = <now> WHERE token_id = ? AND user_id = ? AND revoked_at IS NULL`.
3. If the UPDATE affects zero rows, query the `pats` table to determine
   why:
   - If no row exists for `token_id` AND `user_id`, return HTTP 404 with
     message `"token not found"`.
   - If a row exists but `revoked_at` is already set, return HTTP 400 with
     message `"token already revoked"`.
4. If the UPDATE affects one row, return HTTP 200 with the updated PAT
   metadata (including the newly set `revoked_at`).

**Concurrency:** Using a conditional `UPDATE ... WHERE revoked_at IS NULL`
ensures that concurrent revocation requests are handled correctly. The first
writer wins and receives HTTP 200; any subsequent concurrent request finds
zero rows updated (because `revoked_at` is now set) and receives HTTP 400
`"token already revoked"`. This is safe and correct with SQLite's
single-connection pool.

**Response (HTTP 200):**

```json
{
  "token_id": "a1b2c3d4",
  "name": "ci-deploy",
  "permissions": ["users:read", "orgs:read"],
  "created_at": "2026-07-17T14:30:00Z",
  "expires_at": "2026-10-15T14:30:00Z",
  "revoked_at": "2026-07-18T09:00:00Z"
}
```

The token_id remains in the database. The PAT can no longer authenticate
but remains visible in listings for audit purposes.

---

## Interfaces

### Request/Response Types

```go
// CreatePATRequest is the JSON body for POST /user/tokens.
type CreatePATRequest struct {
    Name        string   `json:"name"`
    Permissions []string `json:"permissions"`
    Expires     *int     `json:"expires,omitempty"`
}

// CreatePATResponse is returned on successful PAT creation (HTTP 201).
// It includes the plaintext token that is not retrievable after this response.
// expires_days is intentionally omitted from all response types.
type CreatePATResponse struct {
    TokenID     string   `json:"token_id"`
    Name        string   `json:"name"`
    Token       string   `json:"token"`
    Permissions []string `json:"permissions"`
    ExpiresAt   *string  `json:"expires_at"`
    CreatedAt   string   `json:"created_at"`
}

// PATResponse is returned for list, get, and revoke operations.
// It never includes the plaintext token, secret hash, or expires_days.
type PATResponse struct {
    TokenID     string   `json:"token_id"`
    Name        string   `json:"name"`
    Permissions []string `json:"permissions"`
    CreatedAt   string   `json:"created_at"`
    ExpiresAt   *string  `json:"expires_at"`
    RevokedAt   *string  `json:"revoked_at"`
}
```

### Token Generation (internal)

```go
// tokenAlphabet is the character set used for both token_id and secret
// generation: strictly lowercase letters and digits [a-z0-9].
const tokenAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// generateTokenID generates a cryptographically random 8-character
// alphanumeric string (alphabet: [a-z0-9]) for use as a PAT token_id.
func generateTokenID() (string, error)

// generateSecret generates a cryptographically random 32-character
// alphanumeric string (alphabet: [a-z0-9]) for use as a PAT secret.
func generateSecret() (string, error)

// hashSecret computes the SHA-256 hash of the input string and returns
// it as a lowercase hex-encoded string.
func hashSecret(secret string) string
```

---

## Testing Strategy

### Unit Tests

- `TestCreatePAT_Success` -- valid request creates a PAT and returns HTTP 201
  with the full plaintext token.
- `TestCreatePAT_MissingName` -- empty or missing name returns HTTP 400.
- `TestCreatePAT_NameTooLong` -- name exceeding 255 characters returns
  HTTP 400 with message `"name must be 255 characters or fewer"`.
- `TestCreatePAT_MissingPermissions` -- missing or empty permissions array
  returns HTTP 400.
- `TestCreatePAT_InvalidPermissionFormat` -- malformed permission string
  (no colon) returns HTTP 400.
- `TestCreatePAT_UnknownPermission` -- unregistered permission returns
  HTTP 400.
- `TestCreatePAT_InvalidExpires` -- expires value not in {0, 30, 60, 90}
  returns HTTP 400.
- `TestCreatePAT_DefaultExpires` -- omitted expires defaults to 90 days.
- `TestCreatePAT_NoExpiry` -- expires=0 creates a token with null expires_at.
- `TestCreatePAT_PrivilegeEscalation_PAT` -- a PAT trying to create a new
  PAT with permissions it does not have returns HTTP 403.
- `TestCreatePAT_APIKey_AnyRegisteredPermission` -- an API key (admin or
  regular) can create a PAT with any registered permission without triggering
  the privilege escalation check.
- `TestCreatePAT_SecretHashed` -- verify the stored secret_hash is the
  SHA-256 of the secret, not the plaintext.
- `TestCreatePAT_TokenFormat` -- verify the returned token matches the
  pattern `<prefix>_pat_<8 chars>_<32 chars>` with alphabet `[a-z0-9]`.
- `TestCreatePAT_DuplicateName` -- creating two PATs with the same name
  succeeds; both are persisted (names are non-unique).
- `TestCreatePAT_PermissionsInsertionOrder` -- permissions in the response
  match the order submitted in the request.
- `TestCreatePAT_TransactionRollback` -- simulated DB failure during INSERT
  results in HTTP 500 and no partial state in the database.
- `TestListPATs_Success` -- returns all PATs for the authenticated user.
- `TestListPATs_Empty` -- returns an empty array when user has no PATs.
- `TestListPATs_IncludesExpiredAndRevoked` -- expired and revoked PATs
  appear in the listing.
- `TestListPATs_NoSecrets` -- response does not contain any secret,
  secret_hash, or expires_days fields.
- `TestListPATs_OtherUserTokensExcluded` -- PATs belonging to other users
  are not returned.
- `TestGetPAT_Success` -- returns the correct PAT metadata.
- `TestGetPAT_NotFound` -- non-existent token_id returns HTTP 404.
- `TestGetPAT_OtherUserToken` -- token_id belonging to another user returns
  HTTP 404 (not 403, to avoid information leakage).
- `TestRevokePAT_Success` -- conditional UPDATE sets revoked_at and returns
  HTTP 200.
- `TestRevokePAT_NotFound` -- non-existent token_id returns HTTP 404.
- `TestRevokePAT_AlreadyRevoked` -- revoking an already-revoked PAT returns
  HTTP 400.
- `TestRevokePAT_OtherUserToken` -- token_id belonging to another user
  returns HTTP 404.
- `TestRevokePAT_Concurrent` -- two concurrent revocation requests: the
  first succeeds (HTTP 200), the second receives HTTP 400 `"token already
  revoked"`.
- `TestGenerateTokenID_Length` -- generated token_id is exactly 8 characters.
- `TestGenerateTokenID_Alphanumeric` -- generated token_id contains only
  characters in `[a-z0-9]`.
- `TestGenerateSecret_Length` -- generated secret is exactly 32 characters.
- `TestGenerateSecret_Alphanumeric` -- generated secret contains only
  characters in `[a-z0-9]`.
- `TestHashSecret_Deterministic` -- same input produces same hash.
- `TestHashSecret_KnownVector` -- known input produces expected SHA-256 hex.

### Integration Tests

- `TestCreateAndListPATs` -- create multiple PATs, then list and verify all
  are present with correct metadata and in `created_at DESC` order.
- `TestCreateAndGetPAT` -- create a PAT, then get it by token_id and verify
  metadata matches.
- `TestCreateAndRevokePAT` -- create a PAT, revoke it, then verify
  revoked_at is set in subsequent get.
- `TestPATPermissionCheck_Read` -- endpoint requires `tokens:read`
  permission when authenticated via PAT.
- `TestPATPermissionCheck_Manage` -- create and revoke endpoints require
  `tokens:manage` permission when authenticated via PAT.
- `TestListPATs_OrderByCreatedAtDesc` -- verify PATs are returned in
  reverse chronological order.
- `TestCacheControl` -- verify all PAT endpoints return
  `Cache-Control: no-store`.
- `TestCreatePAT_NoDuplicateNameError` -- two PATs with identical names for
  the same user are both created successfully (no 409 or constraint error).

---

## Error Handling

| Condition | Status | Message |
|-----------|--------|---------|
| Missing or empty `name` | 400 | `"name is required"` |
| `name` exceeds 255 characters | 400 | `"name must be 255 characters or fewer"` |
| Missing or empty `permissions` array | 400 | `"permissions are required"` |
| Malformed permission string | 400 | `"invalid permission format: <permission>"` |
| Unregistered permission | 400 | `"unknown permission: <permission>"` |
| Invalid `expires` value | 400 | `"expires must be 0, 30, 60, or 90"` |
| Token already revoked | 400 | `"token already revoked"` |
| Unauthenticated request | 401 | (handled by auth middleware) |
| Insufficient permissions (PAT lacks required permission) | 403 | (handled by auth middleware) |
| PAT privilege escalation | 403 | `"cannot grant permission: <permission>"` |
| Token not found (or belongs to another user) | 404 | `"token not found"` |
| Malformed JSON body | 400 | `"invalid request body"` |
| Database error / transaction failure | 500 | `"internal server error"` |

All errors use the standard `APIError()` helper from Server Core to produce
the consistent JSON error envelope:

```json
{
  "error": {
    "code": 400,
    "message": "name is required"
  }
}
```

---

## Design Decisions

- **No refresh capability.** Unlike API keys, PATs are not refreshable. Users
  create a new PAT when the old one expires or is compromised. This matches
  GitHub's PAT model and avoids the complexity of secret rotation on
  fine-grained tokens.

- **Secret returned only at creation.** The plaintext secret is included in
  the create response only. All subsequent reads (list, get) return metadata
  without the secret. This is a security best practice -- the secret is
  hashed with SHA-256 before storage and cannot be recovered.

- **Revoked tokens remain in database.** Revocation sets `revoked_at` but
  does not delete the row. This preserves audit trails. Revoked tokens appear
  in listings so users can see their full token history.

- **Expired tokens remain visible.** Like revoked tokens, expired PATs are
  included in list results. Their status is apparent from the `expires_at`
  field. The auth middleware handles rejection of expired tokens.

- **Own-user scope enforcement via dual-column query.** Get and revoke
  queries filter by both `token_id` AND `user_id`. This ensures a user
  cannot access another user's PATs even by guessing a token_id. The
  response is HTTP 404 (not 403) to avoid leaking information about whether
  the token_id exists.

- **Privilege escalation applies only to PAT-created-by-PAT.** When a PAT
  creates another PAT, the new PAT's permissions must be a subset of the
  creating PAT's permissions. When an API key (admin or regular) creates a
  PAT, any registered permission may be granted. Admin-only endpoint
  enforcement is handled by the auth middleware's `RequireAdmin` check, which
  is independent of the permission model. A regular user's PAT with
  `tokens:manage` can only manage that user's own tokens; the resource scope
  is enforced by handler-level `user_id` filtering, not by the permission
  string.

- **Flat permission registry with no admin/user distinction.** The built-in
  registered permissions (`users:read`, `orgs:read`, `keys:read`,
  `keys:manage`, `tokens:read`, `tokens:manage`) are all valid for any user
  to grant in a PAT. There are no admin-only permission strings in the
  registry. Admin enforcement for admin-only REST endpoints is a separate
  concern handled by `RequireAdmin` in the auth middleware.

- **Unbounded PAT creation.** There is no per-user cap on the number of
  PATs. Rate limiting is explicitly deferred per the master PRD. This design
  keeps the lifecycle handler simple and avoids arbitrary limits that may not
  suit all deployment scenarios.

- **Non-unique PAT names.** PAT names are labels for human identification,
  not unique identifiers. A user may create multiple PATs with the same name
  (e.g., multiple `"ci-deploy"` tokens with different permission sets). This
  matches GitHub's PAT behavior. The `token_id` is the authoritative
  identifier for lookup and path parameters.

- **Random alphanumeric token_id with [a-z0-9] alphabet.** The `token_id`
  is an 8-character random string drawn from the 36-character alphabet
  `abcdefghijklmnopqrstuvwxyz0123456789` (lowercase only). This matches the
  API key `key_id` format. UUIDs are not used for `token_id` because the
  token format specification uses short identifiers. The same alphabet is
  used for the 32-character `secret`.

- **Conditional UPDATE for revocation.** Revocation uses
  `UPDATE ... WHERE revoked_at IS NULL` rather than a SELECT-then-UPDATE
  pattern. This makes concurrent revocation requests safe: the first writer
  wins, and subsequent requests see zero rows updated and receive HTTP 400.
  This is idiomatic and correct with SQLite's single-connection pool.

- **expires_days omitted from responses.** The `expires_days` column is
  stored in the `pats` table for internal and audit use. It is intentionally
  excluded from all API responses (`CreatePATResponse`, `PATResponse`).
  Clients can derive the original expiry window from `created_at` and
  `expires_at` if needed.

- **Permissions stored as JSON array in insertion order.** The `permissions`
  column stores a JSON array of strings in the order the client submitted
  them. The same order is preserved in all responses. This avoids
  client-side sorting surprises and keeps test assertions deterministic.

- **Transactional insert with automatic rollback.** PAT creation uses
  `db.WithTx` to wrap the INSERT. If the transaction or INSERT fails for any
  reason, `db.WithTx` rolls back automatically and no partial state is
  persisted. The handler returns HTTP 500. Token generation happens before
  the transaction opens; a failed transaction does not leak any token
  material (the generated token is simply discarded).

- **Name length capped at 255 characters.** The `name` field is validated
  server-side to be at most 255 characters. This aligns with the expected
  DB column width (TEXT with application-level enforcement) and prevents
  abuse from arbitrarily long inputs.

- **Cache-Control: no-store.** PAT endpoints are mutable resources. The
  `no-store` directive prevents clients and intermediaries from caching
  responses that may become stale after creation or revocation.

- **Ordered listing.** PATs are returned ordered by `created_at DESC` so
  the most recent token appears first. This is the most useful default for
  users managing their tokens.

- **RegisterRoutes uses *echo.Group.** The `RegisterRoutes` method takes
  `*echo.Group` (pointer), consistent with Echo v4's API where route
  registration methods (`g.POST()`, `g.GET()`, etc.) are defined on the
  pointer receiver.

---

## Glossary

| Term | Definition |
|------|------------|
| **PAT** | Personal Access Token -- a user-created, fine-grained credential in the format `<prefix>_pat_<token_id>_<secret>`. Multiple per user, each with specific permissions. |
| **token_id** | The random 8-character alphanumeric identifier embedded in the PAT format. Characters drawn from `[a-z0-9]` (36-character alphabet). Used for database lookup and API path parameters. Stored in plaintext. |
| **secret** | The random 32-character alphanumeric string embedded in the PAT format. Characters drawn from `[a-z0-9]` (36-character alphabet). Stored only as a SHA-256 hash; never persisted in plaintext. |
| **secret_hash** | The SHA-256 hash (hex-encoded) of the PAT secret. Stored in the `pats` table. Used by the auth middleware for credential validation. |
| **tokenAlphabet** | The constant `"abcdefghijklmnopqrstuvwxyz0123456789"` -- the 36-character set from which both `token_id` and `secret` characters are drawn. |
| **PermissionRegistry** | The thread-safe registry of valid `resource_type:action` pairs defined in the auth middleware. Used to validate permissions during PAT creation. The registry is flat with no admin/user distinction; all registered permissions are valid for any user to grant in a PAT. |
| **resource_type:action** | The permission format used by PATs. Each permission is a colon-separated pair of a resource type and an action (e.g. `users:read`, `keys:manage`). |
| **privilege escalation** | An attempt by a PAT to create a new PAT with permissions beyond those the creating PAT itself holds. Prevented by validation during creation. Does not apply when the creating credential is an API key. |
| **TokenPrefix** | The build-time configurable variable `apikit.TokenPrefix` (default `"ak"`) used in the PAT token format. |
| **expires_days** | The original expiry duration stored in the `pats` table (0, 30, 60, or 90). Used for internal/audit purposes only; not included in any API response. |
| **revoked_at** | The timestamp when a PAT was permanently revoked. NULL while the PAT is active. Once set (via conditional UPDATE), the PAT can no longer authenticate. |
| **insertion order** | The order in which the client submitted permissions in the create request. This order is preserved in DB storage and all API responses. |
