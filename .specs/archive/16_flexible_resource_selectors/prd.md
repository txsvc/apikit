---
spec_id: '16'
spec_name: flexible_resource_selectors
title: Flexible Resource Selectors
status: draft
created_at: '2026-07-29T05:38:21.289705+00:00'
updated_at: '2026-07-29T05:39:34.794326+00:00'
owner: Michael Kuehl
source: interactive
schema_version: 1
---
# Flexible Resource Selectors

## Problem

Every API endpoint and CLI command that operates on a user or organization requires the caller to supply a UUID. UUIDs are opaque, hard to remember, and require a prior lookup step. Operators and CLI users typically know an organization by its slug or a user by their username or email — not by UUID.

## Goal

Allow the API `{id}` path parameter (and CLI positional arguments) for user and organization resources to accept human-friendly alternatives in addition to UUIDs:

- **Organizations:** UUID or slug
- **Users:** UUID, username, or email

The server detects the selector type automatically and resolves it to the canonical UUID internally. No new endpoints, no new query parameters, no CLI flags. The change is fully backward-compatible: existing UUID-based calls continue to work unchanged.

## Non-Goals

- Token and key selector resolution — token IDs and key IDs are already short alphanumeric strings that need no friendlier alternatives.
- Self-service endpoints (`/user`, `/user/orgs`, `/user/tokens/{token_id}`) — these derive the user from the auth context, not a path parameter.
- Adding new API endpoints or query parameters.
- Python SDK updates — the `python_sdk` spec will handle any parameter-name or documentation changes separately if needed; this spec is Go-only.
- Caching or performance optimisation of resolver queries — SQLite is process-local and the resolver columns are uniquely indexed, making queries sub-millisecond without a cache layer.
- Latency budgets — no SLA is imposed on the extra resolver DB round-trip.

## Background

The apikit project exposes a REST API (defined in `api/openapi.yaml`) for managing users, organizations, API keys, and tokens. Specs 07–15 (user_management, organization_management, openapi_specification, cli_admin_commands, cli_user_commands, go_sdk, database_layer, cli_core, and related lifecycle specs) are all fully implemented. Every handler and SDK method that targets a specific user or organization currently requires the caller to supply the exact UUID.

Operators interacting via the CLI or API typically know resources by human-readable identifiers (username, email, org slug). The missing lookup step is a persistent friction point. This spec adds a cross-cutting resolver layer on top of the existing implemented handlers, SDK, CLI, and database without changing any of their external contracts.

## Scope

### In scope

- All API endpoints with `{id}` path parameters for users and orgs (admin and user-facing)
- Two-ID endpoints (e.g., `/orgs/{id}/members/{user_id}`) — both IDs accept selectors
- SDK methods that accept user/org ID strings
- CLI commands that pass positional ID arguments
- OpenAPI specification updates
- Database schema migration: add UNIQUE constraint on `users.email`

### Out of scope

See **Non-Goals** above.

## Dependencies

This spec adds a cross-cutting layer on top of the following fully-implemented active specs. Changes produced by this spec modify artifacts owned by each dependency:

| Spec ID | Spec Name | Artifacts modified |
|---------|-----------|-------------------|
| database_layer | Database Layer | Schema migration (`idx_users_email` unique index), startup migration failure handling |
| user_management | User Management | Handler files in `internal/handlers/users.go` — inline UUID parsing replaced by `resolveUserID` |
| organization_management | Organization Management | Handler files in `internal/handlers/orgs.go` — inline UUID parsing replaced by `resolveOrgID` |
| openapi_specification | OpenAPI Specification | `api/openapi.yaml` — parameter descriptions and `format: uuid` removal |
| go_sdk | Go SDK | Parameter documentation updates (no signature changes) |
| cli_admin_commands | CLI Admin Commands | `Use:` strings and `Long:` help text for admin commands |
| cli_user_commands | CLI User Commands | `Use:` strings and `Long:` help text for user commands |

The `cli_resolve.go` file (root `apikit` package, containing `resolveOrgSlugFromJSON`) is **not** owned by any existing spec — it is ad-hoc code that can be deleted freely as part of this spec.

## Detection Heuristic

The server determines the selector type by inspecting the path parameter value:

1. **UUID**: the value parses successfully with `uuid.Parse()` → query by `id` column
2. **Email** (users only): the value contains `@` → query by `email` column
3. **Fallback**: for users → query by `username`; for orgs → query by `slug`

This heuristic is unambiguous:
- UUIDs have a rigid format (8-4-4-4-12 hex with dashes) that cannot collide with slugs or usernames
- The `@` character is not valid in UUIDs or typical usernames/slugs
- The fallback (username or slug) is a catch-all for any remaining string

## Design

### Resolver functions

Introduce two resolver helper functions used by all handlers:

- `resolveUserID(db Executor, selector string) (string, error)` — returns the user's UUID given a UUID, username, or email. Returns `sql.ErrNoRows` if not found.
- `resolveOrgID(db Executor, selector string) (string, error)` — returns the org's UUID given a UUID or slug. Returns `sql.ErrNoRows` if not found.

Both functions live in `internal/handlers/` alongside the handler files that use them, sharing the `db Executor` interface already available in that package.

Each handler replaces its current inline `uuid.Parse()` + `WHERE id = ?` pattern with a call to the resolver. The resolver returns the canonical UUID, which the handler uses for all subsequent queries (member lookups, key/token queries, etc.) exactly as before.

### Handler changes

In each handler that currently does:

```go
id := c.Param("id")
if _, err := uuid.Parse(id); err != nil {
    return WriteAPIError(c, 400, "invalid user id")
}
// ... WHERE id = ?
```

Replace with:

```go
id, err := resolveUserID(h.db, c.Param("id"))
if err != nil {
    if err == sql.ErrNoRows {
        return WriteAPIError(c, 404, "user not found")
    }
    return WriteAPIError(c, 500, "internal server error")
}
// ... WHERE id = ? (id is now always a UUID)
```

For endpoints with two path parameters (e.g., `/orgs/:id/members/:user_id`), both parameters go through their respective resolvers.

### Database migration

Add a UNIQUE constraint on `users.email`:

```sql
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email);
```

This enables email to serve as an unambiguous selector. The schema creation statement in `schema.go` must also be updated for new databases.

**Migration failure behavior:** The migration runs at server startup. If the `CREATE UNIQUE INDEX` statement fails because pre-existing rows contain duplicate email values, the server must:

1. Log an error message that lists all offending email addresses (i.e., emails appearing more than once in the `users` table).
2. Refuse to start with a non-zero exit code.

The operator must deduplicate rows manually before restarting the server. No automatic deduplication is performed; the server will never silently start with a corrupted unique constraint. Example startup log output:

```
FATAL migration failed: duplicate emails detected — deduplicate before restarting:
  alice@example.com (3 rows)
  bob@example.com (2 rows)
```

### SDK changes

No signature changes are needed. The SDK methods already accept `string` parameters (e.g., `GetUserByID(ctx, userID string)`). Since the server now resolves selectors transparently, callers can pass a UUID, username, email, or slug and it works. Documentation and parameter names should be updated to reflect this (e.g., rename `userID` to `user` or document that it accepts selectors).

### CLI changes

No functional changes needed in CLI code — it already passes positional arguments as raw strings to the SDK/API. Update:

- `Use:` strings in cobra commands (e.g., `show <id>` → `show <id|slug>`)
- Help text / `Long:` descriptions to document accepted selector formats
- Remove the client-side `resolveOrgSlugFromJSON` function and its tests from `cli_resolve.go` in the root `apikit` package (now redundant; not owned by any other spec)

### OpenAPI specification

Update `api/openapi.yaml`:

- Change `{id}` parameter descriptions to document accepted selectors
- Remove `format: uuid` from user and org `{id}` parameters (the value is no longer guaranteed to be a UUID)
- Remove `format: uuid` from `{user_id}` in org member endpoints and update descriptions accordingly

## Testing Strategy

All new logic is tested using the **Go standard `testing` package and `testify`**, consistent with existing specs.

### Unit tests — `internal/handlers/` (same package as resolver functions)

File: `internal/handlers/resolve_test.go`

| Test case | What is verified |
|-----------|-----------------|
| UUID selector — valid UUID → returns UUID unchanged | Detection heuristic path 1 |
| Email selector — string containing `@` → resolves by email column | Detection heuristic path 2 |
| Fallback selector — plain string, no `@`, not UUID format → resolves by username / slug | Detection heuristic path 3 |
| Unknown email → `sql.ErrNoRows` | Not-found error contract |
| Unknown username → `sql.ErrNoRows` | Not-found error contract |
| Unknown slug → `sql.ErrNoRows` | Not-found error contract |
| Valid UUID that does not exist in DB → `sql.ErrNoRows` | Not-found error contract |
| Email selector on org resolver (must not be valid) → fallback to slug | Org resolver ignores `@` heuristic |
| Duplicate email guard (pre-migration data) | Migration error path |

### Integration tests — HTTP-level

Integration tests exercise the full flow via HTTP requests against an in-memory SQLite test database (using `database_layer`'s `OpenMemory` helper).

File: `internal/handlers/resolve_integration_test.go`

Cover each endpoint family (users, orgs, two-ID member endpoints) with:
- UUID selector → 200 OK
- Username / slug selector → 200 OK
- Email selector (user endpoints) → 200 OK
- Unknown selector → 404
- Malformed selector that cannot resolve → 404

### Migration tests

File: `internal/db/migrate_test.go` (consistent with `database_layer` conventions)

| Test case | What is verified |
|-----------|-----------------|
| Clean schema — `CREATE UNIQUE INDEX` succeeds | Happy path |
| Schema with duplicate emails — migration logs offending addresses and returns error | Failure behavior |
| Schema with unique emails — index created, server proceeds | No false positives |

## Affected Endpoints

### User endpoints (`:id` resolves via `resolveUserID`)

| Endpoint | Handler file |
|----------|-------------|
| `GET /users/{id}` | `internal/handlers/users.go` |
| `PATCH /users/{id}` | `internal/handlers/users.go` |
| `POST /users/{id}/promote` | `internal/handlers/users.go` |
| `POST /users/{id}/demote` | `internal/handlers/users.go` |
| `POST /users/{id}/block` | `internal/handlers/users.go` |
| `POST /users/{id}/unblock` | `internal/handlers/users.go` |
| `GET /users/{id}/keys` | `internal/handlers/users.go` |
| `DELETE /users/{id}/keys/{key_id}` | `internal/handlers/users.go` |
| `GET /users/{id}/tokens` | `internal/handlers/users.go` |
| `DELETE /users/{id}/tokens/{token_id}` | `internal/handlers/users.go` |

### Organization endpoints (`:id` resolves via `resolveOrgID`)

| Endpoint | Handler file |
|----------|-------------|
| `GET /orgs/{id}` | `internal/handlers/orgs.go` |
| `PATCH /orgs/{id}` | `internal/handlers/orgs.go` |
| `DELETE /orgs/{id}` | `internal/handlers/orgs.go` |
| `POST /orgs/{id}/block` | `internal/handlers/orgs.go` |
| `POST /orgs/{id}/unblock` | `internal/handlers/orgs.go` |
| `GET /orgs/{id}/members` | `internal/handlers/orgs.go` |
| `PUT /orgs/{id}/members/{user_id}` | `internal/handlers/orgs.go` |
| `DELETE /orgs/{id}/members/{user_id}` | `internal/handlers/orgs.go` |

### Two-ID endpoints (both parameters resolve)

| Endpoint | `:id` resolver | `:user_id` resolver |
|----------|---------------|-------------------|
| `PUT /orgs/{id}/members/{user_id}` | `resolveOrgID` | `resolveUserID` |
| `DELETE /orgs/{id}/members/{user_id}` | `resolveOrgID` | `resolveUserID` |

## Affected CLI Commands

All 21 commands listed in the project analysis that take positional ID arguments. Their `Use:` strings and help text are updated; no functional code changes needed since the server resolves selectors.

## Error Handling

| Scenario | HTTP Status | Message |
|----------|-------------|---------|
| Selector matches no record | 404 | `"user not found"` / `"organization not found"` |
| Database error during resolution | 500 | `"internal server error"` |
| Email selector matches multiple users | Should not occur after UNIQUE constraint; if legacy duplicate emails exist, the migration fails at startup and lists offending addresses — operator must deduplicate before the server will start |

## Backward Compatibility

- **Fully backward-compatible.** Existing UUID-based API calls continue to work identically.
- **SDK:** No breaking changes. String parameters still accepted; callers can now also pass slugs/usernames/emails.
- **CLI:** Existing scripts using UUIDs continue to work.

## Tech Stack

- Go 1.25+
- Echo web framework
- SQLite (modernc.org/sqlite)
- google/uuid package
- Test tooling: Go standard `testing` package + `testify` (consistent with existing specs)

## Design Decisions

1. **Email gets a UNIQUE constraint.** Without it, email cannot serve as an unambiguous selector. A schema migration adds a unique index. If existing data has duplicate emails, the migration fails at startup and logs all offending addresses; the operator must resolve duplicates manually before the server will start.

2. **Both IDs in two-ID commands accept selectors.** For commands like `akc admin orgs members add <org_id> <user_id>`, both arguments benefit from human-friendly selectors.

3. **Auto-detect approach.** The server detects selector type from the value itself (UUID format vs. `@` for email vs. fallback). No new endpoints, query parameters, or CLI flags needed.

4. **Token/key commands excluded.** Token IDs and key IDs are already short 8-character strings that don't need friendlier alternatives.

5. **Client-side resolution removed.** The existing `resolveOrgSlugFromJSON` function becomes redundant once the server resolves selectors natively. It is not owned by any active spec and can be deleted freely from `cli_resolve.go` along with its tests.

6. **Python SDK out of scope.** The `python_sdk` spec will handle any corresponding documentation or parameter-name updates separately if required.

7. **No caching layer needed.** Resolver queries target uniquely-indexed columns (`id`, `username`, `email`, `slug`) in a process-local SQLite database. Round-trip cost is sub-millisecond; no performance budget is imposed.

8. **Explicit dependency declarations.** Because this spec modifies artifacts owned by seven other active specs, a formal `## Dependencies` section lists each affected spec and which artifacts it touches — preventing conflicting or duplicate changes during artifact generation.
