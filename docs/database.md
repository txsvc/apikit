# Database Layer Reference

## Overview

apikit uses an embedded SQLite database for all persistent storage. The database
layer lives in `internal/db/` and provides connection management, schema
initialization, transaction helpers, error mapping, and timestamp utilities.

Key characteristics:

- **Engine:** SQLite via `modernc.org/sqlite`, a pure-Go CGo-free implementation.
- **Journal mode:** WAL (Write-Ahead Logging) for file-based databases. In-memory
  databases skip WAL.
- **Foreign keys:** Enforced via `PRAGMA foreign_keys = ON` on every connection.
- **Connection pool:** Single connection (`MaxOpenConns(1)`, `MaxIdleConns(1)`),
  the standard SQLite best practice since SQLite does not support concurrent
  writers.

Source files:

| File | Purpose |
|------|---------|
| `internal/db/db.go` | `DB` struct, `Open`, `OpenMemory`, `initDB`, `Close`, `Ping`, `WithTx` |
| `internal/db/schema.go` | `schemaStatements` (six `CREATE TABLE` statements) |
| `internal/db/errors.go` | `ErrNotFound`, `ErrConflict`, `ErrDatabaseLocked`, `WrapError` |
| `internal/db/executor.go` | `Executor` interface |
| `internal/db/timestamp.go` | `TimeFormat`, `FormatTime`, `ParseTime` |

---

## Schema

Six tables are created in foreign-key dependency order inside a single DEFERRED
transaction. All statements use `CREATE TABLE IF NOT EXISTS` for idempotency.
Primary keys and foreign keys are `TEXT` (UUIDs or string identifiers). All
timestamps are `TEXT` columns stored in the `TimeFormat` layout.

### users

The root entity. Referenced by `api_keys`, `pats`, and `org_members`.

```sql
CREATE TABLE IF NOT EXISTS users (
    id          TEXT NOT NULL PRIMARY KEY,
    username    TEXT NOT NULL UNIQUE,
    email       TEXT NOT NULL,
    full_name   TEXT,
    role        TEXT NOT NULL DEFAULT 'user',
    status      TEXT NOT NULL DEFAULT 'active',
    provider    TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (provider, provider_id)
)
```

| Column | Type | Constraints | Notes |
|--------|------|-------------|-------|
| `id` | TEXT | NOT NULL PRIMARY KEY | UUID v4 |
| `username` | TEXT | NOT NULL UNIQUE | |
| `email` | TEXT | NOT NULL | |
| `full_name` | TEXT | nullable | Only user-updatable field |
| `role` | TEXT | NOT NULL DEFAULT 'user' | `admin` or `user` |
| `status` | TEXT | NOT NULL DEFAULT 'active' | `active` or `blocked` |
| `provider` | TEXT | NOT NULL | OAuth provider name (e.g. `github`) |
| `provider_id` | TEXT | NOT NULL | Provider-side unique user ID |
| `created_at` | TEXT | NOT NULL | `TimeFormat` timestamp |
| `updated_at` | TEXT | NOT NULL | `TimeFormat` timestamp |

Table-level constraint: `UNIQUE (provider, provider_id)` guarantees one user per
external identity.

### api_keys

Machine-to-machine API keys. Each belongs to one user.

```sql
CREATE TABLE IF NOT EXISTS api_keys (
    key_id       TEXT    NOT NULL PRIMARY KEY,
    user_id      TEXT    NOT NULL REFERENCES users(id),
    secret_hash  TEXT    NOT NULL,
    expires_days INTEGER NOT NULL,
    expires_at   TEXT,
    revoked_at   TEXT,
    created_at   TEXT    NOT NULL
)
```

| Column | Type | Constraints | Notes |
|--------|------|-------------|-------|
| `key_id` | TEXT | NOT NULL PRIMARY KEY | 8-char alphanumeric ID |
| `user_id` | TEXT | NOT NULL REFERENCES users(id) | FK to users |
| `secret_hash` | TEXT | NOT NULL | SHA-256 hex digest |
| `expires_days` | INTEGER | NOT NULL | Original expiry duration |
| `expires_at` | TEXT | nullable | NULL for non-expiring keys |
| `revoked_at` | TEXT | nullable | NULL if active |
| `created_at` | TEXT | NOT NULL | `TimeFormat` timestamp |

### pats

Personal Access Tokens. Named, permission-scoped tokens. Each belongs to one
user.

```sql
CREATE TABLE IF NOT EXISTS pats (
    token_id     TEXT    NOT NULL PRIMARY KEY,
    user_id      TEXT    NOT NULL REFERENCES users(id),
    name         TEXT    NOT NULL,
    secret_hash  TEXT    NOT NULL,
    permissions  TEXT    NOT NULL,
    expires_days INTEGER NOT NULL,
    expires_at   TEXT,
    revoked_at   TEXT,
    created_at   TEXT    NOT NULL
)
```

| Column | Type | Constraints | Notes |
|--------|------|-------------|-------|
| `token_id` | TEXT | NOT NULL PRIMARY KEY | 8-char alphanumeric ID |
| `user_id` | TEXT | NOT NULL REFERENCES users(id) | FK to users |
| `name` | TEXT | NOT NULL | Human-readable label (max 255 chars) |
| `secret_hash` | TEXT | NOT NULL | SHA-256 hex digest |
| `permissions` | TEXT | NOT NULL | JSON string array (e.g. `["users:read","orgs:read"]`) |
| `expires_days` | INTEGER | NOT NULL | Original expiry duration |
| `expires_at` | TEXT | nullable | NULL for non-expiring tokens |
| `revoked_at` | TEXT | nullable | NULL if active |
| `created_at` | TEXT | NOT NULL | `TimeFormat` timestamp |

### orgs

Organizations. Standalone entity referenced by `org_members`.

```sql
CREATE TABLE IF NOT EXISTS orgs (
    id         TEXT NOT NULL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    slug       TEXT NOT NULL UNIQUE,
    url        TEXT,
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
)
```

| Column | Type | Constraints | Notes |
|--------|------|-------------|-------|
| `id` | TEXT | NOT NULL PRIMARY KEY | UUID v4 |
| `name` | TEXT | NOT NULL UNIQUE | Display name |
| `slug` | TEXT | NOT NULL UNIQUE | URL-safe identifier, immutable after creation |
| `url` | TEXT | nullable | Optional external URL |
| `status` | TEXT | NOT NULL DEFAULT 'active' | `active` or `blocked` |
| `created_at` | TEXT | NOT NULL | `TimeFormat` timestamp |
| `updated_at` | TEXT | NOT NULL | `TimeFormat` timestamp |

### org_members

Join table linking users to organizations. Composite primary key.

```sql
CREATE TABLE IF NOT EXISTS org_members (
    org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL,
    PRIMARY KEY (org_id, user_id)
)
```

| Column | Type | Constraints | Notes |
|--------|------|-------------|-------|
| `org_id` | TEXT | NOT NULL REFERENCES orgs(id) ON DELETE CASCADE | Cascade: deleting an org removes all memberships |
| `user_id` | TEXT | NOT NULL REFERENCES users(id) | No cascade: deleting a user while memberships exist is blocked by the FK |
| `created_at` | TEXT | NOT NULL | `TimeFormat` timestamp |

### admin_config

Standalone key-value store for administrative configuration. No foreign keys.

```sql
CREATE TABLE IF NOT EXISTS admin_config (
    key   TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL
)
```

| Column | Type | Constraints | Notes |
|--------|------|-------------|-------|
| `key` | TEXT | NOT NULL PRIMARY KEY | Configuration key (e.g. `admin_token_hash`, `admin_email`) |
| `value` | TEXT | NOT NULL | Configuration value |

---

## Entity Relationships

```
users
  |
  +--< api_keys.user_id      (FK, no cascade)
  |
  +--< pats.user_id          (FK, no cascade)
  |
  +--< org_members.user_id   (FK, no cascade)

orgs
  |
  +--< org_members.org_id    (FK, ON DELETE CASCADE)

admin_config                  (standalone, no FKs)
```

`org_members` is the only join table, linking `users` and `orgs` with a composite
primary key `(org_id, user_id)`. The `ON DELETE CASCADE` on `org_id` means
deleting an organization automatically removes all its membership rows. The
`user_id` FK has no cascade, so deleting a user while memberships exist is
blocked by the foreign key constraint.

---

## Path Resolution

The database file path is resolved by `resolveDataPath` in `internal/config/load.go`.

**Resolution order:**

1. If `database.path` contains a directory component (e.g. `"./name.db"`,
   `"/var/lib/name.db"`), the value is used as-is.
2. If `database.path` is a bare filename (e.g. `"myapp.db"`) and
   `XDG_DATA_HOME` is set, the path resolves to `$XDG_DATA_HOME/myapp.db`.
3. If `database.path` is a bare filename and `XDG_DATA_HOME` is unset, the
   value is used as-is.
4. If `database.path` is empty and `XDG_DATA_HOME` is set, the path resolves
   to `$XDG_DATA_HOME/apikit.db`.
5. If `database.path` is empty and `XDG_DATA_HOME` is unset, the path
   defaults to `./data/apikit.db`.

When `Open` is called, it creates the parent directory with mode `0700` if it
does not already exist.

---

## Connection Management

Connection setup is handled by `Open` (file-based) and `OpenMemory` (in-memory).
Both delegate to the unexported `initDB` function, which applies all post-connection
initialization in four steps:

1. **Connection pool** -- `MaxOpenConns(1)` and `MaxIdleConns(1)`. SQLite does
   not support concurrent writers, so a single-connection pool avoids contention.
   WAL mode allows concurrent readers alongside the single writer.

2. **WAL mode** -- Enabled for file-based databases via `PRAGMA journal_mode=WAL`.
   The function verifies the returned mode is `"wal"` and returns an error if
   WAL activation fails. Skipped for in-memory databases.

3. **Foreign key enforcement** -- `PRAGMA foreign_keys = ON`. Without this
   pragma, SQLite does not enforce foreign key constraints.

4. **Schema creation** -- All six `CREATE TABLE IF NOT EXISTS` statements execute
   inside a single DEFERRED transaction. If any statement fails, the transaction
   rolls back and `initDB` returns the error.

### Open

```go
func Open(path string) (*DB, error)
```

Validates the path (non-empty, not a directory), creates parent directories with
mode `0700`, opens the SQLite file, and runs full initialization. Returns a
wrapped error if initialization fails; the underlying `*sql.DB` is closed before
returning on error.

### OpenMemory

```go
func OpenMemory() (*DB, error)
```

Opens an in-memory SQLite database (`:memory:`). Skips WAL mode since in-memory
databases have no journal file. Each call yields an independent, isolated
instance.

### Close

```go
func (d *DB) Close() error
```

Closes the underlying `*sql.DB`.

### Ping

```go
func (d *DB) Ping(ctx context.Context) error
```

Context-aware liveness check via `PingContext`. Used by the readiness probe.

---

## Transaction Patterns

### WithTx

```go
func (d *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error
```

The managed transaction helper:

1. Begins a **DEFERRED** transaction via `BeginTx(ctx, nil)` (nil options
   default to DEFERRED isolation in SQLite).
2. Calls `fn(tx)`. The caller performs all reads and writes through the provided
   `*sql.Tx`.
3. If `fn` returns `nil`: commits. Any commit error is returned to the caller.
4. If `fn` returns non-nil: rolls back. The rollback error is silently discarded
   (`_ = tx.Rollback()`), and the original `fn` error is returned. This ensures
   the caller always sees the business-logic error, not a rollback failure.

Usage example (from the OAuth callback handler):

```go
err := database.WithTx(ctx, func(tx *sql.Tx) error {
    // Look up or create user
    // Revoke existing API keys
    // Generate new API key
    return nil
})
```

### Schema Initialization Transaction

`initDB` uses a manual DEFERRED transaction to execute all six schema statements
atomically. If any statement fails, it rolls back and returns the error. This
guarantees the schema is either fully applied or not at all.

---

## Error Handling

### Sentinel Errors

```go
var (
    ErrNotFound       = errors.New("db: not found")
    ErrConflict       = errors.New("db: conflict")
    ErrDatabaseLocked = errors.New("db: database locked")
)
```

| Error | String Value | When Used |
|-------|-------------|-----------|
| `ErrNotFound` | `"db: not found"` | Returned by callers when a query produces no rows (i.e. `sql.ErrNoRows` scenarios). Not mapped by `WrapError`. |
| `ErrConflict` | `"db: conflict"` | Mapped from `SQLITE_CONSTRAINT_UNIQUE`, `SQLITE_CONSTRAINT_PRIMARYKEY`, `SQLITE_CONSTRAINT_FOREIGNKEY`. |
| `ErrDatabaseLocked` | `"db: database locked"` | Mapped from `SQLITE_BUSY`, `SQLITE_LOCKED`. |

### WrapError

```go
func WrapError(err error) error
```

Maps raw SQLite error codes to sentinel errors. Pure function with no I/O or
side effects.

**Behavior:**

1. `nil` input returns `nil`.
2. Uses `errors.As` to unwrap to the `sqliteErrorCode` interface (any error type
   with `error` and `Code() int` methods). This is deliberately interface-based
   rather than type-asserting a concrete driver error type, making it testable
   with synthetic error types.
3. Known SQLite constraint codes (`SQLITE_CONSTRAINT_UNIQUE`,
   `SQLITE_CONSTRAINT_PRIMARYKEY`, `SQLITE_CONSTRAINT_FOREIGNKEY`) map to
   `ErrConflict`.
4. Known SQLite locking codes (`SQLITE_BUSY`, `SQLITE_LOCKED`) map to
   `ErrDatabaseLocked`.
5. Unknown error codes pass through unchanged.

### Usage Pattern

Repository and handler functions call `WrapError(err)` on errors returned from
`ExecContext` or `QueryContext` before returning them to callers. Callers use
`errors.Is(err, db.ErrConflict)` to branch on error type. `ErrNotFound` is set
manually by callers when `sql.ErrNoRows` is detected (e.g. after
`QueryRowContext(...).Scan()` returns `sql.ErrNoRows`).

---

## Timestamp Format

All database timestamps use a single canonical format.

### TimeFormat

```go
const TimeFormat = "2006-01-02T15:04:05Z"
```

ISO 8601 with whole-second precision, UTC, with a literal `Z` suffix. This is
the Go reference-time layout used for both formatting and parsing.

### FormatTime

```go
func FormatTime(t time.Time) string
```

Truncates to whole-second precision, converts to UTC, and formats with
`TimeFormat`. Use this for all writes to timestamp columns.

### ParseTime

```go
func ParseTime(s string) (time.Time, error)
```

Parses a `TimeFormat`-encoded string back to `time.Time` in UTC. Use this for
all reads from timestamp columns.

### Relationship to apikit Timestamp Functions

The root `apikit` package has its own timestamp functions (`NowUTC`, `FormatUTC`,
`ParseUTC`) that use `time.RFC3339` for HTTP-layer timestamps (ETags, response
bodies). The `db.TimeFormat` constant produces identical output for whole-second
UTC timestamps, but the two sets of functions serve different layers:

| Layer | Package | Format | Precision |
|-------|---------|--------|-----------|
| Database | `internal/db` | `TimeFormat` (`2006-01-02T15:04:05Z`) | Whole seconds |
| HTTP | `apikit` (root) | `time.RFC3339` | Whole seconds |

---

## Executor Interface

```go
type Executor interface {
    ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
```

The `Executor` interface is satisfied by both `*sql.DB` and `*sql.Tx`. It allows
repository and handler functions to work with either a bare connection or an
active transaction handle, enabling callers to compose multiple operations inside
a single transaction or run them standalone.

### How Handlers Use Executor

Handler and store functions accept `Executor` (or sometimes the concrete
`*sql.DB` / `*sql.Tx` directly) as a parameter. This gives callers control over
transactional boundaries:

- **Auto-commit mode:** Pass `db.SqlDB` (the `*sql.DB` field on the `DB`
  struct). Each statement executes in its own implicit transaction.
- **Explicit transaction:** Pass a `*sql.Tx` obtained from `WithTx`. Multiple
  operations compose into a single atomic transaction.

Store functions do not need to know whether they are running inside a
transaction. This pattern is used in the API key generation flow, where
`GenerateAPIKey` accepts a `db.Executor` parameter so it can be called both
within an OAuth callback transaction and from standalone bootstrap code.

```go
func GenerateAPIKey(tx db.Executor, userID string, expiresDays int, logger echo.Logger) (*APIKeyResult, error)
```
