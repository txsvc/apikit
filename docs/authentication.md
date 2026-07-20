# Authentication and Authorization

This guide covers apikit's authentication model, credential lifecycle, authorization framework, and security properties. It is intended for operators deploying apikit and for developers integrating with the API or extending the framework.

## Overview

apikit uses a three-tier credential model delivered via Bearer tokens in the `Authorization` header. Every credential type shares a common prefix (`ak` by default, configurable at build time via `-ldflags`) and is validated through a unified auth middleware applied to the API route group.

| Credential | Audience | Scope | Bound to User |
|---|---|---|---|
| Admin Token | Operators | Global, break-glass | No |
| API Key | Applications, users | Full access within user context | Yes |
| Personal Access Token (PAT) | Automation, integrations | Fine-grained, permission-scoped | Yes |

The system is fail-closed: missing, malformed, expired, or revoked credentials always result in rejection. Unauthenticated requests to protected endpoints receive HTTP 401. Authenticated requests lacking the required authorization level receive HTTP 403.

---

## Credential Types

### Admin Token

The admin token is a break-glass credential for system operators. It is not tied to any user account and grants unrestricted admin access to all API endpoints.

**Format:** `<prefix>_admin_<64 hex characters>`

The 64-character hex suffix is derived from 32 cryptographically random bytes (`crypto/rand`), hex-encoded to lowercase. The full token string, including the prefix and `_admin_` segment, is hashed with SHA-256 before storage.

**Generation and bootstrap.** Admin tokens are never created through the API. They are generated exclusively by the bootstrap sequence at server startup (see [Bootstrap Sequence](#bootstrap-sequence)). The plaintext token is written once to a file on disk (`<config_dir>/admin_token`, mode `0600`). The operator must save the token securely and delete the file before the server will start again on subsequent boots.

**Token rotation.** An operator can force a new admin token by starting the server with the `--reset-admin-token` flag. This generates a new token, overwrites the stored hash in `admin_config`, and writes the new plaintext to the token file. The old token is immediately invalidated.

**Validation flow:**

1. Verify the hex suffix is exactly 64 characters of valid hexadecimal (fast rejection before any database I/O).
2. Compute `SHA-256(full_token_string)` -- the hash covers the entire token including the `ak_admin_` prefix.
3. Query the `admin_config` table for the row where `key = "admin_token_hash"`.
4. Compare the computed hash against the stored hash using `crypto/subtle.ConstantTimeCompare`.
5. On success, return `AuthInfo` with `CredentialType: "admin_token"`, empty `UserID`, and `Role: "admin"`.

**When to use.** The admin token is intended for initial system setup, emergency access, and automated infrastructure tooling. It should not be used for day-to-day operations. Once the system has users with admin roles, prefer API keys authenticated as an admin user.

---

### API Key

API keys provide user-scoped access. Each key is bound to a single user and inherits that user's role. A user can have at most one active (non-revoked) API key at a time -- generating a new key automatically revokes all existing active keys for that user.

**Format:** `<prefix>_<key_id>_<secret>`

- `key_id`: 8-character alphanumeric string (`[a-zA-Z0-9]`), randomly generated via `crypto/rand`.
- `secret`: 32-character alphanumeric string (`[a-zA-Z0-9]`), randomly generated via `crypto/rand`.

The `key_id` serves as the primary key in the `api_keys` database table. Only the SHA-256 hash of the `secret` is stored; the plaintext secret is returned exactly once at creation time and cannot be retrieved afterward.

**Generation.** API keys are generated in two contexts:

- **OAuth callback** (`POST /auth/callback`): After a successful OAuth code exchange, the system revokes all active keys for the user and generates a new API key within the same database transaction. The full key is returned in the response.
- **Key refresh** (`POST /user/keys/:key_id/refresh`): Generates a new secret for an existing key, resets the expiration window, and returns the updated key. This endpoint rejects PAT authentication -- only API key authentication is accepted.

**Expiration.** Keys support configurable expiration: 0 (permanent), 30, 60, or 90 days. The default is 90 days. When `expires_days` is 0, the `expires_at` column is NULL (no expiration). The expiration window is calculated from the creation or refresh time.

**Revocation.** Keys can be revoked by:
- The owning user via `DELETE /user/keys/:key_id`.
- An admin via `DELETE /users/:id/keys/:key_id`.
- Automatic revocation when a new key is generated (OAuth login or key generation).

Revocation sets the `revoked_at` timestamp. The row is never deleted, preserving the audit trail. Revocation is idempotent -- revoking an already-revoked key returns a 400 response ("key is already revoked") rather than an error.

**Validation flow:**

1. Look up the `key_id` in the `api_keys` table, selecting `user_id`, `secret_hash`, `revoked_at`, `expires_at`.
2. Check `revoked_at`: if non-NULL and non-empty, return 401 "credential revoked".
3. Check `expires_at`: if non-NULL and in the past, return 401 "credential expired".
4. Compute `SHA-256(secret)` and compare against `secret_hash` using `crypto/subtle.ConstantTimeCompare`.
5. Query the `users` table for the user's `role` and `status`.
6. If `status == "blocked"`, return 403 "user is blocked".
7. On success, return `AuthInfo` with `CredentialType: "api_key"`, the user's `UserID`, `Role`, and `KeyID`.

---

### Personal Access Token (PAT)

PATs provide fine-grained, permission-scoped access. Each PAT is bound to a user and carries an explicit list of permissions. Unlike API keys, PATs are never treated as admin-level even if the owning user has the `admin` role. This is a deliberate design decision to preserve PAT scoping.

**Format:** `<prefix>_pat_<token_id>_<secret>`

- `token_id`: 8-character string from `[a-z0-9]`, randomly generated via `crypto/rand`.
- `secret`: 32-character string from `[a-z0-9]`, randomly generated via `crypto/rand`.

Note that the PAT alphabet (`[a-z0-9]`, 36 characters) differs from the API key alphabet (`[a-zA-Z0-9]`, 62 characters).

**Creation** (`POST /user/tokens`). Requires the `tokens:manage` permission. The request body specifies:
- `name`: Human-readable label (required, max 255 characters).
- `permissions`: Array of `resource_type:action` strings (required, non-empty). Each permission must be registered in the `PermissionRegistry`.
- `expires`: Optional integer (0, 30, 60, or 90 days; defaults to 90).

**Privilege escalation prevention.** When a PAT is used to create another PAT, the new token's permissions cannot exceed the creating token's permissions. Attempting to grant a permission not held by the calling PAT returns 403 with `"cannot grant permission: <perm>"`. API keys and admin tokens can create PATs with any registered permission.

**Revocation.** PATs are revoked via `DELETE /user/tokens/:token_id` (requires `tokens:manage`) or by an admin via `DELETE /users/:id/tokens/:token_id`. Like API keys, the row is never deleted. The handler disambiguates between "token not found" (404) and "token already revoked" (400) when the conditional UPDATE affects zero rows.

**Validation flow:**

1. Look up `token_id` in the `pats` table, selecting `user_id`, `secret_hash`, `permissions`, `revoked_at`, `expires_at`.
2. Check `revoked_at`: if non-NULL and non-empty, return 401 "credential revoked".
3. Check `expires_at`: if non-NULL and in the past, return 401 "credential expired".
4. Compute `SHA-256(secret)` and compare against `secret_hash` using `crypto/subtle.ConstantTimeCompare`.
5. Query the `users` table for the user's `role` and `status`.
6. If `status == "blocked"`, return 403 "user is blocked".
7. Deserialize the `permissions` column (stored as a JSON string array) into `[]string`.
8. On success, return `AuthInfo` with `CredentialType: "pat"`, the user's `UserID`, `Role`, `TokenID`, and `Permissions`.

---

## Token Format

All tokens use the build-time configurable `TokenPrefix` (default: `ak`) and are delivered as `Bearer <token>` in the `Authorization` header. The auth middleware identifies the credential type by prefix pattern, checked in the following priority order:

| Priority | Credential | Pattern | Example |
|---|---|---|---|
| 1 | Admin Token | `<prefix>_admin_<64 hex chars>` | `ak_admin_a1b2c3d4...` (64 hex chars) |
| 2 | PAT | `<prefix>_pat_<token_id>_<secret>` | `ak_pat_abcd1234_efgh5678...` (32 chars) |
| 3 | API Key | `<prefix>_<key_id>_<secret>` | `ak_AbCdEfGh_iJkLmNoP...` (32 chars) |

Detection priority matters: the admin token prefix (`ak_admin_`) and PAT prefix (`ak_pat_`) are checked before the general API key prefix (`ak_`) to prevent misclassification. An unrecognized prefix returns 401 "unrecognized token format".

### Component summary

| Component | Length | Charset | Generated by |
|---|---|---|---|
| Admin hex suffix | 64 chars | `[0-9a-f]` | `crypto/rand` (32 random bytes, hex-encoded) |
| API Key `key_id` | 8 chars | `[a-zA-Z0-9]` | `crypto/rand` (62-char alphanumeric) |
| API Key `secret` | 32 chars | `[a-zA-Z0-9]` | `crypto/rand` (62-char alphanumeric) |
| PAT `token_id` | 8 chars | `[a-z0-9]` | `crypto/rand` (36-char lowercase alphanumeric) |
| PAT `secret` | 32 chars | `[a-z0-9]` | `crypto/rand` (36-char lowercase alphanumeric) |

---

## Bootstrap Sequence

The bootstrap sequence runs after configuration loading and database initialization but before the HTTP server begins accepting requests. A non-nil error from `Run()` is fatal -- the server must not start.

### First Boot (zero users in the database)

1. **Require admin email.** The `--admin-email` flag must be provided. If empty, the server exits with `"first boot: --admin-email is required"`.
2. **Store admin email.** Writes `admin_email` to the `admin_config` table via `INSERT OR REPLACE`.
3. **Generate admin token.** Creates a token in the format `<prefix>_admin_<64 hex chars>` using 32 bytes from `crypto/rand`.
4. **Store token hash.** Computes `SHA-256(token)` and stores the hex digest in `admin_config` under the key `admin_token_hash`.
5. **Write token file.** Writes the plaintext token to `<config_dir>/admin_token` with file mode `0600` (owner read/write only).
6. **Log warning.** Logs the absolute path to the token file at WARN level: `"Admin token written to <path> -- save the token securely and delete the file"`.

### Subsequent Boot (users exist in the database)

1. **File-presence guard.** If the `<config_dir>/admin_token` file exists on disk, the server refuses to start: `"admin_token file exists at <path>: save the token securely and delete the file before restarting"`. This prevents an operator from accidentally leaving the plaintext token on disk.
2. **Ignore admin email.** The `--admin-email` flag is silently ignored on subsequent boots.
3. **Require ADMIN_TOKEN environment variable.** If `ADMIN_TOKEN` is empty, the server exits with `"ADMIN_TOKEN environment variable is required"`.
4. **Read stored hash.** Queries `admin_config` for `admin_token_hash`. If missing, suggests running with `--reset-admin-token`.
5. **Constant-time comparison.** Computes `SHA-256(ADMIN_TOKEN)` and compares against the stored hash using `crypto/subtle.ConstantTimeCompare`. A mismatch is fatal.

### Token Rotation (`--reset-admin-token`)

Token rotation takes priority over all other bootstrap logic. When the `--reset-admin-token` flag is set:

1. Generate a new admin token.
2. Compute SHA-256 and store the hash via `INSERT OR REPLACE`.
3. Write the new plaintext to the token file (mode `0600`).
4. Log the file path at WARN level.

Token rotation skips the file-presence guard and the `ADMIN_TOKEN` environment variable check. The previous token is immediately invalidated because the stored hash is overwritten.

### Bootstrap decision tree

```
Server starts
  |
  +-- --reset-admin-token set?
  |     YES --> Generate new token, store hash, write file, done
  |     NO  --> Continue
  |
  +-- Check admin_token_hash in admin_config table
        |
        +-- Not found (first boot)
        |     +-- --admin-email provided?
        |     |     NO  --> Fatal: "--admin-email is required"
        |     |     YES --> Store email, generate token, store hash, write file, done
        |     
        +-- Found (subsequent boot)
              +-- admin_token file exists on disk?
              |     YES --> Fatal: "save the token and delete the file"
              |     NO  --> Continue
              +-- ADMIN_TOKEN env var set?
              |     NO  --> Fatal: "ADMIN_TOKEN environment variable is required"
              |     YES --> Continue
              +-- Compare SHA-256(ADMIN_TOKEN) vs stored hash
                    MISMATCH --> Fatal: "does not match"
                    MATCH    --> Boot succeeds
```

---

## Authorization Model

After successful authentication, the auth middleware injects an `AuthInfo` struct into the request context. Handlers use three authorization helpers to enforce access control.

### RequireAdmin

Returns 403 "forbidden" unless the credential has admin-level access.

Admin-level access is granted to:
- Admin tokens (`CredentialType == "admin_token"`).
- API keys where the owning user has `Role == "admin"`.

PATs are never admin-level, even if the owning user has the admin role. This is an intentional design decision to preserve PAT scoping -- a PAT always operates within its declared permissions, never at the admin level.

If `AuthInfo` is nil (unauthenticated context), returns 403.

**Used by:** All `/users/*` admin endpoints, all `/orgs/*` admin endpoints, credential management endpoints under `/users/:id/keys/*` and `/users/:id/tokens/*`.

### RequireOwnerOrAdmin

Returns nil if the authenticated user's ID matches `resourceOwnerID`, or if the credential has admin-level access (falls through to `RequireAdmin`). Returns 403 "forbidden" otherwise.

An empty `resourceOwnerID` is treated as non-matching -- it cannot equal any valid UUID, so the check falls through to the admin test.

**Used by:** Endpoints where a user should access their own resources but admins can access any user's resources.

### RequirePermission

Checks whether the credential carries a specific `resource_type:action` permission.

- **Admin tokens and API keys** bypass the permission check entirely and return nil. These credential types carry implicit full permissions for their access level.
- **PATs** must have the exact `resource_type:action` string in their `Permissions` slice. If the permission is not found, returns 403 "insufficient permissions".
- If `AuthInfo` is nil, returns 403 "forbidden" (fail-closed).

**Used by:** Self-service endpoints under `/user/*`:
- `GET /user` and `PATCH /user` require `users:read`.
- `GET /user/orgs` requires `orgs:read`.
- `GET /user/tokens` requires `tokens:read`.
- `POST /user/tokens` and `DELETE /user/tokens/:token_id` require `tokens:manage`.
- `GET /user/keys`, `POST /user/keys/:key_id/refresh`, and `DELETE /user/keys/:key_id` require any valid credential (Admin Token, API Key, or PAT). No specific permission is enforced by the handlers; the auth middleware validates the credential.

### Authorization summary by credential type

| Capability | Admin Token | API Key (admin role) | API Key (user role) | PAT |
|---|---|---|---|---|
| `RequireAdmin` | Pass | Pass | Fail (403) | Fail (403) |
| `RequireOwnerOrAdmin` | Pass (admin) | Pass (admin) | Pass if owner | Pass if owner |
| `RequirePermission` | Pass (bypass) | Pass (bypass) | Pass (bypass) | Checked against permissions list |
| Admin endpoints | Yes | Yes | No | No |

---

## Permission Registry

The `PermissionRegistry` is a thread-safe registry of valid `resource_type:action` permission pairs. It is used to validate PAT permissions at creation time and is extensible by consuming projects.

### Built-in permissions

The registry is pre-populated with six built-in permissions:

| Permission | Resource Type | Action | Grants |
|---|---|---|---|
| `users:read` | `users` | `read` | View own profile |
| `orgs:read` | `orgs` | `read` | List own organizations |
| `keys:read` | `keys` | `read` | List own API keys |
| `keys:manage` | `keys` | `manage` | Refresh and revoke own API keys |
| `tokens:read` | `tokens` | `read` | List and view own PATs |
| `tokens:manage` | `tokens` | `manage` | Create and revoke own PATs |

### Custom registration

Consuming projects can register additional permissions using `Register(resourceType, action)`. Both identifiers must match `^[a-z0-9_]+$` (non-empty, lowercase letters, digits, and underscores only). Duplicate registrations return an error.

```go
registry := auth.NewPermissionRegistry()
err := registry.Register("reports", "read")    // adds "reports:read"
err := registry.Register("reports", "generate") // adds "reports:generate"
```

`List()` returns all registered permissions as a sorted `[]string` in ascending lexicographic order.

`IsValid(resourceType, action)` checks whether a permission pair is registered. This is called during PAT creation to reject unknown permissions.

---

## OAuth Flow

apikit supports OAuth 2.0 authorization code flow for user authentication. The system is built around a `Provider` interface, allowing multiple identity providers.

### Provider interface

Any OAuth provider must implement four methods:

| Method | Purpose |
|---|---|
| `Name()` | Returns the provider identifier (e.g. `"github"`), used as the registry key and stored in the `users.provider` column |
| `AuthorizeURL(state, redirectURI)` | Constructs the OAuth authorization URL with provider-specific parameters |
| `Exchange(ctx, code, redirectURI)` | Exchanges an authorization code for an access token |
| `UserInfo(ctx, accessToken)` | Retrieves user identity (username, email, provider ID) |

The built-in GitHub provider requests the `user:email` scope and maps GitHub's `login`, `email`, and `id` fields to the `UserInfo` struct.

### Provider discovery

`GET /auth/providers` returns the list of configured providers with their authorization URLs. This endpoint is unauthenticated and cached for 5 minutes (`Cache-Control: public, max-age=300`). Secrets (`client_secret`, `token_url`, `userinfo_url`) are excluded from the response.

### Callback flow

`POST /auth/callback` implements the full code exchange, user upsert, and key generation sequence:

1. **Parse and validate** the request body: `provider`, `code`, and `redirect_uri` are required. `expires` defaults to 90 days.
2. **Redirect URI validation.** The redirect URI must be either `http://localhost:<port>/*` (HTTPS on localhost is rejected) or match the scheme and host of the configured `external_url`.
3. **Code exchange.** Call `provider.Exchange()` to obtain an access token. Failure returns 401.
4. **User info retrieval.** Call `provider.UserInfo()` to get the user's identity. Failure returns 502. Empty email is rejected with 400.
5. **Database transaction** (user upsert + key generation):
   - **Existing user:** Look up by `(provider, provider_id)`. If `status == "blocked"`, return 403. Otherwise, update `username`, `email`, and `updated_at`.
   - **New user:** Insert with `role = "user"` and `status = "active"`. Check auto-promotion (see below).
   - **Key revocation:** Revoke all active API keys for the user.
   - **Key generation:** Generate a new API key and insert it.
6. **Response.** Return 200 with the user object and the new API key (including plaintext secret).

### Auto-promotion (admin seeding)

On first boot, the operator provides `--admin-email`. When the first user logs in via OAuth and their email matches the stored `admin_email`:

1. Count existing users with `role = 'admin'`.
2. If no admin exists, promote the new user to `role = "admin"`.
3. If an admin already exists, the user remains `role = "user"`.

This is a one-time mechanism. Once any admin user exists in the database, no further auto-promotion occurs regardless of email match.

---

## Security Properties

### Constant-time comparison

All hash comparisons use `crypto/subtle.ConstantTimeCompare` to prevent timing side-channel attacks. This applies to:
- Admin token hash verification.
- API key secret hash verification.
- PAT secret hash verification.
- Bootstrap token hash verification (`ADMIN_TOKEN` environment variable).

### SHA-256 hashing

All secrets are hashed with SHA-256 before storage. The hash is stored as a 64-character lowercase hexadecimal string. Plaintext secrets are never stored in the database.

- **Admin tokens:** The hash covers the full token string including the prefix (`ak_admin_...`).
- **API key secrets:** The hash covers only the 32-character secret portion, not the full key string.
- **PAT secrets:** The hash covers only the 32-character secret portion, not the full token string.

### Blocked user checks

Every API key and PAT validation includes a user status check after successful secret verification. If the user's status is `"blocked"`, the request is rejected with 403 "user is blocked". This means blocking a user immediately invalidates all their credentials without requiring individual revocation.

The OAuth callback also checks user status: if a blocked user attempts to log in, the transaction is rolled back and 403 is returned.

### Credential isolation

Self-service endpoints query by both the credential identifier (key_id, token_id) and the authenticated user's ID. This prevents users from accessing each other's credentials and returns 404 (not 403) for credentials belonging to other users, avoiding existence leakage.

### Secret visibility

Plaintext secrets are exposed exactly once:
- **Admin token:** Written to a file on disk at bootstrap time.
- **API key:** Returned in the OAuth callback response or key refresh response.
- **PAT:** Returned in the creation response.

After creation, only metadata (key_id/token_id, creation time, expiration, revocation status) is available. The `secret_hash` column is never included in any API response.

### File permissions

The admin token file is written with mode `0600` (owner read/write only). The CLI config directory is created with mode `0700` (owner full access only). The CLI config file (`config.toml`, which may contain API keys) is written atomically via temp file + rename with mode `0600`.

### Request authentication flow

The complete authentication middleware flow for every request to a protected endpoint:

```
Request arrives at API group
  |
  +-- Authorization header present?
  |     NO  --> 401 "missing authorization header"
  |     YES --> Continue
  |
  +-- Starts with "Bearer "?
  |     NO  --> 401 "invalid authorization header format"
  |     YES --> Continue
  |
  +-- Token string non-empty?
  |     NO  --> 401 "missing token"
  |     YES --> Continue
  |
  +-- Classify token by prefix
  |     ak_admin_* --> validateAdminToken()
  |     ak_pat_*   --> validatePAT()
  |     ak_*       --> validateAPIKey()
  |     other      --> 401 "unrecognized token format"
  |
  +-- Validation result
  |     authError  --> Return authError.Code and authError.Message
  |     other err  --> 500 "internal server error"
  |     success    --> Inject AuthInfo into context, call next handler
  |
  +-- Handler calls RequireAdmin / RequirePermission / RequireOwnerOrAdmin
        PASS --> Process request
        FAIL --> 403 "forbidden" or "insufficient permissions"
```

### Middleware ordering

The auth middleware is applied to the API route group (`/api/v1`), not the server root. This means health probes (`/healthz`, `/readyz`, `/version`) and OAuth endpoints (`/auth/providers`, `/auth/callback`) are not protected by authentication.

The middleware itself panics at construction time if either the database or permission registry is nil, ensuring a fail-fast startup rather than silent misconfiguration that manifests at request time.