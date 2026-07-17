---
spec_id: '07'
spec_name: user_management
title: User Management
status: draft
created_at: '2026-07-17T11:28:35.942655+00:00'
updated_at: '2026-07-17T11:29:54.654079+00:00'
owner: ''
source: interactive
schema_version: 1
---
# User Management

## Source

This spec is derived from the [apikit master PRD](docs/PRD.md). It covers the
**User Management** component -- spec 07 of 15.

## Intent

Implement the HTTP handlers for user administration and self-service profile
endpoints. Admin users manage the user population through CRUD operations,
role changes (promote/demote), and lifecycle actions (block/unblock). Any
authenticated user can view and update their own profile and list their
organization memberships.

Admin endpoints also provide read and revoke access to a user's API keys and
PATs, giving administrators the ability to audit and manage credentials across
the user base.

## Goals

- Provide admin-only CRUD endpoints for user management under `/users`.
- Provide self-service profile endpoints under `/user` for any authenticated user.
- Implement role management (promote/demote) with safeguard against demoting the last admin.
- Implement lifecycle management (block/unblock) without user deletion.
- Provide admin access to per-user API key and PAT listings and revocation.
- Follow GitHub REST API conventions: action endpoints return the updated resource with HTTP 200.
- Return proper HTTP status codes: 201 for creation, 200 for updates and actions, 204 for successful revocation (DELETE), 409 for conflicts.
- Enforce admin-only access on all `/users` endpoints via auth middleware helpers.
- Enforce PAT permission checks (`users:read` for profile access and self-service profile updates).

## Non-Goals

- **User deletion.** Users can be blocked but not deleted. Blocked users remain
  in the database for audit trail integrity.
- **Password management.** Authentication is handled via OAuth; there are no passwords.
- **User search or filtering beyond `include_blocked`.** No full-text search or
  field-based filtering in this iteration.
- **Pagination.** List endpoints return all results without pagination.
- **Bulk operations.** All operations target a single user at a time.
- **API key creation or PAT creation.** These are handled by separate credential
  lifecycle specs. This spec only covers listing and revoking another user's credentials.
- **Provider validation.** The `provider` field stored on a user is a free-form
  string. Validation against configured OAuth providers only occurs during the
  OAuth login flow (handled by `oauth_provider_registry`), not during admin
  user creation via `POST /users`.

## Background

apikit requires a mechanism for administrators to manage the user population at
runtime, independent of the OAuth login flow. While `admin_bootstrap` handles
the one-time seeding of the first admin user during OAuth callback on first boot,
day-to-day user lifecycle management -- creating additional users, adjusting
roles, and blocking compromised accounts -- must be handled via API endpoints
accessible to admin-role users. This spec delivers those admin-operated endpoints
alongside self-service profile endpoints that allow any authenticated user to
view and update their own information.

## Dependencies

| Spec | Relationship |
|------|-------------|
| `01_server_core` | Registers handlers on the Echo `APIGroup()`; uses `apikit.APIError()` for error responses; uses `apikit.SetETag()` / `apikit.CheckETag()` for conditional requests; uses `apikit.CacheNoStore` for caching headers; reads `X-Request-ID` from context |
| `02_database_layer` | Queries `users`, `api_keys`, `pats`, `orgs`, and `org_members` tables via `*sql.DB`; all six application tables are created by `database_layer` on boot; uses `db.FormatTime()` for timestamp formatting; uses `db.ErrNotFound` sentinel for 404 responses |
| `05_auth_middleware` | Uses `auth.RequireAdmin()` for admin-only endpoints; uses `auth.GetAuthInfo()` / `auth.GetUserID()` for authenticated user context; uses `auth.RequirePermission()` for PAT permission checks |

> **Note on `admin_bootstrap` (spec 04):** `admin_bootstrap` seeds the first admin
> user by intercepting the OAuth callback when zero users exist in the database and
> writing a record directly to the `users` table. `POST /users` is a separate,
> runtime API endpoint used by existing admin users to create additional user records.
> The two mechanisms are independent and complementary: `admin_bootstrap` is a
> one-time server-startup concern; `user_management` is runtime API behavior. Both
> write to the same `users` table but serve distinct purposes and are protected by
> different auth mechanisms (bootstrap token vs. admin session/API key).

> **Note on `oauth_provider_registry` (spec 06):** The `provider` field in the
> user object and in the `POST /users` request body is stored as a free-form
> string. No validation against the provider registry is performed during admin
> user creation. An admin may specify any provider string (e.g., `"github"`,
> `"google"`, or a future provider not yet configured). Provider validation is the
> responsibility of the OAuth login flow in `oauth_provider_registry`, not this spec.

---

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25+ |
| HTTP framework | Echo v4 (`github.com/labstack/echo/v4`) |
| Database | SQLite via `internal/db` |
| UUID generation | `github.com/google/uuid` |
| Testing | Go stdlib `testing` package |

## Repository Layout

```
internal/
  handlers/                 Built-in HTTP handler implementations
    users.go                User management handler implementations
    users_test.go           Tests for user management handlers
```

---

## Functional Requirements

### User Object

The user object returned by all endpoints has this shape:

```json
{
  "id": "<uuid>",
  "username": "<string>",
  "email": "<string>",
  "full_name": "<string>",
  "role": "admin|user",
  "status": "active|blocked",
  "provider": "<string>",
  "provider_id": "<string>",
  "created_at": "<RFC 3339 UTC timestamp>",
  "updated_at": "<RFC 3339 UTC timestamp>"
}
```

All fields are always present. `full_name` may be an empty string. `provider`
is a free-form string stored as provided; it is not validated against the
OAuth provider registry.

### POST /users -- Create User (Admin Only)

Creates a new user record. Requires admin-level access. This endpoint is
independent of the `admin_bootstrap` mechanism: `admin_bootstrap` seeds the
first admin user at server startup via the OAuth callback flow, while this
endpoint is used by runtime admin users to create additional user records
directly.

**Request body:**
```json
{
  "username": "<string, required>",
  "email": "<string, required>",
  "provider": "<string, required>",
  "provider_id": "<string, required>"
}
```

**Behavior:**
1. Call `auth.RequireAdmin(c)` -- return 403 if not admin.
2. Bind and validate the request body. All four fields are required and must be
   non-empty strings. Return 400 if any are missing or empty.
3. Generate a new UUID for the user ID.
4. Set `role` to `"user"`, `status` to `"active"`, `full_name` to `""`.
5. Set `created_at` and `updated_at` to the current UTC timestamp.
6. Insert the user into the `users` table.
7. If insertion fails due to a unique constraint violation on `username` or on
   `(provider, provider_id)`, return HTTP 409 with message
   `"username already exists"` or `"provider identity already exists"`.
8. Return HTTP 201 with the created user object.

### GET /users -- List Users (Admin Only)

Lists all users. Blocked users are excluded by default.

**Query parameters:**
- `include_blocked` (optional, boolean) -- when `"true"`, include blocked users
  in the response. Default: `false`.

**Behavior:**
1. Call `auth.RequireAdmin(c)` -- return 403 if not admin.
2. Read the `include_blocked` query parameter.
3. Query the `users` table. If `include_blocked` is not `"true"`, filter
   `WHERE status = 'active'`.
4. Return HTTP 200 with a JSON array of user objects. Return an empty array
   `[]` if no users match.

### GET /users/:id -- Get User (Admin Only)

Returns a single user by ID.

**Behavior:**
1. Call `auth.RequireAdmin(c)` -- return 403 if not admin.
2. Read the `id` path parameter.
3. Query the `users` table for the matching user.
4. If not found, return HTTP 404 with message `"user not found"`.
5. Set the ETag header via `apikit.SetETag(c, user.UpdatedAt)`.
6. Check `If-None-Match` via `apikit.CheckETag(c, user.UpdatedAt)` -- return
   304 if the client cache is current.
7. Return HTTP 200 with the user object.

### PATCH /users/:id -- Update User (Admin Only)

Updates a user's `full_name`. Only `full_name` is mutable via this endpoint.

**Request body:**
```json
{
  "full_name": "<string, required>"
}
```

**Behavior:**
1. Call `auth.RequireAdmin(c)` -- return 403 if not admin.
2. Read the `id` path parameter.
3. Bind and validate the request body. `full_name` must be present (but may
   be an empty string to clear it). Return 400 if the field is missing.
4. Query the `users` table for the matching user. Return 404 if not found.
5. Update `full_name` and `updated_at`.
6. Return HTTP 200 with the updated user object.

### POST /users/:id/promote -- Grant Admin Role (Admin Only)

Promotes a user to the admin role.

**Behavior:**
1. Call `auth.RequireAdmin(c)` -- return 403 if not admin.
2. Read the `id` path parameter.
3. Query the `users` table for the matching user. Return 404 if not found.
4. If the user already has `role = "admin"`, return HTTP 200 with the user
   object unchanged (idempotent).
5. Update `role` to `"admin"` and `updated_at`.
6. Return HTTP 200 with the updated user object.

### POST /users/:id/demote -- Revoke Admin Role (Admin Only)

Demotes a user from the admin role. Cannot demote the last admin.

**Behavior:**
1. Call `auth.RequireAdmin(c)` -- return 403 if not admin.
2. Read the `id` path parameter.
3. Query the `users` table for the matching user. Return 404 if not found.
4. If the user already has `role = "user"`, return HTTP 200 with the user
   object unchanged (idempotent).
5. Count the number of users with `role = "admin"` and `status = "active"`.
6. If the count is 1 (this is the last admin), return HTTP 409 with message
   `"cannot demote the last admin"`.
7. Update `role` to `"user"` and `updated_at`.
8. Return HTTP 200 with the updated user object.

### POST /users/:id/block -- Block User (Admin Only)

Blocks a user. Blocked users cannot authenticate.

**Behavior:**
1. Call `auth.RequireAdmin(c)` -- return 403 if not admin.
2. Read the `id` path parameter.
3. Query the `users` table for the matching user. Return 404 if not found.
4. If the user already has `status = "blocked"`, return HTTP 200 with the user
   object unchanged (idempotent).
5. Update `status` to `"blocked"` and `updated_at`.
6. Return HTTP 200 with the updated user object.

### POST /users/:id/unblock -- Unblock User (Admin Only)

Unblocks a previously blocked user.

**Behavior:**
1. Call `auth.RequireAdmin(c)` -- return 403 if not admin.
2. Read the `id` path parameter.
3. Query the `users` table for the matching user. Return 404 if not found.
4. If the user already has `status = "active"`, return HTTP 200 with the user
   object unchanged (idempotent).
5. Update `status` to `"active"` and `updated_at`.
6. Return HTTP 200 with the updated user object.

### GET /users/:id/keys -- List User's API Keys (Admin Only)

Lists a user's API keys (metadata only, no secrets).

**Behavior:**
1. Call `auth.RequireAdmin(c)` -- return 403 if not admin.
2. Read the `id` path parameter.
3. Verify the user exists in the `users` table. Return 404 if not found.
4. Query the `api_keys` table for all keys belonging to the user.
5. Return HTTP 200 with a JSON array of key metadata objects:
   ```json
   [
     {
       "key_id": "<string>",
       "user_id": "<uuid>",
       "created_at": "<RFC 3339>",
       "expires_at": "<RFC 3339 or null>",
       "revoked_at": "<RFC 3339 or null>"
     }
   ]
   ```

### DELETE /users/:id/keys/:key_id -- Revoke User's API Key (Admin Only)

Revokes a specific API key belonging to a user. Returns 204 No Content on both
the successful revocation path and the already-revoked (idempotent) path. No
response body is returned in either case.

**Behavior:**
1. Call `auth.RequireAdmin(c)` -- return 403 if not admin.
2. Read the `id` and `key_id` path parameters.
3. Query the `api_keys` table for the key matching `key_id` and `user_id`.
4. If not found, return HTTP 404 with message `"api key not found"`.
5. If already revoked (`revoked_at` is not NULL), return HTTP 204 (idempotent, no body).
6. Set `revoked_at` to the current UTC timestamp.
7. Return HTTP 204 (No Content).

### GET /users/:id/tokens -- List User's PATs (Admin Only)

Lists a user's personal access tokens (metadata only).

**Behavior:**
1. Call `auth.RequireAdmin(c)` -- return 403 if not admin.
2. Read the `id` path parameter.
3. Verify the user exists in the `users` table. Return 404 if not found.
4. Query the `pats` table for all tokens belonging to the user.
5. Return HTTP 200 with a JSON array of token metadata objects:
   ```json
   [
     {
       "token_id": "<string>",
       "name": "<string>",
       "permissions": ["<resource_type:action>"],
       "user_id": "<uuid>",
       "created_at": "<RFC 3339>",
       "expires_at": "<RFC 3339 or null>",
       "revoked_at": "<RFC 3339 or null>"
     }
   ]
   ```

### DELETE /users/:id/tokens/:token_id -- Revoke User's PAT (Admin Only)

Revokes a specific PAT belonging to a user. Returns 204 No Content on both
the successful revocation path and the already-revoked (idempotent) path. No
response body is returned in either case.

**Behavior:**
1. Call `auth.RequireAdmin(c)` -- return 403 if not admin.
2. Read the `id` and `token_id` path parameters.
3. Query the `pats` table for the token matching `token_id` and `user_id`.
4. If not found, return HTTP 404 with message `"token not found"`.
5. If already revoked (`revoked_at` is not NULL), return HTTP 204 (idempotent, no body).
6. Set `revoked_at` to the current UTC timestamp.
7. Return HTTP 204 (No Content).

### GET /user -- Get Own Profile (Any Authenticated User)

Returns the authenticated user's own profile.

**Behavior:**
1. Call `auth.RequirePermission(c, "users", "read")` -- return 403 if PAT
   lacks permission.
2. Get the authenticated user ID via `auth.GetUserID(c)`.
3. Query the `users` table for the matching user.
4. Set the ETag header via `apikit.SetETag(c, user.UpdatedAt)`.
5. Check `If-None-Match` via `apikit.CheckETag(c, user.UpdatedAt)` -- return
   304 if the client cache is current.
6. Return HTTP 200 with the user object.

### PATCH /user -- Update Own Profile (Any Authenticated User)

Updates the authenticated user's own `full_name`.

**Request body:**
```json
{
  "full_name": "<string, required>"
}
```

**Behavior:**
1. Call `auth.RequirePermission(c, "users", "read")` -- return 403 if PAT
   lacks permission. Note: self-service profile updates are gated on
   `users:read` because the master PRD does not define a `users:write`
   permission. The defined PAT permission set is: `users:read`, `orgs:read`,
   `keys:read`, `keys:manage`, `tokens:read`, `tokens:manage`. Modifying
   only `full_name` (non-sensitive own metadata) is considered within scope
   of the `users:read` permission.
2. Get the authenticated user ID via `auth.GetUserID(c)`.
3. Bind and validate the request body. Return 400 if `full_name` is missing.
4. Query the `users` table for the matching user. Return 404 if not found.
5. Update `full_name` and `updated_at`.
6. Return HTTP 200 with the updated user object.

### GET /user/orgs -- List Own Organizations (Any Authenticated User)

Lists the authenticated user's organization memberships. The `orgs` and
`org_members` tables are defined and created by `database_layer` (spec 02),
which is already declared as a dependency of this spec.

**Behavior:**
1. Call `auth.RequirePermission(c, "orgs", "read")` -- return 403 if PAT
   lacks permission.
2. Get the authenticated user ID via `auth.GetUserID(c)`.
3. Query the `org_members` table joined with `orgs` for all organizations
   where the user is a member. Exclude blocked organizations.
4. Return HTTP 200 with a JSON array of organization objects:
   ```json
   [
     {
       "id": "<uuid>",
       "name": "<string>",
       "slug": "<string>",
       "url": "<string or empty>",
       "status": "active",
       "created_at": "<RFC 3339>",
       "updated_at": "<RFC 3339>"
     }
   ]
   ```
5. Return an empty array `[]` if the user has no memberships.

---

## Route Registration

All routes are registered on the Echo `APIGroup()` returned by
`server.APIGroup()`. The handler constructor accepts the database handle:

```go
// RegisterUserHandlers registers all user management routes on the
// provided Echo group. It requires a database handle for user queries.
func RegisterUserHandlers(g *echo.Group, database *sql.DB)
```

Routes registered:

```go
// Admin user management
g.POST("/users", createUser)
g.GET("/users", listUsers)
g.GET("/users/:id", getUser)
g.PATCH("/users/:id", updateUser)
g.POST("/users/:id/promote", promoteUser)
g.POST("/users/:id/demote", demoteUser)
g.POST("/users/:id/block", blockUser)
g.POST("/users/:id/unblock", unblockUser)
g.GET("/users/:id/keys", listUserKeys)
g.DELETE("/users/:id/keys/:key_id", revokeUserKey)
g.GET("/users/:id/tokens", listUserTokens)
g.DELETE("/users/:id/tokens/:token_id", revokeUserToken)

// Self-service profile
g.GET("/user", getOwnProfile)
g.PATCH("/user", updateOwnProfile)
g.GET("/user/orgs", listOwnOrgs)
```

---

## Interfaces

### Handler Functions

All handler functions have the signature `func(c echo.Context) error` and are
unexported (lowercase). They are registered by `RegisterUserHandlers()`.

### Request/Response Types

```go
// CreateUserRequest is the request body for POST /users.
type CreateUserRequest struct {
    Username   string `json:"username"`
    Email      string `json:"email"`
    Provider   string `json:"provider"`
    ProviderID string `json:"provider_id"`
}

// UpdateUserRequest is the request body for PATCH /users/:id and PATCH /user.
type UpdateUserRequest struct {
    FullName *string `json:"full_name"`
}

// User is the response object for user endpoints.
type User struct {
    ID         string `json:"id"`
    Username   string `json:"username"`
    Email      string `json:"email"`
    FullName   string `json:"full_name"`
    Role       string `json:"role"`
    Status     string `json:"status"`
    Provider   string `json:"provider"`
    ProviderID string `json:"provider_id"`
    CreatedAt  string `json:"created_at"`
    UpdatedAt  string `json:"updated_at"`
}

// APIKeyMeta is the response object for API key listing endpoints (no secrets).
type APIKeyMeta struct {
    KeyID     string  `json:"key_id"`
    UserID    string  `json:"user_id"`
    CreatedAt string  `json:"created_at"`
    ExpiresAt *string `json:"expires_at"`
    RevokedAt *string `json:"revoked_at"`
}

// PATMeta is the response object for PAT listing endpoints (no secrets).
type PATMeta struct {
    TokenID     string   `json:"token_id"`
    Name        string   `json:"name"`
    Permissions []string `json:"permissions"`
    UserID      string   `json:"user_id"`
    CreatedAt   string   `json:"created_at"`
    ExpiresAt   *string  `json:"expires_at"`
    RevokedAt   *string  `json:"revoked_at"`
}
```

---

## Error Handling

| Condition | Status | Message |
|-----------|--------|---------|
| Not admin on admin-only endpoint | 403 | `"forbidden"` |
| PAT lacks required permission | 403 | `"insufficient permissions"` |
| Missing or empty required field in request body | 400 | `"missing required field: <field_name>"` |
| Invalid JSON body | 400 | `"invalid request body"` |
| User not found | 404 | `"user not found"` |
| API key not found | 404 | `"api key not found"` |
| PAT not found | 404 | `"token not found"` |
| Duplicate username | 409 | `"username already exists"` |
| Duplicate (provider, provider_id) | 409 | `"provider identity already exists"` |
| Cannot demote last admin | 409 | `"cannot demote the last admin"` |
| Database error | 500 | `"internal server error"` |

All errors use the standard `apikit.APIError(c, code, message)` helper.

---

## Testing Strategy

### Unit Tests

- `TestCreateUser_Success` -- valid request creates user, returns 201.
- `TestCreateUser_MissingUsername` -- returns 400 with field name.
- `TestCreateUser_MissingEmail` -- returns 400 with field name.
- `TestCreateUser_MissingProvider` -- returns 400 with field name.
- `TestCreateUser_MissingProviderID` -- returns 400 with field name.
- `TestCreateUser_DuplicateUsername` -- returns 409.
- `TestCreateUser_DuplicateProviderIdentity` -- returns 409.
- `TestCreateUser_NonAdmin` -- returns 403.
- `TestListUsers_ExcludesBlocked` -- blocked users excluded by default.
- `TestListUsers_IncludeBlocked` -- `?include_blocked=true` includes blocked users.
- `TestListUsers_Empty` -- returns empty array when no users.
- `TestListUsers_NonAdmin` -- returns 403.
- `TestGetUser_Success` -- returns user object with 200.
- `TestGetUser_NotFound` -- returns 404.
- `TestGetUser_ETag` -- sets ETag and responds 304 on match.
- `TestGetUser_NonAdmin` -- returns 403.
- `TestUpdateUser_Success` -- updates full_name, returns 200.
- `TestUpdateUser_NotFound` -- returns 404.
- `TestUpdateUser_ClearFullName` -- empty string clears full_name.
- `TestUpdateUser_NonAdmin` -- returns 403.
- `TestPromoteUser_Success` -- user role changes to admin.
- `TestPromoteUser_AlreadyAdmin` -- idempotent, returns 200.
- `TestPromoteUser_NotFound` -- returns 404.
- `TestPromoteUser_NonAdmin` -- returns 403.
- `TestDemoteUser_Success` -- user role changes to user.
- `TestDemoteUser_AlreadyUser` -- idempotent, returns 200.
- `TestDemoteUser_LastAdmin` -- returns 409.
- `TestDemoteUser_NotFound` -- returns 404.
- `TestDemoteUser_NonAdmin` -- returns 403.
- `TestBlockUser_Success` -- user status changes to blocked.
- `TestBlockUser_AlreadyBlocked` -- idempotent, returns 200.
- `TestBlockUser_NotFound` -- returns 404.
- `TestBlockUser_NonAdmin` -- returns 403.
- `TestUnblockUser_Success` -- user status changes to active.
- `TestUnblockUser_AlreadyActive` -- idempotent, returns 200.
- `TestUnblockUser_NotFound` -- returns 404.
- `TestUnblockUser_NonAdmin` -- returns 403.
- `TestListUserKeys_Success` -- returns key metadata without secrets.
- `TestListUserKeys_UserNotFound` -- returns 404.
- `TestListUserKeys_NonAdmin` -- returns 403.
- `TestRevokeUserKey_Success` -- sets revoked_at, returns 204.
- `TestRevokeUserKey_AlreadyRevoked` -- idempotent, returns 204.
- `TestRevokeUserKey_NotFound` -- returns 404.
- `TestRevokeUserKey_NonAdmin` -- returns 403.
- `TestListUserTokens_Success` -- returns token metadata without secrets.
- `TestListUserTokens_UserNotFound` -- returns 404.
- `TestListUserTokens_NonAdmin` -- returns 403.
- `TestRevokeUserToken_Success` -- sets revoked_at, returns 204.
- `TestRevokeUserToken_AlreadyRevoked` -- idempotent, returns 204.
- `TestRevokeUserToken_NotFound` -- returns 404.
- `TestRevokeUserToken_NonAdmin` -- returns 403.
- `TestGetOwnProfile_Success` -- returns authenticated user's profile.
- `TestGetOwnProfile_ETag` -- sets ETag and responds 304 on match.
- `TestGetOwnProfile_PATWithPermission` -- PAT with `users:read` succeeds.
- `TestGetOwnProfile_PATWithoutPermission` -- PAT without `users:read` returns 403.
- `TestUpdateOwnProfile_Success` -- updates own full_name.
- `TestUpdateOwnProfile_MissingField` -- returns 400.
- `TestUpdateOwnProfile_PATWithPermission` -- PAT with `users:read` can update full_name.
- `TestUpdateOwnProfile_PATWithoutPermission` -- PAT without `users:read` returns 403.
- `TestListOwnOrgs_Success` -- returns organizations the user belongs to.
- `TestListOwnOrgs_NoMemberships` -- returns empty array.
- `TestListOwnOrgs_ExcludesBlockedOrgs` -- blocked orgs excluded.
- `TestListOwnOrgs_PATWithPermission` -- PAT with `orgs:read` succeeds.
- `TestListOwnOrgs_PATWithoutPermission` -- PAT without `orgs:read` returns 403.

---

## Design Decisions

- **No user deletion.** Users can be blocked but not deleted. This preserves
  audit trail integrity -- blocked users may appear in logs, org membership
  history, and token provenance records.

- **`POST /users` is independent of `admin_bootstrap`.** `admin_bootstrap` seeds
  the first admin user at server startup via the OAuth callback flow and a
  break-glass bootstrap token. `POST /users` is a runtime API endpoint for
  existing admin users to create additional records directly. Both write to the
  `users` table but serve distinct purposes and are protected by different auth
  mechanisms.

- **`provider` is a free-form string in admin user creation.** When an admin
  creates a user via `POST /users`, the `provider` value is stored as-is without
  validation against the OAuth provider registry. Validation against configured
  providers only occurs during the OAuth login flow (`oauth_provider_registry`).
  This allows admins to pre-register users for future providers not yet active.

- **Action endpoints return the updated resource.** Following GitHub REST API
  conventions, promote/demote/block/unblock return the updated user object with
  HTTP 200. The caller gets the new state without a follow-up GET.

- **Idempotent action endpoints.** Promoting an already-admin user, blocking
  an already-blocked user, etc., returns 200 with the current state rather than
  an error. This simplifies client logic and supports retry-safe operations.

- **Credential revocation endpoints return 204 on all success paths.** Both
  `DELETE /users/:id/keys/:key_id` and `DELETE /users/:id/tokens/:token_id`
  return HTTP 204 No Content whether the credential was just revoked or was
  already revoked. This aligns with standard REST DELETE semantics and simplifies
  client logic: a 204 always means "the credential is not active."

- **Last admin safeguard.** The demote endpoint counts active admin users and
  refuses to demote the last one. This prevents accidental lockout.

- **`full_name` is the only mutable field via PATCH.** Username, email,
  provider, and provider_id are set at creation (via admin create or OAuth
  upsert) and are not changeable through the user management API. This
  prevents identity confusion.

- **`PATCH /user` uses `users:read` permission.** The master PRD defines a
  fixed PAT permission set (`users:read`, `orgs:read`, `keys:read`,
  `keys:manage`, `tokens:read`, `tokens:manage`) with no `users:write`
  permission. Self-service profile updates (only `full_name`) are considered
  within scope of `users:read` since the user is modifying only their own
  non-sensitive metadata.

- **Credential listings are metadata only.** API key and PAT listing endpoints
  never return plaintext secrets. Secrets are only returned at creation time.

- **Blocked users excluded by default.** The list endpoint excludes blocked
  users unless `?include_blocked=true` is specified, matching the pattern
  used by the organization listing endpoint.

- **Self-service endpoints use `/user` (singular).** The `/user` prefix
  indicates "the authenticated user" without requiring a user ID in the path.
  The `/users/:id` prefix is for admin operations on any user.

- **`orgs` and `org_members` tables are owned by `database_layer`.** The
  `GET /user/orgs` endpoint queries these tables, which are created and
  owned by `database_layer` (spec 02). No additional dependency declaration
  is required beyond the existing `database_layer` dependency.

---

## Glossary

| Term | Definition |
|------|------------|
| **User** | A person or service account registered in apikit, identified by a UUID and authenticated via OAuth or admin creation. |
| **Admin role** | A user role granting full access to all endpoints and resources. Designated on first boot via `--admin-email`; delegated by existing admins via promote/demote. |
| **Block** | A reversible action that prevents a user from authenticating. Blocked users' credentials become inert but are not deleted. |
| **Promote** | Granting the admin role to a user. |
| **Demote** | Revoking the admin role from a user. Cannot be performed on the last admin. |
| **Self-service endpoint** | An endpoint under `/user` (singular) that operates on the authenticated user's own resources. |
| **Admin endpoint** | An endpoint under `/users` (plural) that operates on any user and requires admin-level access. |
| **Idempotent** | An operation that produces the same result regardless of how many times it is performed. Action endpoints (promote, demote, block, unblock) are idempotent. Credential revocation DELETE endpoints are also idempotent, returning 204 on all success paths. |
| **API key metadata** | The non-secret fields of an API key: `key_id`, `user_id`, `created_at`, `expires_at`, `revoked_at`. |
| **PAT metadata** | The non-secret fields of a PAT: `token_id`, `name`, `permissions`, `user_id`, `created_at`, `expires_at`, `revoked_at`. |
| **Free-form provider string** | The `provider` field on a user record, stored as provided by the admin without validation against the OAuth provider registry. |
