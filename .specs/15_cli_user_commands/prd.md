---
spec_id: '15'
spec_name: cli_user_commands
title: Cli User Commands
status: draft
created_at: '2026-07-17T13:06:57.376632+00:00'
updated_at: '2026-07-17T13:24:04.293640+00:00'
owner: ''
source: interactive
schema_version: 1
---
# CLI User Commands

## Source Reference

This spec is derived from the [apikit master PRD](docs/PRD.md). It covers the
**CLI User Commands** component — spec 15 of 15. The master PRD sections on
"CLI" (Commands, Config-Mutating Commands, Persistent Client Configuration,
Agent Interface), the Credential Model (OAuth Login Flow, API Key Lifecycle,
PAT Lifecycle), the Authenticated User endpoints, and the Organization
endpoints are the primary sources. The Go SDK spec (spec 12) defines the
client methods each command delegates to. The CLI Core spec (spec 13) provides
the infrastructure (config loading/saving, output formatting, error handling,
Cobra root command, authenticated client construction) that all commands in
this spec build upon.

## Intent

Implement all non-admin CLI commands for `akc` that wrap the Go SDK. These are
the commands available to any authenticated user — login, profile management,
API key lifecycle, PAT management, and organization browsing. Each command is a
thin Cobra command that delegates to the corresponding Go SDK client method,
formats the result as JSON to stdout, and writes human-readable messages to
stderr.

The `login` command is the sole composite command: it orchestrates a multi-step
OAuth flow (provider discovery, browser-based authorization, local callback
server, code exchange) using several SDK methods. All other commands are 1:1
wrappers around a single SDK method.

## Goals

- Implement the `akc login` command with full OAuth flow orchestration:
  - Accept `--provider` flag (string, default `"github"`) and `--expires` flag
    (int, valid values `0`, `30`, `60`, `90`, default `90`).
  - Fetch available OAuth providers from the server via `client.GetProviders(ctx)`.
    This initial call uses a client constructed without an API key (login is
    unauthenticated).
  - Validate that the requested provider exists in the returned list.
  - Generate a cryptographically random `state` parameter (32 bytes, hex-encoded
    to 64 characters) for CSRF protection using `crypto/rand`.
  - Start a local HTTP callback server on a random available port (`:0`). The
    server listens on `127.0.0.1` only (not `0.0.0.0`) and handles a single
    request to `/callback`. Requests to any other path (e.g., `/favicon.ico`)
    receive an HTTP 404 response and do not cause the server to shut down.
  - Construct the authorization URL by parsing the provider's `authorize_url`
    and appending `redirect_uri`, `state`, and `response_type=code` to its
    existing query parameters. Any query parameters already present on
    `authorize_url` (including `client_id`) are preserved as-is. `client_id`
    is never appended separately by the CLI.
  - Open the authorization URL in the user's default browser. Use
    `os/exec` with platform-appropriate commands: `open` on macOS,
    `xdg-open` on Linux. Print the URL to stderr as a fallback if browser
    open fails.
  - Wait for the OAuth callback on the local server. The callback handler
    communicates with the main login goroutine via two channels: a `codeCh`
    channel (carries the extracted authorization code on success) and an
    `errCh` channel (carries an error on state mismatch or other callback
    failure). The main goroutine selects on `codeCh`, `errCh`, and the
    context's `Done()` channel. The callback handler:
    - Validates the `state` parameter matches the generated value. If
      mismatched, sends an error on `errCh`, responds to the browser with
      HTTP 400 and the state-mismatch error HTML page (see below), and
      returns. The main goroutine receives from `errCh`, cancels the context
      (triggering server shutdown via context cancellation), and exits with
      code 2. The server does not accept further requests after a state
      mismatch.
    - Extracts the `code` query parameter.
    - Responds to the browser with the success HTML page (see below).
    - Sends the code on `codeCh`.
  - Exchange the authorization code with the server via
    `client.ExchangeOAuthCode(ctx, &apikit.AuthCallbackRequest{...})`,
    passing `provider`, `code`, `redirect_uri` (the local callback URL),
    and `expires`.
  - On success, save `endpoint_url`, `user_id`, and `api_key` to the config
    file using the CLI Core config save function (atomic write).
  - Print the user object JSON to stdout.
  - Print a human-readable success message to stderr (e.g.,
    `"Logged in as <username>"`).
  - The local callback server shuts down after receiving a successful `/callback`
    with valid state (or after a timeout). Requests to non-`/callback` paths
    return HTTP 404 and do not trigger shutdown. Use a context with timeout
    (120 seconds, defined as `loginTimeoutSeconds = 120` in `login.go`) to
    avoid hanging indefinitely if the user never completes the browser flow.
  - Login timeout: if the local callback server does not receive a callback
    within 120 seconds, the command prints an error to stderr and exits with
    code 2.
  - The `--endpoint-url` flag (from CLI Core's persistent flags) is required
    for login since there may be no config file yet. If not provided via flag,
    env var, or existing config, print an error and exit with code 2.
- Implement `akc user show`:
  - No flags or arguments.
  - Delegates to `client.GetUser(ctx)`.
  - Prints the `User` object (from `Response[User].Data`) as JSON to stdout.
  - Ignores ETag/conditional GET — always fetches fresh data.
- Implement `akc user update`:
  - Accepts `--full-name` flag (string, required). No client-side format or
    length validation is performed — all validation is delegated to the server.
    Server-side validation failures (e.g., empty name, name that fails server
    rules) surface as 4xx API errors with exit code 1.
  - Delegates to `client.UpdateUser(ctx, &apikit.UpdateUserRequest{FullName: fullName})`.
  - Prints the updated `User` object as JSON to stdout.
- Implement `akc keys list`:
  - No flags or arguments.
  - Delegates to `client.ListKeys(ctx)`.
  - Prints the `[]*APIKeyMeta` array as JSON to stdout.
- Implement `akc keys refresh`:
  - No flags or arguments.
  - Requires the current API key's `key_id`. The command extracts the `key_id`
    from the configured `api_key` by parsing the key format
    `<prefix>_<key_id>_<secret>` — the second segment between underscores.
  - Delegates to `client.RefreshKey(ctx, keyID)`.
  - On success, updates `api_key` in the config file with the new full key
    from `APIKeyFull.Key` (atomic write).
  - Prints the `APIKeyFull` object as JSON to stdout.
  - Prints a human-readable message to stderr (e.g., `"API key refreshed"`).
- Implement `akc keys revoke`:
  - No flags or arguments.
  - Extracts `key_id` from the configured `api_key` (same parsing as refresh).
  - Delegates to `client.RevokeKey(ctx, keyID)`.
  - On success, clears `api_key` and `user_id` from the config file (atomic
    write). The values are set to empty strings.
  - Prints the `RevokeKeyResponse` object as JSON to stdout.
  - Prints a human-readable message to stderr (e.g.,
    `"API key revoked. Run 'akc login' to obtain a new key."`).
- Implement `akc tokens list`:
  - No flags or arguments.
  - Delegates to `client.ListTokens(ctx)`.
  - Prints the `[]*PAT` array as JSON to stdout.
- Implement `akc tokens create`:
  - Accepts `--name` flag (string, required), `--permissions` flag (string,
    required, comma-separated `resource_type:action` pairs), and `--expires`
    flag (int, valid values `0`, `30`, `60`, `90`, default `90`).
  - Parses the `--permissions` flag value by splitting on `,` and trimming
    whitespace from each entry.
  - Validates that the parsed permissions slice is non-empty. If `--permissions`
    is an empty string or results in an empty slice after splitting and
    trimming, the command exits with code 2 and an error:
    `{"error":{"code":2,"message":"--permissions must not be empty"}}`. No
    client-side format validation is performed on individual permission entries
    (e.g., `resource_type:action` format) — all entry-level validation is
    delegated to the server.
  - Validates `--expires` value is one of `0`, `30`, `60`, `90`. Exits with
    code 2 and a descriptive error if invalid.
  - Delegates to `client.CreateToken(ctx, &apikit.CreateTokenRequest{...})`.
  - Prints the `PATFull` object as JSON to stdout. This is the only time the
    plaintext token is available.
  - Prints a warning to stderr (e.g.,
    `"Token created. Save the token value — it cannot be retrieved later."`).
- Implement `akc tokens show`:
  - Accepts one positional argument: `token_id` (required).
  - Delegates to `client.GetToken(ctx, tokenID)`.
  - Prints the `PAT` object (from `Response[PAT].Data`) as JSON to stdout.
  - Ignores ETag/conditional GET — always fetches fresh data.
- Implement `akc tokens revoke`:
  - Accepts one positional argument: `token_id` (required).
  - Delegates to `client.RevokeToken(ctx, tokenID)`.
  - RevokeToken returns only `error` (no response body — HTTP 204).
  - Prints `{}` (empty JSON object) to stdout on success.
  - Prints a human-readable message to stderr (e.g.,
    `"Token <token_id> revoked"`).
- Implement `akc orgs list`:
  - No flags or arguments.
  - Delegates to `client.ListUserOrgs(ctx)`.
  - Prints the `[]*Organization` array as JSON to stdout.
- Implement `akc orgs show`:
  - Accepts one positional argument: `id` (required, org UUID).
  - Delegates to `client.GetOrg(ctx, id)`.
  - Prints the `Organization` object (from `Response[Organization].Data`) as
    JSON to stdout.
  - Ignores ETag/conditional GET — always fetches fresh data.
- Implement `akc orgs members`:
  - Accepts one positional argument: `id` (required, org UUID).
  - Delegates to `client.ListOrgMembers(ctx, id)`.
  - Prints the `[]*User` array as JSON to stdout.
- All commands use the JSON output formatting from CLI Core (indented JSON
  with `json.MarshalIndent` using two-space indentation and no HTML escaping).
- All commands use the error handling from CLI Core (API errors as JSON error
  envelope to stdout, client errors as JSON error envelope to stdout, exit
  code 1 for API errors, exit code 2 for client errors).
- Config-mutating commands (`login`, `keys refresh`, `keys revoke`) use the
  atomic config save function from CLI Core.
- Commands that require authentication (`user show`, `user update`, `keys *`,
  `tokens *`, `orgs *`) construct an authenticated SDK client using CLI Core's
  `NewAuthenticatedClient` (or equivalent), which pre-validates that `api_key`
  is present in config/env/flags before making any SDK call. If `api_key` is
  absent, the command exits with code 2 immediately, without attempting any
  network request. Additionally, `keys refresh` and `keys revoke` separately
  validate that the API key string is parseable into a valid `key_id` before
  calling the SDK; an invalid key format exits with code 2.
- All Cobra command registrations include the metadata fields required by
  `akc help --json` (from CLI Core): description, HTTP method, API path,
  args, flags with types and defaults, and auth level. The `login` command
  sets `composite: true` with `method` and `path` as `null`.

## Non-Goals

- **Admin CLI commands.** Admin user management, admin org management, admin
  key/token management are covered by spec 14 (cli_admin_commands).
- **CLI Core infrastructure.** Config loading/saving (including the config
  file path and format), output formatting, error handling, Cobra root
  command, persistent flags, `akc version`, `akc help`, and `akc help --json`
  are spec 13 (cli_core). The config file path is defined entirely by CLI
  Core; this spec references CLI Core's save/load functions without
  duplicating path definitions.
- **Direct HTTP calls.** The CLI never makes HTTP calls directly — all API
  interactions go through the Go SDK (spec 12).
- **OAuth provider implementation.** The CLI does not implement OAuth provider
  logic — it uses the SDK's `GetProviders` and `ExchangeOAuthCode` methods.
- **Multiple simultaneous login sessions.** One active endpoint + credential
  at a time per the master PRD.
- **Browser detection or custom browser selection.** Uses platform defaults
  (`open` / `xdg-open`) only.
- **Windows support.** The CLI targets Unix-like systems per the master PRD.
  Windows behavior is undefined and untested. No compile-time build tag
  exclusion is required; Windows users are simply unsupported.
- **Custom callback server TLS.** The local callback server uses plain HTTP
  on localhost.
- **Token/key expiry validation on the client side.** The CLI does not check
  whether credentials are expired before making API calls. The server rejects
  expired credentials with 401; the CLI surfaces that error.
- **Interactive prompts or confirmation dialogs.** All commands execute
  immediately without user confirmation. The CLI is designed for agent
  (LLM) consumption where interactive prompts are a barrier.
- **Colorized output.** All structured output is plain JSON. Stderr messages
  are plain text without ANSI color codes.
- **Client-side validation of individual `--permissions` entries.** The CLI
  does not validate the `resource_type:action` format of individual permission
  entries. Only the emptiness of the parsed slice is validated client-side;
  all entry-level format validation is delegated to the server.
- **Client-side validation of `--full-name` content.** No length or format
  checks are applied to the `--full-name` flag value. All validation is
  delegated to the server.

## Dependencies

| Spec | From Group | To Group | Relationship |
|------|-----------|----------|--------------|
| `13_cli_core` | 1 | 1 | Uses CLI infrastructure: Cobra root command, config loading/saving (atomic writes, config file path), output formatting (JSON to stdout, messages to stderr), error handling (exit codes, error envelope), persistent flags (`--endpoint-url`, `--api-key`, `--user-id`), `NewAuthenticatedClient` (including pre-validation of `api_key` presence), and `help --json` metadata registration. |
| `12_go_sdk` | 1 | 1 | All commands delegate to Go SDK client methods. Login uses `GetProviders` and `ExchangeOAuthCode`. User commands use `GetUser`, `UpdateUser`. Key commands use `ListKeys`, `RefreshKey`, `RevokeKey`. Token commands use `ListTokens`, `CreateToken`, `GetToken`, `RevokeToken`. Org commands use `ListUserOrgs`, `GetOrg`, `ListOrgMembers`. |

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go (matches project — Go 1.25+ per master PRD) |
| CLI framework | Cobra (`github.com/spf13/cobra`) |
| Config format | TOML via CLI Core (spec 13) |
| JSON output | `encoding/json` (stdlib) — `json.MarshalIndent` with two-space indent |
| Crypto | `crypto/rand` (stdlib) for OAuth state parameter |
| Browser open | `os/exec` (stdlib) — `open` on macOS, `xdg-open` on Linux |
| HTTP server | `net/http` (stdlib) for local OAuth callback server |
| Testing | `go test` (stdlib) |

## Repository Layout

```
cmd/akc/                        CLI binary entry point (spec 13)
  main.go                       Calls AddCommand for each group on root command
internal/cli/                   CLI implementation package
  login.go                      Login command (OAuth flow orchestration)
  user.go                       User show/update commands
  keys.go                       Keys list/refresh/revoke commands
  tokens.go                     Tokens list/create/show/revoke commands
  orgs.go                       Orgs list/show/members commands
  helpers.go                    Shared helpers: expires validation, permissions
                                validation, key ID parsing, browser open
  login_test.go                 Login command tests
  user_test.go                  User command tests
  keys_test.go                  Keys command tests
  tokens_test.go                Tokens command tests
  orgs_test.go                  Orgs command tests
```

All CLI command implementations live in `internal/cli/`. Each file registers
its Cobra commands with the parent command group established by CLI Core
(spec 13). Command group registration with the root command is performed
explicitly in `cmd/akc/main.go` by calling `AddCommand` for each group
command on the root command returned by CLI Core. This approach avoids
`init()` function side effects and import cycles, and allows consuming
projects to selectively embed command groups. Test files use `_test.go`
suffix in the same package.

---

## Functional Requirements

### Command Registration

Each command group (`login`, `user`, `keys`, `tokens`, `orgs`) exposes a
constructor function (e.g., `NewLoginCmd()`, `NewUserCmd()`) that returns a
`*cobra.Command`. These are registered with the root command from CLI Core
by calling `rootCmd.AddCommand(...)` in `cmd/akc/main.go`. There are no
`init()` functions used for registration.

Command metadata for `akc help --json` is attached to each command using the
annotation mechanism from CLI Core. Each command carries:
- `name` — the full command path (e.g., `"tokens create"`)
- `description` — one-line summary
- `method` — HTTP method (`"GET"`, `"POST"`, `"PATCH"`, `"DELETE"`, or `null` for composite)
- `path` — API path relative to mount point (or `null` for composite)
- `args` — positional arguments with name, type, and required flag
- `flags` — flag definitions with name, type, required, default, and description
- `auth` — authentication level: `"api_key"` for all commands except `login`
  which uses `"none"` (login is unauthenticated — it produces the API key)
- `composite` — `true` only for `login`; absent for all other commands

### Login Command

`akc login [--provider github] [--expires 0|30|60|90]`

The login command is the only composite command in this spec. It orchestrates
a multi-step OAuth flow using the Go SDK.

**Flags:**
- `--provider` (string, default `"github"`) — OAuth provider name.
- `--expires` (int, default `90`) — API key expiry in days. Valid values:
  `0`, `30`, `60`, `90`.

**Preconditions:**
- `endpoint_url` must be available (via `--endpoint-url` flag, `ENDPOINT_URL`
  env var, or config file). If not available, exit with code 2 and error:
  `{"error":{"code":2,"message":"endpoint URL is required for login — use --endpoint-url or set ENDPOINT_URL"}}`.
- `--expires` must be one of `0`, `30`, `60`, `90`. Invalid values produce
  exit code 2 with error:
  `{"error":{"code":2,"message":"--expires must be 0, 30, 60, or 90"}}`.

**HTML Response Pages:**

The callback server returns two verbatim HTML responses:

- **Success page** (HTTP 200, returned on valid state + code):
  ```html
  <html><body><h1>Login successful</h1><p>You may close this tab.</p></body></html>
  ```

- **State mismatch error page** (HTTP 400, returned on state mismatch):
  ```html
  <html><body><h1>Login failed</h1><p>OAuth state mismatch. Please try again.</p></body></html>
  ```

These are the only HTML responses produced by the callback server. No other
HTML pages are defined.

**Flow:**

1. Construct an unauthenticated SDK client using only the `endpoint_url`:
   `apikit.NewClient(endpointURL)`.
2. Fetch providers: `client.GetProviders(ctx)`.
3. Find the requested provider by name in the returned list. If not found,
   exit with code 1 and error:
   `{"error":{"code":404,"message":"provider '<name>' not found"}}`.
4. Generate a 64-character hex state parameter:
   `crypto/rand.Read(32 bytes)` → `hex.EncodeToString`.
5. Start local callback server:
   - Listen on `127.0.0.1:0` (localhost, random port).
   - Derive `redirect_uri` from the listener address:
     `http://127.0.0.1:<port>/callback`.
   - The server handles `GET /callback` only. Any request to a path other
     than `/callback` receives an HTTP 404 response and does not trigger
     server shutdown. This is important because browsers frequently issue
     secondary requests (e.g., `/favicon.ico`) after the initial redirect.
6. Construct authorization URL:
   - Parse the provider's `authorize_url` using `url.Parse`.
   - Retrieve the existing query parameters via `url.Query()`.
   - Add `redirect_uri`, `state`, and `response_type=code` to the existing
     query parameters. Do **not** add `client_id` — it is already present
     in `authorize_url` as returned by the server. Do **not** overwrite any
     existing parameters.
   - Re-encode the full query string and set it on the URL.
7. Print to stderr: `"Opening browser for authentication..."`.
8. Open the URL in the default browser via `os/exec`:
   - macOS (`runtime.GOOS == "darwin"`): `exec.Command("open", url)`
   - Linux (`runtime.GOOS == "linux"`): `exec.Command("xdg-open", url)`
   - If the browser command fails, print the URL to stderr:
     `"Open this URL in your browser: <url>"`.
9. Create two channels:
   - `codeCh chan string` — receives the authorization code on successful callback.
   - `errCh chan error` — receives an error if the callback fails (e.g., state mismatch).
   
   Create a context with a 120-second timeout (`loginTimeoutSeconds = 120`,
   defined as a package-level constant in `login.go`). The login flow logic
   is implemented in an internal helper function with the signature:
   
   ```go
   func runLogin(ctx context.Context, timeout time.Duration, ...) error
   ```
   
   The Cobra `RunE` function calls `runLogin` passing
   `time.Duration(loginTimeoutSeconds) * time.Second` as the timeout. Tests
   call `runLogin` directly with a short timeout (e.g., `200ms`) to exercise
   timeout behavior without waiting 120 seconds. The package-level constant
   `loginTimeoutSeconds` is never modified.
   
   Start the callback handler. The handler:
   - On any path other than `GET /callback`: respond with HTTP 404. Do not
     signal either channel. The server continues listening.
   - On `GET /callback`:
     - Validate `state`. If mismatch: send an error on `errCh`, respond with
       HTTP 400 and the state-mismatch error HTML page, and return from the
       handler.
     - If state is valid: extract `code`, respond with the success HTML page,
       and send `code` on `codeCh`.
   
   The main goroutine selects on:
   - `codeCh`: proceed to step 10.
   - `errCh`: cancel the context (triggering server shutdown), exit with code 2
     and error: `{"error":{"code":2,"message":"OAuth state mismatch — possible CSRF attack"}}`.
   - `ctx.Done()`: exit with code 2 and error:
     `{"error":{"code":2,"message":"login timed out waiting for browser callback"}}`.
   
   After a state mismatch the main goroutine cancels the context. The server
   does not process further requests after context cancellation.
10. Exchange code: `client.ExchangeOAuthCode(ctx, &apikit.AuthCallbackRequest{
    Provider: provider, Code: code, RedirectURI: redirectURI, Expires: &expires})`.
11. Save to config (atomic write via CLI Core):
    - `endpoint_url` = the endpoint URL used for this login.
    - `user_id` = `response.User.ID`.
    - `api_key` = `response.APIKey.Key`.
    - If the config write fails, exit with code 2 and error:
      `{"error":{"code":2,"message":"failed to save config: <detail>"}}`.
      The obtained credentials are **not** printed to stdout on config write
      failure — only the error envelope is written. The user must re-run
      `akc login` to obtain new credentials.
12. Print `response.User` as JSON to stdout.
13. Print to stderr: `"Logged in as <username>"`.
14. Shut down the callback server gracefully. Use a fresh
    `context.Background()` with a 5-second deadline for the shutdown call:
    ```go
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    server.Shutdown(shutdownCtx)
    ```
    This avoids using a potentially already-cancelled login context for the
    shutdown, ensuring the server has time to complete graceful teardown.

### User Commands

**`akc user show`**

- Auth: `api_key`
- SDK method: `client.GetUser(ctx)` → `*Response[User]`
- Output: `response.Data` as JSON to stdout.
- No flags or arguments.

**`akc user update --full-name "..."`**

- Auth: `api_key`
- Flag: `--full-name` (string, required). Cobra `MarkFlagRequired`.
- No client-side validation of the flag value — all validation (e.g., empty
  string, length limits, disallowed characters) is delegated to the server.
- SDK method: `client.UpdateUser(ctx, &apikit.UpdateUserRequest{FullName: fullName})`
  → `*User`
- Output: updated `User` as JSON to stdout.
- Server-side validation failures (e.g., empty name or name that fails server
  rules) are returned as 4xx API errors; the CLI surfaces them as exit code 1
  with the server's error envelope JSON to stdout (same as all other API errors).

### Key Commands

**`akc keys list`**

- Auth: `api_key`
- SDK method: `client.ListKeys(ctx)` → `[]*APIKeyMeta`
- Output: array as JSON to stdout.
- No flags or arguments.

**`akc keys refresh`**

- Auth: `api_key`
- Pre-validation (before SDK call):
  1. `NewAuthenticatedClient` verifies `api_key` is present in config/env/flags;
     exits code 2 if absent.
  2. The command parses `key_id` from the configured `api_key` string; exits
     code 2 if the format is invalid (fewer than 3 `_`-delimited segments).
- SDK method: `client.RefreshKey(ctx, keyID)` → `*APIKeyFull`
- Key ID extraction: parse the configured `api_key` string
  (`<prefix>_<key_id>_<secret>`), split on `_`, take `parts[len(parts)-2]`
  as `key_id`. The secret is always the last segment; the `key_id` is always
  the penultimate segment. This handles prefixes with or without embedded
  underscores.
- On success: update `api_key` in config with `response.Key` (atomic write via
  CLI Core).
- Output: `APIKeyFull` as JSON to stdout.
- Stderr: `"API key refreshed"`.
- No flags or arguments.

**`akc keys revoke`**

- Auth: `api_key`
- Pre-validation: same two-step pre-validation as `keys refresh`.
- SDK method: `client.RevokeKey(ctx, keyID)` → `*RevokeKeyResponse`
- Key ID extraction: same parsing as `keys refresh`.
- On success: clear `api_key` and `user_id` in config (set to empty strings,
  atomic write via CLI Core).
- Output: `RevokeKeyResponse` as JSON to stdout.
- Stderr: `"API key revoked. Run 'akc login' to obtain a new key."`.
- No flags or arguments.

### Token Commands

**`akc tokens list`**

- Auth: `api_key`
- SDK method: `client.ListTokens(ctx)` → `[]*PAT`
- Output: array as JSON to stdout.
- No flags or arguments.

**`akc tokens create --name "..." --permissions "..." [--expires 0|30|60|90]`**

- Auth: `api_key`
- Flags:
  - `--name` (string, required). Cobra `MarkFlagRequired`.
  - `--permissions` (string, required). Comma-separated `resource_type:action`
    pairs (e.g., `"users:read,orgs:read"`). Cobra `MarkFlagRequired`.
  - `--expires` (int, default `90`). Valid values: `0`, `30`, `60`, `90`.
- Permission parsing: split `--permissions` value on `,`, trim whitespace
  from each entry, collect into `[]string`.
- Permission validation: if the resulting slice is empty (i.e., `--permissions`
  was an empty string or contained only whitespace/commas), exit with code 2:
  `{"error":{"code":2,"message":"--permissions must not be empty"}}`.
  No client-side format validation is performed on individual entries — all
  entry-level validation (e.g., `resource_type:action` format) is delegated
  to the server.
- Validation: `--expires` must be `0`, `30`, `60`, or `90`. Exit code 2 on
  invalid.
- SDK method: `client.CreateToken(ctx, &apikit.CreateTokenRequest{
  Name: name, Permissions: perms, Expires: &expires})` → `*PATFull`
- Output: `PATFull` as JSON to stdout.
- Stderr: `"Token created. Save the token value — it cannot be retrieved later."`.

**`akc tokens show <token_id>`**

- Auth: `api_key`
- Arg: `token_id` (positional, required). Cobra `Args: cobra.ExactArgs(1)`.
- SDK method: `client.GetToken(ctx, tokenID)` → `*Response[PAT]`
- Output: `response.Data` as JSON to stdout.

**`akc tokens revoke <token_id>`**

- Auth: `api_key`
- Arg: `token_id` (positional, required). Cobra `Args: cobra.ExactArgs(1)`.
- SDK method: `client.RevokeToken(ctx, tokenID)` → `error`
- Output: `{}` (empty JSON object) to stdout on success. The SDK returns
  only `error` for this endpoint (HTTP 204), so there is no response body.
  The empty object maintains the JSON-to-stdout contract.
- Stderr: `"Token <token_id> revoked"`.

### Org Commands

**`akc orgs list`**

- Auth: `api_key`
- SDK method: `client.ListUserOrgs(ctx)` → `[]*Organization`
- Output: array as JSON to stdout.
- No flags or arguments.

**`akc orgs show <id>`**

- Auth: `api_key`
- Arg: `id` (positional, required). Cobra `Args: cobra.ExactArgs(1)`.
- SDK method: `client.GetOrg(ctx, id)` → `*Response[Organization]`
- Output: `response.Data` as JSON to stdout.

**`akc orgs members <id>`**

- Auth: `api_key`
- Arg: `id` (positional, required). Cobra `Args: cobra.ExactArgs(1)`.
- SDK method: `client.ListOrgMembers(ctx, id)` → `[]*User`
- Output: array as JSON to stdout.

### Key ID Parsing

The API key format is `<prefix>_<key_id>_<secret>`. The `key_id` is needed
by `keys refresh` and `keys revoke` but is not stored separately in the
config file — only the full key string is stored.

Parsing strategy: split the key on `_`, take `parts[len(parts)-2]` as
`key_id`. This works because:
- The secret (last segment) never contains `_`.
- The `key_id` (penultimate segment) never contains `_`.
- The prefix may contain `_` in consuming projects (e.g., `my_app`), but
  since `key_id` and `secret` are fixed-format alphanumeric strings, the
  penultimate segment is always the `key_id`.

If the key string has fewer than 3 segments when split on `_`, the key
format is invalid. Exit with code 2:
`{"error":{"code":2,"message":"invalid API key format"}}`.

The key ID parsing logic is implemented as a shared helper function in
`internal/cli/helpers.go` (e.g., `parseKeyID(apiKey string) (string, error)`)
to avoid duplication across `keys refresh` and `keys revoke`.

### Authentication Pre-validation

All commands requiring authentication delegate client construction to CLI
Core's `NewAuthenticatedClient` function. This function:
1. Reads `endpoint_url` and `api_key` from config file, environment
   variables, and/or persistent flags (in precedence order defined by
   CLI Core, spec 13).
2. If `api_key` is absent or empty, returns an error immediately — no SDK
   call is made. The command exits with code 2:
   `{"error":{"code":2,"message":"no API key configured — run 'akc login' first"}}`.

`keys refresh` and `keys revoke` perform an additional pre-validation step
after `NewAuthenticatedClient` succeeds: they parse `key_id` from the
configured `api_key` string before calling the SDK. An invalid key format
exits with code 2: `{"error":{"code":2,"message":"invalid API key format"}}`.

### Config Mutations

Three commands mutate the config file via CLI Core's atomic write function.
The config file format and path are defined by CLI Core (spec 13); this spec
uses the save/load functions from CLI Core without referencing the path directly.

| Command | Fields changed | Values |
|---------|---------------|--------|
| `login` | `endpoint_url`, `user_id`, `api_key` | Set from login response |
| `keys refresh` | `api_key` | Set to new full key from refresh response |
| `keys revoke` | `api_key`, `user_id` | Cleared (set to empty string) |

All mutations use the atomic write function from CLI Core (write to temp file,
`os.Rename` into place). Only the changed fields are updated; other fields
are preserved.

**Config write failure in `login`:** If saving the config after a successful
OAuth code exchange fails, the command exits with code 2 and error:
`{"error":{"code":2,"message":"failed to save config: <detail>"}}`. The
obtained credentials are **not** printed to stdout. The user must re-run
`akc login` to complete authentication.

### Expires Flag Validation

The `--expires` flag appears on `login` and `tokens create`. Valid values are
`0`, `30`, `60`, and `90` (days). Any other integer value is rejected with
exit code 2. The validation logic is a shared helper function defined once
in `internal/cli/helpers.go` (e.g., `validateExpires(v int) error`) to
ensure consistency across commands. The constant `loginTimeoutSeconds = 120`
is defined once in `login.go` and referenced throughout the login flow.

### Permissions Validation

The `--permissions` flag for `tokens create` is parsed by splitting on `,`
and trimming whitespace from each entry. Client-side validation is limited to
checking that the resulting slice is non-empty. Individual entry format
(e.g., `resource_type:action`) is **not** validated client-side; all such
validation is delegated to the server. An empty or whitespace-only
`--permissions` value exits with code 2:
`{"error":{"code":2,"message":"--permissions must not be empty"}}`.

The permissions parsing and empty-check logic is implemented as a shared
helper function in `internal/cli/helpers.go`
(e.g., `parsePermissions(s string) ([]string, error)`) so it can be tested
independently.

### Browser Open

The login command opens the authorization URL in the user's default browser.
Platform detection uses `runtime.GOOS`:
- `"darwin"` → `exec.Command("open", url)`
- `"linux"` → `exec.Command("xdg-open", url)`

If the command fails (e.g., no display server, `xdg-open` not installed), the
URL is printed to stderr as a fallback:
`"Open this URL in your browser: <url>"`.

The browser open logic is extracted into a separate helper function
(e.g., `openBrowser(url string) error`) in `internal/cli/helpers.go` for
testability. Tests replace it with a no-op or capture the URL.

### Callback Server

The local callback server for OAuth login:
- Binds to `127.0.0.1:0` (localhost, random port).
- Handles only `GET /callback`. All other paths receive an HTTP 404 response.
  The server does **not** shut down on a 404 response — it continues listening
  for the real `/callback` request. This prevents browsers' secondary requests
  (e.g., `/favicon.ico`) from terminating the server prematurely.
- Validates the `state` parameter before accepting the code.
- Communicates results to the main goroutine via `codeCh chan string` and
  `errCh chan error`. The main goroutine selects on these channels and
  `ctx.Done()`.
- On state mismatch: sends error on `errCh`, responds with HTTP 400 and the
  verbatim error HTML page:
  `<html><body><h1>Login failed</h1><p>OAuth state mismatch. Please try again.</p></body></html>`.
  The main goroutine cancels the context, causing the server to shut down via
  context cancellation. No further requests are processed.
- On valid callback: sends code on `codeCh`, responds with the verbatim
  success HTML page:
  `<html><body><h1>Login successful</h1><p>You may close this tab.</p></body></html>`.
  The main goroutine proceeds to code exchange, then shuts down the server
  gracefully via `http.Server.Shutdown` using a fresh 5-second background
  context.
- Uses `http.Server` with a context-based shutdown.

The callback server is an implementation detail of the login command. It is
not exposed as a public API.

### Login Timeout Testability

The login flow is implemented in an internal helper function:

```go
func runLogin(ctx context.Context, timeout time.Duration, opts loginOpts) error
```

The Cobra `RunE` function passes `time.Duration(loginTimeoutSeconds) * time.Second`
as the timeout. Integration tests call `runLogin` directly with a reduced
timeout (e.g., `200ms`) to exercise timeout behavior without waiting 120
seconds. The package-level constant `loginTimeoutSeconds = 120` is never
modified; it exists solely to document the production value.

---

## Interfaces

### Public API Surface

This spec adds no public API to the `apikit` package. All code lives in
`internal/cli/` and is not importable by consuming projects.

However, the Cobra commands are designed to be added to a parent command via
`AddCommand`, making them composable for consuming projects that embed the
`akc` command tree. The constructor functions (`NewLoginCmd`, `NewUserCmd`,
etc.) are called in `cmd/akc/main.go` and registered on the root command from
CLI Core. Consuming projects that embed the command tree call the same
constructors and `AddCommand` in their own `main.go`.

### Command Signatures (Cobra)

Each command is implemented as a function that returns a `*cobra.Command`.
The naming convention is `New<Group><Action>Cmd()`:

```go
// login.go
func NewLoginCmd() *cobra.Command

// user.go
func NewUserCmd() *cobra.Command        // parent "user" command
func NewUserShowCmd() *cobra.Command
func NewUserUpdateCmd() *cobra.Command

// keys.go
func NewKeysCmd() *cobra.Command        // parent "keys" command
func NewKeysListCmd() *cobra.Command
func NewKeysRefreshCmd() *cobra.Command
func NewKeysRevokeCmd() *cobra.Command

// tokens.go
func NewTokensCmd() *cobra.Command      // parent "tokens" command
func NewTokensListCmd() *cobra.Command
func NewTokensCreateCmd() *cobra.Command
func NewTokensShowCmd() *cobra.Command
func NewTokensRevokeCmd() *cobra.Command

// orgs.go
func NewOrgsCmd() *cobra.Command        // parent "orgs" command
func NewOrgsListCmd() *cobra.Command
func NewOrgsShowCmd() *cobra.Command
func NewOrgsMembersCmd() *cobra.Command
```

Each function constructs a `*cobra.Command` with:
- `Use`, `Short`, `Long` fields.
- `Args` validation (e.g., `cobra.ExactArgs(1)` for commands with positional args).
- `RunE` function that implements the command logic.
- Flag definitions with defaults and required markers.
- Annotations for `help --json` metadata.

Registration in `cmd/akc/main.go`:

```go
func main() {
    root := clicore.NewRootCmd()
    root.AddCommand(
        cli.NewLoginCmd(),
        cli.NewUserCmd(),
        cli.NewKeysCmd(),
        cli.NewTokensCmd(),
        cli.NewOrgsCmd(),
    )
    if err := root.Execute(); err != nil {
        os.Exit(1)
    }
}
```

---

## Error Handling

| Condition | Exit Code | Error Output |
|-----------|-----------|--------------|
| API error from server (4xx/5xx) | 1 | Server's error envelope JSON to stdout |
| Missing required flag | 2 | `{"error":{"code":2,"message":"required flag --<name> not set"}}` to stdout |
| Missing positional argument | 2 | `{"error":{"code":2,"message":"<arg> is required"}}` to stdout |
| Invalid `--expires` value | 2 | `{"error":{"code":2,"message":"--expires must be 0, 30, 60, or 90"}}` to stdout |
| Empty `--permissions` value | 2 | `{"error":{"code":2,"message":"--permissions must not be empty"}}` to stdout |
| No endpoint URL for login | 2 | `{"error":{"code":2,"message":"endpoint URL is required for login — use --endpoint-url or set ENDPOINT_URL"}}` to stdout |
| No API key configured | 2 | `{"error":{"code":2,"message":"no API key configured — run 'akc login' first"}}` to stdout (pre-validated by `NewAuthenticatedClient` before any SDK call) |
| Invalid API key format | 2 | `{"error":{"code":2,"message":"invalid API key format"}}` to stdout (pre-validated before `keys refresh`/`keys revoke` SDK call) |
| Provider not found | 1 | `{"error":{"code":404,"message":"provider '<name>' not found"}}` to stdout |
| Login timeout | 2 | `{"error":{"code":2,"message":"login timed out waiting for browser callback"}}` to stdout |
| OAuth state mismatch | 2 | `{"error":{"code":2,"message":"OAuth state mismatch — possible CSRF attack"}}` to stdout |
| Browser open failure | — | URL printed to stderr as fallback (not a fatal error) |
| Network error | 2 | `{"error":{"code":2,"message":"<network error description>"}}` to stdout |
| Config write failure | 2 | `{"error":{"code":2,"message":"failed to save config: <detail>"}}` to stdout; credentials are NOT printed (user must re-run login) |
| Server-side validation failure (e.g., `user update` bad name) | 1 | Server's error envelope JSON to stdout (surfaced as API error, exit code 1) |

All error handling delegates to CLI Core's error formatting and exit code
functions. Commands return errors from their `RunE` function; CLI Core's
error handler formats them as JSON and sets the exit code.

---

## Testing Strategy

### Unit Tests

- **Login command flag parsing:** Verify `--provider` defaults to `"github"`,
  `--expires` defaults to `90`.
- **Expires validation:** Verify `0`, `30`, `60`, `90` are accepted; other
  values are rejected with exit code 2.
- **Key ID parsing:** Verify correct extraction from various key formats:
  - `ak_abc12345_secret` → `abc12345`
  - `myapp_abc12345_secret` → `abc12345`
  - `my_app_abc12345_secret` → `abc12345`
  - Keys with fewer than 3 segments → error.
- **Permission parsing:** Verify comma-separated permissions are split
  correctly:
  - `"users:read,orgs:read"` → `["users:read", "orgs:read"]`
  - `"users:read, orgs:read"` → `["users:read", "orgs:read"]` (whitespace trimmed)
  - `"users:read"` → `["users:read"]` (single permission)
  - `""` (empty string) → error (exit code 2, `--permissions must not be empty`)
  - `"  , "` (whitespace/commas only) → error (exit code 2, same message)
- **State parameter generation:** Verify the state parameter is 64 hex
  characters (32 bytes hex-encoded).
- **Authorization URL construction:** Verify query parameters are appended
  correctly to the provider's authorize URL, that existing parameters
  (including `client_id`) are preserved, and that `client_id` is not
  duplicated.

### Integration Tests

Integration tests use an `httptest.Server` to simulate the API server,
following the same mock server pattern used in the Go SDK tests (spec 12).
Each test spins up an `httptest.NewServer` with a handler that matches
expected requests and returns pre-defined responses. The CLI command is
invoked by calling the `runLogin` internal helper directly (or the Cobra
`cmd.Execute()`) with captured stdout/stderr buffers.

- **Login (happy path):** Mock server returns providers and auth callback
  response. Simulate callback by sending a request to the local callback
  server with correct state and code. Verify config is saved with correct
  values. Verify stdout contains user JSON. Verify stderr contains success
  message.
- **Login (provider not found):** Mock server returns providers list that
  does not include the requested provider. Verify exit code 1 and error JSON.
- **Login (invalid expires):** Verify exit code 2 and error JSON without
  making any API calls.
- **Login (no endpoint URL):** Verify exit code 2 and descriptive error.
- **Login (state mismatch):** Send callback with wrong state. Verify errCh
  receives error, exit code 2, CSRF error message, and that the HTTP 400
  response body contains the verbatim state-mismatch HTML page.
- **Login (timeout):** Call `runLogin` directly with a short timeout (e.g.,
  `200ms`) and a mock server that never sends a callback. Verify timeout
  error and exit code 2.
- **Login (config write failure):** Simulate a config save failure after
  successful code exchange. Verify exit code 2, error envelope in stdout,
  and that credentials are NOT printed to stdout.
- **Login (non-/callback path):** Send a request to `/favicon.ico` to the
  callback server. Verify HTTP 404 response and that the server remains up
  (neither channel receives a value).
- **User show:** Mock server returns user JSON. Verify stdout matches.
- **User update:** Mock server accepts PATCH and returns updated user.
  Verify `--full-name` value in request body. Verify server-side 422 error
  surfaces as exit code 1 with server error envelope.
- **Keys list:** Mock server returns key metadata array. Verify stdout.
- **Keys refresh:** Mock server returns new key. Verify config updated with
  new `api_key`. Verify stdout contains `APIKeyFull` JSON.
- **Keys revoke:** Mock server returns revoke response. Verify config
  `api_key` and `user_id` are cleared. Verify stdout contains response.
- **Tokens list:** Mock server returns PAT array. Verify stdout.
- **Tokens create:** Mock server returns `PATFull`. Verify request body
  contains name, permissions array, and expires. Verify stdout contains
  full token. Verify empty `--permissions` exits with code 2 without
  calling the server.
- **Tokens show:** Mock server returns PAT metadata. Verify positional arg
  is used in URL path.
- **Tokens revoke:** Mock server returns 204. Verify stdout is `{}`.
  Verify stderr contains revocation message.
- **Orgs list:** Mock server returns org array. Verify stdout.
- **Orgs show:** Mock server returns org. Verify positional arg in URL.
- **Orgs members:** Mock server returns user array. Verify positional arg
  in URL.
- **API error propagation:** Mock server returns 401. Verify exit code 1 and
  error envelope in stdout.
- **Missing API key:** Run authenticated command without configuring API key.
  Verify exit code 2 and that no HTTP request is made to the mock server
  (confirming pre-validation fires before SDK call).

### Callback Server Tests

- **State validation:** Send a callback with wrong state. Verify HTTP 400
  response, verbatim error HTML in body, error sent on `errCh`, and context
  cancelled.
- **Successful callback:** Send a callback with correct state and code.
  Verify HTTP 200 verbatim success HTML response and code received on
  `codeCh`.
- **Timeout:** Call `runLogin` with a short timeout (e.g., `200ms`). Verify
  `ctx.Done()` is selected and timeout error is returned.
- **Non-/callback path handling:** Send a request to `/favicon.ico`. Verify
  HTTP 404 response and that neither `codeCh` nor `errCh` receives a value
  (server continues listening).

---

## Design Decisions

- **Login is the only composite command.** All other commands are 1:1 SDK
  method wrappers. This separation keeps the individual commands trivially
  simple and testable. The login command's complexity (OAuth flow, callback
  server, browser interaction) is isolated in `login.go`.
- **Key ID is parsed from the full key string, not stored separately.** The
  config file stores only three fields (`endpoint_url`, `user_id`, `api_key`).
  Parsing `key_id` from the key string avoids adding a fourth config field and
  keeps the config minimal. The parsing logic is straightforward and handles
  all valid key formats.
- **`tokens revoke` outputs `{}` instead of no output.** Even though the SDK
  returns only `error` (HTTP 204 has no body), the CLI always writes JSON to
  stdout. An empty object `{}` maintains the JSON output contract without
  inventing response fields.
- **Browser open failure is not fatal.** If the browser cannot be opened, the
  URL is printed to stderr. This supports headless environments and SSH
  sessions where the user can copy the URL to a local browser.
- **120-second login timeout.** Long enough for a user to complete the
  browser-based OAuth flow (which may involve 2FA), short enough to avoid
  hanging the CLI indefinitely. The timeout is defined as the constant
  `loginTimeoutSeconds = 120` in `login.go` and is not user-configurable.
- **Login timeout is testable via `runLogin` helper.** The login flow is
  extracted into `runLogin(ctx, timeout, ...)`. Tests pass a short timeout
  directly to `runLogin` rather than modifying the package-level constant,
  keeping production and test code cleanly separated.
- **Callback server returns HTTP 404 for non-`/callback` paths.** Browsers
  frequently issue secondary requests (e.g., `/favicon.ico`) after the initial
  OAuth redirect. The server must not shut down on these requests — only a
  successful `/callback` with valid state triggers shutdown. This prevents a
  subtle race condition where the server terminates before the actual callback
  arrives.
- **Callback server communicates via channels.** The handler sends results on
  `codeCh` (success) or `errCh` (failure). The main goroutine selects on
  these channels plus `ctx.Done()`. This avoids shared mutable state between
  goroutines and is idiomatic Go concurrency.
- **State mismatch triggers context cancellation.** On receiving from `errCh`,
  the main goroutine cancels the context. This causes the server to shut down
  via context cancellation, ensuring no further requests are processed after a
  CSRF attempt.
- **Verbatim HTML pages for callback server responses.** The success and error
  HTML pages are specified verbatim in the PRD to prevent implementation
  divergence. No HTML templating library is used; the strings are inline
  constants.
- **Authorization URL preserves existing query parameters.** The server
  includes `client_id` (and possibly other parameters) in `authorize_url`.
  The CLI uses `url.Parse` + `url.Values` to add only `redirect_uri`,
  `state`, and `response_type=code`, leaving existing parameters intact.
- **Command registration in `main.go`, not `init()`.** Using explicit
  `AddCommand` calls in `cmd/akc/main.go` avoids import-order side effects,
  makes the command tree visible at a glance, and allows consuming projects
  to embed specific command groups selectively.
- **`NewAuthenticatedClient` pre-validates `api_key` presence.** This
  ensures the "no API key" exit code 2 fires before any SDK call, keeping
  it distinct from the server's 401 response (exit code 1). Implementations
  must not conflate these two error cases.
- **Config file path and format are owned by CLI Core.** This spec references
  CLI Core's save/load functions without repeating path or format details.
  The config file path is defined in spec 13 and must be consulted there.
- **Windows behavior is undefined.** The master PRD targets Unix-like systems.
  No build tag exclusion is applied; Windows users are unsupported and will
  encounter undefined behavior (e.g., browser-open commands will fail). This
  is an acceptable trade-off for the scope of this project.
- **Callback server listens on 127.0.0.1 only.** Security measure — the
  callback server should not be reachable from the network. This matches the
  OAuth redirect to `http://127.0.0.1:<port>/callback`.
- **No ETag usage in CLI commands.** The CLI always fetches fresh data. ETags
  are primarily useful for polling and caching, which are not relevant for
  interactive CLI usage. The SDK methods that return `Response[T]` are called
  without `WithIfNoneMatch`.
- **Shared helper functions in `helpers.go`.** Expires validation, permissions
  parsing, key ID parsing, and browser open are all implemented as shared
  helper functions in `internal/cli/helpers.go`. This avoids duplication
  across command files and centralizes unit-testable logic.
- **Empty `--permissions` is a client-side error (exit code 2).** An empty
  permissions string is unambiguously a caller mistake (Cobra marks the flag
  required but cannot enforce non-empty content). Rejecting it client-side
  gives faster feedback without a round trip. Individual entry format
  validation is still delegated to the server.
- **Individual `--permissions` entries are not validated client-side.** The
  CLI splits and trims but does not enforce the `resource_type:action` format
  on individual entries. This avoids duplicating server-side validation logic
  and allows the server to evolve its permission model without CLI changes.
- **`--full-name` is not validated client-side.** Only Cobra's `required`
  flag enforcement applies. All content validation (empty string, length,
  disallowed characters) is delegated to the server, which returns 4xx errors
  that surface as exit code 1.
- **Shutdown uses a fresh context with a 5-second deadline.** Using the
  original login context for `http.Server.Shutdown` would risk immediate
  return if the context is already cancelled (e.g., after timeout or state
  mismatch). A fresh `context.Background()` with a 5-second timeout ensures
  the server completes graceful teardown regardless of the login context state.
- **Config write failure in login discards credentials.** If the config write
  fails after a successful code exchange, the credentials are not printed.
  This preserves the invariant that stdout only contains data on success and
  error envelopes on failure. The user re-runs `akc login` to obtain new
  credentials; the previous session's API key may remain active on the server
  until it expires, but the CLI has no way to use it.
- **Commands return errors from RunE; CLI Core handles formatting.** This
  follows Cobra's idiomatic error handling pattern and centralizes error
  formatting in one place (CLI Core). Commands don't need to know about
  JSON error envelopes — they return Go errors and let the infrastructure
  format them.
- **Server-side `user update` validation failures surface as exit code 1.**
  The CLI makes no attempt to client-side validate the `--full-name` value.
  Server-returned 4xx responses (e.g., 422 Unprocessable Entity for an empty
  name) are treated as API errors and exit with code 1 carrying the server's
  error envelope.

---

## Glossary

| Term | Definition |
|------|------------|
| **composite command** | A CLI command that involves multiple API calls (e.g., `login` which fetches providers then exchanges a code). Distinguished from 1:1 commands in `help --json` by the `composite: true` flag. |
| **callback server** | A temporary local HTTP server started by the `login` command to receive the OAuth redirect from the browser. Binds to `127.0.0.1` on a random port. Returns HTTP 404 for all paths except `/callback`. |
| **state parameter** | A cryptographically random string (64 hex chars) included in the OAuth authorization request and validated on callback to prevent CSRF attacks. |
| **key_id** | The identifier segment of an API key. Extracted from the full key string by parsing: `<prefix>_<key_id>_<secret>` → `key_id` is the penultimate segment when split on `_`. Parsing implemented in `helpers.go`. |
| **config-mutating command** | A CLI command that writes to the config file (path defined by CLI Core, spec 13). In this spec: `login`, `keys refresh`, `keys revoke`. All use atomic writes via CLI Core's save function. |
| **atomic write** | Write to a temporary file then `os.Rename` into place. Prevents partial writes from corrupting the config file. Implemented by CLI Core (spec 13). |
| **1:1 command** | A CLI command that maps directly to a single SDK method and API endpoint. All commands in this spec except `login` are 1:1 commands. |
| **codeCh / errCh** | The two channels used by the OAuth callback server handler to communicate results back to the main login goroutine. `codeCh chan string` carries the authorization code on success; `errCh chan error` carries an error on state mismatch or callback failure. |
| **NewAuthenticatedClient** | CLI Core function that constructs an authenticated SDK client. Reads `endpoint_url` and `api_key` from config/env/flags and pre-validates that `api_key` is present before returning. Returns exit code 2 if `api_key` is absent. |
| **runLogin** | Internal helper function implementing the login OAuth flow. Accepts a `timeout time.Duration` parameter so tests can reduce the timeout without modifying the `loginTimeoutSeconds` constant. |
| **helpers.go** | File in `internal/cli/` containing shared helper functions: `validateExpires`, `parsePermissions`, `parseKeyID`, `openBrowser`. |

---

## Owner

Michael Kuehl
