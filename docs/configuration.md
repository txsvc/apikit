# Configuration Reference

apikit uses a TOML configuration file to control server behavior, database
location, logging, and OAuth provider setup. Every field has a sensible default,
so the server runs out of the box with no configuration file at all.

## File Location

The configuration loader resolves the path to `config.toml` using the following
rules, evaluated in order:

1. If the `XDG_CONFIG_HOME` environment variable is set, the file is read from
   `$XDG_CONFIG_HOME/config.toml`.
2. Otherwise, the file is read from `./config.toml` in the current working
   directory.

When the resolved file does not exist, all defaults are applied silently and the
server starts normally. When the file exists but contains invalid TOML, startup
fails with a parse error.

Loading the configuration performs no filesystem side effects beyond reading the
file itself -- it does not create directories or files.

## Environment Variable Expansion

Before the TOML file is parsed, all `$VAR` and `${VAR}` references in the file
text are expanded from the process environment using Go's `os.ExpandEnv`. This
lets you keep secrets out of config files:

```toml
[[oauth.providers]]
name = "github"
client_id     = "${GITHUB_CLIENT_ID}"
client_secret = "${GITHUB_CLIENT_SECRET}"
```

Any string value in the file can reference environment variables -- not just
OAuth fields. For example:

```toml
[server]
external_url = "$APP_EXTERNAL_URL"

[database]
path = "${DB_PATH}"
```

**Undefined variables** expand to an empty string. If the resulting value
violates a validation rule (for example, an empty `client_secret`), startup
fails with the usual validation error.

Expansion happens on the raw file text before TOML parsing, so it cannot be
used for non-string types like integers. Writing `port = $PORT` (unquoted)
produces a TOML parse error because the parser sees a bare string where it
expects an integer. For integer fields, write `port = 9090` directly.

## Complete Reference

### `[server]`

HTTP server settings.

| Key | Type | TOML key | Default | Description |
|-----|------|----------|---------|-------------|
| Port | integer | `port` | `8080` | TCP port to listen on. Set to `0` for an ephemeral port assigned by the OS. |
| Bind | string | `bind` | `"0.0.0.0"` | Network address to bind to. |
| ExternalURL | string | `external_url` | `""` | Public-facing base URL of the server. Used for OAuth redirect URI validation. |
| MountPoint | string | `mount_point` | `"/api/v1"` | URL path prefix for all API routes. Health endpoints (`/healthz`, `/readyz`, `/version`) are always served at the root, outside this prefix. |
| MaxBodySize | string | `max_body_size` | `"1MB"` | Maximum allowed request body size. Format: a positive integer followed by `KB`, `MB`, or `GB` (case-sensitive in the regex, but the parser upper-cases input). Examples: `"512KB"`, `"2MB"`, `"1GB"`. |

### `[database]`

Database settings.

| Key | Type | TOML key | Default | Description |
|-----|------|----------|---------|-------------|
| Path | string | `path` | *(resolved, see below)* | File path to the SQLite database. Resolution depends on whether the value contains a directory component and whether `XDG_DATA_HOME` is set (see below). |

**Database path resolution:**

1. If `path` contains a directory component (e.g. `"./name.db"`,
   `"/var/lib/name.db"`): used as-is, regardless of `XDG_DATA_HOME`.
2. If `path` is a bare filename (e.g. `"myapp.db"`) and `XDG_DATA_HOME` is
   set: `$XDG_DATA_HOME/myapp.db`.
3. If `path` is a bare filename and `XDG_DATA_HOME` is unset: used as-is.
4. If `path` is empty and `XDG_DATA_HOME` is set: `$XDG_DATA_HOME/apikit.db`.
5. If `path` is empty and `XDG_DATA_HOME` is unset: `./data/apikit.db`.

### `[logging]`

Logging settings.

| Key | Type | TOML key | Default | Description |
|-----|------|----------|---------|-------------|
| Level | string | `level` | `"info"` | Log verbosity level. Must be one of the seven canonical levels listed below. Comparison is case-insensitive. |
| LogHealthProbes | boolean | `log_health_probes` | `false` | When `true`, requests to `/healthz` and `/readyz` are logged at debug level. When `false` (the default), health-probe requests produce no log output at any level. |

Accepted log levels (in increasing verbosity):

- `panic`
- `fatal`
- `error`
- `warn`
- `info`
- `debug`
- `trace`

The alias `"warning"` is explicitly rejected, even though the underlying logging
library (logrus) accepts it. Use `"warn"` instead.

### `[[oauth.providers]]`

OAuth identity provider configuration. Each entry in this TOML array of tables
defines one OAuth provider. Zero or more providers may be configured.

| Key | Type | TOML key | Required | Description |
|-----|------|----------|----------|-------------|
| Name | string | `name` | Yes | Provider identifier (e.g. `"github"`). Must be unique across all entries. Stored in the `users.provider` database column. |
| ClientID | string | `client_id` | Yes | OAuth application client ID. |
| ClientSecret | string | `client_secret` | Yes | OAuth application client secret. |
| AuthorizeURL | string | `authorize_url` | No | OAuth authorization endpoint URL. When empty, the provider implementation uses its built-in default (e.g. `https://github.com/login/oauth/authorize` for GitHub). |
| TokenURL | string | `token_url` | No | OAuth token exchange endpoint URL. When empty, the provider default is used. |
| UserinfoURL | string | `userinfo_url` | No | User info endpoint URL. When empty, the provider default is used. |

Currently, the only supported provider name is `"github"`. Specifying any other
name causes a startup error from the provider registry.

## Example config.toml

```toml
[server]
port = 8080
bind = "0.0.0.0"
external_url = "https://api.example.com"
mount_point = "/api/v1"
max_body_size = "2MB"

[database]
path = "/var/lib/apikit/apikit.db"

[logging]
level = "info"

[[oauth.providers]]
name = "github"
client_id = "your-github-client-id"
client_secret = "your-github-client-secret"
authorize_url = ""   # empty = use GitHub default
token_url = ""       # empty = use GitHub default
userinfo_url = ""    # empty = use GitHub default
```

## Defaults

When `config.toml` is absent, the server starts with these defaults:

| Field | Default Value |
|-------|---------------|
| `server.port` | `8080` |
| `server.bind` | `"0.0.0.0"` |
| `server.external_url` | `""` (empty) |
| `server.mount_point` | `"/api/v1"` |
| `server.max_body_size` | `"1MB"` (1,048,576 bytes) |
| `database.path` | `$XDG_DATA_HOME/apikit.db` or `./data/apikit.db` |
| `logging.level` | `"info"` |
| `logging.log_health_probes` | `false` |
| `oauth.providers` | `[]` (no providers configured) |

When a config file exists but omits individual fields, the same defaults apply
to the missing fields. TOML is decoded into a struct that is pre-populated with
defaults, so absent keys retain their default values while explicitly-set-to-zero
fields (such as `port = 0` for ephemeral port selection) are honored.

## Validation Rules

The configuration loader validates all fields after parsing. Startup fails with
a descriptive error if any rule is violated.

### Port range

The `server.port` value must be an integer in the range 0--65535 inclusive.
Port 0 is valid and tells the OS to assign an ephemeral port. The actual bound
address is available programmatically via `Server.Addr()` after startup.

### Log level allowlist

The `logging.level` value must be one of exactly seven strings (case-insensitive
comparison):

```
trace  debug  info  warn  error  fatal  panic
```

The string `"warning"` is explicitly rejected. Any value not in this list
produces a validation error.

### Body size format

The `server.max_body_size` value must match the pattern
`^([1-9][0-9]*)(KB|MB|GB)$` -- a positive integer (no leading zeros, no zero
value) followed immediately by one of three unit suffixes. Spaces, lowercase
suffixes in the raw pattern, and other units (e.g. `B`, `TB`) are not accepted.
The parser upper-cases the input before matching, so `"1mb"` is accepted as
equivalent to `"1MB"`.

Examples of valid values: `"1KB"`, `"512KB"`, `"1MB"`, `"10MB"`, `"1GB"`.

Examples of rejected values: `"0MB"` (zero not allowed), `"1 MB"` (space),
`"1024B"` (unsupported suffix), `"1.5MB"` (not an integer).

### OAuth provider requirements

When one or more `[[oauth.providers]]` entries are present, each entry is
validated:

- `name` must be non-empty.
- `client_id` must be non-empty.
- `client_secret` must be non-empty.
- `name` must be unique across all provider entries. Duplicate names produce a
  validation error.

The `authorize_url`, `token_url`, and `userinfo_url` fields are optional. When
left empty, the concrete provider implementation substitutes its built-in
defaults. For the GitHub provider, the defaults are:

| Field | Default URL |
|-------|-------------|
| `authorize_url` | `https://github.com/login/oauth/authorize` |
| `token_url` | `https://github.com/login/oauth/access_token` |
| `userinfo_url` | `https://api.github.com/user` |