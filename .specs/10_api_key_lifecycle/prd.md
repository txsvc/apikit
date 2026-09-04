---
spec_id: '10'
spec_name: api_key_lifecycle
title: Api Key Lifecycle
status: draft
created_at: '2026-07-17T11:29:48.516510+00:00'
updated_at: '2026-07-17T11:57:48.856243+00:00'
owner: ''
source: interactive
schema_version: 1
---
# API Key Lifecycle

## Source

This spec is derived from the [apikit master PRD](docs/PRD.md). It covers the
**API Key Lifecycle** component — spec 09 of 15. The master PRD sections on
"API Key Lifecycle" (under Functional Requirements), the `api_keys` database
schema, and the `/user/keys` authenticated user endpoints are the primary
sources.

## Intent

Implement the API key generation library and the three authenticated REST
endpoints that let a user list, refresh, and revoke their API keys. The
generation function is also consumed by the `oauth_provider_registry` spec
(spec 06) during the OAuth callback flow, making this package the single
source of truth for key format, random generation, hashing, and expiry
calculation.

Each user has **one active API key** at a time. A new OAuth login generates a
fresh key and revokes the previous one (handled by spec 06's callback handler,
which calls the generation function defined here). The endpoints in this spec
allow the authenticated user to inspect, refresh, or revoke their own key
without re-authenticating through OAuth.

## Goals

- Define the canonical API key format: `<prefix>_<key_id>_<secret>` where
  `key_id` is a random 8-character alphanumeric string and `secret` is a
  random 32-character alphanumeric string.
- Implement a key generation function (`GenerateAPIKey`) using `crypto/rand`
  that produces a new key, computes the SHA-256 hash of the secret, and
  calculates `expires_at` from a given expiry duration.
- Expose the generation function for use by `oauth_provider_registry` (spec 06)
  during the OAuth callback flow.
- Implement `GET /api/v1/user/keys` — list the authenticated user's API key(s),
  returning metadata only (`key_id`, `created_at`, `expires_at`, `revoked_at`).
  Never return the plaintext secret. Returns an empty JSON array `[]` with
  HTTP 200 when the user has no keys.
- Implement `POST /api/v1/user/keys/:key_id/refresh` — generate a new secret for an
  existing key (same `key_id`), reset `expires_at` based on the original
  `expires_days`, update `secret_hash` and `created_at`. Return the full key
  with the new plaintext secret.
- Implement `DELETE /api/v1/user/keys/:key_id` — permanently revoke a key by setting
  `revoked_at`. Expired-but-not-revoked keys may also be explicitly revoked
  via this endpoint. The user must re-login via OAuth to obtain a new key.
- Enforce the one-active-key-per-user invariant: revoke any existing active key
  (including expired-but-not-explicitly-revoked keys) before inserting a new
  one during generation.
- Support expiry values of `0` (indefinite), `30`, `60`, or `90` days with
  a default of `90`. Calculate `expires_at` as `created_at + (expires_days * 24h)`.
  Store `expires_at` as `null` when `expires_days` is `0`.
- Store only the SHA-256 hash of the secret; return the plaintext secret only
  at creation (via `GenerateAPIKey`) and on refresh.
- Allow expired keys to remain visible in listings for audit purposes.
- Prevent refresh and delete operations on keys that do not belong to the
  authenticated user.
- Emit structured log entries at INFO level for key lifecycle events (generation,
  refresh, revocation), including `user_id` and `key_id` fields.

## Non-Goals

- **Admin key management endpoints.** Admin endpoints for viewing and revoking
  other users' keys (`GET /users/:id/keys`, `DELETE /users/:id/keys/:key_id`)
  are covered by the `user_management` spec (active). That spec is the
  confirmed home for admin-scoped key operations.
- **OAuth login flow.** Key creation triggered by OAuth login is handled by
  `oauth_provider_registry` (spec 06), which calls this spec's `GenerateAPIKey`
  function.
- **Auth middleware.** Token parsing, validation, and request context injection
  are covered by `auth_middleware` (spec 05).
- **PAT management.** Personal access token lifecycle is a separate spec.
- **Key format parsing for authentication.** The auth middleware (spec 05)
  handles parsing `<prefix>_<key_id>_<secret>` from the `Authorization` header.
  This spec only generates and manages keys.
- **Rate limiting on key endpoints.**
- **Pagination of key listings.** One key per user means pagination is
  unnecessary. Historical revoked/expired keys accumulate indefinitely for
  audit purposes (no row cap); pagination is not warranted at this scale.
- **Additional public re-exports beyond `APIKeyResult` and `GenerateAPIKey`.**
  Custom error types (e.g., `ErrKeyRevoked`, `ErrKeyExpired`), constants, and
  other internal types are not re-exported through the root `apikit` package.
  Only `APIKeyResult` and `GenerateAPIKey` form the public API surface.

## Dependencies

| Spec | Dependency | Relationship |
|------|-----------|--------------|
| `01_server_core` | Upstream | Registers key handlers on the Echo group returned by `APIGroup()`. Uses `APIError()` for error responses. Uses `NowUTC()` and `FormatUTC()` for timestamp handling. Uses `TokenPrefix` for key format construction. Uses `CacheMiddleware(CacheNoStore)` (inherited from mount point group default). Uses `SetETag()` / `CheckETag()` for conditional GET on key listings. Uses the structured logger from spec 01 via `c.Logger()` per request for INFO-level lifecycle events. |
| `02_database_layer` | Upstream | Queries and mutates the `api_keys` table via `db.SqlDB`. Uses `db.FormatTime()` for timestamp storage. Uses `db.WrapError()` for error mapping. Uses `db.WithTx` for transaction management in integration tests. The `db.Executor` interface definition and its method set (`ExecContext`, `QueryContext`, `QueryRowContext`) are defined in spec 02 — implementers must consult spec 02 for the full interface contract. |
| `05_auth_middleware` | Upstream | All three endpoints require authentication. The auth middleware injects the authenticated user's ID and credential metadata (including the credential type flag that distinguishes API key vs. PAT) into the request context. Handlers read the user ID from context to scope queries to the authenticated user. The PAT permission system (`keys:read`, `keys:manage` scopes), the credential-type context key, and the context accessor functions are defined in `auth_middleware` (spec 05) and the `pat_lifecycle` spec — this spec consumes those context values but does not define them. Implementers must consult spec 05 for the concrete context key/accessor used to detect PAT vs. API key authentication. |
| `09_user_management` | Related | The `user_management` spec is the confirmed home for admin-scoped key endpoints (`GET /users/:id/keys`, `DELETE /users/:id/keys/:key_id`). No runtime dependency exists between the two specs; the relationship is a scope boundary clarification. |

> **Note on `oauth_provider_registry` (spec 06):** Spec 06 is a **downstream
> consumer** of this spec's `GenerateAPIKey` function — it calls the function
> during the OAuth callback flow. This spec does not depend on spec 06; the
> dependency flows in the other direction. However, the `GenerateAPIKey`
> function's signature and behavior are designed to serve spec 06's
> transactional callback flow (accepting a `db.Executor` for use within an
> existing transaction).

---

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25+ |
| HTTP framework | Echo v4 (`github.com/labstack/echo/v4`) |
| Token hashing | SHA-256 (stdlib `crypto/sha256`) |
| Random generation | `crypto/rand` (for key IDs and secrets) |
| Database | SQLite via `database/sql` (pure-Go driver from spec 02) |

## Repository Layout

```
internal/
  keys/                   API key generation and lifecycle
    generate.go           GenerateAPIKey function, random helpers
    handlers.go           GET /user/keys, POST .../refresh, DELETE .../revoke
```

The package is `internal/keys` and is not directly importable by consuming
projects. The `GenerateAPIKey` function is re-exported through the root
`apikit` package for use by `internal/oauth` (spec 06) and by consuming
projects that need programmatic key generation.

---

## Functional Requirements

### API Key Format

API keys follow the format `<prefix>_<key_id>_<secret>` where:

- `<prefix>` is the build-time configurable `apikit.TokenPrefix` (default `ak`).
- `<key_id>` is a random 8-character alphanumeric string (`[a-zA-Z0-9]`).
- `<secret>` is a random 32-character alphanumeric string (`[a-zA-Z0-9]`).

Both `key_id` and `secret` are generated using `crypto/rand` for
cryptographic security. The character set is the 62 alphanumeric characters
(`0-9`, `A-Z`, `a-z`). Generation uses rejection sampling or modular
arithmetic on random bytes to produce uniformly distributed characters from
this set. If `crypto/rand.Read()` fails (e.g., system entropy exhaustion),
the generation function returns an error immediately — a single failure is
immediately fatal with no retries. This surfaces as HTTP 500
`"internal server error"` to the caller — identical to the handling for
database errors.

Example key: `ak_aB3xK9mQ_7fG2hJ4kL6mN8pQ0rS2tU4vW6xY8zA0b`

### Key Generation Function

The `GenerateAPIKey` function is the single source of truth for creating API
keys. It is used by:

1. The OAuth callback handler (spec 06) during login — within an existing
   database transaction.
2. The refresh handler in this spec — to generate a new secret for an
   existing key.

```go
// APIKeyResult holds the output of a successful key generation.
type APIKeyResult struct {
    FullKey    string // The complete key: <prefix>_<key_id>_<secret>
    KeyID     string // The 8-char alphanumeric key_id
    SecretHash string // SHA-256 hex digest of the secret
    ExpiresAt *time.Time // Calculated expiry timestamp; nil when expiresDays is 0
}

// GenerateAPIKey creates a new API key for the given user. It:
// 1. Validates expiresDays (must be 0, 30, 60, or 90). Returns an error
//    immediately if the value is invalid — this is a function-level error,
//    not an HTTP response. Callers (e.g., spec 06's OAuth callback) are
//    responsible for passing a valid value; GenerateAPIKey is the enforcement
//    point. Any error propagates back to the caller as a standard Go error.
// 2. Detects whether tx is a *sql.DB or *sql.Tx via type assertion
//    (if _, ok := tx.(*sql.DB); ok { ... }). When called with *sql.DB,
//    begins an internal transaction to make the revoke+insert atomic.
//    When called with *sql.Tx (from the caller), participates in the
//    caller's existing transaction without opening a nested one.
// 3. Revokes any existing key for the user where revoked_at IS NULL
//    (this includes both active keys and expired-but-not-explicitly-revoked
//    keys), setting revoked_at = now. Zero rows affected is a no-op success;
//    no error is returned and execution proceeds to INSERT.
// 4. Generates a random 8-char key_id and 32-char secret using crypto/rand.
//    If crypto/rand.Read() fails, returns an error immediately (no retries,
//    no partial state is written to the database).
//    If the INSERT produces a unique constraint violation on key_id (an
//    astronomically unlikely but theoretically possible collision), the
//    function retries random generation up to 3 times before returning an
//    error. All retries occur within the same transaction.
// 5. Computes SHA-256 hash of the secret.
// 6. Calculates expires_at from expiresDays (nil when 0).
// 7. Inserts the new key record into api_keys.
// 8. Emits a structured INFO log entry via the provided logger with
//    user_id and key_id fields.
// 9. Returns the result including the plaintext full key.
//
// The tx parameter allows the function to participate in an existing
// transaction (used by the OAuth callback flow). Pass db.SqlDB when
// no transaction is active; the function will begin and commit its own
// internal transaction in that case.
//
// expiresDays must be 0, 30, 60, or 90. Any other value returns an error.
//
// The logger parameter is used to emit structured INFO entries. Handlers
// pass the Echo context logger (c.Logger()); the OAuth callback passes an
// appropriate logger from its own context.
func GenerateAPIKey(tx db.Executor, userID string, expiresDays int, logger echo.Logger) (*APIKeyResult, error)
```

The `db.Executor` interface (from spec 02) abstracts over `*sql.DB` and
`*sql.Tx`, allowing the function to work both standalone and within a
transaction. The interface definition and its full method set are specified
in spec 02; implementers must consult spec 02 for the complete contract.

#### Transaction Detection: Type Assertion

`GenerateAPIKey` determines whether to begin an internal transaction by
performing a type assertion on the `tx` parameter:

```go
if _, ok := tx.(*sql.DB); ok {
    // tx is a bare *sql.DB — begin an internal transaction
    // to make the revoke+insert atomic
} else {
    // tx is a *sql.Tx — execute within the caller's transaction
}
```

This is the idiomatic Go approach given that both `*sql.DB` and `*sql.Tx`
satisfy `db.Executor`. No wrapper type or additional interface is introduced.

#### Atomicity of Revoke + Insert

When `GenerateAPIKey` is called with a bare `*sql.DB` (detected via type
assertion as above), it internally begins a transaction to ensure the
revocation UPDATE and the INSERT are executed atomically. This means:

- If the INSERT fails after the UPDATE has executed, the transaction is rolled
  back and no key is revoked, leaving the user's existing key intact.
- If the UPDATE fails, the transaction is rolled back and no new key is inserted.
- There is no scenario where a user is left with their previous key revoked but
  no new key inserted, unless the caller provides a `*sql.Tx` that is
  subsequently rolled back by the caller.

When called with a `*sql.Tx` (e.g., from the OAuth callback flow in spec 06),
`GenerateAPIKey` does **not** begin a nested transaction — it executes the
UPDATE and INSERT within the caller's transaction, and the caller is responsible
for commit/rollback.

#### Revocation During Generation

Before inserting a new key, `GenerateAPIKey` revokes **all keys for the user
where `revoked_at IS NULL`** — this includes both currently active keys and
keys that have expired naturally (past `expires_at`) but were never explicitly
revoked. This ensures a clean slate and an unambiguous audit trail where every
key has an explicit `revoked_at` timestamp after a new key is generated.

The revocation query is:

```sql
UPDATE api_keys SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL
```

This query intentionally matches expired keys (where `expires_at < now` but
`revoked_at IS NULL`). Expired keys left with a `null` `revoked_at` would be
ambiguous in audit logs. Stamping them with `revoked_at` at generation time
makes their final disposition explicit.

**Zero rows affected** by this UPDATE is a silent no-op success (e.g., first-time
user with no prior keys). The implementation does not check or error on the
affected row count from the revocation UPDATE; it proceeds directly to the INSERT.

#### Unique Constraint Collision Retry

The `key_id` for a new key is generated randomly. In the astronomically
unlikely event that the INSERT fails due to a unique constraint violation on
`key_id` (as detected by `db.WrapError()` mapping the error to a known
constraint-violation sentinel — consult spec 02 for the exact sentinel type),
`GenerateAPIKey` retries random generation of a new `key_id` up to **3 times**
in total before returning an error. All retry attempts occur within the same
transaction (the revocation UPDATE is not re-executed on retry). If all
3 attempts fail due to constraint violations, the function returns an error,
which surfaces as HTTP 500 `"internal server error"` to the caller.

A `crypto/rand.Read()` failure during any retry attempt is immediately fatal —
no further retries are attempted.

#### Concurrency Safety

SQLite's serialized write mode is the relied-upon mechanism for enforcing the
one-active-key-per-user invariant under concurrent `GenerateAPIKey` calls for
the same user. SQLite serializes all write transactions, so concurrent OAuth
logins for the same user will execute the revocation UPDATE and the INSERT
sequentially rather than in parallel, preventing duplicate active keys. No
additional application-level locking or `SELECT FOR UPDATE` pattern is
required.

#### Expiry Calculation

- `expiresDays = 0`: `expires_at` is `NULL` (indefinite).
- `expiresDays = 30|60|90`: `expires_at = created_at + (expiresDays * 24h)`.
  Calculated as `time.Now().UTC().Add(time.Duration(expiresDays) * 24 * time.Hour)`.
- Any other value for `expiresDays` returns an error.

#### Secret Hashing

The secret is hashed using `crypto/sha256` from the standard library. The
hash is encoded as a lowercase hexadecimal string (64 characters) and stored
in the `api_keys.secret_hash` column. Example:

```go
hash := sha256.Sum256([]byte(secret))
hexHash := hex.EncodeToString(hash[:])
```

### Logger Injection

The `internal/keys` package does **not** use a package-level logger singleton.
Instead, the Echo context logger (`c.Logger()`) is used per-request in the
HTTP handlers. For `GenerateAPIKey`, the logger is passed explicitly as a
parameter (see the function signature above), allowing the OAuth callback
handler (spec 06) to supply its own logger. This keeps the package free of
global state and consistent with how other apikit packages obtain their loggers.

### Base Mount Point

All API key lifecycle endpoints are mounted under `/api/v1`. The concrete full
paths are:

| Endpoint | Full Path |
|----------|-----------|
| List keys | `GET /api/v1/user/keys` |
| Refresh key | `POST /api/v1/user/keys/:key_id/refresh` |
| Revoke key | `DELETE /api/v1/user/keys/:key_id` |

### `GET /api/v1/user/keys`

**Path:** `/api/v1/user/keys`
**Method:** GET
**Auth:** API key or PAT with `keys:read` permission (defined in `auth_middleware` spec 05 and `pat_lifecycle` spec)
**Cache-Control:** `no-store` (inherited from mount point group default)

Returns the authenticated user's API key(s) as a JSON array. Each entry
contains metadata only — the plaintext secret is **never** returned by this
endpoint.

**Response ordering:** Keys are returned ordered by `created_at DESC` (most
recently created or refreshed key first). This ordering is **part of the API
contract** — clients may rely on the first element of the array being the most
recently created or refreshed key.

**When the user has no keys:** Returns HTTP 200 with an empty JSON array `[]`.
No ETag is set and no `If-None-Match` matching is performed.

**Response (HTTP 200):**
```json
[
  {
    "key_id": "aB3xK9mQ",
    "created_at": "2026-07-17T14:30:00Z",
    "expires_at": "2026-10-15T14:30:00Z",
    "revoked_at": null
  }
]
```

**Response when user has no keys (HTTP 200):**
```json
[]
```

| Field | Type | Description |
|-------|------|-------------|
| `key_id` | string | The 8-character alphanumeric key identifier |
| `created_at` | string (RFC 3339 UTC) | When the key was created or last refreshed |
| `expires_at` | string or null | When the key expires; `null` for indefinite keys |
| `revoked_at` | string or null | When the key was revoked; `null` while active |

> **Note:** `expires_days` is an internal storage field used to recalculate
> `expires_at` on refresh. It is **not** exposed in any API response, including
> this listing. Clients see only the computed `expires_at` timestamp.

The response includes **all** keys for the user — both active and
revoked/expired — for audit visibility. In practice, a user has at most one
active key and zero or more revoked keys. **All historical key records are
retained indefinitely**; there is no cap on the number of revoked/expired keys
returned. This is an intentional design decision to support full audit trails
(see Design Decisions).

The endpoint supports ETag / If-None-Match conditional requests. The ETag is
derived from the most recent value of `MAX(created_at, revoked_at)` across all
of the user's keys. Specifically:
- For each key, the effective timestamp is `MAX(created_at, COALESCE(revoked_at, created_at))`.
- The ETag input is the maximum such value across all keys.
- This ensures that both key creation/refresh (which changes `created_at`) and
  key revocation (which changes `revoked_at`) invalidate the ETag, preventing
  clients from caching a stale `revoked_at: null` listing after a DELETE.
- The ETag SQL query returns this value as a string (SQLite's `MAX` over
  ISO-8601 formatted timestamp strings preserves lexicographic ordering). The
  result is scanned into a `string` (or `sql.NullString` to handle the
  no-keys case) and reformatted via `FormatUTC()` from spec 01 before being
  passed to `SetETag()`. This ensures consistent UTC formatting regardless
  of how SQLite returns the value.
- The exact encoding (quoting, W/ prefix) of the final ETag header value
  follows whatever convention `SetETag()` from spec 01 establishes.
- If no keys exist, no ETag is set and no `If-None-Match` matching is performed.
- A matching ETag results in HTTP 304 with an empty body, per standard HTTP
  semantics.

### `POST /api/v1/user/keys/:key_id/refresh`

**Path:** `/api/v1/user/keys/:key_id/refresh`
**Method:** POST
**Auth:** API key only (PAT authentication is **not** accepted)
**Cache-Control:** `no-store` (inherited)

Generates a new secret for an existing key. The `key_id` remains the same;
a new random 32-character secret is generated, and `expires_at` is
recalculated from the original `expires_days` value stored on the key record.
The `created_at` timestamp is updated to the current time (since the secret
is new).

> **Note:** Unlike `GET /api/v1/user/keys` (which accepts API key or PAT with
> `keys:read`) and `DELETE /api/v1/user/keys/:key_id` (which accepts API key or PAT
> with `keys:manage`), this endpoint deliberately restricts authentication to
> API keys only. A PAT should not be able to mint new key material — doing so
> would allow a lower-trust credential to bootstrap higher-trust access. The
> key being refreshed must be the caller's own active key.
>
> The handler detects PAT authentication by reading the credential-type context
> value injected by the auth middleware. The concrete context key/accessor for
> this value is defined in spec 05 (`auth_middleware`) — implementers must
> consult spec 05 for the exact accessor.

**Request body:** Empty (no body required). The `key_id` is taken from the
URL path.

**Path parameter handling:** No format validation is performed on the `key_id`
path parameter. If the value does not match any key in the database (regardless
of format), HTTP 404 `"key not found"` is returned. This applies uniformly to
malformed, wrong-length, or non-alphanumeric `key_id` values.

**Validation:**
1. The authenticated credential must be an API key (not a PAT).
   If a PAT is detected, return HTTP 401: `"API key authentication required"`.
2. The `key_id` in the path must exist in the `api_keys` table.
   If not found (including malformed values), return HTTP 404: `"key not found"`.
3. The key must belong to the authenticated user.
   If not, return HTTP 404: `"key not found"` (do not reveal existence to
   other users).
4. The key must not be revoked.
   If revoked, return HTTP 400: `"cannot refresh a revoked key"`.
5. The key must not be expired.
   If expired, return HTTP 400: `"cannot refresh an expired key"`.

**Processing:**
1. Generate a new 32-character alphanumeric secret using `crypto/rand`.
   If `crypto/rand.Read()` fails, return HTTP 500 `"internal server error"` immediately
   (single failure is fatal — no retries).
2. Compute the SHA-256 hash of the new secret.
3. Update the key record:
   - `secret_hash` = new hash
   - `created_at` = current UTC timestamp
   - `expires_at` = recalculated from `expires_days` (same rules as generation)
4. If the UPDATE affects 0 rows (e.g., due to a race condition between the
   ownership/validation check and the UPDATE — such as a concurrent DELETE),
   return HTTP 404 `"key not found"`. This is treated identically to a missing
   key, not as an internal error.
5. Construct the full key string: `<prefix>_<key_id>_<new_secret>`.
6. Emit a structured INFO log entry via `c.Logger()` with `user_id` and `key_id` fields.

**Response (HTTP 200):**
```json
{
  "key": "ak_aB3xK9mQ_7fG2hJ4kL6mN8pQ0rS2tU4vW6xY8zA0b",
  "key_id": "aB3xK9mQ",
  "expires_at": "2026-10-15T14:30:00Z"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `key` | string | The full key with the new plaintext secret (32-char alphanumeric secret portion) |
| `key_id` | string | The unchanged 8-character key identifier |
| `expires_at` | string or null | Recalculated expiry; `null` for indefinite keys |

This is the **only** time after initial creation that the plaintext secret is
available. The caller must store it; it cannot be retrieved later.

### `DELETE /api/v1/user/keys/:key_id`

**Path:** `/api/v1/user/keys/:key_id`
**Method:** DELETE
**Auth:** API key or PAT with `keys:manage` permission (defined in `auth_middleware` spec 05 and `pat_lifecycle` spec)
**Cache-Control:** `no-store` (inherited)

Permanently revokes the specified API key by setting `revoked_at` to the
current UTC timestamp. After revocation, the key can no longer authenticate
requests. The user must re-login via OAuth to obtain a new key.

**Path parameter handling:** No format validation is performed on the `key_id`
path parameter. If the value does not match any key in the database (regardless
of format), HTTP 404 `"key not found"` is returned.

**Validation:**
1. The `key_id` in the path must exist in the `api_keys` table.
   If not found (including malformed values), return HTTP 404: `"key not found"`.
2. The key must belong to the authenticated user.
   If not, return HTTP 404: `"key not found"`.
3. The key must not already be revoked.
   If already revoked, return HTTP 400: `"key is already revoked"`.

> **Note on expired keys:** Deleting (revoking) an expired-but-not-explicitly-revoked
> key is **permitted**. The DELETE endpoint sets `revoked_at` on the key, making its
> final disposition explicit in the audit trail. This is consistent with the behavior
> of `GenerateAPIKey`, which also stamps `revoked_at` on expired keys during new key
> generation. The only condition that blocks DELETE is an already-revoked key
> (`revoked_at IS NOT NULL`).

**Processing:**
1. Set `revoked_at` = current UTC timestamp on the key record.
2. Emit a structured INFO log entry via `c.Logger()` with `user_id` and `key_id` fields.

**Response (HTTP 200):**
```json
{
  "key_id": "aB3xK9mQ",
  "revoked_at": "2026-07-17T15:00:00Z"
}
```

Note: revoking the key the caller is currently authenticating with is allowed.
The current request completes successfully (the auth middleware already
validated the credential before the handler executed — the middleware does not
re-validate mid-flight; see spec 05 for the auth middleware's request-scoped
validation behavior). Subsequent requests with that key will be rejected by
the auth middleware.

### One-Active-Key Invariant

At any point in time, a user has **at most one active** (non-revoked,
non-expired) API key. This invariant is enforced by:

1. `GenerateAPIKey` revoking all keys where `revoked_at IS NULL` (including
   expired-but-not-explicitly-revoked keys) before inserting a new one, with
   the revoke + insert executed atomically (internal transaction when called
   with `*sql.DB`; participates in caller's transaction when called with `*sql.Tx`).
2. The refresh endpoint updating the existing key in-place (same `key_id`)
   rather than creating a new one.
3. The delete endpoint revoking the key permanently.
4. SQLite's serialized write mode preventing concurrent `GenerateAPIKey` calls
   for the same user from creating duplicate active keys.

Expired keys are not active but remain in the database for audit visibility.
Revoked keys remain in the database for audit visibility. Neither expired nor
revoked keys can authenticate requests.

### Random String Generation

Both `key_id` (8 characters) and `secret` (32 characters) are generated from
the alphanumeric character set `[0-9A-Za-z]` (62 characters) using
`crypto/rand`. The implementation:

1. Reads random bytes from `crypto/rand.Read()`. If `crypto/rand.Read()`
   returns an error, the generation function returns that error immediately
   with no retries. No partial state is written to the database. The caller
   surfaces this as HTTP 500 `"internal server error"` — identical to database
   error handling.
2. Maps each byte to a character in the alphanumeric set using modular
   arithmetic (`byte % 62`). While this introduces a negligible bias
   (256 is not evenly divisible by 62), the bias is approximately 0.8% per
   character and is acceptable for this use case. Alternatively, rejection
   sampling may be used for uniform distribution.
3. Returns the resulting string.

This function is a private helper within the `internal/keys` package, not
part of the public API.

### Logging

The `internal/keys` package emits structured log entries using the Echo context
logger (`c.Logger()`) per request. For `GenerateAPIKey`, the logger is passed
explicitly as a parameter. This avoids global state and is consistent with the
logging patterns in other apikit packages. Key lifecycle events are logged at
**INFO** level. Each log entry includes at minimum the `user_id` and `key_id` fields.

| Event | Level | Fields | Notes |
|-------|-------|--------|-------|
| Key generated | INFO | `user_id`, `key_id` | Logged by `GenerateAPIKey` after successful INSERT |
| Key refreshed | INFO | `user_id`, `key_id` | Logged by the refresh handler after successful UPDATE |
| Key revoked | INFO | `user_id`, `key_id` | Logged by the DELETE handler after successful UPDATE |

Logging occurs **after** a successful database write to avoid emitting log
entries for operations that are subsequently rolled back. No logging is
performed for failed operations; those surface as error responses to the caller
and may be logged at a higher level by the server core's request middleware.

---

## Interfaces

### Public API (root module re-exports)

The following types and functions are re-exported from `internal/keys`
through the root `apikit` package. **No other types, error values, or
constants from `internal/keys` are part of the public API.**

```go
// APIKeyResult holds the output of a successful key generation.
// Re-exported as apikit.APIKeyResult.
type APIKeyResult = keys.APIKeyResult

// GenerateAPIKey creates a new API key for the given user, revoking any
// existing active key. Re-exported as apikit.GenerateAPIKey.
// See the internal/keys package for full documentation.
func GenerateAPIKey(tx db.Executor, userID string, expiresDays int, logger echo.Logger) (*APIKeyResult, error) {
    return keys.GenerateAPIKey(tx, userID, expiresDays, logger)
}
```

Custom error types (e.g., `ErrKeyRevoked`, `ErrKeyExpired`), valid-value
constants for `expiresDays`, and other internal helpers remain unexported and
are not accessible to consuming projects. Consumers that need to create keys
programmatically call `apikit.GenerateAPIKey`; all error handling is done via
the returned `error` value.

### Handler Registration

```go
// RegisterKeyHandlers registers the API key lifecycle endpoints on the
// given Echo group. Called during server bootstrap with the concrete *sql.DB.
// Using *sql.DB (rather than db.Executor) is intentional: handler registration
// always occurs at startup with a known, concrete database handle. The
// db.Executor interface is only needed for GenerateAPIKey's transaction
// flexibility (used by the OAuth callback flow).
//
// The Cache-Control: no-store header is inherited from the mount point group
// default (applied by the parent Echo group via CacheMiddleware(CacheNoStore)).
// RegisterKeyHandlers does not apply any additional cache middleware.
//
// Endpoints registered (relative to /api/v1 mount point):
//   GET    /user/keys                   — list authenticated user's keys
//   POST   /user/keys/:key_id/refresh   — refresh a key (new secret)
//   DELETE /user/keys/:key_id           — revoke a key
//
// All endpoints require authentication (enforced by auth middleware applied
// to the parent group).
func RegisterKeyHandlers(group *echo.Group, database *sql.DB)
```

### Database Queries

The `internal/keys` package executes the following queries against the
`api_keys` table (schema defined in spec 02). The columns read and written by
this spec are: `key_id`, `user_id`, `secret_hash`, `expires_days`,
`expires_at`, `created_at`, `revoked_at`.

```sql
-- Revoke all keys for a user where revoked_at IS NULL
-- (covers both active keys and expired-but-not-explicitly-revoked keys)
-- Zero rows affected is a no-op success; no error is raised.
UPDATE api_keys SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL;

-- Insert a new key
INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- List all keys for a user (used by GET /api/v1/user/keys)
-- Returns all historical keys (no LIMIT); all records are retained indefinitely.
-- ORDER BY created_at DESC is part of the API contract; clients may rely on
-- the first element being the most recently created or refreshed key.
SELECT key_id, created_at, expires_at, revoked_at
FROM api_keys WHERE user_id = ? ORDER BY created_at DESC;

-- Derive the ETag input for GET /api/v1/user/keys
-- Returns the maximum effective timestamp across all of the user's keys,
-- where each key's effective timestamp is the later of created_at and revoked_at.
-- The result is scanned as a string (sql.NullString to handle the no-keys case)
-- and reformatted via FormatUTC() before being passed to SetETag().
-- Returns no rows / NULL when the user has no keys (no ETag is set in that case).
SELECT MAX(
    MAX(created_at, COALESCE(revoked_at, created_at))
) AS etag_ts
FROM api_keys WHERE user_id = ?;

-- Get a specific key (used by refresh and delete)
SELECT key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at
FROM api_keys WHERE key_id = ?;

-- Update key on refresh (new secret, new expiry, new created_at)
-- If 0 rows are affected (e.g., concurrent delete), the handler returns HTTP 404.
UPDATE api_keys SET secret_hash = ?, expires_at = ?, created_at = ?
WHERE key_id = ? AND user_id = ?;

-- Revoke a specific key
UPDATE api_keys SET revoked_at = ? WHERE key_id = ? AND user_id = ?;
```

> **Note:** `expires_days` is included in INSERT and the full SELECT (for
> refresh/delete logic) but is intentionally excluded from the listing SELECT
> and all API responses. It is an internal storage field, not a client-facing
> value.

#### ETag Timestamp Scanning

The ETag derivation query returns a single string value (SQLite stores
timestamps as ISO-8601 text; `MAX` over such strings is lexicographically
correct for UTC timestamps). The Go implementation scans the result as
`sql.NullString`:

- If `NullString.Valid` is `false` (user has no keys), no ETag is set and
  `If-None-Match` is not evaluated.
- If `NullString.Valid` is `true`, the string value is parsed and reformatted
  via `FormatUTC()` from spec 01 to produce a canonical UTC string, which is
  then passed to `SetETag()`. This ensures the ETag input is consistently
  formatted regardless of SQLite's internal representation.

---

## Error Handling

| Condition | HTTP Status | Error Message |
|-----------|-------------|---------------|
| Key not found (or belongs to another user, or malformed key_id) | 404 | `"key not found"` |
| Refresh UPDATE affects 0 rows (race condition after validation) | 404 | `"key not found"` |
| Attempt to refresh a revoked key | 400 | `"cannot refresh a revoked key"` |
| Attempt to refresh an expired key | 400 | `"cannot refresh an expired key"` |
| Attempt to revoke an already-revoked key | 400 | `"key is already revoked"` |
| PAT used to authenticate refresh endpoint | 401 | `"API key authentication required"` |
| `crypto/rand.Read()` failure during key/secret generation | 500 | `"internal server error"` |
| Database error | 500 | `"internal server error"` |
| `key_id` unique constraint violation (all retries exhausted) | 500 | `"internal server error"` |

All errors use the standard `APIError()` envelope from spec `01_server_core`.

> **Note on `expiresDays` validation:** An invalid `expiresDays` value (not 0, 30, 60,
> or 90) causes `GenerateAPIKey` to return an error at the **function level**, not as
> an HTTP response from this spec's endpoints. No endpoint in this spec accepts
> `expiresDays` as direct HTTP input — it is always read from the stored key record
> (for refresh) or passed programmatically by the caller (for generation via the OAuth
> flow). The OAuth callback handler (spec 06) is responsible for passing a valid value;
> `GenerateAPIKey` enforces the constraint and returns a Go error if violated. This
> error would propagate back to the OAuth callback handler as a standard Go error,
> where spec 06 determines how to surface it.

> **Malformed `key_id` path parameters** (wrong length, non-alphanumeric characters,
> etc.) are not validated at the application layer. They are passed directly to
> the database query, which returns no rows, resulting in HTTP 404 `"key not found"`.
> No format validation or HTTP 400 is returned for malformed path parameters.

---

## Testing Strategy

### Unit Tests

- **Random string generation:** verify that generated strings have the
  correct length (8 for key_id, 32 for secret) and contain only alphanumeric
  characters (`[0-9A-Za-z]`).
- **Random string uniqueness:** generate multiple strings and verify they are
  distinct (probabilistic but effectively guaranteed for `crypto/rand`).
- **Key format construction:** verify that the full key matches the pattern
  `<prefix>_<key_id>_<secret>` with the correct `TokenPrefix`. Verify the
  secret portion is exactly 32 characters.
- **SHA-256 hashing:** verify that the `secret_hash` in `APIKeyResult` is the
  correct lowercase hex SHA-256 digest of the secret extracted from `FullKey`.
- **Expiry calculation:** verify that `expiresDays = 0` produces `nil`
  `ExpiresAt`, and `expiresDays = 30|60|90` produces a timestamp exactly
  `N * 24h` in the future from creation.
- **Invalid expiry rejection:** verify that `expiresDays` values other than
  `0, 30, 60, 90` cause `GenerateAPIKey` to return a non-nil error (not an
  HTTP error — this is a function-level validation).
- **Refresh validation:** verify that refreshing a revoked key returns an
  error. Verify that refreshing an expired key returns an error.
- **Delete validation:** verify that revoking an already-revoked key returns
  an error.
- **Ownership check:** verify that operations on a key belonging to a
  different user return a not-found error (not a forbidden error, to avoid
  information leakage).

### Integration Tests

Integration tests that need an already-expired key (e.g., to exercise the
"cannot refresh an expired key" path) **directly write a past `expires_at`
value to the database** in the test setup (e.g., `UPDATE api_keys SET expires_at = '2020-01-01T00:00:00Z' WHERE key_id = ?`).
No clock injection interface is needed; the `internal/keys` package has no
clock abstraction — expiry checks compare the stored `expires_at` against
`time.Now().UTC()` at request time.

- **GenerateAPIKey (happy path):** call `GenerateAPIKey` with a valid user
  and `expiresDays = 90`. Verify:
  - The returned `FullKey` matches `<prefix>_<key_id>_<secret>`.
  - The `key_id` is 8 alphanumeric characters.
  - The secret portion is exactly 32 alphanumeric characters.
  - The `SecretHash` matches `sha256(secret)`.
  - A row exists in `api_keys` with the correct `key_id`, `user_id`,
    `secret_hash`, `expires_days`, and `expires_at`.
  - An INFO log entry with `user_id` and `key_id` fields is emitted.
- **GenerateAPIKey revokes previous key:** generate a key for a user, then
  generate another. Verify the first key's `revoked_at` is set and the
  second key is the only active key.
- **GenerateAPIKey revokes expired key:** insert a key with `expires_at` in
  the past and `revoked_at IS NULL`, then call `GenerateAPIKey`. Verify the
  expired key's `revoked_at` is now set (not left as `NULL`).
- **GenerateAPIKey — first-time user (no prior keys):** call `GenerateAPIKey`
  for a user with no existing keys. Verify that the zero-rows-affected
  revocation UPDATE is silently treated as success and the new key is inserted
  correctly.
- **GenerateAPIKey indefinite expiry:** call with `expiresDays = 0`. Verify
  `expires_at` is `NULL` in the database and `nil` in the result.
- **GenerateAPIKey atomicity — rollback on INSERT failure:** call
  `GenerateAPIKey` (with `*sql.DB`) in a scenario where the INSERT fails (e.g.,
  duplicate `key_id` injected via test setup to force a constraint violation on
  the first attempt, with no valid `key_id` available for retry). Verify that
  the revocation UPDATE is also rolled back (the pre-existing key's `revoked_at`
  remains `NULL`).
- **GenerateAPIKey — key_id collision retry:** inject a test scenario where
  the first INSERT attempt produces a unique constraint violation on `key_id`
  (e.g., by pre-inserting a row with a known `key_id` and seeding the random
  generator to produce that value first). Verify that `GenerateAPIKey` retries
  and succeeds on the second attempt. Verify that only one new active key exists
  after the call.
- **GenerateAPIKey within transaction:** call within a `db.WithTx` transaction
  (spec 02 helper), then commit. Verify the key is persisted. Call within a
  transaction and rollback. Verify no key is persisted.
- **GET /api/v1/user/keys (happy path):** create a key for a user, then GET
  `/api/v1/user/keys`. Verify HTTP 200 with a JSON array containing one entry with
  `key_id`, `created_at`, `expires_at`, and `revoked_at`. Verify the
  plaintext secret is **not** in the response. Verify `expires_days` is
  **not** in the response.
- **GET /api/v1/user/keys (ordering contract):** create a key, revoke it, create
  another. Verify the response contains the newer key as the first element
  (`created_at DESC` ordering), confirming the ordering is part of the API
  contract.
- **GET /api/v1/user/keys (multiple keys):** create a key, revoke it, create
  another. Verify both keys appear in the listing (one active, one revoked).
- **GET /api/v1/user/keys (no keys):** verify HTTP 200 with an empty JSON array
  `[]` (not `null`) when the user has no keys. Verify no `ETag` header is set.
- **GET /api/v1/user/keys (ETag — creation):** verify that the response includes an
  `ETag` header and that a subsequent request with `If-None-Match` returns HTTP 304
  with an empty body.
- **GET /api/v1/user/keys (ETag — revocation invalidation):** create a key, capture
  the ETag. Revoke the key via DELETE. Perform GET with the original `If-None-Match`.
  Verify HTTP 200 (not 304) is returned, confirming the revocation invalidated the
  cached ETag.
- **GET /api/v1/user/keys (ETag — sql.NullString null case):** verify that when the
  ETag query returns a NULL result (no keys), no ETag header is set and the
  response body is `[]`.
- **POST /api/v1/user/keys/:key_id/refresh (happy path):** create a key, then
  refresh it. Verify:
  - HTTP 200 with the new full key (same `key_id`, different 32-char secret).
  - The `secret_hash` in the database is updated.
  - The `expires_at` is recalculated from the original `expires_days`.
  - The `created_at` is updated to the current time.
  - The old secret no longer validates against `secret_hash`.
  - An INFO log entry with `user_id` and `key_id` fields is emitted.
- **POST /api/v1/user/keys/:key_id/refresh (indefinite key):** create a key with
  `expiresDays = 0`, refresh it. Verify `expires_at` remains `null`.
- **POST /api/v1/user/keys/:key_id/refresh (PAT rejected):** authenticate with a
  valid PAT that has `keys:read` or `keys:manage` permission and attempt to
  refresh. Verify HTTP 401 with `"API key authentication required"`.
- **Refresh revoked key:** revoke a key, then attempt to refresh. Verify
  HTTP 400 with `"cannot refresh a revoked key"`.
- **Refresh expired key:** create a key, then directly update `expires_at`
  to a past timestamp in the database. Attempt to refresh. Verify HTTP 400
  with `"cannot refresh an expired key"`.
- **Refresh nonexistent key:** attempt to refresh a `key_id` that does not
  exist. Verify HTTP 404 with `"key not found"`.
- **Refresh malformed key_id:** attempt to refresh with a `key_id` that is
  malformed (e.g., wrong length or non-alphanumeric characters). Verify HTTP 404
  with `"key not found"` (no format validation, DB query returns no rows).
- **Refresh another user's key:** create a key for user A, authenticate as
  user B, attempt to refresh. Verify HTTP 404 with `"key not found"`.
- **Refresh race condition (0-rows UPDATE):** validate ownership, then simulate
  a concurrent delete (directly zero the row in the database between the validation
  SELECT and the UPDATE, e.g., via a second DB connection in the test), then execute
  the UPDATE. Verify HTTP 404 with `"key not found"` (0 rows affected treated as
  missing key, not internal error).
- **DELETE /api/v1/user/keys/:key_id (happy path):** create a key, then delete it.
  Verify HTTP 200 with `key_id` and `revoked_at`. Verify `revoked_at` is set
  in the database. Verify an INFO log entry with `user_id` and `key_id` fields
  is emitted.
- **Delete already-revoked key:** revoke a key, then attempt to delete again.
  Verify HTTP 400 with `"key is already revoked"`.
- **Delete expired key:** create a key, directly update `expires_at` to a past
  timestamp in the database, then attempt DELETE. Verify HTTP 200 with `revoked_at`
  set (expired-but-not-revoked keys are deletable).
- **Delete nonexistent key:** attempt to delete a `key_id` that does not
  exist. Verify HTTP 404 with `"key not found"`.
- **Delete malformed key_id:** attempt to delete with a malformed `key_id`.
  Verify HTTP 404 with `"key not found"`.
- **Delete another user's key:** create a key for user A, authenticate as
  user B, attempt to delete. Verify HTTP 404 with `"key not found"`.
- **Self-revocation:** authenticate with an API key, then DELETE that key.
  Verify the current request returns HTTP 200 (the key was valid when the
  request arrived). Verify a subsequent request with the same key returns
  HTTP 401.
- **Cache-Control headers:** verify all three endpoints return
  `Cache-Control: no-store`.

---

## Design Decisions

- **`GenerateAPIKey` accepts `db.Executor` for transaction flexibility.** The
  OAuth callback flow (spec 06) creates keys inside a transaction that also
  upserts the user. Accepting `db.Executor` (which `*sql.DB` and `*sql.Tx`
  both satisfy) allows the function to participate in external transactions
  without forcing the caller to manage key generation details.
- **Transaction detection via type assertion (`*sql.DB` vs. `*sql.Tx`).** `GenerateAPIKey`
  uses `if _, ok := tx.(*sql.DB); ok { ... }` to determine whether to begin an
  internal transaction. This is idiomatic Go for this pattern and avoids
  introducing a wrapper type or additional interface solely for this purpose.
- **`GenerateAPIKey` wraps revoke+insert in an internal transaction when called with `*sql.DB`.**
  When no caller-provided transaction is active, `GenerateAPIKey` begins its own
  transaction to make the two-step operation (revoke all existing keys, insert new key)
  atomic. This guarantees the user is never left in a state where their old key is
  revoked but no new key exists due to a partial failure. When called with a `*sql.Tx`,
  the caller owns the transaction and is responsible for commit/rollback.
- **Zero rows affected by the revocation UPDATE is a no-op success.** For first-time
  users with no prior keys, the revocation UPDATE matches zero rows. This is silently
  treated as success — no error is returned, and execution proceeds to INSERT. This
  avoids an unnecessary existence check before the UPDATE.
- **Refresh UPDATE with 0 rows affected returns HTTP 404.** If the UPDATE in the
  refresh handler affects 0 rows (e.g., a concurrent DELETE between the ownership
  check and the UPDATE), the handler returns HTTP 404 `"key not found"`. This is
  consistent with the "key disappeared" semantic and avoids exposing an internal
  race condition as a 500.
- **`crypto/rand.Read()` failures are immediately fatal (no retries).** A single
  `crypto/rand.Read()` failure surfaces immediately as HTTP 500 `"internal server error"`.
  No retries are attempted. Entropy exhaustion is rare enough that a retry loop
  would add complexity for negligible benefit; the client can simply retry the
  request if needed.
- **`key_id` unique constraint collisions trigger bounded retries (up to 3).** While
  a `key_id` collision is astronomically unlikely with 8-char alphanumeric keys (62^8
  ≈ 218 trillion possibilities), `GenerateAPIKey` retries random generation up to
  3 times if `db.WrapError()` signals a unique constraint violation. All retries
  occur within the same transaction (the revocation UPDATE is not re-executed).
  After 3 failed attempts, the function returns an error surfaced as HTTP 500.
  All other database errors (non-constraint-violation) are treated as HTTP 500
  immediately without retry.
- **Historical key records retained indefinitely (no row cap).** All revoked and
  expired key records are retained in the `api_keys` table indefinitely for audit
  purposes. No pruning, archival, or row-limit strategy is applied. The listing
  endpoint (`GET /api/v1/user/keys`) returns all records with no LIMIT clause. Given
  typical OAuth refresh cadence, the number of historical keys per user is expected
  to remain small in practice.
- **`RegisterKeyHandlers` accepts `*sql.DB`, not `db.Executor`.** Handler
  registration always occurs at server startup with a concrete, long-lived
  `*sql.DB`. The `db.Executor` abstraction is only needed for `GenerateAPIKey`'s
  transaction flexibility (i.e., to be callable from the OAuth callback's
  transaction). Handlers internally use `db.Executor` for individual queries but
  receive the concrete DB at registration time, keeping the registration API
  simple and explicit.
- **Refresh updates in-place rather than create-and-revoke.** Refreshing a
  key keeps the same `key_id` and updates `secret_hash`, `created_at`, and
  `expires_at`. This preserves the user's `key_id` across refreshes, which
  is useful when the `key_id` is used as an identifier in logs or external
  systems. Creating a new key and revoking the old one would change the
  `key_id`, which is unnecessary for a secret rotation.
- **404 for ownership violations (not 403).** When a user attempts to
  operate on another user's key, the endpoint returns 404 ("key not found")
  rather than 403 ("forbidden"). This prevents information leakage — the
  caller cannot determine whether a `key_id` exists or belongs to someone
  else.
- **404 for malformed `key_id` path parameters (no format validation).** The
  `key_id` path parameter is passed directly to the database query without
  format validation. A malformed value returns no rows, resulting in HTTP 404.
  This avoids a separate validation step and treats malformed values identically
  to non-existent ones from the client's perspective.
- **Expired keys cannot be refreshed.** An expired key is effectively dead.
  The user must re-login via OAuth to get a new key. This simplifies the
  mental model and prevents stale keys from being silently reactivated.
- **Expired keys can be explicitly revoked via DELETE.** Unlike refresh,
  DELETE on an expired-but-not-revoked key is permitted. This is consistent
  with `GenerateAPIKey`'s behavior (which stamps `revoked_at` on expired keys)
  and makes audit trails explicit. The only condition that blocks DELETE is
  `revoked_at IS NOT NULL`.
- **ETag incorporates both `created_at` and `revoked_at`.** The ETag for
  `GET /api/v1/user/keys` is derived from the most recent value of
  `MAX(created_at, COALESCE(revoked_at, created_at))` across all user keys.
  This ensures that both key creation/refresh (changes `created_at`) and
  explicit revocation (changes `revoked_at`) invalidate any cached ETag,
  preventing clients from receiving a stale `revoked_at: null` listing after
  a DELETE.
- **ETag timestamp scanned as `sql.NullString` and reformatted via `FormatUTC()`.**
  The ETag derivation query result is scanned as `sql.NullString` to handle the
  no-keys case (NULL result → no ETag set). When valid, the string is reformatted
  via `FormatUTC()` from spec 01 before being passed to `SetETag()`, ensuring
  canonical UTC formatting. The exact ETag header encoding (quoting, W/ prefix)
  is determined by `SetETag()` from spec 01.
- **All keys visible in listings.** Revoked and expired keys appear in
  `GET /api/v1/user/keys` for audit visibility. The `revoked_at` and `expires_at`
  fields make their status unambiguous.
- **`created_at` updated on refresh.** When a key is refreshed, `created_at`
  is updated to the current time because the secret is new. This also serves
  as the base for `expires_at` recalculation, ensuring the expiry window
  resets from the refresh time, not the original creation time.
- **`ORDER BY created_at DESC` is part of the API contract.** The listing
  endpoint returns keys ordered by `created_at DESC`. This is a stable,
  documented contract — clients may rely on the first element being the most
  recently created or refreshed key. This ordering is preserved in the SQL
  query and must not be changed without a versioned API update.
- **`crypto/rand` with modular arithmetic.** The negligible bias from
  `byte % 62` is acceptable for key material of this length. The alternative
  (rejection sampling) would produce perfectly uniform output but adds
  complexity for no practical security gain given the 8-char and 32-char
  lengths.
- **Self-revocation is allowed.** A user can revoke the key they are
  currently authenticating with. The request completes because the auth
  middleware validated the key before the handler executed (the middleware
  does not re-validate mid-flight; see spec 05). This is consistent with
  GitHub's behavior where deleting a PAT while using it completes the
  deletion successfully.
- **Empty request body for refresh.** The refresh endpoint takes no body
  because all parameters are derived from the existing key record
  (`expires_days`) and the URL path (`key_id`). This follows the principle
  of minimal input.
- **PAT authentication rejected for refresh.** The refresh endpoint only
  accepts API key authentication. A PAT must not be able to mint new API key
  material, as this would allow a lower-trust, scoped credential to bootstrap
  a higher-trust credential. Accepting PATs for `GET` (read-only metadata) and
  `DELETE` (revocation reduces trust surface) is safe; accepting them for
  refresh (which produces a new secret) is not. The credential-type context
  value injected by spec 05's auth middleware is the mechanism for detecting
  this; implementers must consult spec 05 for the concrete accessor.
- **Admin key endpoints belong to `user_management`.** The `user_management`
  spec is the confirmed owner of admin-scoped key operations
  (`GET /users/:id/keys`, `DELETE /users/:id/keys/:key_id`). This spec
  covers only self-service key lifecycle for authenticated users.
- **Revoke expired keys on new generation.** `GenerateAPIKey` revokes all
  keys where `revoked_at IS NULL`, including expired ones. This ensures
  `revoked_at` is explicitly set for every superseded key, making audit logs
  unambiguous and preventing orphaned rows with a `null` `revoked_at` that
  could confuse future queries.
- **SQLite serialized writes for concurrency safety.** No application-level
  locking is added to `GenerateAPIKey`. SQLite's serialized write mode
  guarantees that concurrent calls for the same user execute sequentially,
  preserving the one-active-key invariant without additional complexity.
- **No clock injection interface.** The package uses `time.Now().UTC()`
  directly. Tests that require a key to appear expired write a past
  `expires_at` directly to the database rather than injecting a mock clock,
  keeping the implementation simple and avoiding a test-only interface.
- **Logger passed explicitly to `GenerateAPIKey`, obtained via `c.Logger()` in handlers.**
  The `internal/keys` package avoids a package-level logger singleton. HTTP handlers
  use `c.Logger()` (the Echo context logger) per request. `GenerateAPIKey` accepts
  an explicit `logger echo.Logger` parameter so the OAuth callback (spec 06) can
  supply its own logger. This is consistent with the logging patterns across other
  apikit packages and avoids global state.
- **`expiresDays` validation is a function-level concern, not an HTTP concern.**
  No endpoint in this spec accepts `expiresDays` as direct HTTP input. The validation
  inside `GenerateAPIKey` (must be 0, 30, 60, or 90) is enforced at the function
  level and propagates as a Go error to the caller. This error does not produce an
  HTTP 400 from this spec's handlers; the OAuth callback handler (spec 06) is
  responsible for surfacing it appropriately.
- **ETag format delegated to `SetETag()` helper.** The exact encoding
  (quoting, formatting) of the ETag value for `GET /api/v1/user/keys` is
  determined by the `SetETag()` helper from spec 01. The input to that helper
  is the derived effective timestamp (max of `created_at` / `revoked_at`),
  reformatted via `FormatUTC()` for canonical representation.
- **`GET /api/v1/user/keys` returns `[]` (not `null`) for empty results.** When
  a user has no keys, the endpoint returns HTTP 200 with an empty JSON array.
  Returning `null` would require special-case handling in clients. An empty
  array is the idiomatic JSON representation of an empty collection and is
  consistent with standard REST conventions.
- **Content-Type header relies on Echo framework defaults.** All JSON responses
  use Echo's built-in `c.JSON()` serializer, which sets `Content-Type: application/json`
  automatically. No explicit per-endpoint documentation of this header is required.
- **Logging after successful write only.** Structured INFO log entries are
  emitted only after a successful database write (not on error paths). Error
  conditions are handled via the APIError response; request-level logging by
  the server core middleware covers failed requests at the HTTP layer.
- **Only `APIKeyResult` and `GenerateAPIKey` are re-exported.** No error
  types, constants, or other internal helpers are part of the public API.
  This keeps the public surface minimal and avoids coupling consuming projects
  to internal implementation details.

---

## Glossary

| Term | Definition |
|------|------------|
| **API key** | A user-scoped credential in the format `<prefix>_<key_id>_<secret>`, issued via OAuth login. One active key per user. |
| **key_id** | The random 8-character alphanumeric identifier portion of an API key, stored in plaintext in the `api_keys` table. |
| **secret** | The random 32-character alphanumeric portion of an API key, stored only as a SHA-256 hash. The plaintext is returned once at creation and once on refresh. |
| **secret_hash** | The SHA-256 hex digest of the API key secret, stored in the `api_keys.secret_hash` column. |
| **expires_days** | The original expiry duration stored on the key record: `0` (indefinite), `30`, `60`, or `90` days. Used to recalculate `expires_at` on refresh. Not exposed in API responses. |
| **expires_at** | The calculated expiry timestamp for a key. `NULL` when `expires_days` is `0` (indefinite). Calculated as `created_at + (expires_days * 24h)`. |
| **revoked_at** | The timestamp when a key was permanently invalidated. `NULL` while the key has not been explicitly revoked (active or naturally expired but not yet superseded). |
| **active key** | A key that has not been revoked (`revoked_at IS NULL`) and has not expired (`expires_at IS NULL OR expires_at > now`). |
| **refresh** | The operation of generating a new secret for an existing key while preserving the `key_id`. Resets `expires_at` based on the original `expires_days`. Only permitted on non-revoked, non-expired keys. |
| **revoke** | Permanently invalidating a key by setting `revoked_at`. Irreversible. Permitted on active and expired (but not already-revoked) keys. |
| **GenerateAPIKey** | The function in `internal/keys` that creates a new API key, revokes all existing keys where `revoked_at IS NULL`, and inserts the new key record. Accepts an explicit `echo.Logger` parameter for structured logging. When called with `*sql.DB` (detected via type assertion), the revoke+insert is wrapped in an internal transaction for atomicity. Retries up to 3 times on `key_id` unique constraint violations. |
| **db.Executor** | An interface (from spec 02's database layer) satisfied by both `*sql.DB` and `*sql.Tx`, allowing functions to work with or without an explicit transaction. Full method signatures are defined in spec 02. |
| **db.WrapError** | A function from spec 02's database layer that maps raw SQLite errors to known sentinel types (e.g., unique constraint violations). Used by `GenerateAPIKey` to detect `key_id` collision errors eligible for retry. Full behavior is defined in spec 02. |
| **db.WithTx** | A helper function from spec 02's database layer that manages transaction lifecycle (begin, commit, rollback) for a given function. Used in integration tests for transaction rollback scenarios. |
| **TokenPrefix** | The build-time configurable prefix (default `ak`) used in all credential formats. |
| **mount point** | The base URL path `/api/v1` under which all API key lifecycle endpoints are registered. |
| **etag_ts** | The derived ETag input timestamp: `MAX(MAX(created_at, COALESCE(revoked_at, created_at)))` across all of the user's keys. Scanned as `sql.NullString`, reformatted via `FormatUTC()`, then passed to `SetETag()`. |

---

## Owner

Michael Kuehl
