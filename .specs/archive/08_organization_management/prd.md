---
spec_id: 08
spec_name: organization_management
title: Organization Management
status: draft
created_at: '2026-07-17T11:28:59.066577+00:00'
updated_at: '2026-07-17T11:28:59.066577+00:00'
owner: ''
source: interactive
schema_version: 1
---
# Organization Management

## Source

This spec is derived from the [apikit master PRD](docs/PRD.md). It covers the
**Organization Management** component — spec 08 of 15.

## Intent

Implement the HTTP handlers for organization CRUD, lifecycle management
(block/unblock), and membership management. Organizations are organizational
groupings of users with no permission implications in this iteration.

An admin can create, list, update, delete, block, and unblock organizations.
An admin can also manage organization membership by adding and removing users.
Organization members (non-admin) can view the organization they belong to and
its member list. All endpoints live under the configured mount point and
require authentication via the auth middleware.

## Goals

- Provide CRUD endpoints for organizations: create, list, get, update, delete.
- Support organization lifecycle actions: block and unblock.
- Provide membership management: list members, add member, remove member.
- Enforce admin-only access for mutating operations and listing all orgs.
- Allow org members to view their own organization and its member list.
- Return HTTP 409 on duplicate organization name or slug on create.
- Exclude blocked organizations from listings by default; include with
  `?include_blocked=true`.
- Cascade-delete `org_members` rows when an organization is deleted; user
  accounts are preserved.
- Follow GitHub REST API conventions for action endpoints (return updated
  resource with HTTP 200).
- Use the standard JSON error envelope from Server Core for all error responses.

## Non-Goals

- **Permission implications.** Organizations are organizational only in this
  iteration — no RBAC, no org-scoped access control. This keeps the first
  iteration simple while preserving the entity for future RBAC work.
- **Organization roles or ownership.** No org-level roles (e.g. org admin vs
  org member). All org mutations require system admin access.
- **Pagination.** List endpoints return all results without pagination (per
  the master PRD).
- **Organization self-service.** Users cannot create or manage their own
  organizations; all org management is admin-only.
- **Slug auto-generation.** The slug is explicitly provided by the caller on
  create; no automatic slugification of the name.
- **Nested organizations.** No parent-child org relationships.

## Dependencies

| Spec | Relationship |
|------|-------------|
| `01_server_core` | Handler registration via `APIGroup()`, `APIError()` for error responses, `NowUTC()` / `FormatUTC()` / `ParseUTC()` for timestamps, `SetETag()` / `CheckETag()` for conditional requests, `CacheMiddleware(CacheNoStore)` inherited from the API group |
| `02_database_layer` | Queries `orgs` and `org_members` tables via `*sql.DB`; `users` table for membership user validation; uses `db.FormatTime` / `db.ParseTime` for timestamp handling; `db.WrapError` and `db.ErrNotFound` for error handling |
| `05_auth_middleware` | Auth middleware protects all endpoints; `GetAuthInfo()` / `GetUserID()` / `IsAdmin()` for access control; `RequireAdmin()` for admin-only endpoints; membership check for org-member endpoints |

---

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25+ |
| HTTP framework | Echo v4 (`github.com/labstack/echo/v4`) |
| Database | SQLite via `internal/db` |
| UUID generation | `github.com/google/uuid` |

## Repository Layout

```
internal/
  handlers/
    orgs.go               Organization CRUD, block/unblock handlers
    org_members.go        Organization membership handlers
    orgs_test.go          Unit and integration tests for org handlers
    org_members_test.go   Unit and integration tests for membership handlers
```

---

## Functional Requirements

### Organization Object

The organization object returned by all endpoints has the following shape:

```json
{
  "id": "<uuid>",
  "name": "<string>",
  "slug": "<string>",
  "url": "<string or empty>",
  "status": "active",
  "created_at": "2026-07-17T14:30:00Z",
  "updated_at": "2026-07-17T14:30:00Z"
}
```

Fields:
- `id` — UUID v4, generated server-side on create.
- `name` — display name, unique across all organizations.
- `slug` — URL-safe identifier, unique across all organizations.
- `url` — optional URL (empty string when not provided).
- `status` — `"active"` or `"blocked"`. Default `"active"` on create.
- `created_at` — RFC 3339 UTC timestamp, set on create.
- `updated_at` — RFC 3339 UTC timestamp, set on create and updated on every
  mutation (update, block, unblock).

### Organization Member Object

The membership object returned by member list endpoints:

```json
{
  "user_id": "<uuid>",
  "username": "<string>",
  "email": "<string>",
  "role": "<string>",
  "joined_at": "2026-07-17T14:30:00Z"
}
```

Fields:
- `user_id` — the member's user UUID.
- `username` — the member's username (joined from the `users` table).
- `email` — the member's email (joined from the `users` table).
- `role` — the member's system role (`"admin"` or `"user"`).
- `joined_at` — RFC 3339 UTC timestamp of when the membership was created
  (from `org_members.created_at`).

### POST /orgs — Create Organization (Admin Only)

Creates a new organization.

**Authorization:** Admin only (admin token or admin-role API key).

**Request body:**
```json
{
  "name": "Acme Corp",
  "slug": "acme-corp",
  "url": "https://acme.example.com"
}
```

- `name` (string, required) — must be non-empty after trimming whitespace.
- `slug` (string, required) — must be non-empty, URL-safe (lowercase
  alphanumeric characters, hyphens, and underscores only; must not start or
  end with a hyphen or underscore).
- `url` (string, optional) — stored as-is; defaults to empty string when
  omitted.

**Behavior:**
1. Validate required fields. Return HTTP 400 if `name` or `slug` is missing
   or empty.
2. Validate slug format. Return HTTP 400 if slug contains invalid characters.
3. Generate a UUID v4 for the org ID.
4. Set `status` to `"active"`, `created_at` and `updated_at` to current UTC
   timestamp.
5. Insert into the `orgs` table.
6. If the insert violates a UNIQUE constraint on `name` or `slug`, return
   HTTP 409 with message `"organization name already exists"` or
   `"organization slug already exists"`.
7. Return HTTP 201 with the created organization object.

**Response:** HTTP 201 with organization object.

### GET /orgs — List Organizations (Admin Only)

Lists all organizations. Blocked organizations are excluded by default.

**Authorization:** Admin only.

**Query parameters:**
- `include_blocked` (boolean, optional) — when `"true"`, include blocked
  organizations in the result. Default: `false`.

**Behavior:**
1. Query the `orgs` table. If `include_blocked` is not `"true"`, filter by
   `status = 'active'`.
2. Order results by `name` ascending (stable, deterministic ordering).
3. Return HTTP 200 with a JSON array of organization objects.
4. Return an empty array `[]` when no organizations exist.

**Response:** HTTP 200 with array of organization objects.

### GET /orgs/:id — Get Organization (Admin or Member)

Returns a single organization by ID.

**Authorization:** Admin or a member of the organization.

**Behavior:**
1. Parse the `:id` path parameter as a UUID. Return HTTP 400 if invalid.
2. Query the `orgs` table for the row matching `id`.
3. If no row is found, return HTTP 404 with message `"organization not found"`.
4. Check authorization: if the authenticated user is not an admin, check
   whether the user is a member of this organization by querying `org_members`.
   If not a member, return HTTP 403 with message `"forbidden"`.
5. Support ETag / If-None-Match using the org's `updated_at` timestamp.
6. Return HTTP 200 with the organization object.

**Response:** HTTP 200 with organization object (or HTTP 304 if ETag matches).

### PATCH /orgs/:id — Update Organization (Admin Only)

Updates an organization's `name` and/or `url`.

**Authorization:** Admin only.

**Request body:**
```json
{
  "name": "New Name",
  "url": "https://new-url.example.com"
}
```

- `name` (string, optional) — if provided, must be non-empty after trimming.
- `url` (string, optional) — if provided, stored as-is.

At least one field must be provided; return HTTP 400 if the body is empty or
contains no recognized fields.

**Behavior:**
1. Parse the `:id` path parameter. Return HTTP 400 if invalid UUID.
2. Query the org to verify it exists. Return HTTP 404 if not found.
3. Apply the updates. Set `updated_at` to current UTC timestamp.
4. If updating `name` violates the UNIQUE constraint, return HTTP 409 with
   message `"organization name already exists"`.
5. Return HTTP 200 with the updated organization object.

**Note:** The `slug` is immutable after creation and cannot be updated. If
`slug` is included in the request body, it is silently ignored.

**Response:** HTTP 200 with updated organization object.

### DELETE /orgs/:id — Delete Organization (Admin Only)

Deletes an organization and cascades membership deletion.

**Authorization:** Admin only.

**Behavior:**
1. Parse the `:id` path parameter. Return HTTP 400 if invalid UUID.
2. Delete the row from the `orgs` table matching `id`.
3. `org_members` rows referencing this org are automatically deleted via the
   `ON DELETE CASCADE` foreign key constraint defined in `02_database_layer`.
4. If no row was deleted (org not found), return HTTP 404 with message
   `"organization not found"`.
5. Return HTTP 204 with no body.

**Response:** HTTP 204 No Content.

### POST /orgs/:id/block — Block Organization (Admin Only)

Blocks an organization, setting its status to `"blocked"`.

**Authorization:** Admin only.

**Behavior:**
1. Parse the `:id` path parameter. Return HTTP 400 if invalid UUID.
2. Query the org. Return HTTP 404 if not found.
3. If the org is already blocked, this is idempotent — return HTTP 200 with
   the current org object (no-op).
4. Set `status` to `"blocked"` and `updated_at` to current UTC timestamp.
5. Return HTTP 200 with the updated organization object.

**Response:** HTTP 200 with updated organization object.

### POST /orgs/:id/unblock — Unblock Organization (Admin Only)

Unblocks a previously blocked organization.

**Authorization:** Admin only.

**Behavior:**
1. Parse the `:id` path parameter. Return HTTP 400 if invalid UUID.
2. Query the org. Return HTTP 404 if not found.
3. If the org is already active, this is idempotent — return HTTP 200 with
   the current org object (no-op).
4. Set `status` to `"active"` and `updated_at` to current UTC timestamp.
5. Return HTTP 200 with the updated organization object.

**Response:** HTTP 200 with updated organization object.

### GET /orgs/:id/members — List Organization Members (Admin or Member)

Lists the members of an organization.

**Authorization:** Admin or a member of the organization.

**Behavior:**
1. Parse the `:id` path parameter. Return HTTP 400 if invalid UUID.
2. Verify the org exists. Return HTTP 404 if not found.
3. Check authorization: if not admin, verify the authenticated user is a
   member. Return HTTP 403 if not a member.
4. Query `org_members` joined with `users` for the org's members.
5. Order results by `username` ascending (stable, deterministic ordering).
6. Return HTTP 200 with a JSON array of member objects.
7. Return an empty array `[]` when no members exist.

**Response:** HTTP 200 with array of member objects.

### PUT /orgs/:id/members/:user_id — Add Member (Admin Only)

Adds a user to an organization.

**Authorization:** Admin only.

**Behavior:**
1. Parse the `:id` and `:user_id` path parameters. Return HTTP 400 if either
   is an invalid UUID.
2. Verify the org exists. Return HTTP 404 with message
   `"organization not found"` if not found.
3. Verify the user exists by querying the `users` table. Return HTTP 404 with
   message `"user not found"` if not found.
4. If the user is already a member (the `(org_id, user_id)` primary key
   already exists), this is idempotent — return HTTP 204 with no body.
5. Insert a row into `org_members` with `created_at` set to current UTC
   timestamp.
6. Return HTTP 204 with no body.

**Response:** HTTP 204 No Content.

### DELETE /orgs/:id/members/:user_id — Remove Member (Admin Only)

Removes a user from an organization.

**Authorization:** Admin only.

**Behavior:**
1. Parse the `:id` and `:user_id` path parameters. Return HTTP 400 if either
   is an invalid UUID.
2. Delete the `org_members` row matching `(org_id, user_id)`.
3. If no row was deleted (membership not found), return HTTP 404 with message
   `"membership not found"`.
4. Return HTTP 204 with no body.

**Response:** HTTP 204 No Content.

---

## Handler Registration

All organization endpoints are registered on the `APIGroup()` Echo group under
the mount point. The handler registration function accepts the database handle
and is called during server setup:

```go
// RegisterOrgHandlers registers all organization management endpoints on the
// provided Echo group. The database handle is used for all org and membership
// queries.
func RegisterOrgHandlers(g *echo.Group, database *sql.DB)
```

Route registration:

```go
g.POST("/orgs", createOrg)
g.GET("/orgs", listOrgs)
g.GET("/orgs/:id", getOrg)
g.PATCH("/orgs/:id", updateOrg)
g.DELETE("/orgs/:id", deleteOrg)
g.POST("/orgs/:id/block", blockOrg)
g.POST("/orgs/:id/unblock", unblockOrg)
g.GET("/orgs/:id/members", listOrgMembers)
g.PUT("/orgs/:id/members/:user_id", addOrgMember)
g.DELETE("/orgs/:id/members/:user_id", removeOrgMember)
```

### Membership Check Helper

A helper function checks whether a user is a member of an organization. This
is used by `GET /orgs/:id` and `GET /orgs/:id/members` to allow org members
to view their own organization:

```go
// isOrgMember checks whether the given user is a member of the given
// organization. Returns true if a matching row exists in org_members.
func isOrgMember(db *sql.DB, orgID, userID string) (bool, error)
```

### Access Control Pattern

Endpoints that allow either admin or org member access follow this pattern:

1. Auth middleware has already validated the credential and injected AuthInfo.
2. The handler calls `IsAdmin(c)` to check for admin access.
3. If not admin, the handler calls `isOrgMember(db, orgID, GetUserID(c))` to
   check membership.
4. If neither admin nor member, return HTTP 403.

Admin-only endpoints simply call `RequireAdmin(c)` at the top of the handler
and return the error if non-nil.

---

## Slug Validation Rules

The `slug` field on organization creation has the following constraints:

- Must be non-empty.
- Must contain only lowercase ASCII letters (`a-z`), digits (`0-9`), hyphens
  (`-`), and underscores (`_`).
- Must not start or end with a hyphen or underscore.
- Maximum length: 128 characters.

Invalid slugs are rejected with HTTP 400 and message
`"invalid slug format"`.

---

## Interfaces

### Handler Functions

```go
// createOrg handles POST /orgs — creates a new organization.
// Requires admin access. Returns 201 with org object or 409 on conflict.
func createOrg(c echo.Context) error

// listOrgs handles GET /orgs — lists all organizations.
// Requires admin access. Excludes blocked orgs by default;
// ?include_blocked=true includes them.
func listOrgs(c echo.Context) error

// getOrg handles GET /orgs/:id — returns an organization by ID.
// Requires admin or org membership.
func getOrg(c echo.Context) error

// updateOrg handles PATCH /orgs/:id — updates an organization.
// Requires admin access.
func updateOrg(c echo.Context) error

// deleteOrg handles DELETE /orgs/:id — deletes an organization.
// Requires admin access. Cascades membership deletion.
func deleteOrg(c echo.Context) error

// blockOrg handles POST /orgs/:id/block — blocks an organization.
// Requires admin access. Returns updated org with 200.
func blockOrg(c echo.Context) error

// unblockOrg handles POST /orgs/:id/unblock — unblocks an organization.
// Requires admin access. Returns updated org with 200.
func unblockOrg(c echo.Context) error

// listOrgMembers handles GET /orgs/:id/members — lists organization members.
// Requires admin or org membership.
func listOrgMembers(c echo.Context) error

// addOrgMember handles PUT /orgs/:id/members/:user_id — adds a member.
// Requires admin access.
func addOrgMember(c echo.Context) error

// removeOrgMember handles DELETE /orgs/:id/members/:user_id — removes a member.
// Requires admin access.
func removeOrgMember(c echo.Context) error
```

### Request/Response Types

```go
// CreateOrgRequest is the request body for POST /orgs.
type CreateOrgRequest struct {
    Name string `json:"name"`
    Slug string `json:"slug"`
    URL  string `json:"url"`
}

// UpdateOrgRequest is the request body for PATCH /orgs/:id.
type UpdateOrgRequest struct {
    Name *string `json:"name,omitempty"`
    URL  *string `json:"url,omitempty"`
}

// OrgResponse is the JSON representation of an organization.
type OrgResponse struct {
    ID        string `json:"id"`
    Name      string `json:"name"`
    Slug      string `json:"slug"`
    URL       string `json:"url"`
    Status    string `json:"status"`
    CreatedAt string `json:"created_at"`
    UpdatedAt string `json:"updated_at"`
}

// OrgMemberResponse is the JSON representation of an organization member.
type OrgMemberResponse struct {
    UserID   string `json:"user_id"`
    Username string `json:"username"`
    Email    string `json:"email"`
    Role     string `json:"role"`
    JoinedAt string `json:"joined_at"`
}
```

---

## Error Handling

| Condition | Status | Message |
|-----------|--------|---------|
| Missing or empty `name` on create | 400 | `"name is required"` |
| Missing or empty `slug` on create | 400 | `"slug is required"` |
| Invalid slug format | 400 | `"invalid slug format"` |
| Invalid UUID in path parameter | 400 | `"invalid organization id"` or `"invalid user id"` |
| Empty update body (no recognized fields) | 400 | `"no fields to update"` |
| Non-admin accessing admin-only endpoint | 403 | `"forbidden"` (via `RequireAdmin`) |
| Non-member accessing member endpoint | 403 | `"forbidden"` |
| Organization not found | 404 | `"organization not found"` |
| User not found (add member) | 404 | `"user not found"` |
| Membership not found (remove member) | 404 | `"membership not found"` |
| Duplicate organization name | 409 | `"organization name already exists"` |
| Duplicate organization slug | 409 | `"organization slug already exists"` |
| Database error | 500 | `"internal server error"` |

All errors use the standard `APIError()` helper from Server Core:

```json
{
  "error": {
    "code": 409,
    "message": "organization name already exists"
  }
}
```

---

## Caching

All organization endpoints inherit `Cache-Control: no-store` from the
`APIGroup()` group-level middleware (see `01_server_core`). Organizations are
mutable resources and must not be cached by intermediaries or clients.

---

## Testing Strategy

### Unit Tests

- `TestCreateOrg_Success` — valid request creates org, returns 201 with correct fields.
- `TestCreateOrg_MissingName` — returns 400 with `"name is required"`.
- `TestCreateOrg_MissingSlug` — returns 400 with `"slug is required"`.
- `TestCreateOrg_InvalidSlug` — slug with special characters returns 400.
- `TestCreateOrg_SlugStartsWithHyphen` — returns 400.
- `TestCreateOrg_SlugTooLong` — 129-character slug returns 400.
- `TestCreateOrg_DuplicateName` — returns 409 with appropriate message.
- `TestCreateOrg_DuplicateSlug` — returns 409 with appropriate message.
- `TestCreateOrg_NonAdmin` — non-admin user returns 403.
- `TestCreateOrg_OptionalURL` — org created with empty URL when omitted.
- `TestListOrgs_ExcludesBlocked` — blocked orgs not in default listing.
- `TestListOrgs_IncludesBlocked` — blocked orgs included with query param.
- `TestListOrgs_Empty` — returns empty array when no orgs exist.
- `TestListOrgs_OrderedByName` — results ordered alphabetically.
- `TestListOrgs_NonAdmin` — non-admin user returns 403.
- `TestGetOrg_AsAdmin` — admin can view any org.
- `TestGetOrg_AsMember` — org member can view their org.
- `TestGetOrg_NotMember` — non-member regular user returns 403.
- `TestGetOrg_NotFound` — returns 404.
- `TestGetOrg_InvalidID` — invalid UUID returns 400.
- `TestGetOrg_ETag` — response includes ETag; conditional request returns 304.
- `TestUpdateOrg_Name` — update name returns 200 with updated org.
- `TestUpdateOrg_URL` — update URL returns 200.
- `TestUpdateOrg_BothFields` — update name and URL simultaneously.
- `TestUpdateOrg_SlugIgnored` — slug in request body is silently ignored.
- `TestUpdateOrg_EmptyBody` — returns 400.
- `TestUpdateOrg_NotFound` — returns 404.
- `TestUpdateOrg_DuplicateName` — name conflict returns 409.
- `TestUpdateOrg_NonAdmin` — returns 403.
- `TestUpdateOrg_UpdatesTimestamp` — `updated_at` changes on update.
- `TestDeleteOrg_Success` — returns 204, org is removed.
- `TestDeleteOrg_CascadesMembers` — org_members rows are removed.
- `TestDeleteOrg_UsersPreserved` — user accounts still exist after org deletion.
- `TestDeleteOrg_NotFound` — returns 404.
- `TestDeleteOrg_NonAdmin` — returns 403.
- `TestBlockOrg_Success` — status changes to blocked, returns 200.
- `TestBlockOrg_Idempotent` — blocking already-blocked org returns 200.
- `TestBlockOrg_NotFound` — returns 404.
- `TestBlockOrg_UpdatesTimestamp` — `updated_at` changes on block.
- `TestBlockOrg_NonAdmin` — returns 403.
- `TestUnblockOrg_Success` — status changes to active, returns 200.
- `TestUnblockOrg_Idempotent` — unblocking already-active org returns 200.
- `TestUnblockOrg_NotFound` — returns 404.
- `TestUnblockOrg_NonAdmin` — returns 403.
- `TestListMembers_AsAdmin` — admin can list any org's members.
- `TestListMembers_AsMember` — member can list their org's members.
- `TestListMembers_NotMember` — non-member returns 403.
- `TestListMembers_OrgNotFound` — returns 404.
- `TestListMembers_Empty` — returns empty array when no members.
- `TestListMembers_OrderedByUsername` — results ordered alphabetically.
- `TestListMembers_IncludesUserDetails` — response includes username, email, role.
- `TestAddMember_Success` — member added, returns 204.
- `TestAddMember_Idempotent` — adding existing member returns 204.
- `TestAddMember_OrgNotFound` — returns 404.
- `TestAddMember_UserNotFound` — returns 404.
- `TestAddMember_InvalidOrgID` — invalid UUID returns 400.
- `TestAddMember_InvalidUserID` — invalid UUID returns 400.
- `TestAddMember_NonAdmin` — returns 403.
- `TestRemoveMember_Success` — member removed, returns 204.
- `TestRemoveMember_NotFound` — returns 404 when membership doesn't exist.
- `TestRemoveMember_NonAdmin` — returns 403.

### Integration Tests

- `TestOrgLifecycle` — full CRUD cycle: create, get, update, delete.
- `TestOrgBlockUnblockCycle` — create, block, verify excluded from list,
  unblock, verify included in list.
- `TestOrgMembershipLifecycle` — create org, add members, list members,
  remove member, verify member list updated.
- `TestOrgDeleteCascade` — create org with members, delete org, verify
  members rows deleted but users still exist.
- `TestOrgMemberAccess` — member can GET org and list members; non-member
  cannot.
- `TestOrgAllEndpointsRequireAuth` — all endpoints return 401 without auth.
- `TestOrgAdminEndpointsRequireAdmin` — mutating endpoints return 403 for
  non-admin users.
- `TestOrgCacheHeaders` — all org endpoints return
  `Cache-Control: no-store`.
- `TestOrgConditionalGet` — create org, GET with ETag, update org, GET with
  old ETag returns 200 (not 304).

---

## Design Decisions

- **Organizations are organizational only.** No permission implications in
  this iteration. Organizations have CRUD and lifecycle operations but
  membership does not affect access control beyond viewing the org itself
  and its member list. This keeps the first iteration simple while preserving
  the entity for future RBAC work.

- **Slug is immutable after creation.** The slug is used as a URL-safe
  identifier and may be referenced externally. Allowing slug changes would
  break external references. The name can be updated freely.

- **Idempotent membership operations.** Adding an already-existing member
  returns 204 (no error); blocking an already-blocked org returns 200. This
  follows GitHub API conventions and simplifies client code.

- **PUT for add member, DELETE for remove member.** PUT is idempotent by
  HTTP semantics, matching the "set this membership" intent. The member's
  user_id is in the URL path, making the operation fully idempotent.

- **Admin-only mutations.** All create/update/delete/block/unblock/membership
  operations require admin access. Organization members can only view (GET).
  This keeps the authorization model simple for this iteration.

- **Cascade delete of memberships.** When an organization is deleted, all
  `org_members` rows are automatically removed via the database's
  `ON DELETE CASCADE` constraint. User accounts are not affected.

- **Deterministic ordering.** Organization lists are ordered by `name`,
  member lists are ordered by `username`. This ensures stable, predictable
  output across requests.

- **Org member view includes user details.** The member list endpoint joins
  with the `users` table to include `username`, `email`, and `role`. This
  avoids requiring clients to make N+1 requests to resolve member details.

---

## Glossary

| Term | Definition |
|------|------------|
| **Organization** | A grouping entity for users with CRUD, block/unblock lifecycle, and membership management. Has no permission implications in this iteration. |
| **Slug** | A URL-safe identifier for an organization, consisting of lowercase letters, digits, hyphens, and underscores. Immutable after creation. |
| **Org member** | A user associated with an organization via a row in the `org_members` table. Membership grants view access to the org and its member list. |
| **Cascade delete** | When an org is deleted, all `org_members` rows referencing that org are automatically deleted by the database's `ON DELETE CASCADE` constraint. |
| **Blocked org** | An organization with `status = "blocked"`. Excluded from default listings; still accessible via direct ID lookup. |
| **Admin** | A user with role `"admin"` or the holder of an admin token. Required for all org mutations. |
| **`orgs`** | Database table storing organization records (id, name, slug, url, status, timestamps). |
| **`org_members`** | Database table storing organization membership as `(org_id, user_id)` pairs with a `created_at` timestamp. |

