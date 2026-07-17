# apikit

## Intent

apikit is a Go library for building authenticated REST API services and their
companion SDKs and CLIs.

It provides the foundational infrastructure that every API-based application
needs: OAuth authentication, access control, API key and personal access token
management, user and organization administration, health probes, structured
logging, and graceful lifecycle management.

A consuming project imports apikit as a Go module, configures it, registers its
own Echo handlers to extend the API surface, and builds its own binary. The
built-in endpoints cover authentication, user management, organization
management, and credential lifecycle. The consuming project adds its
domain-specific endpoints on top.

apikit also ships a reference CLI (`akc`) that is both a standalone tool for
managing the auth/user layer of any apikit-based service and an embeddable
Cobra command tree that consuming projects can incorporate into their own CLI
binaries.

Go and Python SDKs provide typed client libraries for the built-in endpoints.
Both SDKs derive from the OpenAPI 3.1 specification that serves as the single
source of truth for the API contract.

## Design Principles

1. **OpenAPI-first.** The OpenAPI 3.1 specification is the single source of
   truth for the API contract. The server implements it; the SDKs and
   documentation derive from it.
2. **Follow GitHub REST API conventions** unless there is a specific reason to
   diverge. When in doubt, look at how GitHub's REST API handles it.
3. **Configurable, not branded.** Token prefixes, config paths, and route mount
   points are configurable so consuming projects can use their own branding.
4. **Extend by registering handlers.** Consuming projects add functionality by
   registering Echo handlers, not by forking or wrapping apikit.

## Goals

- Provide a pluggable OAuth-based authentication system, shipping with GitHub
  as the first provider.
- Offer an admin bootstrap mechanism for initial system access on a fresh
  deployment, with support for token rotation.
- Implement role-based access control with an admin role and fine-grained
  personal access tokens (PATs).
- Ship a reference CLI and Go/Python SDKs as first-class deliverables.
- Produce a complete OpenAPI 3.1 specification that serves as the API contract.

## Non-Goals

- **Rate limiting.** Not implemented in the first iteration.
- **Database migration tooling.** Schema is applied on boot via
  `CREATE TABLE IF NOT EXISTS`.
- **Additional OAuth providers beyond GitHub.** Others will be added later via
  the same provider interface. The provider registry is designed for this.
- **Billing, metering, or usage tracking.**
- **OS keychain or secret-store integration.** CLI tokens are stored in
  plaintext with restricted file permissions only.
- **Multi-profile or named-context CLI support.** One active endpoint URL and
  one API key at a time.
- **Windows-specific path conventions.** The CLI targets Unix-like systems
  using `$HOME`.
- **Pagination.** List endpoints return all results without pagination in this
  iteration.
- **User deletion.** Users can be blocked but not deleted. Blocked users
  remain in the database for audit trail integrity — they may appear in logs,
  org membership history, and token provenance records.
- **CORS.** Not implemented in the first iteration. API consumers are
  server-side SDKs and CLIs, not browsers.
- **TLS termination.** Not a concern of apikit. TLS is handled by the
  ingress proxy / reverse proxy in front of the server.

---

## Credential Model

apikit uses three credential types. All share a configurable prefix
(documented as `<prefix>` throughout). The prefix is a **build-time
variable** with a default value (e.g. `ak`). Consuming projects override it
at compile time to use their own branding. The prefix is embedded in the
binary and used by both server (to parse incoming tokens) and CLI (to
determine the config directory `$HOME/.<prefix>/`).

### Admin Token (break-glass)

| Aspect       | Detail |
|--------------|--------|
| Format       | `<prefix>_admin_<64 hex chars>` |
| Created by   | Auto-generated on first boot or admin token rotation |
| Scope        | Global — full access to all endpoints and all resources |
| Quantity     | Exactly one |
| Storage      | SHA-256 hash in database; plaintext written to file (mode 0600) |
| Primary use  | Break-glass emergency access when no admin user is available |

The admin token is **not intended for day-to-day use.** Normal admin
operations are performed by users with the admin role using their regular API
key. The admin token exists for two scenarios:
1. **First boot** — before any users exist, the admin token is the only
   credential that can access the API.
2. **Break-glass** — if all admin users are blocked, locked out, or the
   OAuth provider is unavailable, the admin token provides emergency access.

### API Key

| Aspect       | Detail |
|--------------|--------|
| Format       | `<prefix>_<key_id>_<secret>` |
| Created by   | Issued automatically on OAuth login |
| Scope        | User-scoped — full access to own resources |
| Quantity     | One per user; auto-rotated on re-login |
| Storage      | `key_id` stored in plaintext; `secret` stored as SHA-256 hash |
| Expiry       | 0 (indefinite), 30, 60, or 90 days; default 90 |
| Primary use  | Interactive CLI usage, direct API access |

The `key_id` is a random 8-character alphanumeric identifier. The `secret` is a
random 32-character alphanumeric string. The full key (including plaintext
secret) is returned only at creation and on refresh.

### Personal Access Token (PAT)

| Aspect       | Detail |
|--------------|--------|
| Format       | `<prefix>_pat_<token_id>_<secret>` |
| Created by   | User creates explicitly via API or CLI |
| Scope        | Fine-grained — per-resource-type + action permissions |
| Quantity     | Multiple per user |
| Storage      | `token_id` stored in plaintext; `secret` stored as SHA-256 hash |
| Expiry       | Same options as API key: 0, 30, 60, or 90 days; default 90 |
| Primary use  | Scripts, CI pipelines, integrations, agents |

PATs use a fine-grained permission model. Each PAT is granted a set of
permissions, where each permission is a `resource_type:action` pair.

Built-in resource types and actions:

| Resource type | Actions | Description |
|---------------|---------|-------------|
| `users`       | `read`  | View user profiles |
| `orgs`        | `read`  | View organizations and memberships |
| `keys`        | `read`, `manage` | View and manage API keys |
| `tokens`      | `read`, `manage` | View and manage PATs |

Consuming projects register additional resource types and actions via the
apikit configuration. The permission model is extensible by design.

A PAT can only grant permissions that its owning user already has. A PAT
cannot escalate beyond the user's own access level.

### Authentication Flow

All API endpoints (except health probes and OAuth endpoints) require
authentication via a Bearer token in the `Authorization` header:

```
Authorization: Bearer <prefix>_admin_...
Authorization: Bearer <prefix>_abc12345_...
Authorization: Bearer <prefix>_pat_...
```

The server identifies the credential type by its format prefix and validates
accordingly:
- Admin token: SHA-256 hash compared against stored hash
- API key: `key_id` lookup, then SHA-256 hash verification of the secret
- PAT: `token_id` lookup, then SHA-256 hash verification of the secret,
  then permission check against the requested operation

Expired or revoked credentials are rejected with HTTP 401. Blocked users are
rejected with HTTP 403 on every authenticated request, regardless of
credential validity. PATs created by blocked users are effectively inert — if
the user is unblocked, their PATs resume working.

### User Roles

Each user has a `role` field: `admin` or `user` (default).

- **Admin users** have full access to all endpoints and all resources via
  their regular API key. Multiple users can hold the admin role.
- **Regular users** have full access to their own resources only.

Admin role changes:
- On first boot, the server requires `--admin-email <email>`. When a user
  with that email authenticates via OAuth for the first time, they are
  automatically granted the admin role.
- Admin users can promote other users to admin via
  `POST /users/:id/promote` and demote them via `POST /users/:id/demote`.
- An admin cannot demote themselves if they are the last admin user.

### Access Control

| Level | Credential | Scope | Description |
|-------|------------|-------|-------------|
| Break-glass | Admin token | Global | Emergency-only full access via infrastructure credential |
| Admin | API key (admin user) | Global | Full access to all endpoints and all resources |
| User  | API key | Per-user | Full access to own resources |
| Scoped | PAT | Per-permission | Access limited to granted `resource_type:action` pairs |

---

## Functional Requirements

### First Boot and Admin Bootstrap

On first boot (zero users in the database), the server requires the
`--admin-email <email>` flag. This designates which user will become the
first admin when they authenticate via OAuth.

First boot sequence:
1. The server stores the designated admin email.
2. It generates a cryptographically random admin token (break-glass
   credential) in the format `<prefix>_admin_<64 hex chars>`.
3. The SHA-256 hash of the token is stored in the database.
4. The plaintext token is written to an `admin_token` file (mode 0600) next
   to `config.toml`.
5. The server logs the file path at `warn` level.
6. The server starts normally, ready to accept OAuth logins.

When the designated email authenticates via OAuth for the first time, that
user is automatically granted the `admin` role. The server logs this event
at `info` level.

On subsequent boots:
1. The server checks whether the `admin_token` file exists on disk. If it
   does, the server **refuses to start** and logs an error instructing the
   operator to save the token securely and delete the file. This prevents
   the plaintext admin token from persisting on disk beyond the initial
   retrieval.
2. Once the file is removed, the server reads `ADMIN_TOKEN` from the
   environment, hashes it with SHA-256, and compares against the stored
   hash. The server refuses to start if the variable is missing or the hash
   does not match.
3. The `--admin-email` flag is ignored on subsequent boots (an admin already
   exists).

### Admin Token Rotation

The server accepts a `--reset-admin-token` flag on boot. When set:
1. A new admin token is generated (same flow as first boot: new token,
   hash stored, plaintext written to `admin_token` file).
2. The old token is invalidated immediately.
3. The server starts normally for this boot.
4. On the next restart, the same file-presence check applies — the
   operator must save the new token and delete the file before the server
   will start again.

### OAuth Provider Registry

The system authenticates users via a pluggable OAuth provider registry. Each
provider implements a common interface:
- Authorize URL construction
- Authorization code exchange for tokens
- User info extraction (username, email, provider-specific ID)

The first iteration ships with **GitHub**. GitHub's well-known URLs are
built-in defaults; `authorize_url`, `token_url`, and `userinfo_url` in config
are optional overrides for GitHub Enterprise or custom deployments.

Adding a new provider requires registering it in the registry with its URLs
and field mappings — no changes to auth middleware or handlers.

If a provider is removed from configuration, existing users authenticated
through that provider retain their API keys and PATs. Those users cannot
re-authenticate via OAuth until they authenticate through another configured
provider.

### OAuth Login Flow (CLI)

1. `akc login --provider github` fetches the provider list from the server.
2. The CLI opens the authorization URL in the user's browser, including a
   cryptographically random `state` parameter for CSRF protection.
3. The CLI starts a local HTTP callback server on a random available port.
4. The browser completes the OAuth flow and redirects to the local callback.
5. The CLI validates the `state` parameter and captures the authorization code.
6. The CLI exchanges the code with the server via `POST /auth/callback`.
7. The server validates that `redirect_uri` matches the configured allowlist:
   - Development: `http://localhost:*`
   - Production: derived from `external_url` in server config
   - Mismatched URIs are rejected with HTTP 400.
8. The server exchanges the code with the identity provider and retrieves user
   info. If the provider returns a null or empty email, login fails with an
   error — email is a required field.
9. The server upserts the user: creates if new, updates username/email if
   existing. Blocked users are not re-activated on OAuth login.
10. The server generates a new API key for the user, revoking any previously
    active key for that user.
11. The server returns the user object and API key to the CLI.
12. The CLI stores the endpoint URL, user ID, and API key in its config file.

Admin-created users and OAuth-upserted users are the same population. If an
admin creates a user with `provider: github, provider_id: 12345`, and that
GitHub user later authenticates via OAuth, the existing record is matched and
updated.

### API Key Lifecycle

- Each user has **one active API key** at a time.
- A new OAuth login generates a new key, revoking the previous one.
- **Refresh:** generates a new secret for an existing key (same `key_id`),
  resets the expiry based on the original duration. Returns the full key with
  the new plaintext secret.
- **Revoke:** permanently invalidates the key. The user must re-login to
  obtain a new key.
- Expired keys cannot authenticate but remain visible in listings for audit
  purposes (the `expires_at` field makes their status clear).
- When creating a key (via login), `expires` accepts `0` (no expiry), `30`,
  `60`, or `90` (days). Default is `90`. Expiry is calculated as exactly
  `24h x N` from the creation timestamp. The `expires_at` field is nullable
  (`null` when expires is `0`).

### PAT Lifecycle

- A user can have **multiple active PATs**.
- Each PAT has a name (user-provided, for identification), a set of
  permissions, and an optional expiry.
- **Create:** user specifies a name, a list of `resource_type:action`
  permissions, and an expiry. The full token (including plaintext secret) is
  returned only at creation — it cannot be retrieved later.
- **Revoke:** permanently invalidates the PAT. The `token_id` remains in the
  database for audit purposes.
- **No refresh:** unlike API keys, PATs are not refreshable. Create a new one
  instead.
- Expired PATs cannot authenticate but remain visible in listings.
- An admin can view and revoke any user's PATs.

---

## API Endpoints

All built-in endpoints are registered under a configurable mount point
(default: `/api/v1`). The paths below are shown relative to that mount point.

All timestamps in API responses, database storage, and log output use
RFC 3339 format normalized to UTC with the `Z` suffix
(e.g. `2026-07-17T14:30:00Z`). Timestamps with timezone offsets are never
produced; incoming timestamps with offsets are converted to UTC before
storage.

The API accepts and returns `application/json` exclusively. Requests with a
different `Content-Type` on endpoints that expect a body are rejected with
HTTP 415 (Unsupported Media Type). Responses always set
`Content-Type: application/json; charset=utf-8`.

Every response includes an `X-Request-ID` header containing a UUID generated
per request. The same ID is included in the server's structured log entry for
the request.

### Caching

The server sets appropriate `Cache-Control` headers on responses:

- **Mutable resources** (`/user`, `/users/*`, `/orgs/*`, key and token
  endpoints): `Cache-Control: no-store`. These resources change on write
  and must not be cached by intermediaries or clients.
- **Health probes** (`/healthz`, `/readyz`): `Cache-Control: no-cache`.
  Clients may cache but must revalidate — a stale health check is
  dangerous.
- **Static discovery** (`/auth/providers`): `Cache-Control: public, max-age=300`.
  Provider configuration changes rarely; a 5-minute cache is safe.

The server supports conditional requests via `ETag` and `If-None-Match` on
GET endpoints that return a single resource. The ETag is derived from the
resource's `updated_at` timestamp. A matching `If-None-Match` returns
HTTP 304 (Not Modified) with no body, saving bandwidth for polling clients
and agents.

### Health Probes (public, outside mount point)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Liveness probe — always returns 200 |
| `GET` | `/readyz` | Readiness probe — pings the database, returns 200 or 503 |
| `GET` | `/version` | Returns server version, build info, and configured mount point |

### OAuth (public)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/auth/providers` | List configured OAuth providers (name and authorize URL; no secrets) |
| `POST` | `/auth/callback` | Exchange an authorization code for a user record and API key |

`POST /auth/callback` accepts:
- `provider` (string, required)
- `code` (string, required) — the authorization code
- `redirect_uri` (string, required) — must match the configured allowlist
- `expires` (integer, optional) — key expiry in days: `0`, `30`, `60`, or
  `90`; default `90`

Returns:
```json
{
  "user": {
    "id": "<uuid>",
    "username": "<string>",
    "email": "<string>",
    "full_name": "<string>",
    "status": "active",
    "role": "user",
    "provider": "<string>",
    "provider_id": "<string>",
    "created_at": "<RFC 3339 timestamp>",
    "updated_at": "<RFC 3339 timestamp>"
  },
  "api_key": {
    "key": "<prefix>_<key_id>_<secret>",
    "key_id": "<key_id>",
    "expires_at": "<RFC 3339 timestamp or null>"
  }
}
```

### Authenticated User (API key or PAT with appropriate permissions)

These endpoints operate on the authenticated user's own resources. No user ID
is required in the path.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/user` | Get my profile |
| `PATCH` | `/user` | Update my profile (`full_name`) |
| `GET` | `/user/keys` | List my API key(s) |
| `POST` | `/user/keys/:key_id/refresh` | Refresh my API key (new secret, same key_id) |
| `DELETE` | `/user/keys/:key_id` | Revoke my API key |
| `GET` | `/user/tokens` | List my PATs |
| `POST` | `/user/tokens` | Create a new PAT |
| `GET` | `/user/tokens/:token_id` | Get a specific PAT's metadata |
| `DELETE` | `/user/tokens/:token_id` | Revoke a PAT |
| `GET` | `/user/orgs` | List my organization memberships |

`POST /user/tokens` accepts:
- `name` (string, required) — human-readable label
- `permissions` (array of strings, required) — e.g.
  `["users:read", "orgs:read"]`
- `expires` (integer, optional) — days: `0`, `30`, `60`, or `90`; default `90`

Returns the full token including plaintext secret. The secret is not
retrievable after creation.

Key and token listing endpoints (`GET /user/keys`, `GET /user/tokens`,
`GET /users/:id/keys`, `GET /users/:id/tokens`) return metadata only —
never the plaintext secret. Key listings return `key_id`, `created_at`,
`expires_at`, and `revoked_at`. Token listings return `token_id`, `name`,
`permissions`, `created_at`, `expires_at`, and `revoked_at`.

### Users (admin only)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/users` | Create a user |
| `GET` | `/users` | List all users (blocked excluded by default; `?include_blocked=true` to include) |
| `GET` | `/users/:id` | Get a user by ID |
| `PATCH` | `/users/:id` | Update a user (`full_name`) |
| `POST` | `/users/:id/promote` | Grant admin role to a user |
| `POST` | `/users/:id/demote` | Revoke admin role from a user |
| `POST` | `/users/:id/block` | Block a user |
| `POST` | `/users/:id/unblock` | Unblock a user |
| `GET` | `/users/:id/keys` | List a user's API key(s) |
| `DELETE` | `/users/:id/keys/:key_id` | Revoke a user's API key |
| `GET` | `/users/:id/tokens` | List a user's PATs |
| `DELETE` | `/users/:id/tokens/:token_id` | Revoke a user's PAT |

`POST /users` accepts:
- `username` (string, required)
- `email` (string, required)
- `provider` (string, required)
- `provider_id` (string, required)

Returns HTTP 201 with the created user. Returns HTTP 409 on duplicate
username or duplicate `(provider, provider_id)`.

Action endpoints (`promote`, `demote`, `block`, `unblock`) return the
updated user object with HTTP 200. This follows the GitHub convention —
the caller gets the new state without a follow-up GET.

### Organizations

Create, update, delete, block, and unblock operations require admin access.
Members of an organization can view the organization and its member list.

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/orgs` | Admin | Create an organization |
| `GET` | `/orgs` | Admin | List all organizations |
| `GET` | `/orgs/:id` | Admin or member | Get an organization by ID |
| `PATCH` | `/orgs/:id` | Admin | Update an organization (`name`, `url`) |
| `DELETE` | `/orgs/:id` | Admin | Delete an organization (cascades memberships) |
| `POST` | `/orgs/:id/block` | Admin | Block an organization |
| `POST` | `/orgs/:id/unblock` | Admin | Unblock an organization |
| `GET` | `/orgs/:id/members` | Admin or member | List organization members |
| `PUT` | `/orgs/:id/members/:user_id` | Admin | Add a member |
| `DELETE` | `/orgs/:id/members/:user_id` | Admin | Remove a member |

`POST /orgs` accepts:
- `name` (string, required)
- `slug` (string, required)
- `url` (string, optional)

Returns HTTP 409 on duplicate name or slug.

Blocked organizations are excluded from listings by default; include with
`?include_blocked=true`.

Action endpoints (`block`, `unblock`) return the updated organization
object with HTTP 200.

---

## Error Handling

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

| Status | Meaning |
|--------|---------|
| 400 | Bad request — malformed JSON, missing required fields, validation failure |
| 401 | Unauthorized — missing, invalid, expired, or revoked credential |
| 403 | Forbidden — valid credential but insufficient permissions, or user is blocked |
| 404 | Not found — resource does not exist |
| 409 | Conflict — unique constraint violation (duplicate username, slug, etc.) |
| 413 | Payload too large — request body exceeds configured limit |
| 415 | Unsupported media type — request Content-Type is not `application/json` |
| 500 | Internal server error |

---

## CLI

The CLI binary is `akc`. It is implemented in Go using Cobra and serves two
purposes:

1. **Standalone tool** for managing the auth/user layer of any apikit-based
   service.
2. **Embeddable command tree** that consuming projects import into their own
   CLI binary via Cobra's `AddCommand`.

The CLI is designed to be **agent-friendly**: an LLM or autonomous agent
must be able to discover the CLI's capabilities programmatically and consume
its output without parsing human-readable text. See the Agent Interface
section below for details.

The CLI is a thin wrapper around the **Go SDK** — it does not make HTTP
calls directly. Every CLI command delegates to the corresponding Go SDK
client method. This ensures the CLI and SDK stay in sync and that the SDK
is the single implementation of the API client logic in Go.

All commands print JSON to stdout and human-readable messages to stderr.

### Commands

```
akc version                       Show CLI version, build info,
                                  configured prefix, and server version
                                  (fetched from the server if reachable;
                                  omitted with a warning on stderr if not).

akc login [--provider github] [--expires 0|30|60|90]
                                  Run the OAuth login flow. Default provider:
                                  github. Default expiry: 90 days.

akc user show                     Show my profile.
akc user update --full-name "..." Update my full name.

akc keys list                     List my API key(s).
akc keys refresh                  Refresh my API key (new secret, same key_id).
akc keys revoke                   Revoke my API key. Clears credentials from
                                  config. Must re-login to get a new key.

akc tokens list                   List my PATs.
akc tokens create --name "..."    Create a new PAT.
    --permissions "users:read,orgs:read"
    [--expires 0|30|60|90]
akc tokens show <token_id>        Show a PAT's metadata.
akc tokens revoke <token_id>      Revoke a PAT.

akc orgs list                     List my organization memberships.
akc orgs show <id>                Show an organization I belong to.
akc orgs members <id>             List members of an organization I belong to.

akc admin users list [--include-blocked]
                                  List all users.
akc admin users show <id>         Show a user.
akc admin users create            Create a user.
    --username "..." --email "..."
    --provider "..." --provider-id "..."
akc admin users update <id>       Update a user.
    --full-name "..."
akc admin users promote <id>      Grant admin role to a user.
akc admin users demote <id>       Revoke admin role from a user.
akc admin users block <id>        Block a user.
akc admin users unblock <id>      Unblock a user.

akc admin orgs list [--include-blocked]
                                  List all organizations.
akc admin orgs create             Create an organization.
    --name "..." --slug "..."
    [--url "..."]
akc admin orgs update <id>        Update an organization.
    --name "..." [--url "..."]
akc admin orgs delete <id>        Delete an organization.
akc admin orgs block <id>         Block an organization.
akc admin orgs unblock <id>       Unblock an organization.
akc admin orgs members list <id>  List organization members.
akc admin orgs members add <org_id> <user_id>
                                  Add a member to an organization.
akc admin orgs members remove <org_id> <user_id>
                                  Remove a member from an organization.

akc admin keys list <user_id>     List a user's API key(s).
akc admin keys revoke <user_id> <key_id>
                                  Revoke a user's API key.
akc admin tokens list <user_id>   List a user's PATs.
akc admin tokens revoke <user_id> <token_id>
                                  Revoke a user's PAT.
```

### Config-Mutating Commands

| Command | Config change |
|---------|---------------|
| `akc login` | Sets `endpoint_url`, `user_id`, and `api_key` |
| `akc keys refresh` | Updates `api_key` |
| `akc keys revoke` | Clears `api_key` and `user_id` |

All config mutations use atomic writes (write to temp file, rename into
place).

### Persistent Client Configuration

The CLI stores configuration at `$HOME/.<prefix>/config.toml` (where
`<prefix>` is the configurable token prefix).

On startup, if the config file does not exist, `akc` creates the directory
(mode 0700) and config file (mode 0600) with empty values. Existing config
files are not modified on startup.

**Config file structure:**

```toml
endpoint_url = "https://api.example.com"
user_id = "550e8400-e29b-41d4-a716-446655440000"
api_key = "<prefix>_a1b2c3d4_deadbeef..."
```

Three fields only:
- `endpoint_url` — the server's base URL
- `user_id` — the authenticated user's UUID
- `api_key` — the user's active API key

**Resolution precedence** (for all three fields):

1. Command-line flag (`--endpoint-url`, `--user-id`, `--api-key`)
2. Environment variable (`ENDPOINT_URL`, `USER_ID`, `API_KEY`)
3. Config file value (empty string treated as unset)
4. Error with descriptive message

### Agent Interface

The CLI is designed to be operated by LLMs and autonomous agents as a
first-class use case. Three properties make this work: structured
discoverability, machine-readable output, and predictable error handling.

**Structured discoverability.** `akc help --json` returns a complete,
machine-readable description of the CLI's command tree:

```json
{
  "name": "akc",
  "version": "0.1.0",
  "commands": [
    {
      "name": "user show",
      "description": "Show my profile",
      "method": "GET",
      "path": "/user",
      "args": [],
      "flags": [],
      "auth": "api_key"
    },
    {
      "name": "tokens create",
      "description": "Create a new PAT",
      "method": "POST",
      "path": "/user/tokens",
      "args": [],
      "flags": [
        {"name": "--name", "type": "string", "required": true,
         "description": "Human-readable label"},
        {"name": "--permissions", "type": "string", "required": true,
         "description": "Comma-separated resource:action pairs"},
        {"name": "--expires", "type": "int", "required": false,
         "default": 90, "description": "Expiry in days: 0, 30, 60, or 90"}
      ],
      "auth": "api_key"
    }
  ]
}
```

Each command entry includes: the command name, a one-line description, the
HTTP method and API path it maps to, its positional arguments and flags
with types and defaults, and the required authentication level (`none`,
`api_key`, or `admin`). An agent can read this once to understand the full
CLI surface without parsing help text.

Individual command help is also available in JSON:
`akc user show --help --json` returns the entry for that command only.

When a consuming project adds commands via Cobra's `AddCommand`, those
commands appear in the `akc help --json` output automatically — the
discoverability surface grows with the CLI.

Most commands map 1:1 to a single API endpoint — `method` and `path`
tell the agent exactly which API call the command wraps. Composite
commands that involve multiple API calls (e.g. `login`, which fetches
providers then exchanges a code via callback) set `method` and `path`
to `null` and instead carry a `"composite": true` flag with a
`"description"` that explains the multi-step flow. An agent can
distinguish the two cases by checking for the `composite` field.

**Machine-readable output.** Every command writes its result as JSON to
stdout. Human-readable messages (progress, warnings, prompts) go to
stderr only. An agent captures stdout and parses it directly:

```
$ akc user show 2>/dev/null
{"id":"550e...","username":"alice","email":"alice@example.com",...}

$ akc tokens list 2>/dev/null
[{"token_id":"t1a2b3","name":"ci-deploy","permissions":["orgs:read"],...},...]
```

Successful commands return the resource or resource list directly — no
wrapper envelope. This matches the API response shapes defined by the
OpenAPI spec.

**Predictable error handling.** On failure, the CLI writes a JSON error
object to stdout matching the API error envelope, and exits with a
non-zero code:

```
$ akc admin users show nonexistent-id 2>/dev/null; echo $?
{"error":{"code":404,"message":"User not found"}}
1
```

Exit codes:
- `0` — success
- `1` — API error (4xx/5xx from the server; details in stdout JSON)
- `2` — client error (missing config, invalid flags, network failure;
  details in stdout JSON)

An agent can branch on the exit code and parse the error JSON for details,
without guessing whether output is a result or an error message.

---

## Server Configuration

The server loads `config.toml` from the current directory (TOML format).
`XDG_CONFIG_HOME` and `XDG_DATA_HOME` override default locations for config
and data.

### Required Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| `[server] port` | `8080` | HTTP listen port |
| `[server] bind` | `0.0.0.0` | Bind address |
| `[server] external_url` | — | Public URL; used for OAuth redirect URI in production |
| `[server] mount_point` | `/api/v1` | Base path for all API routes |
| `[database] path` | `./data/apikit.db` | SQLite database file path |
| `[logging] level` | `info` | Log level: `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic` |
| `[server] max_body_size` | `1MB` | Maximum request body size |

### OAuth Provider Configuration

```toml
[[oauth.providers]]
name = "github"
client_id = "..."
client_secret = "..."
# Optional overrides (GitHub well-known URLs are built-in):
# authorize_url = "https://github.example.com/login/oauth/authorize"
# token_url = "https://github.example.com/login/oauth/access_token"
# userinfo_url = "https://github.example.com/api/v3/user"
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `ADMIN_TOKEN` | Required on subsequent boots (validated against stored hash) |
| `ENDPOINT_URL` | Default endpoint URL for CLI commands |
| `USER_ID` | Default user ID for CLI commands |
| `API_KEY` | Default API key for CLI commands |

---

## Operational Requirements

- **Database:** embedded SQLite with WAL mode for concurrent write safety.
  Pure-Go driver (`modernc.org/sqlite`) — no CGo.
- **Schema management:** `CREATE TABLE IF NOT EXISTS` on boot; no migration
  tooling.
- **Logging:** structured JSON via logrus. Every request is logged with
  method, path, status, duration, and request ID.
- **Request ID:** every response includes an `X-Request-ID` header (UUID).
  The same ID appears in the corresponding log entry.
- **Graceful shutdown:** on SIGTERM/SIGINT with a 15-second drain timeout.
- **Request body limit:** configurable, default 1 MB. Requests exceeding this
  limit are rejected with HTTP 413.
- **Health probes:** Kubernetes-compatible at `/healthz` (liveness) and
  `/readyz` (readiness, pings database).

---

## Database Schema

SQLite with WAL mode. All tables are created on boot via
`CREATE TABLE IF NOT EXISTS`. This is a sketch — the implementation may add
indexes or adjust column types, but the table structure and relationships
are normative.

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

### pats

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `token_id` | TEXT | PK | Random identifier |
| `user_id` | TEXT | FK → users.id, NOT NULL | Owning user |
| `name` | TEXT | NOT NULL | User-provided label |
| `secret_hash` | TEXT | NOT NULL | SHA-256 hash of the secret |
| `permissions` | TEXT (JSON) | NOT NULL | JSON array of `resource_type:action` strings |
| `expires_days` | INTEGER | NOT NULL | Original expiry duration |
| `expires_at` | TEXT (RFC 3339) | | NULL when expires_days is 0 |
| `revoked_at` | TEXT (RFC 3339) | | NULL while active |
| `created_at` | TEXT (RFC 3339) | NOT NULL | |

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

### org_members

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `org_id` | TEXT | FK → orgs.id, NOT NULL | |
| `user_id` | TEXT | FK → users.id, NOT NULL | |
| `created_at` | TEXT (RFC 3339) | NOT NULL | |

Primary key on `(org_id, user_id)`. Cascade delete when the org is deleted.

### admin_config

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `key` | TEXT | PK | Config key |
| `value` | TEXT | NOT NULL | Config value |

Stores singleton server state: `admin_token_hash` (SHA-256 hash of the
admin token) and `admin_email` (the designated first-admin email from
`--admin-email`). Single-row-per-key design; no migration needed when new
keys are added.

---

## Repository Layout

```
api/
  openapi.yaml              OpenAPI 3.1 specification (source of truth)
cmd/
  apikit/                   Example server binary entry point
  akc/                      CLI binary entry point
internal/                   Private packages (not importable by consumers)
  auth/                     Auth middleware, token validation
  db/                       Database layer, schema, queries
  handlers/                 Built-in HTTP handler implementations
  config/                   Server configuration loading
packages/
  sdk-python/               Python SDK (uv-managed)
docs/                       Documentation
bin/                        Compiled binaries (git-ignored)
```

The root of the module (`github.com/txsvc/apikit`) exposes the public API
that consuming projects import: server creation, handler registration, OAuth
provider registration, permission registration, and the embeddable CLI
command tree.

Reusable public sub-packages (e.g. `github.com/txsvc/apikit/telemetry`) live
in top-level directories. Internal implementation details live under
`internal/`.

---

## SDKs

### Go SDK

The Go SDK is part of the apikit module itself. Consuming projects that import
apikit for the server framework also have access to the client library.

The SDK provides typed request/response structs matching the OpenAPI schemas
and a client that wraps each built-in endpoint with a Go function. Example:

```go
client := apikit.NewClient("https://api.example.com", apikit.WithAPIKey(key))
user, err := client.GetUser(ctx)
tokens, err := client.ListTokens(ctx)
```

### Python SDK

The Python SDK lives under `packages/sdk-python/` in the same repository. It
is managed with `uv` and provides the same capabilities as the Go SDK: typed
request/response classes and a client wrapping each built-in endpoint.

```python
client = apikit.Client("https://api.example.com", api_key=key)
user = client.get_user()
tokens = client.list_tokens()
```

Both SDKs use request/response objects matching the format defined by the
OpenAPI specification.

---

## Documentation Deliverables

| Document | Location | Description |
|----------|----------|-------------|
| Project overview | `README.md` | Prerequisites, quickstart, project structure |
| Architecture | `docs/architecture.md` | Library design, integration model, request lifecycle, storage |
| API reference | `docs/api.md` | Complete REST API documentation derived from OpenAPI spec |
| CLI reference | `docs/cli.md` | `akc` usage: all commands, flags, environment variables, config |
| Configuration | `docs/configuration.md` | Server `config.toml` and client config reference |
| OpenAPI spec | `api/openapi.yaml` | OpenAPI 3.1 description of all built-in endpoints |

---

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language (server, Go SDK, CLI) | Go 1.25+ |
| Language (Python SDK) | Python 3.12+ |
| HTTP framework | Echo (`github.com/labstack/echo/v4`) |
| CLI framework | Cobra (`github.com/spf13/cobra`) |
| Database | SQLite, WAL mode, pure-Go driver (`modernc.org/sqlite`) |
| Config format | TOML (`github.com/BurntSushi/toml`) |
| Logging | logrus (`github.com/sirupsen/logrus`) |
| UUID generation | `github.com/google/uuid` |
| Token hashing | SHA-256 (stdlib `crypto/sha256`) |
| Build | `make build` compiles binaries to `bin/` |
| Test | `make test` runs all tests |
| Lint | `make lint` runs `go vet` |

---

## Design Decisions

- **Organizations are organizational only in this iteration.** Organizations
  have CRUD and lifecycle operations but no permission implications. This
  keeps the first iteration simple while preserving the entity for future RBAC
  work.

- **One active API key per user.** A new login replaces the existing key. This
  prevents key sprawl and simplifies the mental model.

- **OAuth callback generates a fresh key each login.** The previous key is
  revoked on re-login. The user always has a single, known credential.

- **CLI uses persistent config at `$HOME/.<prefix>/config.toml`.** Three
  fields only: `endpoint_url`, `user_id`, `api_key`. Plaintext with 0600
  permissions; keychain integration deferred.

- **Admin is a user role, not just a credential.** Users have a `role` field
  (`admin` or `user`). Admin users get full access via their regular API key.
  This provides audit trails, supports multiple admins, and avoids passing a
  shared secret around for routine operations.

- **Admin bootstrap via `--admin-email`.** On first boot, the operator
  designates the first admin by email. When that user authenticates via
  OAuth, they receive the admin role. This is deterministic and avoids race
  conditions (vs. "first user wins").

- **Admin token is break-glass only.** The admin token is still generated on
  first boot and validated on subsequent boots via `ADMIN_TOKEN` env var, but
  it is not intended for routine use. It exists for emergency access when all
  admin users are unavailable.

- **Admin token rotation via boot flag.** `--reset-admin-token` reuses
  first-boot token generation logic. Chosen over a runtime CLI command because
  rotation should work even when the service is stopped.

- **Fresh schema rebuild when needed.** The project is pre-production with no
  deployed users; no ALTER TABLE or data migration tooling.

- **Config file atomic writes, no locking.** Last writer wins. Concurrent
  mutations are not a realistic scenario for a single-user interactive CLI.

- **Blocked user handling at credential level.** API keys and PATs are inert
  while user is blocked, functional again if unblocked. Credentials are not
  deleted on block.

- **PATs have no refresh.** Unlike API keys, PATs are not refreshable. Users
  create a new PAT when the old one expires or is compromised. This matches
  GitHub's PAT model and avoids the complexity of secret rotation on
  fine-grained tokens.

- **CLI is agent-friendly by design.** `akc help --json` exposes the full
  command tree as structured JSON — an agent reads it once and knows every
  command, flag, type, and default. All output is JSON to stdout, errors
  use the same envelope as the API, and exit codes are predictable. This
  makes the CLI a first-class interface for LLMs and autonomous agents,
  not just humans.

---

## Glossary

| Term | Definition |
|------|------------|
| **Admin role** | A user role granting full access to all endpoints and resources via the user's regular API key. Designated on first boot via `--admin-email`; delegated by existing admins via promote/demote. |
| **Admin token** | A break-glass infrastructure credential in the format `<prefix>_admin_<64 hex>` for emergency access when no admin user is available. Not for routine use. |
| **API key** | A user-scoped credential in the format `<prefix>_<key_id>_<secret>`, issued via OAuth login. One active key per user. |
| **Personal access token (PAT)** | A user-created, fine-grained credential in the format `<prefix>_pat_<token_id>_<secret>`. Multiple per user, each with specific permissions. |
| **Provider** | An OAuth identity provider (e.g. GitHub) registered in the provider registry. |
| **Mount point** | The URL path prefix where apikit registers its built-in endpoints. Configurable; default `/api/v1`. |
| **Consuming project** | A Go project that imports apikit as a library and extends it with additional handlers. |
