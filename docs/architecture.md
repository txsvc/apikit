# Architecture

This document describes the architecture of apikit, a Go library and server framework for building authenticated JSON APIs. The code is the source of truth; this document reflects the implementation as built.

## 1. Overview

apikit is four things in one module:

1. **Library** -- the root `apikit` package provides a configurable HTTP server (`Server`), middleware pipeline, error handling, caching, ETag support, and timestamp utilities. Consuming projects import `github.com/txsvc/apikit` and call `LoadConfig` / `NewServer` / `Start`.

2. **Server binary** (`cmd/apikit`) -- a minimal reference binary that exercises the three-step caller contract: `LoadConfig`, `NewServer`, `Start`.

3. **CLI binary** (`cmd/akc`) -- a Cobra-based command-line client for user management, API key lifecycle, PAT management, organization administration, and browser-based OAuth login.

4. **SDK client** (`sdk*.go` in the root package) -- a typed Go HTTP client (`Client`) that mirrors every server endpoint, providing `doJSON[T]`, `doList[T]`, and `doEmpty` generic helpers for consuming the API programmatically.

The server uses the [Echo v4](https://echo.labstack.com/) framework internally but does not expose Echo types to consumers beyond the `echo.MiddlewareFunc` and `echo.Group` types needed for route registration. All internal packages live under `internal/` and are inaccessible to external importers. Type aliases in the root package (`Config`, `Provider`, `UserInfo`, `APIKeyResult`) re-export selected internal types to maintain a clean public API surface.

---

## 2. Package Layout

```
github.com/txsvc/apikit
|
|-- apikit.go               Root exports: LoadConfig, GenerateAPIKey, type aliases
|-- server.go               Server struct, NewServer, Start, Shutdown, health endpoints
|-- middleware.go            All middleware functions (unexported)
|-- errors.go               WriteAPIError, HTTPErrorHandler, error envelope types
|-- cache.go                CacheCategory, CacheMiddleware
|-- etag.go                 SetETag, CheckETag
|-- timestamp.go            NowUTC, FormatUTC, ParseUTC
|-- cli.go                  Public RootCommand() for embedding the CLI subtree
|-- sdk.go                  Client struct, NewClient, do, doJSON, doList, doEmpty
|-- sdk_types.go            All request/response/domain types for the SDK
|-- sdk_health.go           Healthz, Readyz, Version methods
|-- sdk_auth.go             GetProviders, ExchangeOAuthCode methods
|-- sdk_user.go             Authenticated user endpoint methods
|-- sdk_admin.go            Admin user and organization endpoint methods
|-- sdk_errors.go           APIError, ErrNotModified, Response[T]
|
|-- cmd/
|   |-- apikit/main.go      Server binary entry point
|   |-- akc/main.go         CLI binary entry point
|
|-- internal/
|   |-- config/             Configuration loading, validation, XDG resolution
|   |   |-- config.go       Config, ServerConfig, DatabaseConfig, LoggingConfig, OAuthConfig structs
|   |   |-- load.go         Load(), applyDefaults, validate, parseBodySize, XDG helpers
|   |
|   |-- db/                 Database abstraction layer
|   |   |-- db.go           DB struct, Open, OpenMemory, Close, Ping, WithTx, initDB
|   |   |-- schema.go       Six CREATE TABLE statements
|   |   |-- errors.go       ErrNotFound, ErrConflict, ErrDatabaseLocked, WrapError
|   |   |-- executor.go     Executor interface (ExecContext, QueryContext, QueryRowContext)
|   |   |-- timestamp.go    TimeFormat, FormatTime, ParseTime
|   |
|   |-- auth/               Authentication middleware and authorization helpers
|   |   |-- auth.go         NewAuthMiddleware (Bearer token validation)
|   |   |-- context.go      SetAuthInfo, GetAuthInfo, GetUserID (delegates to authctx)
|   |   |-- credentials.go  parseToken, validateAdminToken, validateAPIKey, validatePAT
|   |   |-- permissions.go  PermissionRegistry, IsAdmin, RequireAdmin, RequirePermission
|   |
|   |-- authctx/            Shared auth context types (breaks import cycle)
|   |   |-- authctx.go      AuthInfo struct, typed context key, SetAuthInfo, GetAuthInfo
|   |
|   |-- oauth/              OAuth provider abstraction, callback handling, key generation
|   |   |-- provider.go     Provider interface, UserInfo struct
|   |   |-- registry.go     Registry, BuildRegistryFromConfig
|   |   |-- github.go       GitHubProvider implementation
|   |   |-- google.go       GoogleProvider implementation
|   |   |-- handler.go      RegisterOAuthHandlers, handleCallback
|   |   |-- callback.go     APIKeyResult, GenerateAPIKey, callbackResponse types
|   |   |-- key.go          GenerateKeyMaterial, HashSecret, randomString
|   |   |-- redirect.go     ValidateRedirectURI
|   |   |-- providers.go    handleProviders, providerResponse
|   |
|   |-- keys/               API key handlers and generation
|   |   |-- handlers.go     RegisterKeyHandlers (list, refresh, revoke own keys)
|   |   |-- generate.go     GenerateAPIKey (core generation with revocation)
|   |
|   |-- handlers/           Domain resource HTTP handlers
|   |   |-- users.go        RegisterUserHandlers (CRUD, promote/demote/block, admin keys/tokens)
|   |   |-- orgs.go         RegisterOrgHandlers (CRUD, block, members)
|   |   |-- pat.go          PATHandler (create, list, get, revoke personal access tokens)
|   |
|   |-- bootstrap/          First-boot and admin token management
|   |   |-- bootstrap.go    Run(), admin token generation, admin email seeding
|   |
|   |-- cli/                CLI command tree and client infrastructure
|       |-- root.go         RootCommand, PersistentPreRunE, credential resolution
|       |-- config.go       InitConfig, LoadConfig, SaveConfig, ResolveField
|       |-- login.go        Browser-based OAuth login flow
|       |-- user.go         User commands + cmdClient HTTP wrapper
|       |-- keys.go         Keys commands
|       |-- tokens.go       Tokens commands
|       |-- orgs.go         Orgs commands
|       |-- admin*.go       Admin command subtree (users, keys, tokens, orgs, members)
|       |-- runners.go      UsersRunner, OrgsRunner, KeysRunner, TokensRunner (DI)
|       |-- output.go       ExitCode, PrintError, PrintJSON, Warnf
|       |-- help.go         Custom help command, --json tree walker
|       |-- context.go      Context key types and accessors
|       |-- helpers.go      parseKeyID, validateExpires, parsePermissions, openBrowser
|       |-- version.go      Version command
|       |-- version_info.go Build-time variable declarations
```

### Import Constraints

- **Root package** imports `internal/config`, `internal/db`, `internal/keys`, `internal/oauth` for type aliases and delegation. It never imports `internal/auth`, `internal/handlers`, `internal/bootstrap`, or `internal/cli`.
- **`internal/authctx`** has zero internal dependencies. It exists solely to break the import cycle between `internal/keys` and `internal/auth`.
- **`internal/auth`** imports `internal/authctx`, `internal/db`, and the root `apikit` package (for `WriteAPIError` and `TokenPrefix`).
- **`internal/handlers`** imports `internal/auth`, `internal/db`, and the root `apikit` package (for `WriteAPIError`).
- **`internal/keys`** imports `internal/authctx` and `internal/db`. It uses its own `writeAPIError` copy to avoid a circular import with the root package.
- **`internal/oauth`** imports `internal/bootstrap` and `internal/db`.
- **`internal/cli`** imports nothing from the root package or other internal packages at the type level. It uses Runners with `any`-typed function fields to avoid import cycles.

---

## 3. Dependency Graph

```
                          ┌─────────────────┐
                          │   cmd/apikit     │  (server binary)
                          │   cmd/akc        │  (CLI binary)
                          └────────┬────────┘
                                   │
                    ┌──────────────┼──────────────┐
                    │              │               │
                    v              v               v
            ┌───────────┐  ┌────────────┐  ┌────────────┐
            │  apikit    │  │ internal/  │  │ internal/  │
            │  (root)    │  │   cli      │  │ bootstrap  │
            │            │  └──────┬─────┘  └─────┬──────┘
            │ Server     │         │              │
            │ SDK Client │         │              │
            │ Middleware  │         │              │
            │ Errors     │         │              │
            │ Cache/ETag │         │              │
            └──────┬─────┘         │              │
                   │               │              │
        ┌──────────┼───────────────┼──────────────┘
        │          │               │
        v          v               v
  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────┐
  │ internal/ │  │ internal/ │  │ internal/ │  │  internal/    │
  │  config   │  │  oauth    │  │  keys     │  │  handlers     │
  └──────────┘  └─────┬─────┘  └─────┬─────┘  └───────┬──────┘
                      │              │                 │
                      │         ┌────┘                 │
                      │         │                      │
                      v         v                      v
                ┌───────────┐              ┌──────────────┐
                │ internal/ │              │  internal/    │
                │   db      │◄─────────────│    auth       │
                └───────────┘              └───────┬──────┘
                                                   │
                                                   v
                                           ┌──────────────┐
                                           │  internal/    │
                                           │   authctx     │
                                           └──────────────┘

Layer boundaries:
  ┌─────────────────────────────────────────────────────┐
  │  BINARY LAYER     cmd/apikit, cmd/akc               │
  ├─────────────────────────────────────────────────────┤
  │  PUBLIC API LAYER  apikit (root package)             │
  ├─────────────────────────────────────────────────────┤
  │  DOMAIN LAYER      handlers, keys, oauth, bootstrap │
  ├─────────────────────────────────────────────────────┤
  │  AUTH LAYER         auth, authctx                    │
  ├─────────────────────────────────────────────────────┤
  │  DATA LAYER         db, config                       │
  └─────────────────────────────────────────────────────┘
```

Imports flow strictly downward. No package in a lower layer imports from a higher one. The `authctx` package at the bottom of the auth layer breaks what would otherwise be a circular dependency between `keys` and `auth`.

---

## 4. Server Lifecycle

### Startup Sequence

```
LoadConfig()                          config.Load()
    |                                     |
    |  Reads config.toml (XDG)            |  Apply defaults, validate,
    |  Returns *Config                    |  parse body size, resolve DB path
    v                                     v
NewServer(cfg, checker)
    |
    |  1. Panic if cfg == nil
    |  2. Create echo.New()
    |  3. Set HTTPErrorHandler
    |  4. Configure logrus (level from config)
    |  5. Register middleware in corrected order
    |  6. Register health endpoints at root
    |  7. Pre-create API group at mount point
    |  8. Return *Server
    v
server.Start()
    |
    |  1. Check not already shut down
    |  2. net.Listen("tcp", bind:port)
    |  3. Store actual address (supports port=0)
    |  4. Install SIGTERM/SIGINT handler
    |  5. echo.Start("") using the listener
    |  6. Block until shutdown
    |  7. Clear address, return nil
    v
server.Shutdown(ctx)
    |
    |  sync.Once guarantees single execution:
    |  1. Set shutdown = true
    |  2. Create timeout context (15s drain)
    |  3. echo.Shutdown(shutdownCtx)
    v
    (process exits)
```

### Full Application Boot (with database and bootstrap)

A production server extends the basic lifecycle:

```
1. LoadConfig()                        -- read config.toml
2. db.Open(cfg.Database.Path)          -- open SQLite, init schema
3. bootstrap.Run(ctx, params)          -- conditional: runs when --admin-email,
                                          --reset-admin-token, or existing hash
4. NewServer(cfg, db.Ping)             -- construct server with health checker
5. oauth.BuildRegistryFromConfig(...)  -- configure OAuth providers
6. auth.NewPermissionRegistry()        -- register built-in permissions
7. RegisterOAuthHandlers(api, ...)     -- mount OAuth (before auth middleware)
8. api.Use(auth.NewAuthMiddleware(...))-- attach auth middleware to API group
9. Register domain handlers            -- mount user, org, key, PAT routes
10. server.Start()                     -- listen and serve
```

Note: OAuth handlers are registered before the auth middleware (step 7 before
step 8) so that `/auth/providers` and `/auth/callback` are accessible without
authentication. Domain handlers (step 9) are registered after the middleware
so they are protected.

### Key Lifecycle Properties

- **Ephemeral port**: Setting `port=0` in config causes `net.Listen` to select an available port. The actual address is accessible via `server.Addr()` after `Start()` begins.
- **Idempotent shutdown**: `sync.Once` ensures `Shutdown` runs exactly once regardless of concurrent calls from signal handlers, application code, or both.
- **No restart**: Once `shutdown` is set to `true`, `Start()` returns an error immediately. The `Server` instance is single-use.
- **Drain timeout**: Fixed at 15 seconds (`drainTimeout`). Not user-configurable. In-flight requests have this window to complete before the server force-closes connections.

---

## 5. Middleware Pipeline

Middleware executes in registration order on the way in and reverse order on the way out. The ordering was explicitly corrected from the original specification to fix two problems: security headers must appear on every response (including short-circuited ones), and logging must wrap error-producing middleware to capture all status codes.

```
Request ──►
    │
    ▼
┌─────────────────────────────┐
│ (1) Panic Recovery          │  Outermost. Catches panics from everything
│     panicRecoveryMiddleware │  downstream. Logs error + stack trace.
│                             │  Returns 500 JSON envelope.
├─────────────────────────────┤
│ (2) Request ID              │  Assigns UUID v4. Reuses valid X-Request-ID
│     requestIDMiddleware     │  from the request header if present. Sets
│                             │  response header and context value.
├─────────────────────────────┤
│ (3) Security Headers        │  Sets X-Content-Type-Options, X-Frame-Options,
│     securityHeadersMiddleware  Referrer-Policy on EVERY response. Runs
│                             │  before any short-circuit middleware.
├─────────────────────────────┤
│ (4) Logging                 │  Structured JSON log entry per request.
│     loggingMiddleware       │  Health probes (/healthz, /readyz) logged at
│                             │  debug level; all others at info level.
│                             │  Fields: method, path, status, duration, request_id.
├─────────────────────────────┤
│ (5) Body Size Limit         │  Rejects bodies exceeding MaxBodySize (default
│     bodySizeLimitMiddleware │  1MB). Returns 413 via WriteAPIError.
│                             │  Wraps chunked/unknown-length bodies with a
│                             │  limitedReadCloser for streaming enforcement.
├─────────────────────────────┤
│ (6) Content-Type Enforce    │  Rejects POST/PUT/PATCH with non-JSON
│     contentTypeEnforcement  │  Content-Type. Returns 415 via WriteAPIError.
│     Middleware              │  GET/DELETE/HEAD/OPTIONS pass through.
├─────────────────────────────┤
│ (7) Cache-Control           │  Per-route middleware, not global. Applied to
│     CacheMiddleware(cat)    │  individual routes or route groups. The API
│                             │  group gets CacheNoStore by default.
├─────────────────────────────┤
│ (8) Auth Middleware          │  Applied to the API group only (not health
│     NewAuthMiddleware       │  probes or OAuth endpoints). Validates
│                             │  Bearer tokens, injects AuthInfo into context.
├─────────────────────────────┤
│     Route Handler           │  The actual business logic.
└─────────────────────────────┘
                                  ◄── Response
```

**Why each position matters:**

| Position | Rationale |
|----------|-----------|
| (1) Panic Recovery first | Must be outermost to catch panics from any middleware or handler. If placed later, a panic in an earlier middleware would crash the process. |
| (2) Request ID second | Generates the ID before logging so all log entries include it. Before security headers so the ID is available for error responses. |
| (3) Security Headers third | Must execute before any middleware that can short-circuit (body limit, content-type) to guarantee security headers appear on 413 and 415 responses. |
| (4) Logging fourth | Wraps the error-producing middleware (5, 6) so their short-circuit responses are captured in logs with accurate status codes and durations. |
| (5) Body Size Limit fifth | Rejects oversized bodies before the handler reads them, preventing memory exhaustion. Must run after logging. |
| (6) Content-Type sixth | Rejects wrong content types before the handler attempts JSON binding. The cheapest check runs last among the global middleware. |

---

## 6. Request Processing

### Full Request Lifecycle

```
TCP Connection
    │
    ▼
net.Listener.Accept()
    │
    ▼
Echo Router (path matching)
    │
    ├── /healthz, /readyz, /version     ──► Health handler (no auth)
    ├── /api/v1/auth/providers           ──► OAuth handler (no auth)
    ├── /api/v1/auth/callback            ──► OAuth callback (no auth)
    └── /api/v1/**                       ──► API group
            │
            ▼
        Global middleware chain (1-6)
            │
            ▼
        CacheMiddleware(CacheNoStore)    (group-level)
            │
            ▼
        Auth Middleware                  (group-level)
            │
            ├── Extract Authorization header
            ├── Validate Bearer prefix
            ├── parseToken() to classify credential type
            ├── Dispatch to validate{AdminToken|APIKey|PAT}
            ├── On failure: return 401/403/500 JSON error
            └── On success: inject AuthInfo into context
                    │
                    ▼
                Route handler
                    │
                    ├── Authorization check (RequireAdmin / RequirePermission / etc.)
                    ├── Request binding and validation
                    ├── Database query/mutation
                    ├── ETag check (conditional GET → 304)
                    ├── Response serialization
                    └── Return JSON with appropriate status code
                            │
                            ▼
                    Middleware chain unwinds (response path)
                            │
                            ├── Logging middleware records status + duration
                            ├── Security headers already set
                            ├── Request ID in response header
                            └── Panic recovery (no-op on clean path)
                                    │
                                    ▼
                            HTTP Response to client
```

### Handler Registration Pattern

Handlers are registered on the `*echo.Group` returned by `server.APIGroup()`. Each handler package provides a `Register*Handlers` function that mounts routes on the group:

```go
api := server.APIGroup()

// OAuth registered before auth middleware (public endpoints)
oauth.RegisterOAuthHandlers(api, oauthRegistry, db, externalURL)

// Auth middleware applied to the group
api.Use(auth.NewAuthMiddleware(database, registry))

// Domain handlers (protected by auth middleware)
handlers.RegisterUserHandlers(api, sqlDB)
handlers.RegisterOrgHandlers(api, sqlDB)
keys.RegisterKeyHandlers(api, sqlDB)
patHandler.RegisterRoutes(api)
```

### Response Patterns

All successful responses follow these conventions:

| Pattern | HTTP Status | Body | Used By |
|---------|-------------|------|---------|
| Single resource created | 201 | JSON object | POST /users, POST /orgs, POST /user/tokens |
| Single resource returned | 200 | JSON object | GET /user, GET /users/:id, GET /orgs/:id |
| List of resources | 200 | JSON array (never null) | GET /users, GET /orgs, GET /user/keys |
| Void operation | 204 | Empty | DELETE operations, idempotent adds |
| Not modified | 304 | Empty | Conditional GET with matching ETag |

All error responses use the standard JSON envelope via `WriteAPIError` (see section 7).

---

## 7. Error Handling

### JSON Error Envelope

Every error response across the entire API uses a single consistent envelope:

```json
{
  "error": {
    "code": 404,
    "message": "user not found"
  }
}
```

The `code` field always mirrors the HTTP status code. This is enforced by two mechanisms:

1. **`WriteAPIError(c echo.Context, code int, message string)`** -- Used directly by handlers and middleware to produce error responses. Explicitly sets `Content-Type: application/json; charset=utf-8` to work around an Echo v4.15+ behavior that omits the charset.

2. **`HTTPErrorHandler(err error, c echo.Context)`** -- A custom Echo error handler set on the Echo instance in `NewServer`. It catches any error that propagates out of the handler chain. It unwraps `*echo.HTTPError` to extract code and message; for non-Echo errors, it defaults to 500 "internal server error". If the response is already committed (e.g., by `WriteAPIError`), it is a no-op.

### Error Propagation Flow

```
Handler
  │
  ├── Validation failure ──► WriteAPIError(c, 400, "missing required field: name")
  │                          (returns directly, bypasses HTTPErrorHandler)
  │
  ├── Auth failure ──► echo.NewHTTPError(403, "forbidden")
  │                    (propagates up, caught by HTTPErrorHandler)
  │
  ├── Database error ──► db.WrapError(err)
  │                      │
  │                      ├── db.ErrConflict ──► WriteAPIError(c, 409, "...")
  │                      ├── db.ErrNotFound ──► WriteAPIError(c, 404, "...")
  │                      └── other ──► WriteAPIError(c, 500, "internal server error")
  │
  └── Panic ──► panicRecoveryMiddleware
                 │
                 └── WriteAPIError(c, 500, "internal server error")
                     + logs panic value and stack trace
```

### Database Error Mapping

The `db.WrapError` function maps raw SQLite error codes to sentinel errors:

| Sentinel | SQLite Codes | HTTP Status |
|----------|-------------|-------------|
| `db.ErrConflict` | SQLITE_CONSTRAINT_UNIQUE, SQLITE_CONSTRAINT_PRIMARYKEY, SQLITE_CONSTRAINT_FOREIGNKEY | 409 |
| `db.ErrDatabaseLocked` | SQLITE_BUSY, SQLITE_LOCKED | 503 (typically) |
| `db.ErrNotFound` | Not mapped by WrapError; set manually when `sql.ErrNoRows` is detected | 404 |

### SDK Error Handling

The SDK client (`Client.do`) maps server error responses to `*APIError`:

```go
type APIError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}
```

For 4xx/5xx responses, `decodeErrorResponse` attempts to parse the JSON envelope. If parsing fails, it falls back to an `APIError` with `http.StatusText(statusCode)`. HTTP 304 responses produce the sentinel `ErrNotModified` error, checked via `errors.Is`.

The CLI translates `APIError` into JSON error output on stdout and sets the process exit code to 1 (API error) or 2 (client error).

---

## 8. Caching Strategy

### Three Cache Categories

apikit defines exactly three caching behaviors via the `CacheCategory` type:

| Category | `Cache-Control` Header | Applied To |
|----------|----------------------|------------|
| `CacheNoStore` (0) | `no-store` | All routes under the API mount point (group default) |
| `CacheNoCache` (1) | `no-cache` | Health probe endpoints (`/healthz`, `/readyz`) |
| `CachePublic` (2) | `public, max-age=300` | `/version`, `/auth/providers` |

`CacheMiddleware(cat CacheCategory)` returns an `echo.MiddlewareFunc` that sets the appropriate `Cache-Control` header. It is the exclusive owner of cache header management -- `securityHeadersMiddleware` explicitly does not set `Cache-Control`.

The API group has `CacheNoStore` pre-applied at construction time in `NewServer`, so all API routes default to `no-store` without any per-route configuration. Individual routes override this by applying `CacheMiddleware` with a different category.

### ETag Implementation

ETags provide conditional GET support for individual resource endpoints:

```go
// Setting (in handler, after fetching resource):
apikit.SetETag(c, resource.UpdatedAt)

// Checking (in handler, before querying):
if apikit.CheckETag(c, resource.UpdatedAt) {
    return c.NoContent(http.StatusNotModified)  // 304
}
```

**Format**: Weak ETags using RFC 3339 UTC timestamps: `W/"2026-07-17T14:30:00Z"`.

**Behavior**:
- `SetETag` sets the `ETag` response header. No-op for zero-value `time.Time`.
- `CheckETag` compares the `If-None-Match` request header against the computed ETag. Returns `true` when they match (client cache is current). Returns `false` for zero-value timestamps without checking the header.

**Endpoints supporting conditional GET**: `GET /user`, `GET /users/:id`, `GET /user/tokens/:token_id`, `GET /orgs/:id`, `GET /user/keys` (keys use a composite ETag incorporating revocation count).

The SDK surfaces ETags through the `Response[T]` wrapper:

```go
resp, _ := client.GetUser(ctx)
etag := resp.ETag()
// Later:
resp, err := client.GetUser(ctx, apikit.WithIfNoneMatch(etag))
if errors.Is(err, apikit.ErrNotModified) {
    // Cache is current
}
```

---

## 9. Database Layer

### SQLite with WAL Mode

apikit uses SQLite via `modernc.org/sqlite`, a pure-Go (CGo-free) SQLite implementation. The database layer is in `internal/db/`.

**Connection management**:
- `db.Open(path)` opens a file-based database with WAL (Write-Ahead Logging) mode enabled, foreign keys enforced, and the schema initialized.
- `db.OpenMemory()` opens an in-memory database for testing. WAL mode is skipped (not applicable to `:memory:` databases).
- Both set `MaxOpenConns(1)` and `MaxIdleConns(1)`, enforcing a single-connection pool. This is the standard SQLite best practice since SQLite does not support concurrent writers. WAL mode allows concurrent readers alongside the single writer.

**Path resolution**: The database path is resolved through a hierarchy:
1. `database.path` with a directory component (e.g. `"./name.db"`) is used as-is
2. Bare filename (e.g. `"myapp.db"`) combined with `$XDG_DATA_HOME` when set
3. `$XDG_DATA_HOME/apikit.db` when `database.path` is empty and `XDG_DATA_HOME` is set
4. `./data/apikit.db` as the fallback

Parent directories are created with mode `0700` by `Open`.

### Schema

Six tables created atomically in a single DEFERRED transaction using `CREATE TABLE IF NOT EXISTS` (idempotent):

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│    users      │     │     orgs     │     │ admin_config │
│──────────────│     │──────────────│     │──────────────│
│ id        PK │◄─┐  │ id        PK │◄─┐  │ key       PK │
│ username  UQ │  │  │ name      UQ │  │  │ value        │
│ email        │  │  │ slug      UQ │  │  └──────────────┘
│ full_name    │  │  │ url          │  │
│ role         │  │  │ status       │  │
│ status       │  │  │ created_at   │  │
│ provider     │  │  │ updated_at   │  │
│ provider_id  │  │  └──────────────┘  │
│ created_at   │  │                    │
│ updated_at   │  │  ┌──────────────┐  │
│              │  │  │ org_members  │  │
│ UQ(provider, │  ├──┤──────────────│  │
│  provider_id)│  │  │ org_id    FK─┼──┘ (ON DELETE CASCADE)
└──────────────┘  │  │ user_id   FK─┼──┐
                  │  │ created_at   │  │
┌──────────────┐  │  │ PK(org_id,   │  │
│   api_keys   │  │  │    user_id)  │  │
│──────────────│  │  └──────────────┘  │
│ key_id    PK │  │                    │
│ user_id   FK─┼──┤                    │
│ secret_hash  │  │  ┌──────────────┐  │
│ expires_days │  │  │     pats     │  │
│ expires_at   │  │  │──────────────│  │
│ revoked_at   │  │  │ token_id  PK │  │
│ created_at   │  │  │ user_id   FK─┼──┘
└──────────────┘  │  │ name         │
                  │  │ secret_hash  │
                  │  │ permissions  │
                  │  │ expires_days │
                  │  │ expires_at   │
                  │  │ revoked_at   │
                  │  │ created_at   │
                  │  └──────────────┘
                  │
                  └─── (all user_id FKs reference users.id)
```

All primary keys and foreign keys are `TEXT` (UUIDs). All timestamps are `TEXT` columns stored in the format `2006-01-02T15:04:05Z` (whole-second precision, UTC, literal Z suffix).

### Transaction Patterns

**`WithTx` -- Managed transaction helper:**

```go
func (d *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error
```

1. Begins a DEFERRED transaction
2. Calls `fn(tx)` -- the caller performs all operations through the `*sql.Tx`
3. If `fn` returns nil: commits (commit error propagated to caller)
4. If `fn` returns non-nil: rolls back (rollback error silently discarded), original error returned

The rollback error is intentionally discarded so the caller always sees the business-logic error, not a rollback failure.

**`Executor` interface -- Flexible transactional composition:**

```go
type Executor interface {
    ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
```

Satisfied by both `*sql.DB` and `*sql.Tx`. Repository functions accept `Executor` so callers can compose multiple operations inside a single transaction or run them standalone without the function needing to know which mode it is in.

---

## 10. Authentication and Authorization

### Credential Types

apikit supports three credential types, all delivered as `Bearer <token>` in the `Authorization` header:

| Type | Format | Scope | Admin-Level |
|------|--------|-------|-------------|
| Admin Token | `ak_admin_<64 hex chars>` | Global break-glass | Always |
| API Key | `ak_<key_id>_<secret>` | Full access for owning user | If user role is "admin" |
| PAT | `ak_pat_<token_id>_<secret>` | Scoped to declared permissions | Never (by design) |

Detection priority in `parseToken`: Admin token (longest prefix) is checked first, then PAT, then API key.

### Validation Flows

All three credential types follow a similar validation pipeline:

1. Format validation (fast rejection before any I/O)
2. Database lookup (credential existence)
3. Revocation check (`revoked_at IS NOT NULL`)
4. Expiry check (`expires_at` in the past)
5. Secret comparison (SHA-256 + `crypto/subtle.ConstantTimeCompare`)
6. User status check (blocked users get 403)

The admin token hashes the entire token string (including prefix) before comparison.

### Permission Model

The `PermissionRegistry` is a thread-safe set of `"resource_type:action"` strings. Six permissions are built in:

`users:read`, `orgs:read`, `keys:read`, `keys:manage`, `tokens:read`, `tokens:manage`

**Authorization hierarchy**:
- Admin tokens and API keys (with admin role) bypass permission checks entirely
- API keys (with user role) have implicit full access to own resources
- PATs are always scoped to their declared permissions, even if the underlying user is an admin

This is a deliberate design decision: PATs preserve scoping regardless of user role, preventing privilege escalation through PAT creation.

### Bootstrap and Admin Seeding

The bootstrap sequence (`internal/bootstrap`) handles first-boot admin provisioning:

1. **First boot** (no `admin_token_hash` in `admin_config` table): Requires `--admin-email` flag. Generates admin token, stores hash in `admin_config`, writes plaintext to `<config_dir>/admin_token` (mode 0600). The `admin_email` is stored for auto-promotion of the first matching OAuth user. The process exits after writing the token file — the server is not started.

2. **Subsequent boots** (`admin_token_hash` exists): Refuses to start if the `admin_token` file still exists on disk (forces the operator to save and delete it). Once the file is removed, the HTTP server starts normally. The admin token is validated at request time by the auth middleware, not at startup.

3. **Token rotation**: `--reset-admin-token` flag generates a new token regardless of boot state. The process exits after writing the token file — the server is not started.

Bootstrap runs conditionally: only when `--admin-email`, `--reset-admin-token`, or an existing `admin_token_hash` is present. Token generation operations (`--admin-email` on first boot, `--reset-admin-token`) are offline: they write the token and exit. The subsequent-boot path checks only the file-presence guard before allowing server startup.

---

## 11. Design Decisions

### Why Echo?

Echo provides the HTTP router, context, and middleware chain. apikit wraps it rather than exposing it directly -- consumers interact with `*Server`, `*echo.Group` (for route registration), and `echo.MiddlewareFunc` (for custom middleware). This limits the coupling surface.

### Why SQLite with Single-Connection Pool?

SQLite with `MaxOpenConns(1)` avoids write contention entirely. WAL mode enables concurrent reads alongside the single writer. This is appropriate for the target deployment model (single-instance API servers) and eliminates the operational complexity of a separate database server.

### Why Internal Packages?

All domain logic lives under `internal/` to enforce a clean public API boundary. The root package exposes only what consumers need: `Server`, `Config`, `Client`, middleware utilities, and type aliases. This prevents consumers from depending on implementation details.

### Why Type Aliases Instead of Wrapper Types?

`Config = config.Config`, `Provider = oauth.Provider`, etc. are type aliases (not wrapper types). This means consumers can use `*apikit.Config` transparently without any conversion, while the implementation details remain in internal packages.

### Why the `authctx` Package?

`internal/authctx` exists solely to break an import cycle. Both `internal/keys` and `internal/auth` need access to `AuthInfo` and the context helpers. Without `authctx`, they would need to import each other. The package has zero internal dependencies and contains only the `AuthInfo` struct, the typed context key, and three accessor functions.

### Why Two Key Generation Implementations?

`internal/oauth/key.go` and `internal/keys/generate.go` both generate API keys. The oauth package generates keys during the OAuth callback flow (where it has its own transaction), while the keys package generates keys for the refresh flow and provides the `GenerateAPIKey` function exposed at the root level. Both use `crypto/rand` and SHA-256 hashing. The duplication avoids an import cycle between the two packages.

### Why Runner-Based DI for Admin CLI Commands?

Admin CLI commands use Runner structs with function-valued fields (`UsersRunner`, `OrgsRunner`, etc.) rather than importing the SDK or handler packages. This breaks the import cycle between `internal/cli` and the root package. In production, `PersistentPreRunE` wires the runners to SDK client methods. In tests, the runners are wired to mock functions.

### Why JSON-Only CLI Output?

The CLI outputs all data as indented JSON to stdout and all human messages to stderr. There is no table output mode. This design makes the CLI composable with `jq` and other JSON tools, and ensures machine-readable output without format negotiation.

### Why Idempotent State-Change Operations?

All state-change operations (promote, demote, block, unblock, revoke) are idempotent. Repeating an operation when the target is already in the desired state returns success without modifying the database. This simplifies client logic and makes operations safe to retry.

### Why 404 Instead of 403 for Cross-User Resource Access?

Self-service endpoints query by both resource ID and authenticated user ID. When a user attempts to access another user's resource, the handler returns 404 "not found" rather than 403 "forbidden". This prevents existence leakage -- an attacker cannot determine whether a resource exists by observing the difference between 403 and 404.