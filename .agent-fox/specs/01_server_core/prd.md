---
spec_id: '01'
spec_name: server_core
title: Server Core
status: draft
created_at: '2026-07-17T09:50:21.277352+00:00'
updated_at: '2026-07-17T10:18:38.125907+00:00'
owner: ''
source: interactive
schema_version: 1
---
# Server Core

## Source

This spec is derived from the [apikit master PRD](docs/PRD.md). It covers the
foundational server infrastructure upon which all other specs depend.

## Intent

Server Core provides the HTTP server skeleton, configuration loading, health
probes, request lifecycle middleware, structured logging, error handling, and
the handler registration API. It is the first thing that must work before
authentication, database, or business logic can be layered on.

A consuming project imports apikit, calls `apikit.LoadConfig()` to obtain a
`*apikit.Config`, creates a server with `apikit.NewServer(cfg, checker)`,
registers its own Echo handlers on the group returned by `APIGroup()`, and
calls `server.Start()`. Server Core owns everything between "binary starts"
and "first handler runs," plus the cross-cutting middleware that wraps every
handler.

## Goals

- Provide a fully configured Echo HTTP server loadable from `config.toml`.
- Support XDG base directory conventions for config and data paths.
- Expose Kubernetes-compatible health probe endpoints.
- Implement graceful shutdown with a fixed 15-second drain timeout.
- Generate a unique request ID per request and propagate it in headers and logs.
- Enforce JSON content type, request body size limits, and consistent error responses.
- Set appropriate Cache-Control headers and support ETag / If-None-Match.
- Apply a standard set of HTTP security hardening headers on every response.
- Normalize all timestamps to RFC 3339 UTC with Z suffix.
- Provide a handler registration API so consuming projects can extend the API surface.
- Support a build-time configurable token prefix variable.

## Non-Goals

- Database schema creation or queries (covered by a database spec).
- Authentication middleware and token validation (covered by an auth spec).
- OAuth provider registry and login flow (covered by an OAuth spec).
- Any specific business domain handlers (users, orgs, keys, tokens).
- Rate limiting, CORS, TLS termination (explicitly deferred in the master PRD).
- Metrics (Prometheus), distributed tracing (OpenTelemetry), and profiling endpoints (explicitly out of scope for this spec).

## Dependencies

None. This is a foundation spec with no upstream dependencies. The `/readyz`
health probe accepts an injected health-check function and does not import or
depend on the `database_layer` spec — see [Readiness Probe](#health-probe-endpoints-public-outside-mount-point).

---

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25+ |
| HTTP framework | Echo v4 (`github.com/labstack/echo/v4`) |
| Config format | TOML (`github.com/BurntSushi/toml`) |
| Logging | logrus (`github.com/sirupsen/logrus`) |
| UUID generation | `github.com/google/uuid` |
| Build | `make build` compiles binaries to `bin/` |
| Test | `make test` runs `go test ./...` |
| Lint | `make lint` runs `go vet ./...` |

## Repository Layout (relevant packages)

```
internal/
  config/       Server configuration loading (TOML, XDG) — internal implementation
cmd/
  apikit/       Minimal reference binary demonstrating the caller usage contract
```

The root module (`github.com/txsvc/apikit`) exposes the public API:
`LoadConfig()`, `Config`, `NewServer()`, handler registration, and server
lifecycle. The `Version`, `Build`, and `TokenPrefix` build-time variables also
live at the root module level so that consuming projects can override them via
`-ldflags`.

The `internal/config` package is an **implementation detail**. Its types and
functions are re-exported through the root `apikit` package so that external
consumers never need to import `internal/config` directly — see
[Config Package Re-Export](#config-package-re-export).

---

## Functional Requirements

### Caller Usage Contract

The intended usage pattern is:

1. Call `apikit.LoadConfig()` to obtain a `*apikit.Config`. `LoadConfig()`
   handles file I/O, XDG resolution, defaults, and validation. It returns
   `(nil, error)` on malformed or invalid config. (`LoadConfig()` is a
   re-export of `internal/config.Load()` — see
   [Config Package Re-Export](#config-package-re-export).)
2. Pass the `*apikit.Config` (and an optional `HealthChecker`) to
   `NewServer()`. `NewServer()` never calls `LoadConfig()` internally — it
   performs no file I/O and never returns a config-related error. This
   separation keeps `NewServer()` focused on server construction and makes
   tests straightforward (pass a test `*apikit.Config` directly without
   touching the filesystem).
   **`NewServer()` panics with a descriptive message if `cfg` is nil** — a
   nil config is a programming error, not a runtime condition, and fail-fast
   behaviour surfaces the mistake immediately.
3. Register handlers via `APIGroup()`.
4. Call `Start()`, which blocks until the server shuts down.

```go
cfg, err := apikit.LoadConfig()
if err != nil {
    log.Fatal(err)
}
srv := apikit.NewServer(cfg, myHealthChecker)
srv.APIGroup().GET("/widgets", listWidgets)
if err := srv.Start(); err != nil {
    log.Fatal(err)
}
```

### `cmd/apikit` Reference Binary

The `cmd/apikit` binary is a **minimal reference implementation** whose sole
purpose is to demonstrate the caller usage contract and serve as both a smoke
test and documentation-by-example. It:

1. Calls `apikit.LoadConfig()`.
2. Calls `apikit.NewServer(cfg, nil)` (no custom health checker).
3. Calls `server.Start()`.

It registers **no additional handlers** beyond the built-in endpoints
(`/healthz`, `/readyz`, `/version`). When run, it produces a functioning
server that responds to health probes and the version endpoint, confirming
that the library wires up correctly.

`make build` compiles this binary to `bin/apikit`. It is not intended for
production use — production consumers implement their own `main` package
following the same three-step pattern. The reference binary is also useful
as a manual integration smoke test: starting it and curling `/healthz`
confirms the build is sound.

The reference binary is covered by the standard `make test` (`go test ./...`)
build check (compilation is verified) but does not have dedicated functional
test cases in the test suite — its behavior is fully covered by the
integration tests for the built-in endpoints.

### Server Configuration

The server loads `config.toml` from the current directory (TOML format).
`XDG_CONFIG_HOME` and `XDG_DATA_HOME` override default locations for config
and data.

#### Example `config.toml`

The following annotated example shows all supported keys with their default
values. All fields are optional; omitting a key is equivalent to specifying
its default value.

```toml
[server]
port         = 8080          # HTTP listen port; 0 = OS-assigned ephemeral port
bind         = "0.0.0.0"     # Bind address; validated by the OS at Start() time
external_url = ""            # Public URL for OAuth redirect URIs; stored as-is, no validation
mount_point  = "/api/v1"     # Base path for all API routes
max_body_size = "1MB"        # Max request body size; valid suffixes: KB, MB, GB

[database]
path = "./data/apikit.db"    # SQLite database file path; overridden by XDG_DATA_HOME

[logging]
level = "info"               # Log level: trace, debug, info, warn, error, fatal, panic
```

#### Startup Failure Behavior

- If `config.toml` is **absent**, `LoadConfig()` returns a `*apikit.Config`
  populated entirely with defaults and a nil error. This enables zero-config
  development use without requiring a config file.
- If `config.toml` **exists but is malformed** (invalid TOML syntax),
  `LoadConfig()` returns `(nil, error)` with a descriptive parse error. The
  server must not start with a malformed config.
- If the listen port is already in use when `Start()` is called, `Start()`
  returns a non-nil error. The server does **not** call `os.Exit()` — that
  responsibility belongs to `main()`. This keeps the library composable.

#### Configuration Settings

| Setting | Default | Required | Description |
|---------|---------|----------|-------------|
| `[server] port` | `8080` | No | HTTP listen port (0–65535); port `0` binds to an OS-assigned ephemeral port (useful for tests) |
| `[server] bind` | `0.0.0.0` | No | Bind address; any non-empty string is accepted by `LoadConfig()` — the OS is the authoritative validator at `Start()` time |
| `[server] external_url` | `""` (empty string) | No | Public URL for OAuth redirect URI; stored as-is with no validation by Server Core. When absent or empty, `Config.Server.ExternalURL` holds an empty string. Server Core takes no action on an empty value — detecting absence and enforcing requirements is the responsibility of the OAuth spec. |
| `[server] mount_point` | `/api/v1` | No | Base path for all API routes |
| `[server] max_body_size` | `1MB` | No | Maximum request body size (see format below) |
| `[database] path` | `./data/apikit.db` | No | SQLite database file path (resolved and stored by Server Core; consumed at startup by the database layer spec) |
| `[logging] level` | `info` | No | Log level: `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic` |

All fields are optional in `config.toml`; defaults are applied programmatically
in `LoadConfig()` when a field is absent or when the file is missing entirely.

> **Note on `[database] path`:** This field lives in Server Core's config
> struct because `LoadConfig()` is the single config-loading entrypoint for
> the entire process. The `database_layer` spec consumes the resolved path
> value from `*apikit.Config` at startup. Server Core itself performs no
> database operations.

#### Config Validation Rules

`LoadConfig()` validates the parsed configuration before returning. Validation
failures return `(nil, error)`:

- `[server] port`: must be an integer in the range 0–65535. Port `0` is valid
  and causes the OS to assign an ephemeral port (see [Port 0 / Ephemeral Port Support](#port-0--ephemeral-port-support)).
- `[server] bind`: `LoadConfig()` accepts any non-empty string. An empty
  string is replaced with the default `0.0.0.0`. Bind address format is
  **not** validated by `LoadConfig()`; the OS is the authoritative validator
  and will reject an invalid address when `Start()` attempts to bind. This
  avoids re-implementing OS-level IP/hostname validation in `LoadConfig()`.
- `[server] external_url`: `LoadConfig()` performs **no validation**. The
  value is stored as-is (empty string when absent) and passed through to
  consumers. Format validation (e.g. must be a well-formed URL) is the
  responsibility of the OAuth spec, which is the primary consumer of this
  field. Server Core takes no action on an empty or missing `external_url`.
- `[logging] level`: must be one of `trace`, `debug`, `info`, `warn`, `error`,
  `fatal`, `panic` (case-insensitive). Invalid values return an error.
- `[server] max_body_size`: must match the format `<number><suffix>` where
  suffix is `KB`, `MB`, or `GB` (case-insensitive, no spaces). Examples:
  `512KB`, `1MB`, `2GB`. Plain byte integers are not accepted. An empty
  string (`max_body_size = ""`) is treated as absent — the default of `1MB`
  is applied and no error is returned. Invalid non-empty values return an
  error. The parsed result is stored internally as `int64` bytes and exposed
  via `MaxBodyBytes()`.

#### MaxBodySize Format

- Accepted format: a positive integer immediately followed by a unit suffix.
- Valid suffixes: `KB`, `MB`, `GB` (case-insensitive).
- Examples of valid values: `512KB`, `1MB`, `2GB`, `512kb`, `1mb`.
- Examples of invalid values: `1 MB` (space), `1048576` (no suffix), `1TB`
  (unsupported suffix), `0MB` (zero or negative).
- An empty string is treated as absent; the default `1MB` is applied with no error.
- Any other invalid value causes `LoadConfig()` to return an error.

#### XDG Support

- When `XDG_CONFIG_HOME` is set, config is loaded from
  `$XDG_CONFIG_HOME/apikit/config.toml` instead of `./config.toml`.
  - If `XDG_CONFIG_HOME` is set but `$XDG_CONFIG_HOME/apikit/config.toml`
    does not exist, `LoadConfig()` applies defaults directly and returns a nil
    error. It does **not** fall back to `./config.toml`. When `XDG_CONFIG_HOME`
    is set it is authoritative; looking for config in the working directory
    after an XDG path is configured would violate the XDG contract.
- When `XDG_DATA_HOME` is set, the default database path resolves to
  `$XDG_DATA_HOME/apikit/apikit.db`. `LoadConfig()` stores this resolved path
  string in `Config.Database.Path` without performing any filesystem operations
  (no directory creation, no existence check). Directory creation is deferred
  to the `database_layer` spec, which is responsible for all database
  filesystem setup. `LoadConfig()` must not have filesystem side effects
  beyond reading the config file itself.
- When neither is set, the server looks for `config.toml` in the current
  working directory.

### Health Probe Endpoints (public, outside mount point)

Health probes are registered at the server root, not under the mount point.
They do not require authentication.

| Method | Path | Cache-Control | Description |
|--------|------|---------------|-------------|
| `GET` | `/healthz` | `no-cache` (`CacheNoCache`) | Liveness probe — always returns HTTP 200 with `{"status": "ok"}` |
| `GET` | `/readyz` | `no-cache` (`CacheNoCache`) | Readiness probe — invokes the injected health-check function; returns 200 `{"status": "ready"}` or 503 `{"status": "not ready"}` |
| `GET` | `/version` | `public, max-age=300` (`CachePublic`) | Returns server version, build info, and configured mount point. Uses `CachePublic` because it returns static build-time information that does not change while the server is running. |

#### Readiness Probe — Health-Check Injection

- `NewServer()` accepts an optional `HealthChecker func() error` parameter.
- When a `HealthChecker` is provided, `/readyz` calls it; a non-nil error
  causes a 503 response.
- When no `HealthChecker` is provided (nil), `/readyz` always returns 200
  `{"status": "ready"}` without performing any external check.
- This design keeps Server Core free of any dependency on `database_layer`.
  The database layer (or any other component) plugs in by supplying a
  `func() error` at startup.

`/version` response shape:
```json
{
  "version": "<string>",
  "build": "<string>",
  "mount_point": "<string>"
}
```

- `version` — semantic version string (e.g. `"1.0.0"`), read from the
  `apikit.Version` package-level variable in the root module. Defaults to
  `"dev"` when not overridden at compile time (i.e. when the binary is built
  without `-ldflags`). If overridden via `-ldflags` to an empty string, the
  endpoint returns an empty string — no fallback to `"dev"` is performed at
  runtime; the caller is responsible for supplying a meaningful value.
- `build` — short git commit SHA (e.g. `"abc1234"`), read from the
  `apikit.Build` package-level variable in the root module. Defaults to
  `"dev"` when not overridden at compile time. If overridden via `-ldflags`
  to an empty string, the endpoint returns an empty string — no runtime
  fallback is performed.
- `mount_point` — the configured API mount point (e.g. `"/api/v1"`).

Both `Version` and `Build` are declared as package-level variables in the
root `apikit` package (not in `cmd/apikit/main.go`), so that consuming
projects can inject their own values via `-ldflags` targeting
`github.com/txsvc/apikit.Version` and `github.com/txsvc/apikit.Build`.
The Makefile sets both flags automatically when building from a git
repository using `git describe --tags` for `Version` and
`git rev-parse --short HEAD` for `Build`, injected via:
```makefile
LDFLAGS := -ldflags "-X github.com/txsvc/apikit.Version=$(VERSION) \
                      -X github.com/txsvc/apikit.Build=$(BUILD)"
```

When building outside a git repository (e.g. `git describe --tags` or
`git rev-parse --short HEAD` fail), the Makefile falls back to `dev` for
both `VERSION` and `BUILD` using shell conditionals. The package-level
variable defaults (`"dev"`) already handle this case at runtime; the Makefile
fallback ensures the build does not fail in environments without git history
(CI archives, vendor builds, etc.):

```makefile
VERSION := $(shell git describe --tags 2>/dev/null || echo "dev")
BUILD   := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
```

Consuming projects that override these variables should target the same
`github.com/txsvc/apikit` import path in their own `-ldflags`.

Build-time variable declarations (in the root `apikit` package):
```go
var Version = "dev" // overridden by -ldflags at build time
var Build   = "dev" // overridden by -ldflags at build time
```

### Graceful Shutdown

- The server listens for `SIGTERM` and `SIGINT` signals.
- On signal receipt, the server stops accepting new connections and waits up
  to **15 seconds** (fixed compile-time constant) for in-flight requests to
  complete (drain timeout).
- If requests do not complete within 15 seconds, the server force-closes and
  exits.
- The drain timeout is logged at `info` level when shutdown begins.
- The drain timeout is not user-configurable; it is defined as a named
  constant in the server package: `const drainTimeout = 15 * time.Second`.
- `Shutdown(ctx context.Context)` may also be called externally (e.g., in
  tests) to trigger graceful shutdown programmatically.
- **Signal-triggered shutdown**: when the server receives `SIGTERM` or
  `SIGINT`, the signal handler calls `Shutdown(context.Background())`. The
  15-second drain timeout is applied inside `Shutdown()` via
  `context.WithTimeout(context.Background(), drainTimeout)`. The caller's
  context is `context.Background()` in this case — the 15-second drain
  timeout is the sole bound.

#### Logging During the Drain Window

- In-flight requests that complete successfully during the 15-second drain
  window produce **normal structured log entries**, identical in format and
  level to entries produced during normal operation. The drain window does
  not suppress or alter request logging.
- The shutdown initiation itself is logged at `info` level with a message
  indicating graceful shutdown has begun and the drain timeout duration.

#### Shutdown Context and Timeout Semantics

The `ctx` parameter passed to `Shutdown()` and the internal 15-second drain
timeout are combined such that **the earlier of the two wins**:

- If the caller's context is cancelled or its deadline expires before the
  15-second drain timeout, shutdown proceeds to force-close immediately.
- If the 15-second drain timeout expires first, shutdown also proceeds to
  force-close immediately, regardless of the caller's context state.
- This is standard Go context composition: `Shutdown()` internally creates a
  derived context using `context.WithTimeout(ctx, drainTimeout)` and passes
  it to Echo's `(*Echo).Shutdown()`. The derived context is cancelled by
  whichever source (caller cancellation or internal timeout) fires first.
- When triggered by an OS signal, `ctx` is `context.Background()` (no
  external deadline), so the 15-second drain timeout is the sole bound.

#### Shutdown Idempotency

- `Shutdown()` is idempotent. If called more than once — whether sequentially
  or concurrently — only the first call initiates the shutdown sequence. All
  subsequent calls return `nil` immediately without blocking.
- This is implemented using `sync.Once` (or an equivalent atomic guard) so
  that concurrent calls (e.g., a SIGTERM arriving while a test also calls
  `Shutdown()`) are safe and deterministic.
- After `Shutdown()` has returned from the first call, `Start()` will also
  have returned. Any subsequent call to `Shutdown()` remains a no-op.

### Port 0 / Ephemeral Port Support

- The server supports `[server] port = 0` in config, which instructs the OS
  to assign an available ephemeral port.
- After `Start()` begins listening (but before it blocks waiting for
  shutdown), the actual bound address is available via `Server.Addr() string`.
- `Addr()` returns the full `host:port` string of the listener (e.g.,
  `"127.0.0.1:54321"`).
- `Addr()` returns an empty string if called before `Start()` has bound the
  listener or after the server has shut down.
- This is the standard mechanism for integration tests to discover the
  listening port without hard-coding one, preventing port collisions in
  parallel or CI environments.

### Start() Blocking Contract

- `Start()` is a **blocking call**. It binds the listener, begins serving
  requests, and does not return until the server has fully shut down (either
  via signal or an external `Shutdown()` call).
- `Start()` returns a non-nil error if the port is already in use or any
  other network-level failure occurs during bind or serve — including when
  `[server] bind` contains an address that the OS rejects.
- `Start()` returns `nil` after a clean graceful shutdown.
- If `Start()` is called a second time on the same `Server` instance — for
  example after a clean graceful shutdown — it returns a **non-nil error
  immediately**. A server that has been shut down cannot be restarted; callers
  must create a new `Server` instance via `NewServer()` instead. This is a
  programming error and the error message should clearly indicate that the
  server has already been shut down.
- Callers must not assume `Start()` returns quickly; it should always be
  called in a goroutine if the caller needs to perform other work
  concurrently (e.g., registering signal handlers, running tests).

### Middleware Execution Order

Middleware is applied in the following fixed order for every request that
reaches a registered handler. This order is deterministic and must not be
changed without updating this spec:

1. **Panic Recovery** — catches any unhandled panic in downstream middleware
   or handlers, logs the stack trace at `error` level, and returns a standard
   JSON error envelope (HTTP 500) via `APIError`. Echo's built-in recovery
   middleware is **not** used; a custom implementation ensures all panic
   responses match the consistent error envelope.
2. **Request ID** — assigns a UUID v4 (or reuses a validated incoming
   `X-Request-ID`) so all subsequent middleware and log entries carry the
   request ID.
3. **Body Size Limit** — rejects oversized payloads with HTTP 413 before any
   further processing, preventing unnecessary work.
4. **Content-Type Enforcement** — rejects POST/PUT/PATCH requests with a
   non-JSON Content-Type with HTTP 415.
5. **Security Headers** — sets security hardening headers (`X-Content-Type-Options`,
   `X-Frame-Options`, `Referrer-Policy`) on every response. **This middleware
   does NOT set `Cache-Control`**; cache behavior is managed separately (see
   [Cache-Control Headers](#cache-control-headers)).
6. **Logging** — wraps the handler to capture the final HTTP status code and
   request duration for the structured log entry.
7. **Handler** — the registered route handler executes.

This ordering means:
- A 413 response will include a request ID (assigned in step 2).
- A 415 response will include a request ID and will have already passed the
  size limit check.
- The log entry always captures the actual status code returned, including
  error codes produced by middleware.
- Panics are recovered before logging, so the log entry captures the 500
  status code produced by the recovery middleware.

#### Cache-Control and Security Headers: Separation of Concerns

The Security Headers middleware (step 5) is responsible **only** for the
three security-related headers: `X-Content-Type-Options`, `X-Frame-Options`,
and `Referrer-Policy`. It never sets `Cache-Control`.

`Cache-Control` is set by a dedicated cache mechanism:
- The `APIGroup()` Echo group has `CacheMiddleware(CacheNoStore)` pre-applied
  as group-level middleware (default for all consumer routes).
- Health probe routes (`/healthz`, `/readyz`) have `CacheMiddleware(CacheNoCache)`
  applied internally.
- The `/version` route has `CacheMiddleware(CachePublic)` applied internally.
- Per-route `CacheMiddleware()` calls by consumers override the group-level
  default (Echo's per-route middleware takes precedence over group-level
  middleware for the same header).

There is no conflict or precedence ambiguity between Security Headers and
Cache-Control because they are completely separate headers managed by
completely separate mechanisms.

### Panic Recovery Middleware

- The server uses a **custom** panic recovery middleware (not Echo's built-in
  `middleware.Recover()`).
- When a panic occurs in any downstream middleware or handler, the recovery
  middleware:
  1. Recovers the panic value.
  2. Logs the panic value and full stack trace at `error` level using logrus,
     including the following structured fields:
     - `request_id` — the assigned request ID for the panicking request.
     - `panic` — the recovered panic value (converted to string via `fmt.Sprintf("%v", recovered)`).
     - `stack_trace` — the full goroutine stack trace as a string (captured via `runtime/debug.Stack()`).
  3. Returns an HTTP 500 response using `APIError()` so the response matches
     the standard JSON error envelope with `Content-Type: application/json; charset=utf-8`.
- Echo's default recovery middleware is explicitly disabled. Enabling it in
  addition to the custom middleware would result in duplicate recovery and
  inconsistent error responses.

### Request ID Middleware

- Every request is assigned a UUID (v4) as its request ID.
- The request ID is set in the `X-Request-ID` response header.
- The same request ID is included in the structured log entry for the request.
- If the incoming request already contains an `X-Request-ID` header, the
  server validates the value before reusing it:
  - **Accepted format**: UUID v4 — exactly 36 characters in the canonical
    `xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx` format (lowercase or uppercase hex
    digits with dashes at positions 9, 14, 19, 24).
  - **Validation**: the value is parsed using `uuid.Parse()`. If parsing
    succeeds and the version is 4, the incoming value is reused. Otherwise,
    a new UUID v4 is generated and used instead.
  - **Rationale**: accepting only UUID v4 format prevents log injection
    attacks, caps the value at a known fixed length (36 characters), and
    ensures consistent request ID format across all log entries and response
    headers regardless of whether the ID was client-supplied or server-generated.

### Structured JSON Logging

- Logging uses logrus configured for JSON output.
- Every HTTP request produces a structured log entry with at minimum:

  | Field | Type | Description |
  |-------|------|-------------|
  | `method` | string | HTTP method (e.g. `"GET"`, `"POST"`) |
  | `path` | string | Request path (e.g. `"/api/v1/widgets"`) |
  | `status` | integer | HTTP response status code (e.g. `200`, `404`) |
  | `duration` | float64 | Request duration in **milliseconds** (e.g. `12.345`). Float64 gives sub-millisecond precision while remaining numeric for log aggregation tools. |
  | `request_id` | string | The UUID v4 value from `X-Request-ID` |

- Log level is configurable via `[logging] level` in `config.toml`.

#### Health Probe Log Level

- Requests to `/healthz` and `/readyz` are logged at **`debug` level**
  regardless of the configured log level. This prevents Kubernetes liveness
  and readiness probe traffic (which can fire every few seconds) from
  polluting production logs.
- Health probe log entries are **identical in structure** to other request log
  entries — they include `method`, `path`, `status`, `duration` (float64 ms),
  and `request_id` fields. The `duration` field is always present, even for
  health probe entries emitted at `debug` level.
- When the configured log level is `debug` or `trace`, health probe entries
  are visible. At `info` and above they are suppressed.
- `/version` is not a health probe and is logged at the normal request log
  level.

### Content-Type Enforcement

- Content-Type enforcement applies automatically to all **POST, PUT, and
  PATCH** requests based on HTTP method. GET, DELETE, HEAD, and OPTIONS are
  never subject to this check.
- Requests matching those methods with a `Content-Type` other than
  `application/json` are rejected with HTTP 415 (Unsupported Media Type).
- All responses — including error responses produced by middleware — set
  `Content-Type: application/json; charset=utf-8`. The `APIError()` helper
  function is responsible for setting this header on all middleware-generated
  error responses (HTTP 413, 415, 500), ensuring consistent content-type
  regardless of how early in the middleware chain the error occurs.

### Request Body Size Limit

- The maximum request body size is configurable via `[server] max_body_size`
  in `config.toml`, with a default of `1MB`.
- Requests exceeding this limit are rejected with HTTP 413 (Payload Too
  Large) via `APIError()`, which sets `Content-Type: application/json; charset=utf-8`.
- See [MaxBodySize Format](#maxbodysize-format) for accepted config values.

### Security Hardening Headers

The following HTTP headers are set on **every response** by the Security
Headers middleware (step 5 in middleware execution order). **This middleware
sets only these three headers and does not set `Cache-Control`** — cache
behavior is managed by a separate, dedicated cache middleware (see
[Cache-Control Headers](#cache-control-headers)):

| Header | Value | Purpose |
|--------|-------|---------|
| `X-Content-Type-Options` | `nosniff` | Prevents MIME-type sniffing |
| `X-Frame-Options` | `DENY` | Prevents clickjacking via iframes |
| `Referrer-Policy` | `no-referrer` | Prevents referrer leakage |

These headers are not configurable — they represent universally appropriate
defaults. There is no overlap or conflict with `Cache-Control`, which is
managed entirely separately.

### Cache-Control Headers

The server sets `Cache-Control` headers on responses by endpoint category.
Three named categories are defined:

| Category Constant | Header Value | Default Usage |
|-------------------|--------------|---------------|
| `CacheNoStore` | `Cache-Control: no-store` | All endpoints under mount point that modify or return mutable data |
| `CacheNoCache` | `Cache-Control: no-cache` | Health probes (`/healthz`, `/readyz`) |
| `CachePublic` | `Cache-Control: public, max-age=300` | Static discovery endpoints (e.g. `/auth/providers`) and `/version` |

#### Default Cache Behavior for Consumer Routes

`CacheNoStore` is applied automatically to **all routes registered under the
mount point** via `APIGroup()`. This is an **opt-out** model: the
`APIGroup()` Echo group has `CacheMiddleware(CacheNoStore)` pre-applied as
group-level middleware. Consumers override the cache behavior for specific
routes by attaching `CacheMiddleware()` with a different category directly to
that route — Echo's per-route middleware takes precedence over group-level
middleware for the `Cache-Control` header.

Built-in routes (health probes, `/version`) apply their respective cache
categories internally and are not affected by the mount-point group middleware.

Cache-Control is managed entirely by `CacheMiddleware` — the Security Headers
middleware (step 5) is a completely separate concern and never sets
`Cache-Control`. There is no precedence conflict between them.

### ETag / If-None-Match Support

- Single-resource GET endpoints support conditional requests via `ETag` and
  `If-None-Match`.
- The ETag value is a **weak ETag** derived from the resource's `updated_at`
  timestamp formatted as RFC 3339 UTC with Z suffix.
  - Format: `W/"<RFC3339-UTC-timestamp>"`, e.g. `W/"2026-07-17T14:30:00Z"`.
  - Weak ETags are used because the server does not guarantee byte-for-byte
    identical response representations.
- When a request includes `If-None-Match` and the value matches the current
  ETag, the server returns HTTP 304 (Not Modified) with no body.
- This is implemented as utility functions that handlers can call, not as
  blanket middleware.
- When a resource has no meaningful `updated_at` timestamp (e.g., a newly
  created resource being returned in the creation response, or a synthetic
  computed response), the handler should **not** call `SetETag()` / `CheckETag()`.
  ETag support is opt-in per handler; handlers that cannot derive a stable,
  meaningful timestamp simply omit ETag headers. Callers will receive a normal
  200 response with no `ETag` header.
- **Zero-time behavior**: if `SetETag()` or `CheckETag()` is called with the
  zero value of `time.Time` (i.e. `time.Time{}`), the function is a **no-op**
  — no `ETag` header is set and no `If-None-Match` check is performed. This is
  the safest possible fallback and prevents panics from edge-case callers.
  Handlers are still expected to avoid calling these functions with a zero
  time; the no-op behavior is a safety net, not intended usage.

### Error Handling

All API errors use a consistent JSON envelope:

```json
{
  "error": {
    "code": 409,
    "message": "Human-readable description"
  }
}
```

The `code` field is an integer, not a string.

All error responses — whether from handlers or from middleware (body size
limit, content-type enforcement, panic recovery) — must use `APIError()` to
produce this envelope. `APIError()` always sets `Content-Type: application/json; charset=utf-8`
on the response, ensuring consistency regardless of where in the middleware
chain the error is produced.

`APIError()` returns the error value from `c.JSON()` directly. This propagates
any underlying write error to Echo's error handler, which is the standard Echo
pattern. Handlers return the result directly:

```go
return APIError(c, 404, "not found")
```

Standard error codes used by Server Core:

| Status | Meaning |
|--------|---------|
| 400 | Bad request — malformed JSON, missing required fields |
| 413 | Payload too large — request body exceeds configured limit |
| 415 | Unsupported media type — Content-Type is not `application/json` on POST/PUT/PATCH |
| 500 | Internal server error — also returned for unhandled panics via the custom recovery middleware |

### Timestamp Normalization

- All timestamps in API responses use RFC 3339 format normalized to UTC with
  the `Z` suffix (e.g. `2026-07-17T14:30:00Z`).
- Timestamps with timezone offsets are never produced.
- Incoming timestamps with offsets are accepted by `ParseUTC` and normalized
  to UTC before being returned — the conversion happens inside `ParseUTC`
  itself. Callers receive a UTC `time.Time` guaranteed; no additional
  conversion step is required on the caller's part.
- A utility function handles this normalization for use by all handlers.

### Configurable Mount Point

- All built-in API endpoints are registered under a configurable mount point
  (default: `/api/v1`).
- Health probes (`/healthz`, `/readyz`, `/version`) are outside the mount
  point and always at the server root.
- The mount point is set via `[server] mount_point` in `config.toml`.

### Handler Registration API

Consuming projects extend the API surface by registering their own Echo
handlers. The registration API provides:

- A method to register handlers under the configured mount point.
- Access to the Echo group for the mount point so consumers can define their
  own sub-routes via `APIGroup()`.
- **Post-shutdown behavior**: `APIGroup()` always returns the already-constructed
  Echo group, even if called after `Shutdown()` has completed. It never returns
  nil and never panics. It is the caller's responsibility not to register
  handlers after shutdown begins. Routes registered after shutdown may never
  receive traffic, as the server is no longer accepting connections. This
  contract is documented in the `APIGroup()` godoc.
- **Concurrent registration safety**: `APIGroup()` itself is safe to call
  from multiple goroutines concurrently (it returns the same pre-created Echo
  group each time). However, registering routes on the returned `*echo.Group`
  concurrently is subject to Echo's own concurrency guarantees. To avoid
  races, all route registration should be completed in a single goroutine
  before `Start()` is called. `APIGroup()` is only safe to call after
  `NewServer()` has returned; concurrent calls during `NewServer()` construction
  are not supported and would be a programming error.
- Registered handlers automatically inherit all middleware (panic recovery,
  request ID, body size limit, content-type enforcement, security headers,
  logging) in the order defined in [Middleware Execution Order](#middleware-execution-order).
- Routes registered via `APIGroup()` have `CacheNoStore` applied by default
  (opt-out model; see [Cache-Control Headers](#cache-control-headers)).
- A `CacheMiddleware` constructor that returns Echo middleware for a given
  cache category, allowing consumers to override cache behaviour per-route or
  per-group.

#### Cache Middleware Constructor

```go
// CacheCategory identifies one of the three supported cache behaviours.
type CacheCategory int

const (
    CacheNoStore CacheCategory = iota // Cache-Control: no-store (default for mutable resources)
    CacheNoCache                       // Cache-Control: no-cache (health probes)
    CachePublic                        // Cache-Control: public, max-age=300 (static discovery)
)

// CacheMiddleware returns Echo middleware that sets the appropriate
// Cache-Control header for the given category.
func CacheMiddleware(cat CacheCategory) echo.MiddlewareFunc
```

Consumers apply the middleware directly to routes or groups to override the
default `CacheNoStore` applied at the group level:

```go
cfg, err := apikit.LoadConfig()
if err != nil {
    log.Fatal(err)
}
server := apikit.NewServer(cfg, nil) // nil = no health checker
api := server.APIGroup()             // returns the Echo group at the mount point
                                     // CacheNoStore is pre-applied to this group

api.GET("/widgets", listWidgets)     // inherits CacheNoStore from group
api.POST("/widgets", createWidget)   // inherits CacheNoStore from group
api.GET("/providers", listProviders, apikit.CacheMiddleware(apikit.CachePublic)) // overrides to CachePublic
server.Start()
```

### Build-Time Configurable Token Prefix

- The token prefix is a build-time variable with a default value of `ak`.
- It lives in the **root `apikit` package** so that all consumers — including
  the auth spec, CLI tooling, and any other component — can reference it from
  a single import path. This avoids duplicating the variable across packages
  and ensures every component uses the same prefix. Consuming packages import
  it as `apikit.TokenPrefix`.
- Consuming projects override it at compile time using `-ldflags` targeting
  `github.com/txsvc/apikit.TokenPrefix`. This single override point covers all
  consumers simultaneously.
- The prefix is exposed via the public API so other packages can reference it.
- Variable declaration: `var TokenPrefix = "ak"` (overridable via linker,
  declared in the root `apikit` package).

---

## Interfaces

### Config Package Re-Export

The `internal/config` package is the implementation home for config loading
and the `Config` type hierarchy. Because Go's `internal/` directory restriction
prevents external importers from directly using `github.com/txsvc/apikit/internal/config`,
the root `apikit` package re-exports the public surface:

```go
// Config is the re-exported configuration type. External consumers use
// apikit.Config; the underlying type is internal/config.Config.
type Config = config.Config

// LoadConfig reads config.toml respecting XDG_CONFIG_HOME and returns a
// fully validated *Config. It is a thin re-export of internal/config.Load().
// External consumers call apikit.LoadConfig(); they never need to import
// internal/config directly.
func LoadConfig() (*Config, error) {
    return config.Load()
}
```

This pattern keeps the internal implementation private (no external package
can import `internal/config` directly) while providing a clean, stable public
API surface. The `Config` type alias means that `*apikit.Config` and
`*config.Config` are the same type — no wrapping or conversion is needed when
passing the value to `NewServer()`.

### Public API (root module)

```go
// HealthChecker is a function that returns nil when the server is ready
// to serve traffic, or an error when it is not.
type HealthChecker func() error

// NewServer creates a configured Echo server from the given config.
// checker may be nil; if nil, /readyz always returns 200.
// NewServer never calls LoadConfig() internally; the caller is responsible
// for loading config first and passing the result here.
//
// NewServer panics with a descriptive message if cfg is nil. A nil config
// is a programming error — the caller must always check the error returned
// by LoadConfig() before passing the result to NewServer.
//
// NewServer must be called before APIGroup(). It is not safe to call
// APIGroup() concurrently with or before NewServer() returns.
func NewServer(cfg *Config, checker HealthChecker) *Server

// Server wraps the Echo instance and provides lifecycle management.
type Server struct { ... }

// APIGroup returns the Echo group at the configured mount point.
// Consuming projects register their handlers on this group.
// CacheNoStore is pre-applied to this group as the default Cache-Control policy.
//
// APIGroup always returns the same pre-constructed Echo group. It is safe to
// call from multiple goroutines after NewServer() has returned. It never
// returns nil and never panics, even if called after Shutdown() has completed.
// However, routes registered after Shutdown() has been called may never
// receive traffic — the server no longer accepts connections. All route
// registration should be completed before Start() is called.
//
// It is not safe to call APIGroup() concurrently with NewServer() construction.
func (s *Server) APIGroup() *echo.Group

// Start binds the listener and begins serving requests. It is a blocking
// call that does not return until the server has fully shut down (either via
// OS signal or an external Shutdown() call). Returns a non-nil error if the
// port is already in use, the bind address is rejected by the OS, or another
// network-level failure occurs. Returns nil after a clean graceful shutdown.
// Does not call os.Exit(). Callers that need to perform concurrent work
// should call Start() in a goroutine.
//
// If Start() is called a second time on the same Server instance (e.g., after
// a clean shutdown), it returns a non-nil error immediately. A shut-down
// server cannot be restarted; create a new Server instance via NewServer().
func (s *Server) Start() error

// Addr returns the actual bound address of the listener as a "host:port"
// string (e.g. "0.0.0.0:54321"). This is useful when the server is
// configured with port 0 (OS-assigned ephemeral port). Returns an empty
// string if called before Start() has bound the listener or after the
// server has shut down.
func (s *Server) Addr() string

// Shutdown gracefully stops the server. It is idempotent: the first call
// initiates shutdown; all subsequent calls return nil immediately without
// blocking. It may be triggered internally by SIGTERM/SIGINT or called
// externally (e.g., in tests).
//
// The earlier of the caller's context cancellation/deadline and the fixed
// 15-second internal drain timeout wins. Internally, Shutdown() creates a
// derived context via context.WithTimeout(ctx, drainTimeout) and passes it
// to Echo's shutdown. If the caller's context is cancelled before the 15
// seconds elapse, force-close proceeds immediately; if the 15-second timeout
// fires first, force-close also proceeds immediately.
//
// When triggered by an OS signal (SIGTERM/SIGINT), the signal handler calls
// Shutdown(context.Background()), making the 15-second drain timeout the
// sole bound on the shutdown duration.
func (s *Server) Shutdown(ctx context.Context) error
```

### Config Package (internal/config)

```go
// Config holds all server configuration loaded from TOML.
// Re-exported at the root package level as apikit.Config.
type Config struct {
    Server   ServerConfig
    Database DatabaseConfig
    Logging  LoggingConfig
}

type ServerConfig struct {
    Port        int    `toml:"port"`
    Bind        string `toml:"bind"`
    // ExternalURL stores the public URL for OAuth redirect URIs exactly as
    // provided in config.toml. When the field is absent or set to an empty
    // string, ExternalURL holds "". Server Core takes no action on an empty
    // ExternalURL — detecting absence and enforcing requirements is the
    // responsibility of the OAuth spec.
    ExternalURL string `toml:"external_url"`
    MountPoint  string `toml:"mount_point"`
    MaxBodySize string `toml:"max_body_size"`
    maxBodyBytes int64  // parsed internal representation, not exported directly
}

// MaxBodyBytes returns the parsed body size limit in bytes.
// The raw string MaxBodySize is the TOML-facing field; the parsed int64
// is computed once during Load() and exposed via this getter.
func (c *ServerConfig) MaxBodyBytes() int64

type DatabaseConfig struct {
    // Path is the resolved SQLite database file path. When XDG_DATA_HOME is
    // set, this resolves to $XDG_DATA_HOME/apikit/apikit.db; otherwise it
    // defaults to ./data/apikit.db. Load() stores the resolved path string
    // without creating any directories — directory creation is the
    // responsibility of the database_layer spec.
    Path string `toml:"path"`
}

type LoggingConfig struct {
    Level string `toml:"level"`
}

// Load reads config.toml respecting XDG_CONFIG_HOME.
// If config.toml is absent (or XDG_CONFIG_HOME is set but the file does not
// exist at the XDG path), all defaults are applied and nil error is returned.
// XDG_CONFIG_HOME is authoritative; Load() does not fall back to ./config.toml
// when XDG_CONFIG_HOME is set.
// If config.toml exists but is malformed, (nil, error) is returned.
// If config.toml is present but contains invalid field values (port out of
// range, invalid log level, invalid max_body_size), (nil, error) is returned.
// An empty string for max_body_size is treated as absent; the default 1MB
// is applied with no error.
// The bind address and external_url fields are not validated by Load(); bind
// is validated at Start() time by the OS, and external_url validation is the
// responsibility of the OAuth spec. An absent or empty external_url results
// in an empty string stored in Config.Server.ExternalURL with no error.
// Load() never performs network I/O and does not interact with NewServer().
// Load() has no filesystem side effects beyond reading the config file — it
// does not create directories, write files, or validate that paths exist.
// External consumers call apikit.LoadConfig() rather than this function directly.
func Load() (*Config, error)
```

### Error Helper

```go
// APIError returns a JSON error response in the standard envelope and sets
// Content-Type: application/json; charset=utf-8 on the response. All
// middleware and handler error paths must use this function to ensure
// consistent error envelopes and content-type headers across the entire
// response surface, including errors produced before handler execution.
//
// APIError returns the error value from c.JSON() directly, propagating any
// underlying write error to Echo's error handler. Handlers should return
// the result of APIError directly:
//
//   return APIError(c, 404, "not found")
func APIError(c echo.Context, code int, message string) error
```

### Timestamp Utility

```go
// NowUTC returns the current time as RFC 3339 UTC with Z suffix.
func NowUTC() string

// FormatUTC formats a time.Time as RFC 3339 UTC with Z suffix.
func FormatUTC(t time.Time) string

// ParseUTC parses any valid RFC 3339 timestamp — including those with
// timezone offsets (e.g., "2026-07-17T14:30:00+05:00") — and normalizes
// the result to UTC before returning. The UTC conversion is performed
// inside ParseUTC; callers receive a UTC time.Time guaranteed and do not
// need to perform any additional conversion.
// On parse failure, returns (time.Time{}, error) — the zero value of
// time.Time. Callers must always check the returned error before using
// the time value, consistent with standard Go conventions.
func ParseUTC(s string) (time.Time, error)
```

### ETag Utility

```go
// SetETag sets a weak ETag header derived from the updatedAt timestamp.
// The ETag format is: W/"<RFC3339-UTC-timestamp>", e.g. W/"2026-07-17T14:30:00Z".
// Handlers that cannot derive a stable, meaningful updatedAt timestamp
// (e.g., newly created resources, synthetic computed responses) should
// not call SetETag — omitting the header is correct behavior in those cases.
//
// If updatedAt is the zero value of time.Time, SetETag is a no-op: no ETag
// header is set. This is a safety net for edge-case callers; handlers should
// not rely on this behavior and should avoid calling SetETag with a zero time.
func SetETag(c echo.Context, updatedAt time.Time)

// CheckETag checks If-None-Match against the weak ETag for updatedAt.
// Returns true if the client's cached version is still current
// (handler should return 304 Not Modified with no body).
// Handlers that cannot derive a stable updatedAt should not call CheckETag.
//
// If updatedAt is the zero value of time.Time, CheckETag is a no-op and
// always returns false (no 304 will be issued).
func CheckETag(c echo.Context, updatedAt time.Time) bool
```

### Cache Middleware

```go
// CacheCategory identifies one of the three supported cache behaviours.
type CacheCategory int

const (
    CacheNoStore CacheCategory = iota
    CacheNoCache
    CachePublic
)

// CacheMiddleware returns Echo middleware that sets Cache-Control headers
// for the given category. This is entirely separate from the Security Headers
// middleware — CacheMiddleware manages only Cache-Control; Security Headers
// manages only X-Content-Type-Options, X-Frame-Options, and Referrer-Policy.
func CacheMiddleware(cat CacheCategory) echo.MiddlewareFunc
```

### Build-Time Variables

All build-time variables are declared in the root `apikit` package so that
consuming projects can override them via `-ldflags` without forking the
reference binary.

```go
// TokenPrefix is the token prefix used for token parsing throughout all
// components (auth spec, CLI, etc.). It lives in the root package so that
// a single -ldflags target overrides it for all consumers simultaneously.
// Consuming packages import it as apikit.TokenPrefix.
// Override at compile time: -ldflags '-X github.com/txsvc/apikit.TokenPrefix=myprefix'
var TokenPrefix = "ak"

// Version is the semantic version string. Defaults to "dev" for non-release
// builds (i.e. when the binary is built without -ldflags). The /version
// endpoint reads this variable directly. If overridden via -ldflags to an
// empty string, the /version endpoint returns an empty string — no runtime
// fallback to "dev" is performed.
// Override at compile time: -ldflags '-X github.com/txsvc/apikit.Version=1.0.0'
var Version = "dev"

// Build is the short git commit SHA injected at compile time. Defaults to
// "dev" for non-release builds (i.e. when the binary is built without
// -ldflags). The /version endpoint reads this variable directly. If
// overridden via -ldflags to an empty string, the /version endpoint returns
// an empty string — no runtime fallback to "dev" is performed.
// Override at compile time: -ldflags '-X github.com/txsvc/apikit.Build=abc1234'
var Build = "dev"
```

---

## Testing Strategy

All public functions and middleware must have test coverage. No formal
percentage target is set, but the following categories are required:

### Unit Tests

- All middleware functions (panic recovery, request ID assignment, body size
  limiting, content-type enforcement, security headers, cache headers, logging).
- `CachePublic` middleware behavior in isolation — verify that a handler
  decorated with `CacheMiddleware(CachePublic)` produces
  `Cache-Control: public, max-age=300` on the response. Similarly verify
  `CacheNoStore` and `CacheNoCache` in isolation.
- Request ID middleware — valid UUID v4 passthrough, invalid/non-UUID value
  replacement with a new UUID, missing header generates new UUID.
- All utility functions (`NowUTC`, `FormatUTC`, `ParseUTC`, `SetETag`,
  `CheckETag`, `APIError`).
- Panic recovery middleware — verify that a panicking handler returns HTTP 500
  with the standard JSON error envelope (`Content-Type: application/json; charset=utf-8`)
  and does not propagate the panic. Verify that the structured log entry
  includes `panic` and `stack_trace` fields.
- `LoadConfig()` / `Load()` — covering: missing file (defaults), malformed
  TOML (error), valid file, invalid port, invalid log level, invalid
  max_body_size.
- `LoadConfig()` — `max_body_size = ""` (empty string): verify default `1MB`
  is applied and no error is returned (empty string treated as absent).
- `LoadConfig()` — `external_url` absent: verify `Config.Server.ExternalURL`
  is `""` and no error is returned.
- `LoadConfig()` — `external_url = "not-a-url"`: verify the value is stored
  as-is and no error is returned (no validation by Server Core).
- `LoadConfig()` — XDG path: `XDG_CONFIG_HOME` set, file absent → defaults
  applied (no fallback to `./config.toml`).
- `LoadConfig()` — XDG data path: `XDG_DATA_HOME` set →
  `Config.Database.Path` resolves to `$XDG_DATA_HOME/apikit/apikit.db`;
  verify no directory creation occurs as a side effect.
- `LoadConfig()` — bind address: empty string in config is replaced with
  default `0.0.0.0`; non-empty strings (including invalid IP formats) are
  stored as-is without error.
- `MaxBodyBytes()` getter — verify it returns the correct parsed byte value
  for representative inputs.
- Config validation edge cases (boundary port values including port 0,
  case-insensitive log levels, case-insensitive size suffixes).
- `ParseUTC` — verify that a timestamp with a non-UTC timezone offset (e.g.,
  `"2026-07-17T14:30:00+05:00"`) is accepted and the returned `time.Time` is
  in UTC (i.e. `.UTC()` location and correct wall-clock time).
- `ParseUTC` — verify that invalid input returns `(time.Time{}, error)` (zero
  value of `time.Time`).
- Shutdown idempotency — verify that calling `Shutdown()` twice concurrently
  and sequentially both result in the second call returning nil immediately
  without blocking or panicking.
- `Start()` called after shutdown — verify it returns a non-nil error
  immediately with a message indicating the server has already been shut down.
- `APIError()` — verify response sets `Content-Type: application/json; charset=utf-8`
  and produces the correct JSON envelope for various status codes. Verify that
  the return value is the error from `c.JSON()` (not always nil).
- `SetETag()` with zero `time.Time` — verify no `ETag` header is set (no-op).
- `CheckETag()` with zero `time.Time` — verify returns false and no 304 is
  issued (no-op).
- `NewServer()` with nil `cfg` — verify it panics with a descriptive message.
- Structured log entry fields — verify that a captured log entry for a normal
  request contains `method` (string), `path` (string), `status` (integer),
  `duration` (float64, milliseconds), and `request_id` (string) fields with
  the correct types.
- Security Headers middleware — verify it sets `X-Content-Type-Options`,
  `X-Frame-Options`, and `Referrer-Policy` but does NOT set `Cache-Control`.
- Cache middleware and Security Headers middleware in combination — verify that
  when both are applied, `Cache-Control` is set by the cache middleware and
  the three security headers are set by the security middleware with no
  interference between them.

### Integration Tests

- Full server startup and graceful shutdown cycle (SIGTERM handling, drain
  timeout behavior).
- External `Shutdown()` call (e.g. from a test goroutine) completes within the
  drain timeout and does not block indefinitely.
- Shutdown context cancellation — verify that if a caller-supplied context is
  cancelled before the 15-second drain timeout, force-close proceeds
  immediately (the earlier of the two wins).
- Signal-triggered shutdown — verify that the signal handler calls
  `Shutdown(context.Background())` and that the server shuts down cleanly
  within the drain timeout with no external context deadline.
- Concurrent `Shutdown()` calls — both return nil; server shuts down exactly
  once.
- In-flight requests that complete during the drain window produce normal
  structured log entries (same format and level as regular request logs).
- Health probe endpoints (`/healthz`, `/readyz` with and without a
  HealthChecker, `/version`).
- `/version` response body contains `"version": "dev"` and `"build": "dev"`
  when the binary is built without `-ldflags` overrides, confirming the
  default values flow through to the endpoint.
- `/version` response includes `Cache-Control: public, max-age=300` header.
- `/healthz` and `/readyz` responses include `Cache-Control: no-cache` header.
- Middleware ordering verification (e.g. confirm 413 includes `X-Request-ID`).
- Middleware error responses (413, 415, 500) all include
  `Content-Type: application/json; charset=utf-8`.
- Handler registration via `APIGroup()` and route reachability.
- `APIGroup()` called after `Shutdown()` returns the group without panicking
  or returning nil.
- Default `CacheNoStore` header present on consumer-registered routes; override
  with `CacheMiddleware(CachePublic)` on a specific route produces
  `Cache-Control: public, max-age=300`.
- ETag / If-None-Match round-trip (200 with ETag on first request, 304 on
  conditional re-request).
- **Port allocation**: integration tests use `[server] port = 0` in their
  test configs. After calling `Start()` (in a goroutine), tests call
  `server.Addr()` to discover the actual bound address and construct request
  URLs. This eliminates port collisions in parallel and CI environments.
- `Server.Addr()` returns empty string before `Start()` is called and after
  shutdown completes.
- Health probe requests are not logged at `info` level; verify log output is
  absent (or present at `debug` level when log level is set to `debug`).
- Health probe log entries at `debug` level include the `duration` field as
  a float64 millisecond value.
- `Start()` returns a non-nil error when `[server] bind` contains a string
  that the OS rejects (e.g. `bind = "not-an-ip"`); verify `Start()` does not
  panic and returns promptly.
- `Start()` called a second time on the same instance after shutdown returns
  a non-nil error immediately; verify no panic and the error message indicates
  the server has already been shut down.
- `apikit.LoadConfig()` (re-export) behaves identically to `internal/config.Load()`;
  verify that calling `apikit.LoadConfig()` with a missing config file returns
  defaults and nil error (smoke-test the re-export path).
- `ParseUTC` integration — verify that a handler storing a timestamp via
  `ParseUTC` with an offset input (e.g. `+05:00`) and returning it via
  `FormatUTC` produces a `Z`-suffixed UTC string in the response.

---

## Owner

Michael Kuehl
