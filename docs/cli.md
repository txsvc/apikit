# akc CLI Reference

`akc` is the command-line client for the apikit API server. It provides
authenticated access to user management, API key lifecycle, personal access
tokens, organization management, and full administrative operations.

All data output is JSON on stdout. Human-readable messages (progress,
warnings) go to stderr. Errors are returned as JSON envelopes on stdout.

## Installation

Build from source with Go:

```
go build -o akc ./cmd/akc
```

Set version metadata at build time with `-ldflags`:

```
go build -ldflags "\
  -X github.com/txsvc/apikit/internal/cli.Version=v1.0.0 \
  -X github.com/txsvc/apikit/internal/cli.Build=$(git rev-parse --short HEAD) \
  -X github.com/txsvc/apikit/internal/cli.TokenPrefix=ak" \
  -o akc ./cmd/akc
```

Or use `make`:

```
make build
```

---

## Global Flags

These persistent flags are available on every subcommand:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--endpoint-url` | string | `""` | Server endpoint URL |
| `--user-id` | string | `""` | Authenticated user UUID |
| `--api-key` | string | `""` | API key for authentication |
| `--json` | bool | `false` | Output help in JSON format (for agent/LLM discovery) |

Flags override environment variables, which override config file values.
See the Configuration section for the full precedence chain.

---

## Configuration

### Config file location

```
$HOME/.<prefix>/config.toml
```

The default prefix is `ak`, so the default path is `$HOME/.ak/config.toml`.
The prefix is set at build time via `-X github.com/txsvc/apikit/internal/cli.TokenPrefix=<prefix>`.

### Config file format

```toml
endpoint_url = "https://api.example.com"
user_id = "550e8400-e29b-41d4-a716-446655440000"
api_key = "ak_AbCdEfGh_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

The config directory is created with mode `0700` and the config file with
mode `0600`. The `akc login` command writes this file automatically.

### Credential resolution precedence

Each credential field is resolved using a four-level precedence chain
(highest to lowest):

1. **CLI flag** -- `--endpoint-url`, `--api-key`, `--user-id`
2. **Environment variable** -- `ENDPOINT_URL`, `API_KEY`, `USER_ID`
3. **Config file** -- values from `config.toml`
4. **Error** (for required fields) or empty string (for optional fields)

Required fields: `endpoint_url`, `api_key`. Optional: `user_id`.

### Config initialization

The config directory and a blank `config.toml` are created automatically
the first time any authenticated command runs. You do not need to create
them manually. Run `akc login` to populate the config with real credentials.

---

## Command Reference

### Command tree

```
akc
  version                          Show CLI and server version information
  help [command]                   Help about any command
  login                            Authenticate via browser-based OAuth
  user
    show                           Show your user profile
    update                         Update your user profile
  keys
    list                           List your API keys
    refresh                        Refresh your API key
    revoke                         Revoke your API key
  tokens
    list                           List your Personal Access Tokens
    create                         Create a new Personal Access Token
    show <token_id>                Show a Personal Access Token
    revoke <token_id>              Revoke a Personal Access Token
  orgs
    list                           List your organizations
    show <id>                      Show organization details
    members <id>                   List organization members
  admin
    users
      list                         List all users
      show <id>                    Show a user by ID
      create                       Create a new user
      update <id>                  Update a user by ID
      promote <id>                 Grant admin role to a user
      demote <id>                  Revoke admin role from a user
      block <id>                   Block a user
      unblock <id>                 Unblock a user
    keys
      list <user_id>               List a user's API keys
      revoke <user_id> <key_id>    Revoke a user's API key
    tokens
      list <user_id>               List a user's personal access tokens
      revoke <user_id> <token_id>  Revoke a user's personal access token
    orgs
      list                         List all organizations
      create                       Create a new organization
      update <id>                  Update an organization by ID
      delete <id>                  Delete an organization by ID
      block <id>                   Block an organization
      unblock <id>                 Unblock an organization
      members
        list <id>                  List members of an organization
        add <org_id> <user_id>     Add a member to an organization
        remove <org_id> <user_id>  Remove a member from an organization
```

---

### akc version

Show CLI version and, when a server endpoint is configured, the server
version. Does not require authentication.

```
akc version
```

If an endpoint URL is available (from flag, environment, or config), the
command fetches `GET /version` from the server with a 5-second timeout.
When the server is unreachable, a warning is printed to stderr and the
`server_version` field is omitted.

**Example output:**

```json
{
  "cli_version": "v1.0.0",
  "build": "abc1234",
  "prefix": "ak",
  "server_version": {
    "version": "v1.0.0",
    "build_time": "2026-07-01T12:00:00Z",
    "commit": "def5678",
    "mount_point": "/api/v1"
  }
}
```

When no endpoint is configured, `server_version` is silently omitted:

```json
{
  "cli_version": "dev",
  "build": "unknown",
  "prefix": "ak"
}
```

---

### akc login

Authenticate via browser-based OAuth and persist credentials to the config
file. Does not require an existing API key.

```
akc login [--provider <name>] [--expires <days>] --endpoint-url <url>
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--provider` | string | `github` | OAuth provider name |
| `--expires` | int | `90` | Credential expiry in days (0, 30, 60, or 90) |

**Flow:**

1. Discovers available OAuth providers from the server
2. Generates a cryptographic state token (32 random bytes, hex-encoded)
3. Starts a local callback server on `127.0.0.1` (random port)
4. Opens the authorization URL in the default browser
5. Waits up to 120 seconds for the OAuth callback
6. Exchanges the authorization code for credentials
7. Saves `endpoint_url`, `user_id`, and `api_key` to `config.toml`

**stdout:** User profile JSON

**stderr:** `"Opening browser for authentication..."` followed by `"Logged in as <username>"`

**Example:**

```
akc login --endpoint-url https://api.example.com
```

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "username": "jdoe",
  "email": "jdoe@example.com",
  "full_name": "",
  "status": "active",
  "role": "user",
  "provider": "github",
  "provider_id": "12345678",
  "created_at": "2026-07-18T10:00:00Z",
  "updated_at": "2026-07-18T10:00:00Z"
}
```

If the browser cannot be opened automatically, the authorization URL is
printed to stderr so you can open it manually.

---

### akc user show

Show the authenticated user's profile.

```
akc user show
```

Calls `GET /api/v1/user`. Requires authentication.

**Example output:**

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "username": "jdoe",
  "email": "jdoe@example.com",
  "full_name": "Jane Doe",
  "status": "active",
  "role": "user",
  "provider": "github",
  "provider_id": "12345678",
  "created_at": "2026-07-01T10:00:00Z",
  "updated_at": "2026-07-15T14:30:00Z"
}
```

### akc user update

Update the authenticated user's display name.

```
akc user update --full-name <name>
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--full-name` | string | Yes | Full display name |

Calls `PATCH /api/v1/user`. Returns the updated user profile JSON.

**Example:**

```
akc user update --full-name "Jane Doe"
```

---

### akc keys list

List all API keys associated with the authenticated user.

```
akc keys list
```

Calls `GET /api/v1/user/keys`. Returns a JSON array of key metadata.
Secrets are never included in the response.

**Example output:**

```json
[
  {
    "key_id": "AbCdEfGh",
    "created_at": "2026-07-18T10:00:00Z",
    "expires_at": "2026-10-16T10:00:00Z",
    "revoked_at": null
  }
]
```

### akc keys refresh

Rotate the current API key -- generates a new secret while keeping the
same key ID. The new key is automatically saved to the config file.

```
akc keys refresh
```

Calls `POST /api/v1/user/keys/:key_id/refresh`. The `key_id` is parsed
automatically from the configured API key.

**stdout:** The new key JSON (including the plaintext key, shown once)

**stderr:** `"API key refreshed"`

**Example output:**

```json
{
  "key": "ak_AbCdEfGh_newSecretValueHere1234567890ab",
  "key_id": "AbCdEfGh",
  "expires_at": "2026-10-16T10:00:00Z"
}
```

Only API key authentication is accepted -- PAT authentication is rejected
with HTTP 401.

### akc keys revoke

Revoke the current API key and clear it from the config file. You will
need to run `akc login` afterward to obtain a new key.

```
akc keys revoke
```

Calls `DELETE /api/v1/user/keys/:key_id`. The `key_id` is parsed
automatically from the configured API key.

**stdout:** Revocation confirmation JSON

**stderr:** `"API key revoked. Run 'akc login' to obtain a new key."`

**Example output:**

```json
{
  "key_id": "AbCdEfGh",
  "revoked_at": "2026-07-18T15:00:00Z"
}
```

---

### akc tokens list

List all Personal Access Tokens for the authenticated user.

```
akc tokens list
```

Calls `GET /api/v1/user/tokens`. Returns all tokens (active, expired, and
revoked). Secrets are never included.

**Example output:**

```json
[
  {
    "token_id": "abcd1234",
    "name": "ci-pipeline",
    "permissions": ["keys:read", "tokens:read"],
    "created_at": "2026-07-10T08:00:00Z",
    "expires_at": "2026-10-08T08:00:00Z",
    "revoked_at": null
  }
]
```

### akc tokens create

Create a new Personal Access Token with specific permissions.

```
akc tokens create --name <name> --permissions <perms> [--expires <days>]
```

| Flag | Type | Default | Required | Description |
|------|------|---------|----------|-------------|
| `--name` | string | | Yes | Human-readable token name |
| `--permissions` | string | | Yes | Comma-separated permissions |
| `--expires` | int | `90` | No | Expiry in days (0, 30, 60, or 90) |

Calls `POST /api/v1/user/tokens`.

**Available permissions:** `users:read`, `orgs:read`, `keys:read`,
`keys:manage`, `tokens:read`, `tokens:manage`

**stdout:** Token JSON including the plaintext token (shown once only)

**stderr:** `"Token created. Save the token value — it cannot be retrieved later."`

**Example:**

```
akc tokens create --name "ci-pipeline" --permissions "keys:read,tokens:read" --expires 30
```

```json
{
  "token": "ak_pat_abcd1234_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "token_id": "abcd1234",
  "name": "ci-pipeline",
  "permissions": ["keys:read", "tokens:read"],
  "expires_at": "2026-08-17T10:00:00Z"
}
```

### akc tokens show

Show metadata for a specific token.

```
akc tokens show <token_id>
```

Calls `GET /api/v1/user/tokens/:token_id`. Does not return the plaintext
secret.

**Example:**

```
akc tokens show abcd1234
```

### akc tokens revoke

Revoke a Personal Access Token immediately.

```
akc tokens revoke <token_id>
```

Calls `DELETE /api/v1/user/tokens/:token_id`.

**stdout:** `{}`

**stderr:** `"Token abcd1234 revoked"`

---

### akc orgs list

List organizations the authenticated user belongs to.

```
akc orgs list
```

Calls `GET /api/v1/orgs`. Returns only active organizations (blocked
organizations are excluded).

**Example output:**

```json
[
  {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "name": "Acme Corp",
    "slug": "acme-corp",
    "url": "https://acme.example.com",
    "status": "active",
    "created_at": "2026-06-01T12:00:00Z",
    "updated_at": "2026-06-15T09:00:00Z"
  }
]
```

### akc orgs show

Show details for a specific organization. Non-admin users can only view
organizations they are a member of.

```
akc orgs show <id>
```

### akc orgs members

List members of a specific organization. Non-admin users can only list
members of organizations they belong to.

```
akc orgs members <id>
```

---

### akc admin users

Administrative commands for managing users. All commands in this group
require admin authentication (admin token or API key with admin role).

#### akc admin users list

List all users in the system.

```
akc admin users list [--include-blocked]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--include-blocked` | bool | `false` | Include blocked users |

#### akc admin users show

Show a user by ID.

```
akc admin users show <id>
```

#### akc admin users create

Create a new user.

```
akc admin users create --username <name> --email <email> --provider <provider> --provider-id <id>
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--username` | string | Yes | Username for the new user |
| `--email` | string | Yes | Email address |
| `--provider` | string | Yes | OAuth provider name (e.g., `github`) |
| `--provider-id` | string | Yes | Provider-specific user ID |

New users are created with `role=user` and `status=active`.

**Example:**

```
akc admin users create \
  --username jsmith \
  --email jsmith@example.com \
  --provider github \
  --provider-id 98765432
```

#### akc admin users update

Update a user's display name.

```
akc admin users update <id> --full-name <name>
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--full-name` | string | Yes | Full name of the user |

#### akc admin users promote

Grant admin role to a user. Idempotent -- promoting an existing admin
returns success without modification.

```
akc admin users promote <id>
```

#### akc admin users demote

Revoke admin role from a user. The server prevents demoting the last
remaining admin (returns HTTP 409).

```
akc admin users demote <id>
```

#### akc admin users block

Block a user account. Blocked users cannot authenticate. Idempotent.

```
akc admin users block <id>
```

#### akc admin users unblock

Unblock a user account, restoring access. Idempotent.

```
akc admin users unblock <id>
```

---

### akc admin keys

Administrative commands for managing any user's API keys.

#### akc admin keys list

List a specific user's API keys.

```
akc admin keys list <user_id>
```

#### akc admin keys revoke

Revoke a specific API key for a user.

```
akc admin keys revoke <user_id> <key_id>
```

---

### akc admin tokens

Administrative commands for managing any user's personal access tokens.

#### akc admin tokens list

List a specific user's personal access tokens.

```
akc admin tokens list <user_id>
```

#### akc admin tokens revoke

Revoke a specific personal access token for a user.

```
akc admin tokens revoke <user_id> <token_id>
```

---

### akc admin orgs

Administrative commands for managing organizations.

#### akc admin orgs list

List all organizations.

```
akc admin orgs list [--include-blocked]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--include-blocked` | bool | `false` | Include blocked organizations |

#### akc admin orgs create

Create a new organization.

```
akc admin orgs create --name <name> --slug <slug> [--url <url>]
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--name` | string | Yes | Organization display name |
| `--slug` | string | Yes | URL-safe identifier (lowercase alphanumeric, hyphens, underscores) |
| `--url` | string | No | Organization URL |

**Example:**

```
akc admin orgs create --name "Acme Corp" --slug acme-corp --url https://acme.example.com
```

#### akc admin orgs update

Update an organization. At least one of `--name` or `--url` should be
provided. The slug is immutable and cannot be changed.

```
akc admin orgs update <id> [--name <name>] [--url <url>]
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--name` | string | No | New organization name |
| `--url` | string | No | New organization URL |

#### akc admin orgs delete

Delete an organization and all its memberships (cascading delete).

```
akc admin orgs delete <id>
```

#### akc admin orgs block

Block an organization. Idempotent.

```
akc admin orgs block <id>
```

#### akc admin orgs unblock

Unblock an organization. Idempotent.

```
akc admin orgs unblock <id>
```

#### akc admin orgs members list

List members of an organization.

```
akc admin orgs members list <id>
```

#### akc admin orgs members add

Add a user to an organization. Idempotent -- adding an existing member
returns success.

```
akc admin orgs members add <org_id> <user_id>
```

#### akc admin orgs members remove

Remove a user from an organization. Does not delete the user account.

```
akc admin orgs members remove <org_id> <user_id>
```

---

## Output Formats

### JSON output

All command output is JSON with two-space indentation, written to stdout.
There is no table output mode. HTML escaping is disabled in the JSON
encoder for non-admin commands (`user`, `keys`, `tokens`, `orgs`, `login`),
so characters like `<`, `>`, and `&` appear unescaped. Admin commands
and the top-level error handler use Go's default JSON encoding, which
escapes these characters.

### Error envelopes

Errors are returned as JSON on stdout in this format:

```json
{
  "error": {
    "code": 404,
    "message": "user not found"
  }
}
```

For API/server errors, `code` is the HTTP status code. For client-side
errors (missing config, validation failures), `code` is `0` (from the
top-level error handler and admin command handlers) or `2` (from
non-admin command handlers like `user`, `keys`, `tokens`, `orgs`).

### Human-readable messages

Informational and warning messages are written to stderr only. They never
appear on stdout, keeping machine-readable JSON output clean for piping
and scripting:

- Login progress: `"Opening browser for authentication..."`
- Login success: `"Logged in as <username>"`
- Key refresh: `"API key refreshed"`
- Key revoke: `"API key revoked. Run 'akc login' to obtain a new key."`
- Token create: `"Token created. Save the token value — it cannot be retrieved later."`
- Token revoke: `"Token <id> revoked"`
- Server unreachable: `"warning: could not reach server: <error>"`

### Agent/LLM discovery with --json help

The `--json` flag on help commands produces machine-readable command
descriptions for use by LLMs and automation tools:

```
akc help --json          # Full command tree
akc version --help --json  # Single command details
```

**Full tree output:**

```json
{
  "name": "akc",
  "version": "v1.0.0",
  "commands": [
    {
      "name": "akc version",
      "description": "Show CLI and server version information",
      "method": null,
      "path": null,
      "args": [],
      "flags": [...],
      "auth": "none",
      "composite": true
    }
  ]
}
```

---

## Exit Codes

| Code | Meaning | Example |
|------|---------|---------|
| `0` | Success | Command completed without error |
| `1` | API/server error | HTTP 4xx/5xx response from the server |
| `2` | Client/usage error | Missing config, invalid flags, validation failure |

---

## Authentication Types

The server accepts three credential types as Bearer tokens. The CLI
primarily uses API keys, but admin operations may use admin tokens.

| Type | Format | Scope |
|------|--------|-------|
| Admin Token | `ak_admin_<64 hex chars>` | Full administrative access |
| API Key | `ak_<key_id>_<secret>` | User-scoped, full access to own resources |
| PAT | `ak_pat_<token_id>_<secret>` | Permission-scoped access |

---

## Examples

### First-time setup

```bash
# Log in and save credentials
akc login --endpoint-url https://api.example.com

# Verify your profile
akc user show
```

### Credential management

```bash
# Rotate your API key (new secret, same key ID)
akc keys refresh

# Create a read-only token for CI
akc tokens create \
  --name "ci-readonly" \
  --permissions "users:read,orgs:read" \
  --expires 30

# Revoke a compromised token
akc tokens revoke abcd1234

# Revoke your API key (requires re-login afterward)
akc keys revoke
akc login --endpoint-url https://api.example.com
```

### Using environment variables

```bash
# Override config for a single command
ENDPOINT_URL=https://staging.example.com API_KEY=ak_XyZ12345_secret akc user show

# Or export for an entire session
export ENDPOINT_URL=https://api.example.com
export API_KEY=ak_XyZ12345_secret
akc keys list
```

### Admin workflows

```bash
# Create a new user
akc admin users create \
  --username newuser \
  --email newuser@example.com \
  --provider github \
  --provider-id 11223344

# Promote a user to admin
akc admin users promote 550e8400-e29b-41d4-a716-446655440000

# Block a compromised account
akc admin users block 550e8400-e29b-41d4-a716-446655440000

# Revoke all of a user's keys
akc admin keys list 550e8400-e29b-41d4-a716-446655440000
akc admin keys revoke 550e8400-e29b-41d4-a716-446655440000 AbCdEfGh
```

### Organization management

```bash
# Create an organization
akc admin orgs create --name "Engineering" --slug engineering

# Add a member
akc admin orgs members add <org_id> <user_id>

# List members
akc admin orgs members list <org_id>

# Remove a member
akc admin orgs members remove <org_id> <user_id>

# Delete the organization (cascading member removal)
akc admin orgs delete <org_id>
```

### Building custom commands

apikit exposes a public API for building custom CLI commands that make
authenticated API calls, use the same JSON output formatting, and inherit
credential resolution automatically. See
[docs/custom-cli.md](custom-cli.md) for the full guide.

Key functions:

| Function | Purpose |
|----------|---------|
| `CLIClientFromCmd(cmd)` | Get the authenticated client from a Cobra command |
| `client.DoRequest(ctx, method, path, body)` | Make an authenticated API request (returns decoded JSON) |
| `client.DoRequestRaw(ctx, method, path, body)` | Make an authenticated API request (returns raw bytes) |
| `CLIPrintResult(cmd, v)` | Print JSON result to stdout |
| `CLIHandleError(cmd, err)` | Print JSON error envelope to stdout |
| `NewCLIError(code, message)` | Create a typed client-side error |
| `CLIResolveOrgSlug(ctx, client, slug)` | Resolve an org slug to its UUID |

---

### Scripting with jq

```bash
# Get your user ID
akc user show | jq -r '.id'

# List active (non-revoked) keys
akc keys list | jq '[.[] | select(.revoked_at == null)]'

# Get all admin users
akc admin users list | jq '[.[] | select(.role == "admin")]'

# Count organization members
akc admin orgs members list <org_id> | jq 'length'
```
