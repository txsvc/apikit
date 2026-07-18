# Errata: Spec 10 — ETag Computation and Import Cycle

## ETag Composite Key (10-REQ-5.4, 10-REQ-5.E2)

### Spec says

> computes `etag_ts` as `MAX(MAX(created_at, COALESCE(revoked_at, created_at)))` across all user keys, reformats it via `FormatUTC()`, passes it to `SetETag()`

### Divergence

The spec's ETag formula is based solely on timestamp values. Because
`db.FormatTime()` truncates to whole seconds, a revocation that occurs
within the same second as key creation produces the same `etag_ts` value,
violating the requirement that revoking a key must invalidate the prior
ETag (10-REQ-5.E2, TS-10-E8).

The implementation augments the ETag with a count of revoked keys:

```sql
SELECT MAX(MAX(created_at, COALESCE(revoked_at, created_at))),
       COUNT(CASE WHEN revoked_at IS NOT NULL THEN 1 END)
FROM api_keys WHERE user_id = ?
```

The ETag format is `W/"<timestamp>-<revokedCount>"` instead of
`W/"<timestamp>"`. This ensures that any state mutation (revocation,
refresh, or new key creation) changes the ETag, even within the same
truncated second.

### SetETag/CheckETag type mismatch

The spec prescribes scanning the ETag query result as `sql.NullString`,
reformatting via `FormatUTC()` (returns string), and passing to
`SetETag()`. However, `SetETag(c echo.Context, updatedAt time.Time)` and
`CheckETag()` accept `time.Time`, not `string`. The implementation parses
the scanned string back to `time.Time` via `db.ParseTime()` before
computing the ETag value.

## Import Cycle: internal/authctx Package

### Problem

`apikit` (root) imports `internal/keys` (for type alias + wrapper).
`internal/auth` imports `apikit` (for `WriteAPIError`, `TokenPrefix`).
Therefore `internal/keys` cannot import `internal/auth` — Go rejects
the cycle.

### Solution

A new `internal/authctx` package was created containing only the shared
authentication context types and helpers:

- `AuthInfo` struct
- `SetAuthInfo(c, info)` / `GetAuthInfo(c)` / `GetUserID(c)`
- The unexported context key type

`internal/auth/context.go` was refactored to delegate to `authctx`,
using `type AuthInfo = authctx.AuthInfo` (type alias) so all existing
code referencing `auth.AuthInfo` continues to work unchanged.

`internal/keys/handlers.go` imports `internal/authctx` directly to read
the authenticated user's ID.

## Cross-Spec Wiring: OAuth Callback (10-PATH-1, 10-REQ-3.1)

### Spec says

> 10-PATH-1: OAuth callback handler calls `apikit.GenerateAPIKey(tx, userID, 90, logger)` passing the active `*sql.Tx`
> 10-REQ-3.1: Consumers calling `apikit.GenerateAPIKey` receive identical behavior to calling `keys.GenerateAPIKey` directly

### Current state

The OAuth callback handler (`internal/oauth/handler.go`) does NOT call
`apikit.GenerateAPIKey`. It uses a local `GenerateAPIKey(tokenPrefix, expires)`
function in `internal/oauth/callback.go` with a different signature that:

- Does not accept a `db.Executor` or `logger` parameter
- Performs key material generation only (no database operations)
- Has its own revocation UPDATE inline in the handler, separate from the
  key generation function
- Does not include the key_id collision retry logic from `internal/keys`

The database operations (revocation UPDATE, INSERT) are inlined in the
OAuth callback handler transaction rather than delegated to the centralized
`GenerateAPIKey` function.

### Impact

The duplicate code path works correctly but bypasses the collision retry
logic (10-REQ-2.7) and the centralized structured logging (10-REQ-2.9).
This is an expected divergence: spec 06 was implemented before spec 10
established the centralized `GenerateAPIKey` function. A future
integration pass should wire the OAuth callback to call
`apikit.GenerateAPIKey(tx, userID, expires, logger)`.

## Cross-Spec Wiring: RegisterKeyHandlers Bootstrap (10-REQ-4.1)

### Spec says

> 10-REQ-4.1: `RegisterKeyHandlers` accepts `(group *echo.Group, database *sql.DB)` and registers three routes

### Current state

`RegisterKeyHandlers` is implemented and exported but is NOT called from
any server bootstrap or `main()` code path. The `Server.APIGroup()` method
exists to expose the API group, but no production code wires
`keys.RegisterKeyHandlers(apiGroup, db)`.

### Impact

The key lifecycle HTTP endpoints (list, refresh, revoke) are not registered
in the production server. They are only exercised in tests. A future
integration pass should add the `RegisterKeyHandlers` call to the server
bootstrap sequence after auth middleware is applied to the API group.

## WriteAPIError Import Cycle Avoidance (10-REQ-5.E3, 10-REQ-6, 10-REQ-7)

### Divergence

`internal/keys/handlers.go` cannot import the root `apikit` package (to
call `apikit.WriteAPIError` / `apikit.APIError`) because that would create
an import cycle (`apikit` → `internal/keys` → `apikit`). Instead, a local
`writeAPIError` function is inlined in `handlers.go` that produces the
identical JSON error envelope `{"error": {"code": N, "message": "..."}}`.
The behavior is equivalent to `APIError` from spec 01.
