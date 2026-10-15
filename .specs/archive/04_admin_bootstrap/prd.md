---
spec_id: '04'
spec_name: admin_bootstrap
title: Admin Bootstrap
status: draft
created_at: '2026-07-17T10:44:35.501728+00:00'
updated_at: '2026-07-17T10:45:31.432024+00:00'
owner: ''
source: interactive
schema_version: 1
---
# Admin Bootstrap

## Source

This spec is derived from the [apikit master PRD](docs/PRD.md). It covers the
**Admin Bootstrap** component — the first-boot detection, admin token
generation, admin email designation, and token rotation lifecycle.

## Intent

Admin Bootstrap is the mechanism that establishes initial administrative access
to a fresh apikit deployment. On first boot (zero users in the database), the
operator designates an admin email and the server generates a break-glass admin
token. On subsequent boots, the server enforces that the operator has secured
the token and provides it via environment variable. The admin token exists for
emergency access only — not day-to-day use.

This spec owns everything between "database is initialized" and "server is
ready to accept requests" that relates to admin credential lifecycle: first-boot
detection, admin email storage, token generation, token file management,
file-presence guards, environment variable validation, token rotation, and the
automatic admin role grant on first OAuth login by the designated email.

## Goals

- Detect first boot by checking for zero users in the database.
- Require `--admin-email <email>` flag on first boot; store the designated
  admin email in the `admin_config` table.
- Generate a cryptographically random admin token in the format
  `<prefix>_admin_<64 hex chars>`.
- Store the SHA-256 hash of the admin token in the `admin_config` table.
- Write the plaintext admin token to an `admin_token` file (mode 0600) next to
  `config.toml`.
- Log the admin token file path at `warn` level on first boot.
- Automatically grant the `admin` role when the designated email authenticates
  via OAuth for the first time; log the event at `info` level.
- On subsequent boots, refuse to start if the `admin_token` file still exists
  on disk, logging an error instructing the operator to save the token and
  delete the file.
- On subsequent boots, read `ADMIN_TOKEN` from the environment, SHA-256 hash
  it, and compare against the stored hash; refuse to start if missing or
  mismatched.
- Ignore `--admin-email` on subsequent boots.
- Support `--reset-admin-token` flag for token rotation: generate a new token
  (same flow as first boot), invalidate the old token, start normally for this
  boot, and apply the file-presence check on the next restart.
- Ensure the admin token is treated as a break-glass credential, not for
  day-to-day use.

## Non-Goals

- **Authentication middleware and token validation.** Covered by the auth
  middleware spec. This spec generates and stores the admin token; validating
  it on incoming API requests is the auth middleware's responsibility.
- **OAuth provider registry and login flow.** Covered by the OAuth spec (to be
  authored). This spec defines the hook that runs when the designated email
  first authenticates but does not implement the OAuth flow itself. The OAuth
  spec will declare a dependency on `admin_bootstrap` when it is written.
- **User CRUD operations.** Covered by the user management spec. This spec only
  checks user count and sets the admin role on designated-email first login.
- **CLI flags parsing infrastructure.** The `--admin-email` and
  `--reset-admin-token` flags are consumed by the server binary's `main()`
  function. This spec defines the bootstrap logic those flags feed into, not
  the flag parsing itself.
- **Admin token usage in API requests.** The admin token's role in
  request-level authentication is the auth middleware's concern.
- **Multiple admin token support.** Exactly one admin token exists at any time.

## Dependencies

| Spec | Relationship |
|------|--------------|
| 01_server_core | Uses server config loading, Echo setup, logging, `TokenPrefix` build-time variable, and `LoadConfig()` for determining config file location |
| 02_database_layer | Uses `admin_config` table for token hash and email storage; uses `users` table for zero-user detection; uses `DB.SqlDB` for queries |

This spec depends on both `01_server_core` and `02_database_layer` being
implemented. The auth middleware spec will consume the stored admin token hash
for runtime validation. The OAuth spec (planned, not yet authored) will call
`ShouldAutoPromote` from the OAuth callback handler; that spec will declare its
dependency on `admin_bootstrap` when it is created.

---

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25+ |
| Token generation | `crypto/rand` (stdlib) |
| Token hashing | SHA-256 (`crypto/sha256`, stdlib) |
| Hex encoding | `encoding/hex` (stdlib) |
| File I/O | `os` (stdlib) |
| Logging | logrus (`github.com/sirupsen/logrus`) |
| Database | SQLite via `internal/db` (from `02_database_layer`) |
| Config | `internal/config` via `apikit.LoadConfig()` (from `01_server_core`) |
| Token prefix | `apikit.TokenPrefix` (from `01_server_core`) |

## Repository Layout (relevant packages)

```
internal/
  bootstrap/              Admin bootstrap logic
    bootstrap.go          Core bootstrap functions
    bootstrap_test.go     Unit and integration tests
```

The bootstrap package is `internal/bootstrap` and is not importable by
consuming projects. The server binary's `main()` function calls bootstrap
functions after loading config and opening the database, before starting the
HTTP server.

---

## Functional Requirements

### First Boot Detection

First boot is defined as: zero rows exist in the `users` table. The bootstrap
logic queries `SELECT COUNT(*) FROM users` to determine this.

This check runs early in the server startup sequence, after the database has
been opened and schema created (by `02_database_layer`), but before the HTTP
server begins accepting requests.

### First Boot Sequence

When zero users exist in the database:

1. **Require `--admin-email`.** If the `--admin-email` flag was not provided,
   the bootstrap function returns an error. The server must not start without
   a designated admin email on first boot.

2. **Store admin email.** Insert the designated admin email into the
   `admin_config` table with key `admin_email`. Use `INSERT OR REPLACE` to
   handle idempotent re-runs.

3. **Generate admin token.** Generate 32 cryptographically random bytes using
   `crypto/rand.Read`, encode as 64 lowercase hex characters, and prepend the
   token prefix to form: `<prefix>_admin_<64 hex chars>`. The prefix is read
   from `apikit.TokenPrefix`.

4. **Store token hash.** Compute the SHA-256 hash of the full token string
   (including prefix), hex-encode it, and store in the `admin_config` table
   with key `admin_token_hash`. Use `INSERT OR REPLACE`.

5. **Write token file.** Write the plaintext token to a file named
   `admin_token` in the same directory as `config.toml`. The file is created
   with mode `0600` (owner read/write only). If the file already exists, it is
   overwritten.

   The directory for the token file is determined by the same logic that
   `LoadConfig()` uses to find `config.toml`:
   - If `XDG_CONFIG_HOME` is set: `$XDG_CONFIG_HOME/apikit/admin_token`
   - Otherwise: `./admin_token` (current working directory)

   > **Note on the non-XDG case:** When `XDG_CONFIG_HOME` is not set, the
   > server's working directory is guaranteed to be the same directory as
   > `config.toml` by operational convention. Operators must start the server
   > binary from the directory containing `config.toml` in non-XDG deployments.
   > This is consistent with how `LoadConfig()` resolves the config file path.

6. **Log file path.** Log the absolute path of the admin token file at `warn`
   level with a message instructing the operator to save the token securely.

7. **Server starts normally.** The server proceeds to start the HTTP server and
   accept requests.

### Admin Email Auto-Promotion

When a user authenticates via OAuth and their email matches the stored
`admin_email` value in `admin_config`:

- If this is the user's **first authentication** (user is being created, not
  updated), the user is automatically granted `role: admin`.
- The server logs this event at `info` level with a message identifying the
  promoted user.
- This hook is called by the OAuth callback handler (owned by the OAuth spec)
  during user upsert. The bootstrap package exports a function that the OAuth
  handler calls to check whether a newly created user should be auto-promoted.

```go
// ShouldAutoPromote checks whether the given email matches the stored
// admin_email in admin_config. Returns true if the email matches and the
// user should be granted the admin role. Returns false if no admin_email
// is configured or the email does not match.
// This function is called by the OAuth callback handler when creating a
// new user (not on update of existing users).
func ShouldAutoPromote(ctx context.Context, sqlDB *sql.DB, email string) (bool, error)
```

### Subsequent Boot Sequence

When at least one user exists in the database:

1. **File-presence guard.** Check whether the `admin_token` file exists on
   disk (same path resolution as first boot). If the file exists, the
   bootstrap function returns an error with a message instructing the operator
   to save the admin token securely and delete the file. The server must not
   start while the plaintext token file remains on disk.

2. **Environment variable validation.** Read the `ADMIN_TOKEN` environment
   variable. If it is empty or not set, the bootstrap function returns an error
   with a message indicating that `ADMIN_TOKEN` is required.

3. **Hash comparison.** Compute the SHA-256 hash of the `ADMIN_TOKEN` value,
   hex-encode it, and compare against the `admin_token_hash` stored in the
   `admin_config` table. If the hashes do not match, the bootstrap function
   returns an error indicating the admin token is invalid.

4. **Ignore `--admin-email`.** The `--admin-email` flag is silently ignored on
   subsequent boots. No warning is logged.

5. **Server starts normally.** The bootstrap function returns nil and the
   server proceeds to start.

### Token Rotation (`--reset-admin-token`)

When the `--reset-admin-token` flag is set on boot:

1. **Generate new token.** Use the same token generation flow as first boot:
   generate 32 random bytes, hex-encode, prepend prefix.

2. **Store new hash.** Overwrite the `admin_token_hash` in the `admin_config`
   table with the SHA-256 hash of the new token. The old token is immediately
   invalidated.

3. **Write token file.** Write the new plaintext token to the `admin_token`
   file (same path, same mode 0600).

4. **Log file path.** Log the admin token file path at `warn` level.

5. **Server starts normally for this boot.** The file-presence guard is
   skipped during this boot because the file was just created. The `ADMIN_TOKEN`
   environment variable check is also skipped during a reset boot.

6. **Next restart.** On the next restart (without `--reset-admin-token`), the
   file-presence guard and environment variable validation apply as normal. The
   operator must save the new token, delete the file, and set `ADMIN_TOKEN` to
   the new value.

### Bootstrap Execution Order

The bootstrap logic executes in this order during server startup:

1. Config loaded (`01_server_core`).
2. Database opened and schema created (`02_database_layer`).
3. **Bootstrap runs** (this spec):
   a. Query user count.
   b. If `--reset-admin-token`: run token rotation, return.
   c. If zero users (first boot): run first boot sequence, return.
   d. If users exist (subsequent boot): run subsequent boot sequence, return.
4. HTTP server starts.

### Concurrency and Multi-Instance Startup

Concurrent first-boot starts against the same database are **not supported**.
Operators must ensure that only a single server instance starts at a time,
particularly on first boot and during token rotation. SQLite's single-writer
model provides some serialization, but a race between two simultaneous first-boot
sequences could result in divergent token hashes being written and an
inconsistent `admin_config` state. Deployment tooling (e.g., init systems,
container orchestrators) must be configured to prevent concurrent startup.

This constraint applies to:
- First boot (two instances racing to write `admin_email` and `admin_token_hash`).
- Token rotation (two instances racing to rotate the token).

Normal subsequent-boot starts (read-only validation of `ADMIN_TOKEN`) are safe
to race because they only read from `admin_config`.

### Token Format

The admin token format is: `<prefix>_admin_<64 hex chars>`

- `<prefix>` is the build-time configurable token prefix from
  `apikit.TokenPrefix` (default: `ak`).
- `_admin_` is a fixed infix identifying this as an admin token.
- `<64 hex chars>` is 32 cryptographically random bytes encoded as lowercase
  hexadecimal.

Example with default prefix: `ak_admin_a1b2c3d4e5f6...` (64 hex chars total
in the random portion).

### Token Hashing

All token hash operations use SHA-256 from `crypto/sha256`:

1. Compute `sha256.Sum256([]byte(token))` where `token` is the full token
   string including prefix.
2. Hex-encode the resulting 32-byte hash to produce a 64-character lowercase
   hex string.
3. Store this hex string in the `admin_config` table.

Comparison is performed by computing the SHA-256 hash of the candidate token
and comparing the hex-encoded result against the stored value using
`subtle.ConstantTimeCompare` to prevent timing attacks.

---

## Interfaces

### Public API (internal/bootstrap)

```go
// BootstrapParams holds the inputs for the bootstrap sequence.
type BootstrapParams struct {
    DB             *sql.DB     // database handle from db.Open()
    AdminEmail     string      // value of --admin-email flag (may be empty)
    ResetToken     bool        // true when --reset-admin-token is set
    ConfigDir      string      // directory containing config.toml (for token file placement)
    TokenPrefix    string      // token prefix (from apikit.TokenPrefix)
    Logger         *logrus.Logger
}

// Run executes the admin bootstrap sequence. It detects first boot,
// generates or validates the admin token, and manages the token file.
//
// On first boot (zero users): requires AdminEmail, generates token,
// writes token file, returns nil.
//
// On subsequent boots: validates file-presence guard and ADMIN_TOKEN
// env var against stored hash, returns nil on success.
//
// When ResetToken is true: generates a new token regardless of boot
// state, returns nil on success.
//
// Returns a non-nil error if the server should not start (missing
// admin email on first boot, token file still present, invalid or
// missing ADMIN_TOKEN, etc.).
//
// Concurrent calls to Run() against the same database are not supported.
// Callers must ensure single-instance startup, especially on first boot
// and during token rotation.
func Run(ctx context.Context, params BootstrapParams) error

// ShouldAutoPromote checks whether the given email matches the stored
// admin_email in admin_config. Returns true if the email matches and the
// user should be granted the admin role on first OAuth login.
// Returns (false, nil) if no admin_email is configured or the email
// does not match. Returns (false, error) on database errors.
func ShouldAutoPromote(ctx context.Context, sqlDB *sql.DB, email string) (bool, error)
```

### admin_config Table Usage

The bootstrap package reads and writes the following keys in the `admin_config`
table (created by `02_database_layer`):

| Key | Value | Written by |
|-----|-------|------------|
| `admin_token_hash` | SHA-256 hash of the admin token (64 hex chars) | `Run()` on first boot and token rotation |
| `admin_email` | Designated admin email address | `Run()` on first boot |

### Token File

| Property | Value |
|----------|-------|
| Filename | `admin_token` |
| Location | Same directory as `config.toml` (see note on non-XDG case in First Boot Sequence) |
| Permissions | `0600` (owner read/write only) |
| Contents | Plaintext admin token (single line, no trailing newline) |
| Lifecycle | Created on first boot and token rotation; must be deleted by operator before next restart |

---

## Error Handling

| Condition | Behavior |
|-----------|----------|
| First boot without `--admin-email` | Return error: `"first boot: --admin-email is required"` |
| `admin_token` file exists on subsequent boot | Return error: `"admin_token file exists at %s: save the token securely and delete the file before restarting"` |
| `ADMIN_TOKEN` env var missing on subsequent boot | Return error: `"ADMIN_TOKEN environment variable is required"` |
| `ADMIN_TOKEN` hash does not match stored hash | Return error: `"ADMIN_TOKEN does not match the stored admin token"` |
| `admin_token_hash` not found in admin_config | Return error: `"no admin token hash found in database; run with --reset-admin-token to generate a new token"` |
| Failed to generate random bytes | Return error wrapping `crypto/rand` error |
| Failed to write token file | Return error wrapping `os.WriteFile` error |
| Failed to query user count | Return error wrapping database error |
| Failed to read/write admin_config | Return error wrapping database error |

All errors returned from `Run()` are fatal — the server must not start when
`Run()` returns a non-nil error. The server binary's `main()` function should
call `log.Fatal(err)` on bootstrap failure.

---

## Testing Strategy

### Unit Tests

| Test | Description |
|------|-------------|
| `TestGenerateToken_Format` | Verify token matches `<prefix>_admin_<64 hex chars>` format. Verify the hex portion is exactly 64 characters and contains only lowercase hex digits. |
| `TestGenerateToken_Uniqueness` | Generate two tokens and verify they are different (not a deterministic output). |
| `TestHashToken` | Verify SHA-256 hash of a known token produces the expected hex digest. Verify the hash is 64 lowercase hex characters. |
| `TestHashToken_Comparison` | Verify that hashing the same token twice produces the same result, and hashing different tokens produces different results. |
| `TestTokenFormat_Prefix` | Verify token generation uses the provided prefix, not a hardcoded default. |

### Integration Tests (using `db.OpenMemory`)

| Test | Description |
|------|-------------|
| `TestRun_FirstBoot_Success` | Zero users in DB, admin email provided. Verify: admin_email stored in admin_config, admin_token_hash stored in admin_config, token file created with mode 0600, token file contains a valid-format token, function returns nil. |
| `TestRun_FirstBoot_NoEmail` | Zero users in DB, admin email empty. Verify: function returns an error containing "admin-email". |
| `TestRun_SubsequentBoot_Success` | Users exist, token file absent, ADMIN_TOKEN env var set to matching token. Verify: function returns nil. |
| `TestRun_SubsequentBoot_FileExists` | Users exist, token file present on disk. Verify: function returns an error containing "admin_token file exists". |
| `TestRun_SubsequentBoot_NoEnvVar` | Users exist, token file absent, ADMIN_TOKEN env var unset. Verify: function returns an error containing "ADMIN_TOKEN". |
| `TestRun_SubsequentBoot_WrongToken` | Users exist, token file absent, ADMIN_TOKEN set to wrong value. Verify: function returns an error containing "does not match". |
| `TestRun_SubsequentBoot_IgnoresAdminEmail` | Users exist, admin email provided. Verify: function does not error on the email flag and does not overwrite the stored admin_email. |
| `TestRun_ResetToken` | Users exist, reset flag set. Verify: new admin_token_hash stored (different from old), token file created, function returns nil. |
| `TestRun_ResetToken_InvalidatesOld` | After reset, verify old token hash no longer matches the stored hash. |
| `TestShouldAutoPromote_Match` | Admin email stored, query with matching email. Verify: returns true. |
| `TestShouldAutoPromote_NoMatch` | Admin email stored, query with different email. Verify: returns false. |
| `TestShouldAutoPromote_NoConfig` | No admin_email in admin_config. Verify: returns false, no error. |
| `TestTokenFile_Permissions` | Verify token file is created with mode 0600 (skip on Windows). |
| `TestTokenFile_Content` | Verify token file contains the plaintext token with no trailing newline. |
| `TestConstantTimeComparison` | Verify hash comparison uses constant-time comparison (test by confirming the function uses `subtle.ConstantTimeCompare` — structural test via code inspection or by verifying correct results for matching and non-matching inputs). |

---

## Design Decisions

- **Zero-user detection for first boot.** Checking `SELECT COUNT(*) FROM users`
  is simple, reliable, and avoids the need for a separate "initialized" flag.
  If users exist, the system has been bootstrapped.

- **Admin email via `--admin-email` flag, not config file.** The admin email is
  a one-time bootstrap parameter, not a persistent configuration setting. Using
  a flag makes the first-boot command explicit and visible in process listings
  and deployment scripts.

- **Token file next to config.toml.** Placing the `admin_token` file in the
  same directory as `config.toml` ensures the operator knows where to find it
  and that the file follows the same XDG conventions as the config. In the
  non-XDG case, the server must be started from the directory containing
  `config.toml` to maintain this co-location.

- **File-presence guard on subsequent boots.** Refusing to start when the token
  file exists forces the operator to acknowledge and secure the token. This
  prevents the plaintext token from persisting on disk indefinitely.

- **ADMIN_TOKEN via environment variable.** Environment variables are the
  standard mechanism for injecting secrets in containerized deployments
  (Kubernetes secrets, Docker secrets, systemd credentials). The admin token
  is validated on every boot to ensure the operator has the break-glass
  credential available.

- **Constant-time hash comparison.** Using `subtle.ConstantTimeCompare` for
  hash comparison prevents timing side-channel attacks, even though the admin
  token is not used in high-frequency request paths.

- **`--admin-email` ignored on subsequent boots.** Once an admin exists, the
  email designation is a historical fact stored in the database. Re-specifying
  it on subsequent boots has no effect and does not change the stored value.

- **Token rotation via boot flag.** `--reset-admin-token` reuses the first-boot
  token generation logic. This is safer than a runtime API endpoint because
  rotation can happen even when the service is stopped or the admin token is
  compromised.

- **Break-glass only.** The admin token is explicitly not for day-to-day use.
  Normal admin operations use the admin role granted to users via OAuth. The
  token exists for two scenarios: first boot (before any users exist) and
  emergency access (when all admin users are unavailable).

- **No trailing newline in token file.** The token file contains the raw token
  string with no trailing newline. This makes it safe to use with shell
  commands like `cat admin_token | xargs` or `export ADMIN_TOKEN=$(cat admin_token)`
  without accidental whitespace.

- **Token generation uses crypto/rand.** All randomness comes from
  `crypto/rand.Read`, which is cryptographically secure. The `math/rand`
  package is never used for token generation.

- **Single-instance startup required.** Concurrent bootstrap runs against the
  same database are not supported. Operators must use deployment tooling to
  ensure single-instance startup, particularly on first boot and during token
  rotation. This constraint is documented rather than enforced in code, as
  SQLite's single-writer model provides only partial protection against
  concurrent writes.

- **OAuth spec integration deferred.** The `ShouldAutoPromote` function is
  exported and ready for the OAuth callback handler to call. The OAuth spec
  will declare a dependency on `admin_bootstrap` when it is authored; no
  changes to this spec are required at that time.

---

## Glossary

| Term | Definition |
|------|------------|
| **First boot** | A server start where zero users exist in the database, indicating the system has never been bootstrapped. |
| **Admin token** | A break-glass infrastructure credential in the format `<prefix>_admin_<64 hex>` for emergency access. Not for routine use. |
| **File-presence guard** | A startup check that refuses to start the server if the plaintext `admin_token` file still exists on disk. Forces the operator to secure and remove the file. |
| **Token rotation** | The process of generating a new admin token and invalidating the old one, triggered by `--reset-admin-token`. |
| **Auto-promotion** | The automatic granting of the `admin` role to a user whose email matches the designated `admin_email` on their first OAuth login. |
| **Break-glass** | An emergency-only access mechanism bypassing normal authentication flows. The admin token is a break-glass credential. |
| **admin_config** | A key-value table in SQLite that stores singleton server state including the admin token hash and designated admin email. |
| **Single-instance startup** | The operational requirement that only one server instance runs the bootstrap sequence at a time, preventing concurrent writes to `admin_config`. |

---

## Owner

Michael Kuehl
