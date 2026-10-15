---
spec_id: '17'
spec_name: pat_permission_updates
title: Pat Permission Updates
status: draft
created_at: '2026-07-29T08:03:22.373957+00:00'
updated_at: '2026-07-29T08:08:41.839107+00:00'
owner: ''
source: interactive
schema_version: 1
---
# PAT Permission Updates

## Intent

Add three new API endpoints and corresponding CLI commands to modify the
permissions of an existing Personal Access Token (PAT). Currently, PAT
permissions are immutable after creation — the only way to change them is to
revoke the token and create a new one. This spec adds in-place permission
modification to avoid unnecessary secret rotation.

The three operations are:

- **Replace** — replace the entire permissions set with a new one.
- **Add** — merge new permissions into the existing set (idempotent).
- **Remove** — delete specific permissions from the existing set (idempotent).

## Goals

- Implement `PUT /user/tokens/:token_id/permissions` — replace all permissions
  on a PAT with a new set.
- Implement `PATCH /user/tokens/:token_id/permissions` — add permissions to
  a PAT's existing set. Permissions already present are silently ignored.
- Implement `DELETE /user/tokens/:token_id/permissions` — remove permissions
  from a PAT's existing set. Permissions not present are silently ignored.
- Add CLI commands `akc tokens replace`, `akc tokens add`, and
  `akc tokens remove` that map to the three endpoints above.
- Register a new `tokens:write` permission in the `PermissionRegistry`. PATs
  must hold either `tokens:manage` or `tokens:write` to call the three new
  endpoints. API keys bypass this check as usual.
- Auto-revoke tokens whose permissions become empty: if a `replace` or `remove`
  operation results in zero permissions, set `revoked_at` to the current
  timestamp. The response includes the `revoked_at` field so the caller sees
  the revocation.
- Block modifications on revoked or expired tokens. Return HTTP 400 with a
  descriptive error message.
- Enforce the same privilege escalation rules as `POST /user/tokens` (create):
  when the caller is authenticated via PAT, the new/added permissions must be
  a subset of the caller's own PAT permissions. API keys can set any registered
  permission.
- All three endpoints return the updated `PATResponse` (same shape as
  `GET /user/tokens/:token_id`).

## Non-Goals

- Modifying other PAT fields (name, expiry). Only permissions are mutable.
- Admin endpoints for modifying other users' token permissions.
- Changing the existing `POST /user/tokens` (create) or
  `DELETE /user/tokens/:token_id` (revoke) endpoints.
- Rate limiting on the new endpoints.
- Updating `api/openapi.yaml` — a separate spec or PR will update the OpenAPI
  document; this spec covers only the Go implementation and CLI.
- Go SDK or Python SDK updates — the CLI calls the HTTP endpoints directly via
  `DoRequest` (the existing pattern in `tokens.go`); SDK updates are out of
  scope for this spec.
- ETag / conditional-request (If-Match) support — last-write-wins is acceptable
  for PAT permission updates; the existing PAT endpoints do not use ETags.
- Permission inheritance or hierarchy enforcement beyond the OR-check described
  below — `tokens:manage` is treated as a superset of `tokens:write` by
  convention, but the registry does not encode this relationship automatically.

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25+ |
| HTTP framework | Echo v4 (`github.com/labstack/echo/v4`) |
| Database | SQLite via `internal/db` |
| CLI framework | Cobra (`github.com/spf13/cobra`) |
| JSON marshaling | stdlib `encoding/json` |

## Repository Layout

```
internal/
  auth/
    permissions.go        Add tokens:write to built-in permissions
  handlers/
    pat.go                Add replacePATPermissions, addPATPermissions, removePATPermissions handlers
                          (modifies PATHandler.RegisterRoutes in-place — see Route Registration)
    pat_test.go           Tests for the new handlers
  cli/
    tokens.go             Add replace, add, remove subcommands (using DoRequest directly)
```

## Database Schema

Permissions are stored as a JSON-serialized array in the `permissions TEXT`
column on the `pats` table (confirmed from `internal/db/schema.go`). Example
stored value: `'["users:read","orgs:read"]'`. There is no separate join table.
All three handlers read and update this single column in a `db.WithTx`
transaction using a single `UPDATE` statement. Because SQLite serializes
writers by default, no additional row-level locking (e.g., `SELECT ... FOR
UPDATE`) is required; last-write-wins semantics within `db.WithTx` are
sufficient and acceptable.

## Functional Requirements

### New Permission: `tokens:write`

Register `tokens:write` as a built-in permission in `NewPermissionRegistry()`
alongside the existing 6 permissions. This brings the total to 7 built-in
permissions.

**Relationship to `tokens:manage`:** `tokens:write` is a strict subset of
`tokens:manage`. `tokens:manage` grants everything `tokens:write` does (modify
permissions in-place), plus the ability to create and revoke tokens. A PAT
holding `tokens:manage` therefore satisfies the `tokens:write` OR-check. The
`PermissionRegistry` does not encode this relationship automatically; the OR-
check is implemented explicitly in the handler.

The three new endpoints require either `tokens:write` or `tokens:manage`.
Since `RequirePermission()` only checks a single permission, the handler must
implement an OR check: try `tokens:write` first, then fall back to
`tokens:manage`. If both fail, return HTTP 403.

**No maximum permissions per token.** Any combination of the 7 registered
built-in permissions is valid. The registry is bounded by design, so unbounded
growth via repeated PATCH/add calls is not a concern in practice.

### Route Registration

The three new routes are added **directly to `PATHandler.RegisterRoutes` in
`pat_lifecycle`** (in-place modification of the existing method — no separate
registration call is introduced):

```
PUT    /user/tokens/:token_id/permissions  → replacePATPermissions
PATCH  /user/tokens/:token_id/permissions  → addPATPermissions
DELETE /user/tokens/:token_id/permissions  → removePATPermissions
```

### Common Behavior (all three endpoints)

All three endpoints share these behaviors:

1. **Permission check:** Require `tokens:write` or `tokens:manage` permission.
   API keys and admin tokens bypass this check.

2. **Request body:** All three accept the same JSON body:
   ```json
   {
     "permissions": ["users:read", "orgs:read"]
   }
   ```
   The `permissions` field is a non-empty array of `resource_type:action`
   strings.

3. **Token lookup:** Query the `pats` table by `token_id` AND `user_id`
   (from auth context) to enforce user isolation. If not found, return
   HTTP 404 with message `"token not found"`.

4. **Revocation guard:** If the token's `revoked_at` is non-NULL, return
   HTTP 400 with message `"token is revoked"`.

5. **Expiry guard:** If the token's `expires_at` is non-NULL and in the past,
   return HTTP 400 with message `"token is expired"`.

6. **Permission validation (replace and add only):** Each permission in the
   request body must be in `resource_type:action` format (exactly one colon)
   and must be registered in the `PermissionRegistry`. Invalid format returns
   HTTP 400 with `"invalid permission format: <permission>"`. Unknown
   permission returns HTTP 400 with `"unknown permission: <permission>"`.

7. **Permission validation (remove only):** Permissions in the request body
   that are not registered in the `PermissionRegistry` are silently ignored
   (they can't exist on the token anyway). Format validation (exactly one
   colon) still applies.

8. **Privilege escalation check (replace and add only):** When the caller is
   authenticated via PAT, each new permission being set/added must be present
   in the caller's own PAT permissions. If not, return HTTP 403 with
   `"cannot grant permission: <permission>"`. API keys bypass this check.
   For `replace`, only permissions not already on the target token count as
   "new" for escalation purposes — keeping existing permissions is always
   allowed. A PAT caller **may** modify their own token; the escalation rule
   still applies (they can only keep existing permissions or add permissions
   they already hold on the calling PAT). Because keeping an existing
   permission is always allowed, a PAT caller modifying their own token will
   never be blocked for trying to retain permissions they already hold.

9. **Auto-revocation:** If the resulting permissions set is empty (after
   replace or remove), set `revoked_at` to the current UTC timestamp. The
   response will include `revoked_at`, making the revocation visible to the
   caller.

10. **Response:** HTTP 200 with `PATResponse` showing the updated token
    metadata including the new permissions set. If the token was auto-revoked,
    `revoked_at` is included.

11. **Database update:** Update the `permissions` column (JSON-serialized
    array) in a single UPDATE statement within a `db.WithTx` transaction.
    If auto-revoking, also set `revoked_at` in the same UPDATE. SQLite's
    default serialized write mode ensures read-modify-write safety within the
    transaction; no explicit row-level locking is needed.

### PUT /user/tokens/:token_id/permissions — Replace

Replaces the token's entire permissions set with the provided list.

**Empty-array behavior (definitive rule):**

| `permissions` field value | Behavior |
|---------------------------|----------|
| Absent or `null` | HTTP 400 `"permissions are required"` |
| Empty array `[]` | Auto-revoke: set `revoked_at`, return HTTP 200 with revoked `PATResponse` |
| Non-empty array | Validate, then replace permissions; return HTTP 200 with updated `PATResponse` |

- If the new permissions set is identical to the current set (same elements,
  regardless of order), the update still proceeds (idempotent, no special
  handling needed).
- The new permissions are stored in the order provided by the client.

### PATCH /user/tokens/:token_id/permissions — Add

Adds the provided permissions to the token's existing set.

- `permissions` must be non-empty (absent, null, or `[]` returns HTTP 400
  with `"permissions are required"`).
- Permissions already present on the token are silently ignored (no error,
  no duplication).
- New permissions are appended to the end of the existing list (preserving
  insertion order for existing permissions).
- Because PATCH can only add permissions (never remove them), the resulting
  set is always non-empty; auto-revocation can never be triggered by this
  operation.

### DELETE /user/tokens/:token_id/permissions — Remove

Removes the specified permissions from the token's existing set.

- `permissions` must be non-empty (absent, null, or `[]` returns HTTP 400
  with `"permissions are required"`).
- Permissions not present on the token are silently ignored.
- If all permissions are removed, the token is auto-revoked (set `revoked_at`).
- No privilege escalation check — removing permissions never grants new access.
- Unregistered permissions in the request body are silently ignored (format
  validation still applies: each string must contain exactly one colon).

### CLI Commands

All three CLI commands call the HTTP endpoints directly via `DoRequest` (the
same pattern used in `tokens.go`). They do **not** delegate to the Go SDK.
The `--permissions` flag uses the same `parsePermissions` comma-split helper
already defined in `tokens.go` (from `cli_user_commands`), which splits on
commas and trims whitespace.

**Revocation warning asymmetry (by design):** Only `akc tokens replace` and
`akc tokens remove` can produce an empty permissions set, so only those two
commands print the revocation warning to stderr. `akc tokens add` never
triggers auto-revocation and therefore does not include this check.

#### `akc tokens replace <token_id> --permissions <perms>`

- Positional arg: `token_id` (exactly one).
- Flag: `--permissions` (required, comma-separated, same parsing as `create`).
- Sends `PUT /user/tokens/<token_id>/permissions`.
- Prints updated PATResponse JSON to stdout.
- If the token was auto-revoked, prints
  `"Token <token_id> has been revoked (no permissions remaining)"` to stderr.

#### `akc tokens add <token_id> --permissions <perms>`

- Positional arg: `token_id` (exactly one).
- Flag: `--permissions` (required, comma-separated, same parsing as `create`).
- Sends `PATCH /user/tokens/<token_id>/permissions`.
- Prints updated PATResponse JSON to stdout.
- No revocation warning (add cannot produce an empty set).

#### `akc tokens remove <token_id> --permissions <perms>`

- Positional arg: `token_id` (exactly one).
- Flag: `--permissions` (required, comma-separated, same parsing as `create`).
- Sends `DELETE /user/tokens/<token_id>/permissions`.
- Prints updated PATResponse JSON to stdout.
- If the token was auto-revoked, prints
  `"Token <token_id> has been revoked (no permissions remaining)"` to stderr.

## Interfaces

### Request Type

```go
// UpdatePATPermissionsRequest is the JSON body for PUT, PATCH, and DELETE
// on /user/tokens/:token_id/permissions.
type UpdatePATPermissionsRequest struct {
    Permissions []string `json:"permissions"`
}
```

A `nil` slice (absent or null `permissions` field) and an empty slice (`[]`)
are distinguished at the handler level:
- `nil` → HTTP 400 `"permissions are required"` (for all three operations).
- `[]` → HTTP 400 `"permissions are required"` for PATCH and DELETE;
  auto-revocation for PUT.

### Response Type

Reuses the existing `PATResponse` struct from `internal/handlers/pat.go`.

## Error Handling

| Condition | Status | Message |
|-----------|--------|---------|
| Absent or null `permissions` field | 400 | `"permissions are required"` |
| Empty `permissions` array (PATCH / DELETE) | 400 | `"permissions are required"` |
| Empty `permissions` array (PUT) | 200 | *(auto-revoke, returns revoked PATResponse)* |
| Malformed permission string (no colon) | 400 | `"invalid permission format: <permission>"` |
| Unregistered permission (replace/add only) | 400 | `"unknown permission: <permission>"` |
| Token is revoked | 400 | `"token is revoked"` |
| Token is expired | 400 | `"token is expired"` |
| Unauthenticated request | 401 | (handled by auth middleware) |
| Insufficient permissions | 403 | `"insufficient permissions"` |
| PAT privilege escalation (replace/add) | 403 | `"cannot grant permission: <permission>"` |
| Token not found (or belongs to another user) | 404 | `"token not found"` |
| Malformed JSON body | 400 | `"invalid request body"` |
| Database error | 500 | `"internal server error"` |

## Testing Strategy

### Unit Tests

- Replace: valid request updates permissions, returns HTTP 200 with updated PATResponse.
- Replace: replaces with a subset of current permissions (fewer permissions after).
- Replace: replaces with a superset (adds new permissions).
- Replace: empty permissions array (`[]`) auto-revokes the token, returns HTTP 200 with revoked PATResponse.
- Replace: absent/null `permissions` field returns HTTP 400 `"permissions are required"`.
- Replace: revoked token returns HTTP 400 "token is revoked".
- Replace: expired token returns HTTP 400 "token is expired".
- Replace: privilege escalation blocked when authenticated via PAT.
- Replace: API key bypasses privilege escalation.
- Replace: unknown permission returns HTTP 400.
- Replace: token not found returns HTTP 404.
- Replace: other user's token returns HTTP 404.
- Replace: PAT caller modifying their own token — keeping existing permissions is allowed without triggering escalation.
- Add: valid request adds new permissions, returns HTTP 200.
- Add: adding permissions already present is idempotent (no duplicates).
- Add: adding a mix of new and existing permissions works correctly.
- Add: empty permissions array or absent field returns HTTP 400 `"permissions are required"`.
- Add: privilege escalation blocked when authenticated via PAT.
- Add: revoked token returns HTTP 400.
- Add: expired token returns HTTP 400.
- Remove: valid request removes specified permissions, returns HTTP 200.
- Remove: removing permissions not present is idempotent (no error).
- Remove: removing all permissions auto-revokes the token.
- Remove: empty permissions array or absent field returns HTTP 400 `"permissions are required"`.
- Remove: no privilege escalation check on remove.
- Remove: unregistered permissions silently ignored.
- Remove: revoked token returns HTTP 400.
- Remove: expired token returns HTTP 400.
- Permission check: requires tokens:write or tokens:manage.
- Permission check: tokens:write alone is sufficient.
- Permission check: tokens:manage alone is sufficient.
- Permission check: PAT without either permission returns HTTP 403.

### Integration Tests

- Create a PAT, replace its permissions, verify via get.
- Create a PAT, add permissions, verify via get.
- Create a PAT, remove permissions, verify via get.
- Create a PAT, remove all permissions, verify it is revoked.
- Create a PAT, replace with empty array `[]`, verify it is revoked and response contains `revoked_at`.
- Full lifecycle: create → add → replace → remove → verify state at each step.
- PAT caller modifies its own token: verify escalation rule — can keep or reduce, cannot add permissions it doesn't hold.

## Dependencies

| Spec | From Group | To Group | Relationship |
|------|-----------|----------|--------------|
| 09_pat_lifecycle | 2 | 1 | PATHandler struct, PATResponse type, `RegisterRoutes` (modified in-place by this spec), request validation helpers |
| 05_auth_middleware | 2 | 1 | RequirePermission, GetAuthInfo, GetUserID, PermissionRegistry |
| 15_cli_user_commands | 3 | 3 | CLI token command registration pattern, `parsePermissions` helper, `DoRequest` HTTP call pattern |

## Clarifications

1. **API endpoint design:** Three distinct HTTP methods on the same path
   `/user/tokens/:token_id/permissions` — PUT (replace), PATCH (add),
   DELETE (remove).

2. **Revoked/expired tokens:** Cannot be modified. HTTP 400 with descriptive
   message.

3. **Empty permissions after remove:** Token is auto-revoked (sets
   `revoked_at`).

4. **Empty permissions on replace (definitive):** Sending `[]` triggers
   auto-revocation and returns HTTP 200 with the revoked `PATResponse`. An
   absent or null `permissions` field returns HTTP 400
   `"permissions are required"`. The prior contradiction in the spec is
   resolved: for PUT only, `[]` is valid input that triggers auto-revocation,
   not a 400 error.

5. **Privilege escalation:** Same rules as create. Only applies to PAT
   callers on replace/add. Does not apply to remove. A PAT caller may modify
   their own token; keeping existing permissions is always allowed and never
   triggers the escalation guard.

6. **Permission validation on remove:** Unregistered permissions are silently
   ignored. Format validation (colon check) still applies.

7. **Response format:** All three return `PATResponse` (HTTP 200).

8. **New permission:** `tokens:write` is added as a built-in permission (7th
   built-in total). It is a strict subset of `tokens:manage`: `tokens:manage`
   grants create, revoke, and in-place modification; `tokens:write` grants
   in-place modification only. PATs need either `tokens:manage` or
   `tokens:write` for the three new endpoints.

9. **OpenAPI, Go SDK, Python SDK:** Out of scope. A separate spec or PR will
   update `api/openapi.yaml`. The CLI uses `DoRequest` directly rather than
   the Go SDK, so no SDK updates are required for these endpoints.

10. **Route registration:** The three new handler methods are added directly
    to `PATHandler.RegisterRoutes` in `pat_lifecycle` (in-place modification).
    No separate registration call or extension hook is introduced.

11. **ETag / optimistic concurrency:** Not applicable. The existing PAT
    endpoints do not use ETags; last-write-wins is acceptable here. SQLite's
    serialized write mode within `db.WithTx` provides sufficient safety for
    concurrent writes — no additional locking is required.

12. **Database schema:** The `pats` table stores permissions as a `TEXT`
    column containing a JSON array (e.g., `'["users:read","orgs:read"]'`).
    Confirmed from `internal/db/schema.go`. No join table is used.

13. **No maximum permissions per token:** Any combination of registered
    permissions is valid. With 7 built-in permissions in the registry, growth
    is naturally bounded.

14. **CLI revocation warning asymmetry:** Only `akc tokens replace` and
    `akc tokens remove` print the stderr revocation warning. `akc tokens add`
    does not include this check because adding permissions can never produce an
    empty set.

15. **`parsePermissions` helper:** The CLI reuses the existing
    `parsePermissions` comma-split helper from `tokens.go`, which splits on
    commas and trims whitespace — the same behavior as `akc tokens create`.
