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
