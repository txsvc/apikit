# apikit

A batteries-included Go toolkit for building authenticated JSON APIs with OAuth, API keys, personal access tokens, and multi-tenant organizations.

## Overview

apikit provides the foundational infrastructure for building secure, production-ready JSON API servers in Go. It handles the concerns that every API project needs but nobody wants to build from scratch: configuration management, middleware stacks, authentication with multiple credential types, user and organization management, and a complete CLI for both operators and consumers.

The server is built on the [Echo](https://echo.labstack.com/) framework and uses SQLite (via the pure-Go [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) driver) for storage. Configuration follows XDG base directory conventions and is driven by a single `config.toml` file. The project ships two binaries: `apikit` (the server) and `akc` (the CLI client), plus a Go SDK for programmatic access.

apikit is designed to be used as a library. Import the root package, call three functions, and you have a running server. Mount your own routes on the API group and let apikit handle authentication, error formatting, health probes, and graceful shutdown.

## Features

- **Three-step server bootstrap** -- `LoadConfig`, `NewServer`, `Start`
- **TOML configuration** with XDG path resolution and sensible defaults
- **Middleware stack** -- panic recovery, request IDs, security headers, structured logging, body size limits, content-type enforcement
- **Health probes** -- `/healthz` (liveness), `/readyz` (readiness), `/version`
- **Three credential types** -- admin tokens, API keys, and personal access tokens (PATs)
- **OAuth integration** -- browser-based login flow with GitHub support (extensible via the `Provider` interface)
- **Permission-scoped PATs** -- fine-grained access control with a registry of `resource:action` pairs
- **User and organization management** -- full CRUD with admin endpoints, blocking, role promotion/demotion
- **Consistent JSON error envelope** -- `{"error": {"code": N, "message": "..."}}`
- **ETag support** -- conditional GET with `If-None-Match` / `304 Not Modified`
- **Go SDK client** -- typed client with generics, ETag support, and `ClientOption` pattern
- **CLI client (`akc`)** -- browser-based OAuth login, full admin command set
- **Embeddable command tree** -- import `apikit.RootCommand()` to add the full CLI to your own Cobra app
- **OpenAPI 3.1 spec** -- machine-readable API definition at `api/openapi.yaml`
- **Graceful shutdown** -- SIGTERM/SIGINT handling with a 15-second drain timeout
- **Pure-Go SQLite** -- no CGo, no external dependencies for the database

## Quick Start

```go
package main

import (
    "log"

    "github.com/txsvc/apikit"
)

func main() {
    // 1. Load configuration from config.toml (or use defaults)
    cfg, err := apikit.LoadConfig()
    if err != nil {
        log.Fatal(err)
    }

    // 2. Create the server (nil HealthChecker means always ready)
    server := apikit.NewServer(cfg, nil)

    // 3. Mount your own routes on the API group
    api := server.APIGroup() // pre-configured at the mount point
    api.GET("/hello", helloHandler)

    // 4. Start blocks until shutdown signal
    if err := server.Start(); err != nil {
        log.Fatal(err)
    }
}
```

## Installation

```bash
go get github.com/txsvc/apikit
```

To install the CLI and server binaries:

```bash
# From within the repository
make build
# Produces bin/apikit and bin/akc
```

## Project Structure

```
apikit.go                 # Root package exports (Config, LoadConfig, NewServer, etc.)
server.go                 # Server struct, lifecycle, health endpoints
middleware.go             # Middleware stack (all unexported, registered by NewServer)
errors.go                 # WriteAPIError, HTTPErrorHandler, error envelope
cache.go                  # CacheCategory, CacheMiddleware
etag.go                   # SetETag, CheckETag
timestamp.go              # NowUTC, FormatUTC, ParseUTC
sdk.go                    # SDK Client, NewClient, request pipeline
sdk_types.go              # All request/response/domain types
sdk_health.go             # Healthz, Readyz, Version
sdk_auth.go               # GetProviders, ExchangeOAuthCode
sdk_user.go               # Authenticated user endpoint methods
sdk_admin.go              # Admin user and organization methods
sdk_errors.go             # APIError, ErrNotModified, Response[T]
cli.go                    # Public RootCommand() for embedding
cmd/
  apikit/main.go          # Server binary entry point
  akc/main.go             # CLI client binary entry point
internal/
  config/                 # TOML config loading, validation, XDG resolution
  db/                     # SQLite database layer, schema, transactions
  auth/                   # Auth middleware, credential validation, permissions
  authctx/                # Shared auth context types (breaks import cycle)
  oauth/                  # OAuth provider interface, GitHub provider, callback flow
  handlers/               # HTTP handlers for users, orgs, PATs
  keys/                   # API key generation and key handlers
  bootstrap/              # First-boot admin seeding, token rotation
  cli/                    # CLI command implementations
api/
  openapi.yaml            # OpenAPI 3.1.0 specification
docs/                     # Documentation and ADRs
```

## Configuration

apikit uses a `config.toml` file for server configuration. The file is resolved using XDG conventions:

1. If `XDG_CONFIG_HOME` is set: `$XDG_CONFIG_HOME/apikit/config.toml`
2. Otherwise: `./config.toml` in the current working directory

When the config file is absent, all defaults are applied and the server starts normally.

```toml
[server]
port = 8080              # Listen port (0 = ephemeral, range: 0-65535)
bind = "0.0.0.0"         # Bind address
external_url = ""        # External URL for OAuth redirect validation
mount_point = "/api/v1"  # API route prefix
max_body_size = "1MB"    # Max request body (format: <int><KB|MB|GB>)

[database]
path = ""                # SQLite database path (see resolution below)

[logging]
level = "info"           # trace, debug, info, warn, error, fatal, panic

[[oauth.providers]]
name = "github"
client_id = ""
client_secret = ""
authorize_url = ""       # Defaults to GitHub's OAuth URL if empty
token_url = ""           # Defaults to GitHub's token URL if empty
userinfo_url = ""        # Defaults to GitHub's user API if empty
```

### Database Path Resolution

1. If `database.path` is set in config: used as-is
2. If `XDG_DATA_HOME` is set: `$XDG_DATA_HOME/apikit/apikit.db`
3. Otherwise: `./data/apikit.db`

See [docs/configuration.md](docs/configuration.md) for the complete configuration reference.

## Server

### Middleware Stack

`NewServer` registers middleware in this order (each layer wraps the next):

1. **Panic Recovery** -- catches panics, logs stack traces, returns 500
2. **Request ID** -- assigns or reuses a UUID v4 `X-Request-ID` header
3. **Security Headers** -- `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`
4. **Structured Logging** -- JSON log entries with method, path, status, duration, request_id (health probes logged at debug level)
5. **Body Size Limit** -- rejects oversized requests with 413
6. **Content-Type Enforcement** -- rejects POST/PUT/PATCH without `application/json` with 415

### Health Probes

Registered at the server root (not under the mount point):

| Endpoint | Cache | Behavior |
|---|---|---|
| `GET /healthz` | `no-cache` | Liveness. Always returns `{"status":"ok"}` |
| `GET /readyz` | `no-cache` | Readiness. Calls `HealthChecker` if provided |
| `GET /version` | `public, max-age=300` | Returns version, build, and mount point |

#### Verify the server is running

```sh
curl http://localhost:8080/healthz
# {"status": "ok"}

curl http://localhost:8080/readyz
# {"status": "ready"}
```

### API Mount Point

`server.APIGroup()` returns an Echo route group at the configured mount point (default `/api/v1`). The group has `Cache-Control: no-store` pre-applied. Mount your routes on this group:

```go
api := server.APIGroup()
api.GET("/widgets", listWidgets)
api.POST("/widgets", createWidget)
```

### Graceful Shutdown

The server handles `SIGTERM` and `SIGINT` signals automatically. On shutdown, it drains active connections with a 15-second timeout. `Shutdown()` uses `sync.Once` so concurrent calls are safe no-ops.

After `Start()` begins listening, call `server.Addr()` to get the actual bound `host:port` (useful with ephemeral port 0).

## Authentication

apikit supports three credential types, all delivered as `Bearer <token>` in the `Authorization` header. The token prefix defaults to `ak` and is configurable at build time via `-ldflags`.

### Admin Token

```
ak_admin_<64 hex characters>
```

Break-glass credential with global scope. Generated during bootstrap and stored as a SHA-256 hash in the `admin_config` table. The plaintext is written to a file on first boot and must be saved securely.

### API Key

```
ak_<key_id>_<secret>
```

User-scoped credential issued during OAuth login. The 8-character `key_id` identifies the key; the 32-character `secret` is hashed with SHA-256 for storage and returned only once. API keys carry implicit full permissions for the owning user.

### Personal Access Token (PAT)

```
ak_pat_<token_id>_<secret>
```

Fine-grained, permission-scoped credential. Each PAT carries a list of `resource:action` permission strings. PATs are never treated as admin-level, even if the underlying user has the admin role.

**Built-in permissions:**

| Permission | Description |
|---|---|
| `users:read` | Read own user profile |
| `orgs:read` | Read own organization memberships |
| `keys:read` | List own API keys |
| `keys:manage` | Refresh and revoke own API keys |
| `tokens:read` | List and view own PATs |
| `tokens:manage` | Create and revoke PATs |

See [docs/authentication.md](docs/authentication.md) for the full authentication and authorization guide.

## SDK Client

The Go SDK provides a typed client for all API endpoints.

### Creating a Client

```go
client := apikit.NewClient("https://api.example.com",
    apikit.WithAPIKey("ak_abCdEfGh_..."),
    apikit.WithHTTPClient(customHTTPClient),
    apikit.WithMountPoint("/api/v1"),
    apikit.WithRequestID("trace-123"),
)
```

### Example Usage

```go
ctx := context.Background()

// Get the authenticated user's profile
resp, err := client.GetUser(ctx)
if err != nil {
    log.Fatal(err)
}
fmt.Println(resp.Data.Username)

// Conditional GET with ETag
etag := resp.ETag()
resp2, err := client.GetUser(ctx, apikit.WithIfNoneMatch(etag))
if errors.Is(err, apikit.ErrNotModified) {
    // Resource hasn't changed
}

// List organizations
orgs, err := client.ListUserOrgs(ctx)

// Create a PAT
expires := 90
pat, err := client.CreateToken(ctx, &apikit.CreateTokenRequest{
    Name:        "ci-token",
    Permissions: []string{"users:read", "orgs:read"},
    Expires:     &expires,
})
// pat.Token contains the one-time plaintext secret
```

### Error Handling

API errors are returned as `*apikit.APIError` with `Code` and `Message` fields:

```go
user, err := client.GetUserByID(ctx, "nonexistent-id")
var apiErr *apikit.APIError
if errors.As(err, &apiErr) {
    fmt.Printf("HTTP %d: %s\n", apiErr.Code, apiErr.Message)
}
```

## CLI (akc)

`akc` is the command-line client for apikit servers.

### Commands

```bash
# Authenticate via browser-based OAuth
akc login --provider github --expires 90

# User profile
akc user show
akc user update --full-name "Jane Doe"

# API keys
akc keys list
akc keys refresh
akc keys revoke

# Personal access tokens
akc tokens create --name "ci" --permissions "users:read,orgs:read" --expires 90
akc tokens list
akc tokens show <token_id>
akc tokens revoke <token_id>

# Organizations
akc orgs list

# Admin commands (require admin credentials)
akc admin users list --include-blocked
akc admin users create --username alice --email alice@example.com \
    --provider github --provider-id 12345
akc admin users promote <id>
akc admin users block <id>
akc admin orgs create --name "Acme" --slug acme --url "https://acme.com"
akc admin orgs members add <org_id> <user_id>

# Version information
akc version
```

See [docs/cli.md](docs/cli.md) for the complete CLI reference.

## API Reference

The full API is defined in the OpenAPI 3.1.0 specification at [`api/openapi.yaml`](api/openapi.yaml).

See [docs/api-reference.md](docs/api-reference.md) for the complete endpoint reference.

## Development

### Prerequisites

- Go 1.25+

### Make Targets

```bash
make build       # Build bin/apikit and bin/akc
make test        # Run all tests
make lint        # Run go vet
make check-spec  # Validate OpenAPI spec
make check       # Run lint + test + check-spec
make clean       # Remove build artifacts
```

### Build-Time Variables

Version and build info are injected via `-ldflags`:

```bash
go build -ldflags "-X github.com/txsvc/apikit.Version=1.0.0 \
    -X github.com/txsvc/apikit.Build=$(git rev-parse --short HEAD) \
    -X github.com/txsvc/apikit.TokenPrefix=ak" \
    ./cmd/apikit
```

| Variable | Default | Purpose |
|---|---|---|
| `Version` | `dev` | Semantic version string |
| `Build` | `dev` | Short git commit SHA |
| `TokenPrefix` | `ak` | Token namespace prefix |

## Documentation

- [Architecture](docs/architecture.md) -- package layout, middleware pipeline, request lifecycle
- [Authentication](docs/authentication.md) -- credential types, bootstrap, authorization model
- [API Reference](docs/api-reference.md) -- complete HTTP endpoint reference
- [CLI Reference](docs/cli.md) -- akc command reference
- [Database](docs/database.md) -- schema, transactions, error handling
- [Configuration](docs/configuration.md) -- config.toml reference

## License

MIT -- see [LICENSE](LICENSE) for details.
