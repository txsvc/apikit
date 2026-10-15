---
spec_id: '02'
spec_name: database_layer
title: Database Layer
status: draft
created_at: '2026-07-17T09:50:23.734952+00:00'
updated_at: '2026-07-17T10:03:12.942051+00:00'
owner: ''
source: interactive
schema_version: 1
---
# Database Layer

## Source

This spec is derived from the master PRD at `docs/PRD.md` in the apikit
repository. It covers the **Database Layer** component — spec 03 of 15.

## Intent

Implement the SQLite database layer for apikit: connection management, schema
creation, foreign key enforcement, and query helper patterns. This is the
foundational persistence layer that all other components build upon.

## Goals

- Provide a reliable SQLite connection using a pure-Go driver (no CGo).
- Enable WAL mode for concurrent read/write safety, with validation that WAL mode was successfully applied.
- Create all six tables on boot via `CREATE TABLE IF NOT EXISTS`.
- Enforce foreign key constraints.
- Auto-create the database directory if it does not exist.
- Store all timestamps as RFC 3339 UTC with `Z` suffix, truncated to whole-second precision.
- Expose reusable query helper patterns for the Go codebase.
- Provide an in-memory test helper (`OpenMemory`) for use by downstream test suites.

## Non-Goals

- **Database migration tooling.** Schema is applied on boot via
  `CREATE TABLE IF NOT EXISTS`. No ALTER TABLE or migration framework.
- **Connection pooling beyond single-connection enforcement.** SQLite is an
  embedded database; the `*sql.DB` handle is constrained to a single open
  connection (see Connection Pool Settings below).
- **ORM.** Raw SQL queries with `database/sql` are used directly.
- **Permission value validation.** The database layer stores `pats.permissions`
  strings as-is. Validation of `resource_type:action` values is the
  responsibility of the handler/auth layer before insertion.
- **Repository layer.** The `db` package is a thin initialization wrapper.
  It does not define query methods for individual tables; callers execute
  their own queries using the exposed `*sql.DB` handle.
- **Logger dependency.** The `db` package takes no logger argument and emits
  no log output.

## Dependencies

This spec has **no upstream dependencies**. The `Open(path string)` constructor
accepts the database file path as a plain function argument. The caller
(server bootstrap code, owned by `server_core`) reads the configuration and
passes the resolved path. This keeps the database layer a pure infrastructure
component, decoupled from the config loading system.

---

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25+ |
| Database | SQLite, WAL mode |
| Driver | `modernc.org/sqlite` (pure-Go, no CGo) |
| SQL interface | `database/sql` (Go stdlib) |
| UUID generation | `github.com/google/uuid` |

## Repository Layout

The database layer lives in:

```
internal/
  db/                       Database layer, schema, queries
    db.go                   DB struct, Open, OpenMemory, Close, Ping, WithTx
    schema.go               CREATE TABLE IF NOT EXISTS statements
    errors.go               Sentinel error definitions and WrapError helper
    timestamp.go            FormatTime, ParseTime helpers
    db_test.go              Integration tests (real SQLite file in temp dir)
```

The package is `internal/db` and is not importable by consuming projects.

---

## Server Configuration (Database)

| Setting | Default | Description |
|---------|---------|-------------|
| `[database] path` | `./data/apikit.db` | SQLite database file path |

The database path is read from the server configuration (`config.toml`) by the
server bootstrap code and passed directly to `db.Open(path)`. If the directory
containing the database file does not exist, it must be created automatically
before opening the database.

---

## Operational Requirements

- **Database:** embedded SQLite with WAL mode for concurrent read safety.
  Pure-Go driver (`modernc.org/sqlite`) — no CGo.
- **Schema management:** `CREATE TABLE IF NOT EXISTS` on boot; no migration
  tooling.
- **Foreign key enforcement:** `PRAGMA foreign_keys = ON` must be set
  programmatically via `db.Exec` after opening.
- **WAL mode:** `PRAGMA journal_mode=WAL` must be set programmatically and
  **validated** by reading the returned journal mode value. If the returned
  mode is not `wal`, `Open` must return an error. This prevents silent
  misconfiguration on filesystems that do not support WAL (e.g. network shares,
  certain Docker volumes).
- **Connection pool:** `SetMaxOpenConns(1)` and `SetMaxIdleConns(1)` are
  required to prevent concurrent-writer errors (SQLite only supports one writer
  at a time, even in WAL mode).
- **DSN:** the file path is passed directly to `database/sql.Open`. No DSN
  query parameters are used; all configuration is applied programmatically via
  PRAGMAs after the connection is established.

---

## Database Schema

SQLite with WAL mode. All tables are created on boot via
`CREATE TABLE IF NOT EXISTS`. The DDL snippets below are normative — they must
be used verbatim (or functionally equivalent) in implementation.

All timestamps in database storage use RFC 3339 format normalized to UTC with
the `Z` suffix (e.g. `2026-07-17T14:30:00Z`). Timestamps are **truncated to
whole-second precision** before storage; sub-second components are discarded.
Timestamps with timezone offsets are never produced; incoming timestamps with
offsets are converted to UTC and truncated before storage.

### users

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT (UUID) | PK | User identifier |
| `username` | TEXT | UNIQUE, NOT NULL | Display name |
| `email` | TEXT | NOT NULL | Email address |
| `full_name` | TEXT | | Optional display name |
| `role` | TEXT | NOT NULL, DEFAULT `'user'` | `admin` or `user` |
| `status` | TEXT | NOT NULL, DEFAULT `'active'` | `active` or `blocked` |
| `provider` | TEXT | NOT NULL | OAuth provider name (e.g. `github`) |
| `provider_id` | TEXT | NOT NULL | Provider-specific user ID |
| `created_at` | TEXT (RFC 3339) | NOT NULL | |
| `updated_at` | TEXT (RFC 3339) | NOT NULL | |

Unique constraint on `(provider, provider_id)`.

**DDL:**
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
);
```

### api_keys

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `key_id` | TEXT | PK | Random 8-char alphanumeric identifier |
| `user_id` | TEXT | FK → users.id, NOT NULL | Owning user |
| `secret_hash` | TEXT | NOT NULL | SHA-256 hash of the secret |
| `expires_days` | INTEGER | NOT NULL | Original expiry duration (0, 30, 60, 90) |
| `expires_at` | TEXT (RFC 3339) | | NULL when expires_days is 0 |
| `revoked_at` | TEXT (RFC 3339) | | NULL while active |
| `created_at` | TEXT (RFC 3339) | NOT NULL | |

**DDL:**
```sql
CREATE TABLE IF NOT EXISTS api_keys (
    key_id      TEXT    NOT NULL PRIMARY KEY,
    user_id     TEXT    NOT NULL REFERENCES users(id),
    secret_hash TEXT    NOT NULL,
    expires_days INTEGER NOT NULL,
    expires_at  TEXT,
    revoked_at  TEXT,
    created_at  TEXT    NOT NULL
);
```

### pats

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `token_id` | TEXT | PK | Random identifier |
| `user_id` | TEXT | FK → users.id, NOT NULL | Owning user |
| `name` | TEXT | NOT NULL | User-provided label |
| `secret_hash` | TEXT | NOT NULL | SHA-256 hash of the secret |
| `permissions` | TEXT (JSON) | NOT NULL | JSON array of `resource_type:action` strings; validated by the handler/auth layer before insertion. Example: `["repos:read","repos:write"]` |
| `expires_days` | INTEGER | NOT NULL | Original expiry duration |
| `expires_at` | TEXT (RFC 3339) | | NULL when expires_days is 0 |
| `revoked_at` | TEXT (RFC 3339) | | NULL while active |
| `created_at` | TEXT (RFC 3339) | NOT NULL | |

**DDL:**
```sql
CREATE TABLE IF NOT EXISTS pats (
    token_id    TEXT    NOT NULL PRIMARY KEY,
    user_id     TEXT    NOT NULL REFERENCES users(id),
    name        TEXT    NOT NULL,
    secret_hash TEXT    NOT NULL,
    permissions TEXT    NOT NULL,
    expires_days INTEGER NOT NULL,
    expires_at  TEXT,
    revoked_at  TEXT,
    created_at  TEXT    NOT NULL
);
```

### orgs

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT (UUID) | PK | Organization identifier |
| `name` | TEXT | UNIQUE, NOT NULL | Display name |
| `slug` | TEXT | UNIQUE, NOT NULL | URL-safe identifier |
| `url` | TEXT | | Optional URL |
| `status` | TEXT | NOT NULL, DEFAULT `'active'` | `active` or `blocked` |
| `created_at` | TEXT (RFC 3339) | NOT NULL | |
| `updated_at` | TEXT (RFC 3339) | NOT NULL | |

**DDL:**
```sql
CREATE TABLE IF NOT EXISTS orgs (
    id         TEXT NOT NULL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    slug       TEXT NOT NULL UNIQUE,
    url        TEXT,
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

### org_members

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `org_id` | TEXT | FK → orgs.id ON DELETE CASCADE, NOT NULL | |
| `user_id` | TEXT | FK → users.id, NOT NULL | |
| `created_at` | TEXT (RFC 3339) | NOT NULL | |

Primary key on `(org_id, user_id)`. Rows are cascade-deleted when the
referenced org is deleted (`ON DELETE CASCADE` on `org_id`). No cascade on
`user_id`; deleting a user requires explicit membership cleanup at the
application layer.

**DDL:**
```sql
CREATE TABLE IF NOT EXISTS org_members (
    org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL,
    PRIMARY KEY (org_id, user_id)
);
```

### admin_config

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `key` | TEXT | PK | Config key |
| `value` | TEXT | NOT NULL | Config value |

Stores singleton server state: `admin_token_hash` (SHA-256 hash of the admin
token) and `admin_email` (the designated first-admin email from
`--admin-email`). Single-row-per-key design; no migration needed when new keys
are added.

**DDL:**
```sql
CREATE TABLE IF NOT EXISTS admin_config (
    key   TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL
);
```

---

## Functional Requirements

### Input Validation in `Open`

Before attempting to open the database, `Open` must validate its `path`
argument and return a descriptive error immediately if any of the following
conditions are true:

- `path` is an empty string → return `errors.New("db: path must not be empty")`.
- `path` refers to an existing filesystem entry that is a directory (not a
  file) → return `fmt.Errorf("db: path %q is a directory, not a file", path)`.

These checks occur before directory creation and before `database/sql.Open` is
called. No partial state is created for invalid inputs.

### Database Initialization

`Open` and `OpenMemory` share a single private initialization function,
`initDB(sqlDB *sql.DB) error`, which both constructors call after opening their
respective database connections. This is a normative code-reuse requirement —
parallel reimplementations of the initialization sequence in the two
constructors are not acceptable. It ensures that WAL mode, foreign key
enforcement, connection pool settings, and schema creation are applied
identically by both paths, and that future changes to initialization logic only
need to be made in one place.

The full initialization sequence, performed by `initDB`, is:

1. **Input validation** *(performed by `Open` only, before calling `initDB`).*
   Validate `path` as described above before proceeding.

2. **Directory creation** *(performed by `Open` only, before calling `initDB`).*
   If the parent directory of the configured database path does not exist,
   create it recursively with mode **0700** (owner read/write/execute only).
   The database directory contains sensitive data (hashed tokens, user records);
   only the process owner requires access. The mode is hardcoded and not
   configurable.

3. **Connection.** Open the SQLite database using `modernc.org/sqlite` via
   `database/sql`. The DSN is the bare file path (no query parameters) for
   `Open`, or `:memory:` for `OpenMemory`. All configuration is applied
   programmatically via PRAGMAs after the connection is established.

4. **Connection pool settings.** Immediately after opening, set:
   - `db.SetMaxOpenConns(1)` — enforces a single active connection, preventing
     concurrent-writer errors (`database is locked`) since SQLite supports only
     one writer at a time even in WAL mode.
   - `db.SetMaxIdleConns(1)` — keeps the single connection warm and avoids
     repeated open/close overhead.

5. **WAL mode.** Execute `PRAGMA journal_mode=WAL` as a **query** (not `Exec`)
   immediately after setting pool limits, and read the returned journal mode
   value. If the returned mode string is not `"wal"`, `Open` must return an
   error:
   ```
   fmt.Errorf("db: failed to enable WAL mode: journal_mode is %q", mode)
   ```
   This validation prevents silent misconfiguration on filesystems that do not
   support WAL (e.g. network shares, certain Docker volumes) where SQLite may
   silently fall back to a different journal mode without returning an error
   from `db.Exec`.

   > **Note:** `PRAGMA journal_mode=WAL` is a no-op on `:memory:` databases —
   > SQLite always returns `"memory"` as the journal mode for in-memory
   > databases, not `"wal"`. `OpenMemory` therefore skips the WAL PRAGMA and
   > WAL mode validation step entirely. All other `initDB` steps apply equally
   > to both `Open` and `OpenMemory`.

6. **Foreign keys.** Execute `PRAGMA foreign_keys = ON` via `db.Exec`
   immediately after WAL mode. This is required because SQLite does not enforce
   foreign keys by default. With `SetMaxOpenConns(1)`, per-connection
   re-execution is not a concern.

7. **Schema creation.** Execute all six `CREATE TABLE IF NOT EXISTS` statements
   in a single transaction. Table creation order must respect foreign key
   dependencies:
   - `users` (no FK dependencies)
   - `api_keys` (FK → users)
   - `pats` (FK → users)
   - `orgs` (no FK dependencies)
   - `org_members` (FK → orgs, users)
   - `admin_config` (no FK dependencies)

   The schema creation transaction executes after PRAGMAs are set. This is
   safe because the WAL and foreign key PRAGMAs are already active on the
   single connection before the transaction begins. Opening an existing
   database file is handled transparently — `CREATE TABLE IF NOT EXISTS` is
   idempotent and the PRAGMAs are re-applied on each `Open` call regardless
   of whether the database is new or pre-existing.

   The schema creation transaction uses SQLite's default DEFERRED isolation.
   No context or timeout is applied; schema creation is deterministic and fast,
   and startup hangs during this step are acceptable.

8. **Return handle.** Return the `*DB` handle for use by other packages.

### Corrupt or Invalid Database File

If the database file at `path` exists but is corrupt or is not a valid SQLite
database, `modernc.org/sqlite` will return an error when the connection is
first used (typically during PRAGMA execution or schema creation). `Open` must
wrap this error with additional context identifying the file path:

```go
fmt.Errorf("db: failed to open database at %q: %w", path, err)
```

This wrapper is applied to any driver-level error encountered after
`database/sql.Open` succeeds but before `initDB` returns, so that operators
can identify which file caused the failure. Errors from input validation and
directory creation are returned as-is (they already contain sufficient context).

### Public API

```go
// DB wraps *sql.DB and exposes it directly. Other packages execute their
// own queries using the SqlDB field. The db package does not define
// per-table query methods.
type DB struct {
    SqlDB *sql.DB
}

// Open validates path (non-empty, not a directory), then initializes a
// database at the given file path. It creates the parent directory (mode 0700)
// if needed, opens the SQLite file, then delegates to the shared initDB
// function which applies pool settings, sets WAL mode (with validation),
// sets the foreign key PRAGMA, and runs schema creation.
// Returns a fully initialized *DB or an error; no partial states are possible.
// If the file at path is corrupt or not a valid SQLite database, the error
// is wrapped with the file path for operator diagnostics.
func Open(path string) (*DB, error)

// OpenMemory opens an in-memory SQLite database (:memory:) and applies the
// same initialization steps as Open via the shared initDB function
// (pool settings, foreign key PRAGMA, schema). WAL mode is not set for
// in-memory databases; SQLite does not support WAL on :memory: and always
// reports "memory" as the journal mode. Each call returns an independent
// in-memory database. Isolation between calls is guaranteed by
// SetMaxOpenConns(1): the single-connection constraint ensures each :memory:
// DSN maps to exactly one connection, which in SQLite corresponds to a
// distinct, private in-memory database instance with no shared state.
// Intended for use in tests by downstream packages (handlers, auth, etc.).
func OpenMemory() (*DB, error)

// Close closes the underlying *sql.DB.
func (db *DB) Close() error

// Ping verifies the database connection is alive. Implemented as a direct
// call to db.SqlDB.PingContext(ctx). Used by the readiness probe; a non-nil
// error causes the probe to return HTTP 503. No additional checks beyond
// connection liveness are performed.
func (db *DB) Ping(ctx context.Context) error

// WithTx begins a DEFERRED transaction, calls fn with the transaction handle,
// and commits on success. If fn returns an error, the transaction is rolled
// back and the original error is returned. Rollback errors are silently
// discarded; the original error is always the return value when fn fails.
// WithTx does not wrap errors returned by fn; callers are responsible for
// wrapping SQLite errors via WrapError before or after calling WithTx.
// Context cancellation is propagated to fn via the ctx argument; callers
// are responsible for respecting ctx inside fn and returning a context error,
// which will trigger rollback in the normal way.
func (db *DB) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error
```

**Transaction isolation:** `WithTx` always uses SQLite's default DEFERRED
transaction mode. With `SetMaxOpenConns(1)` there is only one connection, so
there is no inter-connection contention. DEFERRED is sufficient for all
current use cases; IMMEDIATE and EXCLUSIVE modes are not supported by this API.

**Context cancellation in `WithTx`:** The `ctx` passed to `WithTx` is
forwarded to `db.SqlDB.BeginTx`. Callers are responsible for propagating and
respecting the context inside `fn` (e.g. using `tx.ExecContext(ctx, ...)` or
`tx.QueryContext(ctx, ...)`). If `fn` returns an error due to context
cancellation, `WithTx` rolls back the transaction and returns the original
error in the normal way. `WithTx` does not install any additional timeout or
deadline beyond what the caller provides in `ctx`.

### Error Wrapping: `WrapError`

The `db` package exposes a standalone `WrapError` helper that maps raw SQLite
error codes to sentinel errors. Callers invoke it explicitly at the query
execution point. `WithTx` itself does **not** wrap errors automatically.

```go
// WrapError maps known SQLite error codes to db sentinel errors:
//   - SQLITE_CONSTRAINT_UNIQUE / SQLITE_CONSTRAINT_PRIMARYKEY → ErrConflict
//   - SQLITE_BUSY / SQLITE_LOCKED                             → ErrDatabaseLocked
// If err is nil, WrapError returns nil.
// If err does not match a known SQLite error code, it is returned unchanged.
// ErrNotFound is not produced by WrapError; callers must return ErrNotFound
// explicitly when sql.ErrNoRows is encountered.
// WrapError is a pure function: it inspects the error code of the given error
// and maps it to a sentinel. It does not open any database connections and
// can be called with a synthetically constructed error in tests.
func WrapError(err error) error
```

**Usage pattern for callers:**

```go
// Insert example — caller wraps the error explicitly.
_, err := tx.ExecContext(ctx, `INSERT INTO users ...`, args...)
if err != nil {
    return db.WrapError(err)
}

// Query example — caller handles no-rows explicitly.
err := row.Scan(&dest)
if errors.Is(err, sql.ErrNoRows) {
    return db.ErrNotFound
}
if err != nil {
    return db.WrapError(err)
}
```

### Sentinel Errors

The `db` package defines sentinel errors for common database conditions. Callers
use `errors.Is` for clean error handling without depending on SQLite-specific
error codes.

```go
var (
    // ErrNotFound is returned when a query produces no rows.
    // Callers return this explicitly when sql.ErrNoRows is encountered;
    // WrapError does not produce ErrNotFound automatically.
    ErrNotFound = errors.New("db: not found")

    // ErrConflict is returned when an INSERT or UPDATE violates a UNIQUE
    // or PRIMARY KEY constraint. Produced by WrapError.
    ErrConflict = errors.New("db: conflict")

    // ErrDatabaseLocked is returned when SQLite returns SQLITE_BUSY or
    // SQLITE_LOCKED, indicating a concurrent write conflict.
    // Produced by WrapError.
    ErrDatabaseLocked = errors.New("db: database locked")
)
```

### Timestamp Helpers

All timestamps stored in the database use RFC 3339 format normalized to UTC
with the `Z` suffix. Timestamps are **truncated to whole-second precision**
before storage; sub-second components are intentionally discarded.

```go
const TimeFormat = "2006-01-02T15:04:05Z"

// FormatTime truncates t to whole-second precision, converts to UTC,
// and formats using TimeFormat.
func FormatTime(t time.Time) string

// ParseTime parses a canonical timestamp string back to a time.Time in UTC.
func ParseTime(s string) (time.Time, error)
```

---

## Testing Requirements

### Integration Tests (`db_test.go`)

Integration tests use a real SQLite file in a temporary directory created by
`t.TempDir()`. They verify end-to-end initialization behavior.

Required coverage:

| Test | Description |
|------|-------------|
| `TestOpen_EmptyPath` | Verifies that `Open("")` returns a descriptive error immediately without creating any filesystem state. |
| `TestOpen_PathIsDirectory` | Verifies that `Open` returns a descriptive error when given a path that points to an existing directory. |
| `TestOpen_CreatesDirectory` | Verifies that `Open` creates a missing parent directory. On non-Windows platforms (`runtime.GOOS != "windows"`), also asserts the directory has mode 0700. The mode assertion is skipped on Windows, which does not support Unix-style permissions. |
| `TestOpen_CreatesSchema` | Verifies all six tables exist after `Open` on a new database. |
| `TestOpen_Idempotent` | Verifies that calling `Open` twice on the same path succeeds (idempotent schema). |
| `TestOpen_WALMode` | Queries `PRAGMA journal_mode` and asserts the value is `wal`. Also verifies that `Open` returns an error if WAL mode cannot be set (tested by providing a path on a filesystem known not to support WAL, or by asserting the error-path logic directly). |
| `TestOpen_CorruptFile` | Verifies that `Open` returns a wrapped error (containing the file path) when the target file exists but is not a valid SQLite database (e.g. a file containing arbitrary bytes). |
| `TestOpen_ForeignKeys` | Verifies that a FK violation is rejected (insert a child row with a non-existent parent). |
| `TestPing` | Verifies `Ping` returns nil on a healthy database. |
| `TestWithTx_Commit` | Verifies that changes made inside `WithTx` are visible after the function returns nil. |
| `TestWithTx_Rollback` | Verifies that changes made inside `WithTx` are not visible after the function returns an error. |
| `TestWrapError_Conflict` | Verifies that a UNIQUE constraint violation is mapped to `ErrConflict` by `WrapError`. |
| `TestWrapError_Locked` | Verifies that a SQLITE_BUSY error code is mapped to `ErrDatabaseLocked` by `WrapError`. Because `WrapError` is a pure function that maps error codes to sentinels without requiring a live database, this test constructs a synthetic SQLite error with the SQLITE_BUSY error code and passes it directly to `WrapError`. No real database lock is required. |
| `TestWrapError_Passthrough` | Verifies that an unknown error is returned unchanged by `WrapError`. |
| `TestFormatTime` | Verifies truncation, UTC normalization, and format string. |
| `TestParseTime` | Verifies round-trip: `ParseTime(FormatTime(t)) == t.Truncate(time.Second).UTC()`. |

### In-Memory Helper (`OpenMemory`)

`OpenMemory() (*DB, error)` opens an in-memory SQLite database (`:memory:`)
and applies the same initialization steps as `Open` via the shared `initDB`
function (pool settings, foreign key PRAGMA, schema). WAL mode is not applied
to in-memory databases (see Functional Requirements above). Each call returns
an independent database instance with no shared state. Isolation is guaranteed
by `SetMaxOpenConns(1)`: a single connection to `:memory:` in SQLite maps to a
distinct, private in-memory database instance.

Intended usage in downstream test suites:

```go
func TestSomething(t *testing.T) {
    db, err := db.OpenMemory()
    require.NoError(t, err)
    t.Cleanup(func() { db.Close() })
    // use db.SqlDB for queries
}
```

`OpenMemory` is exported so that packages outside `internal/db` (handlers,
auth, etc.) can set up a fully initialized test database without duplicating
initialization logic.

---

## Error Handling

| Condition | Behavior |
|-----------|----------|
| `path` is empty string | Return descriptive error from `Open` before any filesystem access |
| `path` points to an existing directory | Return descriptive error from `Open` before any filesystem access |
| Database directory cannot be created | Return error from `Open` |
| Database file cannot be opened | Return error from `Open` |
| Database file is corrupt or not a valid SQLite file | Return error wrapped with file path: `fmt.Errorf("db: failed to open database at %q: %w", path, err)` |
| Pool settings fail | Return error from `Open` |
| WAL mode PRAGMA fails (driver error) | Return error from `Open` |
| WAL mode PRAGMA returns unexpected mode (not `"wal"`) | Return `fmt.Errorf("db: failed to enable WAL mode: journal_mode is %q", mode)` from `Open` |
| Foreign keys PRAGMA fails | Return error from `Open` |
| Schema creation fails | Return error from `Open` |
| Transaction begin fails | Return error from `WithTx` |
| Transaction function returns error | Rollback; return the original error (unwrapped) |
| Rollback fails after function error | Silently discard rollback error; return the original error |
| Ping fails | Return error (readiness probe returns HTTP 503) |
| UNIQUE/PK constraint violation | Caller wraps via `WrapError` → `ErrConflict` |
| No rows found | Caller returns `ErrNotFound` explicitly when `sql.ErrNoRows` is encountered |
| SQLite BUSY / LOCKED | Caller wraps via `WrapError` → `ErrDatabaseLocked` |

---

## Design Decisions

- **Pure-Go driver.** `modernc.org/sqlite` eliminates CGo dependency, making
  cross-compilation straightforward and CI simpler.
- **WAL mode with validation.** Required for concurrent read safety. Without
  WAL, concurrent readers block writers. The returned journal mode value is
  explicitly validated after setting `PRAGMA journal_mode=WAL`; if the mode is
  not `"wal"`, `Open` returns an error. This prevents silent misconfiguration
  on filesystems (e.g. network shares, certain Docker volumes) where SQLite may
  silently fall back to a different journal mode without returning an error
  from `db.Exec`.
- **WAL skipped for `:memory:`.** SQLite does not support WAL mode on in-memory
  databases and always reports `"memory"` as the journal mode. `OpenMemory`
  therefore skips the WAL PRAGMA and validation step. All other initialization
  steps apply identically to both file and in-memory databases.
- **Single-connection pool.** `SetMaxOpenConns(1)` is required because SQLite
  only supports one writer at a time even in WAL mode. Without this constraint,
  Go's connection pool may attempt concurrent writes, causing `database is
  locked` errors. If concurrent read-only connections are needed in the future,
  a separate read-only `*sql.DB` handle can be opened.
- **No ORM.** Raw SQL via `database/sql` keeps the codebase simple and avoids
  ORM abstraction leaks. The schema is small enough (6 tables) that an ORM
  adds complexity without proportional benefit.
- **Schema on boot.** `CREATE TABLE IF NOT EXISTS` is idempotent and avoids
  the need for migration tooling. This is appropriate for a pre-production
  project with no deployed users.
- **Foreign key enforcement.** SQLite does not enforce foreign keys by default.
  `PRAGMA foreign_keys = ON` must be set programmatically; it is not persistent
  across connections.
- **Programmatic PRAGMAs, no DSN parameters.** Both `journal_mode=WAL` and
  `foreign_keys=ON` are set via `db.QueryRow` / `db.Exec` after `Open`. This
  is explicit, testable, and avoids reliance on driver-specific DSN parameter
  syntax.
- **WAL PRAGMA executed as a query, not Exec.** `PRAGMA journal_mode=WAL`
  returns the current journal mode as a result row. Using `QueryRow` instead
  of `Exec` allows reading and validating the returned value, catching silent
  fallbacks.
- **Shared `initDB` function.** Both `Open` and `OpenMemory` call a shared
  private `initDB(*sql.DB) error` function for all post-connection
  initialization (pool settings, PRAGMAs, schema). This is a normative
  requirement — it ensures initialization is identical for both constructors
  and prevents drift when initialization logic changes. The only behavioral
  difference between `Open` and `OpenMemory` is that `OpenMemory` passes a
  flag or calls a variant that skips the WAL PRAGMA.
- **Thin wrapper / exposed SqlDB.** The `DB` struct exposes `SqlDB *sql.DB`
  directly. Other packages are responsible for their own query logic. This
  avoids turning `internal/db` into a monolithic repository layer and keeps
  the package focused on initialization and lifecycle management.
- **`WrapError` helper, not automatic wrapping in `WithTx`.** Error wrapping
  is the caller's responsibility. `WithTx` begins, commits, and rolls back
  transactions but does not inspect or transform errors returned by the
  function it calls. `WrapError` is provided as a standalone helper for callers
  to invoke at the query execution point. This keeps `WithTx` simple and gives
  callers explicit control over error classification.
- **`WrapError` is a pure function.** `WrapError` inspects the SQLite error
  code of the given error and maps it to a sentinel value. It requires no
  database connection and can be exercised in tests by constructing a synthetic
  error with the target error code, without needing to trigger a real database
  lock or constraint violation. This makes `TestWrapError_Locked` reliable and
  fast.
- **`ErrNotFound` is caller-produced, not `WrapError`-produced.** `sql.ErrNoRows`
  is not a SQLite error code; it is returned by `database/sql` when a scan
  finds no rows. Callers check for `sql.ErrNoRows` explicitly and return
  `db.ErrNotFound`. `WrapError` does not handle this case to keep the mapping
  between SQLite error codes and sentinels consistent.
- **DEFERRED transactions only.** `WithTx` always uses SQLite's default
  DEFERRED isolation. With `SetMaxOpenConns(1)`, only one connection exists,
  so inter-connection contention cannot occur. DEFERRED is sufficient for all
  current use cases.
- **Context propagation in `WithTx`.** The context passed to `WithTx` is
  forwarded to `BeginTx`. Callers are responsible for passing the context to
  individual query calls inside `fn`. If `fn` returns a context error, rollback
  proceeds normally. `WithTx` does not install additional timeouts.
- **`Ping` as `PingContext`.** `Ping(ctx context.Context)` is implemented as
  a direct call to `db.SqlDB.PingContext(ctx)`. No additional query or schema
  check is performed; connection liveness is sufficient for the readiness probe.
- **Context-free `Open`.** `Open(path string)` takes no `context.Context`.
  Startup hangs during database initialization are acceptable during server
  bootstrap. Schema creation is deterministic and fast, making a context
  deadline unnecessary complexity.
- **Input validation in `Open`.** `Open` validates `path` before any
  filesystem access, returning a descriptive error for empty paths or paths
  pointing to existing directories. This prevents confusing errors from the
  SQLite driver on misconfigured deployments.
- **Corrupt file error wrapping.** Driver errors encountered after
  `database/sql.Open` succeeds (e.g. PRAGMA execution or schema creation on a
  corrupt file) are wrapped with the file path before being returned. This
  gives operators the context needed to identify the problematic file without
  inspecting stack traces.
- **`:memory:` isolation via single-connection pool.** `OpenMemory` uses the
  `:memory:` DSN. Because `SetMaxOpenConns(1)` is applied, the Go runtime
  maintains exactly one connection to this DSN, which SQLite maps to a single
  private in-memory database. Each `OpenMemory` call creates an independent
  `*sql.DB` handle with its own connection and therefore its own isolated
  in-memory database.
- **No logger dependency.** The `db` package emits no log output. Rollback
  errors in `WithTx` are silently discarded; the original error is always
  returned. This keeps the package a pure infrastructure component with no
  external dependencies beyond the driver.
- **Directory mode 0700.** The database directory contains sensitive data
  (hashed tokens, user records). Mode 0700 restricts access to the process
  owner only. The mode is hardcoded and not configurable.
- **Cascade delete on org_members.** `ON DELETE CASCADE` on `org_members.org_id`
  ensures membership rows are automatically removed when an org is deleted.
  No cascade is applied on `user_id`; user deletion requires explicit
  membership cleanup at the application layer.
- **Whole-second timestamp precision.** Sub-second precision is intentionally
  dropped. All apikit use cases (user records, token lifecycle, org membership)
  are satisfied by whole-second resolution, and the uniform truncation policy
  prevents round-trip mismatches.
- **Path injection over config coupling.** `Open(path string)` accepts the
  database path as a plain argument. The server bootstrap code (in
  `server_core`) reads the config and passes the resolved path. This keeps
  the database layer a pure infrastructure component with no dependency on the
  config loading system.
- **Single `Open` constructor.** All initialization (validation, directory,
  connection, pool settings, PRAGMAs, schema) happens in one call. Callers
  get a fully initialized handle or an error — no partial states.
- **`OpenMemory` for tests.** An exported `OpenMemory()` constructor applies
  identical initialization (via the shared `initDB`) to an in-memory SQLite
  database. Downstream packages (handlers, auth) use this in their test suites
  for a fast, hermetic database setup without temp files.
- **Windows permission skip.** `TestOpen_CreatesDirectory` asserts directory
  mode 0700 only when `runtime.GOOS != "windows"`. Windows does not support
  Unix-style permissions; the mode assertion is silently skipped on that
  platform without requiring build tags.

---

## Glossary

| Term | Definition |
|------|------------|
| **WAL mode** | Write-Ahead Logging — a SQLite journal mode that allows concurrent readers and a single writer without blocking. |
| **PRAGMA** | SQLite configuration command that sets database-level options (e.g. journal mode, foreign key enforcement). |
| **DSN** | Data Source Name — the connection string used to open a database. For modernc.org/sqlite, this is the bare file path (no query parameters). |
| **RFC 3339** | A date/time format standard. apikit uses the UTC variant with `Z` suffix and whole-second precision: `2006-01-02T15:04:05Z`. |
| **CGo** | Go's mechanism for calling C code. Avoided in this project by using a pure-Go SQLite driver. |
| **ON DELETE CASCADE** | A referential action that automatically deletes child rows when the referenced parent row is deleted. Applied to `org_members.org_id`. |
| **Sentinel error** | A package-level `var` error value that callers test with `errors.Is`. Used to abstract driver-specific error codes. |
| **WrapError** | A standalone pure-function helper exported by the `db` package that maps raw SQLite error codes to sentinel errors. Called explicitly by callers at the query execution point. Can be tested with synthetic errors without a live database. |
| **OpenMemory** | An exported constructor that opens an in-memory SQLite database with full initialization (via the shared `initDB` function, minus the WAL step). Used by downstream test suites. Each call returns an independent, isolated database instance. |
| **DEFERRED** | SQLite's default transaction isolation mode. Locks are acquired lazily (on first read or write). Used exclusively by `WithTx`. |
| **initDB** | A private function shared by `Open` and `OpenMemory` that applies all post-connection initialization: pool settings, PRAGMAs (WAL for file DBs only), and schema creation. |
