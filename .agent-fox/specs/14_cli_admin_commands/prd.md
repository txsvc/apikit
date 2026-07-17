---
spec_id: '14'
spec_name: cli_admin_commands
title: Cli Admin Commands
status: draft
created_at: '2026-07-17T13:06:40.526139+00:00'
updated_at: '2026-07-17T13:11:35.670922+00:00'
owner: ''
source: interactive
schema_version: 1
---
# CLI Admin Commands

## Source Reference

This spec is derived from the [apikit master PRD](docs/PRD.md). It covers the
**CLI Admin Commands** component — spec 15 of 15. The master PRD sections on
"CLI" (commands, agent interface, config, error handling) and the Go SDK spec
(spec 12, admin endpoint methods) are the primary sources. All CLI admin
commands delegate exclusively to Go SDK `Client` methods; no direct HTTP calls
are made.

## Background

The `akc admin` command group exists to support operator and automation use
cases that require administrative access to the apikit deployment. Typical
actors include:

- **Human operators** bootstrapping a new deployment, managing user accounts,
  or auditing organization membership via a terminal.
- **Automated agents and CI scripts** that drive user/org lifecycle as part of
  infrastructure-as-code pipelines. Because agents parse stdout as JSON, all
  output is machine-readable JSON — no interactive prompts, no colored
  formatting, no pagination.
- **Break-glass scenarios** where the operator uses the admin token (generated
  by `admin_bootstrap`, spec 05) to recover access or triage incidents.

All commands in this spec require admin-level credentials. The deliberate
design choice to emit all output (including errors) as JSON to stdout — rather
than splitting human messages to stderr — is intentional and documented in
Non-Goals and Design Decisions. This diverges from conventional CLI behavior
but is the correct tradeoff for agent/automation consumption.

## Intent

Implement all `akc admin` subcommands — the Cobra command tree that wraps the
Go SDK's admin-only client methods for user administration, organization
administration, and credential (key/token) management. Each command constructs
an SDK `Client` from the CLI's persistent configuration (endpoint URL, API key),
calls the corresponding SDK method, and prints the result as JSON to stdout.
Human-readable messages (progress, warnings) go to stderr only.

These commands require admin-level access: the API key used must belong to a
user with the admin role, or be the break-glass admin token. The agent interface
(`akc help --json`) reports `"auth": "admin"` for every command in this spec.

## Goals

- Implement the `akc admin users` command group with subcommands: `list`,
  `show`, `create`, `update`, `promote`, `demote`, `block`, `unblock`.
- Implement the `akc admin orgs` command group with subcommands: `list`,
  `create`, `update`, `delete`, `block`, `unblock`.
- Implement the `akc admin orgs members` command group with subcommands:
  `list`, `add`, `remove`.
- Implement the `akc admin keys` command group with subcommands: `list`,
  `revoke`.
- Implement the `akc admin tokens` command group with subcommands: `list`,
  `revoke`.
- All commands print JSON to stdout and messages to stderr.
- All commands return exit code 0 on success, 1 on API error, 2 on client
  error.
- All commands delegate to the Go SDK (spec 12) — no direct HTTP calls.
- All commands use the CLI infrastructure from the CLI core spec (spec 13)
  for configuration loading, SDK client construction, error formatting, and
  JSON output.
- All commands register in the agent interface (`akc help --json`) with
  `"auth": "admin"`, the correct HTTP method and API path, positional
  arguments, and flags with types and defaults.

## Non-Goals

- **User self-service commands.** Commands under `akc user`, `akc keys`,
  `akc tokens`, and `akc orgs` (non-admin) are in the CLI user commands spec
  (spec 14), not here.
- **OAuth login flow.** The `akc login` command is in the CLI core spec
  (spec 13).
- **Direct HTTP calls.** The CLI never makes HTTP requests directly; it calls
  SDK methods.
- **Interactive prompts or confirmation dialogs.** All commands are
  non-interactive. Destructive operations (delete, revoke, block) execute
  immediately without confirmation.
- **Colored or formatted terminal output.** All stdout is JSON. Stderr
  messages are plain text. Note: this deliberately diverges from conventional
  CLI behavior (where validation errors often go to stderr) in favour of
  consistent, agent-parseable JSON on stdout. Future maintainers should treat
  this as an intentional design constraint, not an oversight.
- **Pagination.** Not implemented in the first iteration per the master PRD.
- **Filtering beyond `--include-blocked`.** No search, sort, or field-based
  filtering.
- **Config file mutations.** Admin commands do not modify the CLI config file.
  Only `login`, `keys refresh`, and `keys revoke` mutate config (spec 13/14).
- **UUID format validation.** The CLI does not validate that positional ID
  arguments conform to UUID format. Any non-empty string is passed through to
  the SDK and server. Malformed IDs result in a 404 or 400 API error (exit
  code 1), not a client validation error (exit code 2).
- **Integration or end-to-end tests.** Integration testing against a live or
  stubbed HTTP server is out of scope for this component. Unit tests with mocks
  are sufficient.

## Dependencies

| Spec | From Group | To Group | Relationship |
|------|-----------|----------|--------------|
| `13_cli_core` | 1 | 1 | Uses CLI infrastructure: persistent config loading (`loadConfig`), SDK client construction (`newClient`), error handling (`handleError`), JSON output (`printJSON`), stderr warnings (`warnf`), Cobra root command registration (including `NewAdminCmd` wiring), agent interface metadata |
| `12_go_sdk` | 1 | 1 | All commands delegate to Go SDK admin client methods: `ListUsers`, `GetUserByID`, `CreateUser`, `UpdateUserByID`, `PromoteUser`, `DemoteUser`, `BlockUser`, `UnblockUser`, `ListOrgs`, `CreateOrg`, `UpdateOrg`, `DeleteOrg`, `BlockOrg`, `UnblockOrg`, `ListOrgMembers`, `AddOrgMember`, `RemoveOrgMember`, `ListUserKeys`, `RevokeUserKey`, `ListUserTokens`, `RevokeUserToken`. Output JSON shapes for all resource types are defined by spec 12's Go types — see the Output Types cross-reference table below. |

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25+ |
| CLI framework | Cobra (`github.com/spf13/cobra`) |
| SDK | `apikit` root package (Go SDK from spec 12) |
| JSON output | `encoding/json` (stdlib) |
| Testing | Go stdlib `testing` package |

## Repository Layout

```
cmd/
  akc/                        CLI binary entry point
    main.go                   Main entry point (imports CLI package)
internal/
  cli/                        CLI implementation package
    admin.go                  Admin root command
    admin_users.go            Admin user subcommands
    admin_orgs.go             Admin org subcommands (including members)
    admin_keys.go             Admin key subcommands
    admin_tokens.go           Admin token subcommands
    admin_users_test.go       Tests for admin user commands
    admin_orgs_test.go        Tests for admin org commands
    admin_keys_test.go        Tests for admin key commands
    admin_tokens_test.go      Tests for admin token commands
```

All CLI implementation code lives in `internal/cli/`. The `cmd/akc/main.go`
entry point imports and executes the root command. `NewAdminCmd()` is wired
into the Cobra root command by spec 13 (CLI core), which is the authoritative
source for all subcommand registrations on the root. See spec 13 for the
registration snippet.

---

## Output Types Cross-Reference

The JSON shape of each resource printed to stdout is determined by the
corresponding Go type in spec 12 (Go SDK). This spec does not duplicate those
definitions; implementors must consult spec 12 for the authoritative field
list, types, and JSON tag names.

| stdout output | Spec 12 Go type | Notes |
|--------------|-----------------|-------|
| User object | `apikit.User` | Returned by `GetUserByID`, `CreateUser`, `UpdateUserByID`, `PromoteUser`, `DemoteUser`, `BlockUser`, `UnblockUser`. The `Response[User].Data` wrapper is unwrapped before printing. |
| User array | `[]*apikit.User` | Returned by `ListUsers`, `ListOrgMembers`. |
| Org object | `apikit.Organization` | Returned by `CreateOrg`, `UpdateOrg`, `BlockOrg`, `UnblockOrg`. |
| Org array | `[]*apikit.Organization` | Returned by `ListOrgs`. |
| API key metadata object | `apikit.APIKeyMeta` | Returned inside key arrays by `ListUserKeys`. Secret key material is never included. |
| API key metadata array | `[]*apikit.APIKeyMeta` | Returned by `ListUserKeys`. |
| PAT metadata object | `apikit.PAT` | Returned inside token arrays by `ListUserTokens`. Secret token material is never included. |
| PAT metadata array | `[]*apikit.PAT` | Returned by `ListUserTokens`. |
| Empty success | `{}` (literal) | Printed for void-response commands: `DeleteOrg`, `AddOrgMember`, `RemoveOrgMember`, `RevokeUserKey`, `RevokeUserToken`. |

---

## Functional Requirements

### Command Tree Structure

The admin commands form a nested Cobra command tree under the `admin` parent:

```
akc admin
  akc admin users
    akc admin users list [--include-blocked]
    akc admin users show <id>
    akc admin users create --username "..." --email "..." --provider "..." --provider-id "..."
    akc admin users update <id> --full-name "..."
    akc admin users promote <id>
    akc admin users demote <id>
    akc admin users block <id>
    akc admin users unblock <id>
  akc admin orgs
    akc admin orgs list [--include-blocked]
    akc admin orgs create --name "..." --slug "..." [--url "..."]
    akc admin orgs update <id> [--name "..."] [--url "..."]
    akc admin orgs delete <id>
    akc admin orgs block <id>
    akc admin orgs unblock <id>
    akc admin orgs members
      akc admin orgs members list <id>
      akc admin orgs members add <org_id> <user_id>
      akc admin orgs members remove <org_id> <user_id>
  akc admin keys
    akc admin keys list <user_id>
    akc admin keys revoke <user_id> <key_id>
  akc admin tokens
    akc admin tokens list <user_id>
    akc admin tokens revoke <user_id> <token_id>
```

The `admin` command itself has no `Run` function — it only serves as a parent
for subcommands. Same for `admin users`, `admin orgs`, `admin orgs members`,
`admin keys`, and `admin tokens`.

### Common Command Pattern

Every admin command follows the same execution pattern:

1. Load persistent configuration (endpoint URL, API key) via CLI core
   infrastructure (`loadConfig`).
2. Construct an SDK `Client` via CLI core infrastructure (`newClient`).
3. Parse positional arguments and flags.
4. Validate required arguments and flags; exit with code 2 and a JSON error
   to stdout if validation fails.
5. Call the corresponding Go SDK method with `context.Background()`.
6. On success: print the SDK response as JSON to stdout (via `printJSON`),
   exit 0.
7. On `*apikit.APIError`: print the error as a JSON error envelope to stdout
   (via `handleError`), exit 1.
8. On other errors (network, context): print a JSON error envelope to stdout
   with code 0 and the error message (via `handleError`), exit 2.

### Argument and Flag Validation

All commands validate positional arguments and flags before calling the SDK:

- **Missing positional argument:** Print a JSON error envelope to stdout with
  code 0 and message `"missing required argument: <arg_name>"`. Exit with
  code 2.
- **Missing required flag:** Print a JSON error envelope to stdout with code 0
  and message `"missing required flag: --<flag_name>"`. Exit with code 2.
- **Empty positional argument (ID fields):** Not validated by the CLI. Any
  non-empty string is passed through to the SDK and server. Cobra's
  `ExactArgs(N)` rejects too-few arguments; UUID format is not checked.
- **Empty string for required string flags (e.g., `--name ""` in `admin orgs create`):**
  Passed through to the SDK and server without client-side rejection. The
  server validates and returns an appropriate 4xx error if the value is
  invalid, which the CLI forwards as exit code 1. This is consistent with the
  `--full-name ""` behavior in `admin users update` (which clears the field).
- **Extra positional arguments:** Cobra handles this via `Args:
  cobra.ExactArgs(N)` or `cobra.NoArgs`.

The `<arg_name>` in error messages uses the descriptive name from the command
signature (e.g., `id`, `user_id`, `key_id`, `org_id`, `token_id`).

### Admin User Commands

#### `akc admin users list [--include-blocked]`

Lists all users. Blocked users excluded by default.

| Aspect | Detail |
|--------|--------|
| Positional args | None |
| Flags | `--include-blocked` (bool, default `false`) |
| SDK method | `client.ListUsers(ctx, &apikit.ListUsersOptions{IncludeBlocked: includeBlocked})` |
| Success output | JSON array of user objects (`[]*apikit.User`) to stdout |
| Agent interface | `method: "GET"`, `path: "/users"`, `auth: "admin"` |

When `--include-blocked` is `false` (default), the options struct is passed
with `IncludeBlocked: false` (server default applies).

#### `akc admin users show <id>`

Shows a single user by ID.

| Aspect | Detail |
|--------|--------|
| Positional args | `<id>` (required; any non-empty string passed through to server) |
| Flags | None |
| SDK method | `client.GetUserByID(ctx, id)` |
| Success output | JSON user object (`apikit.User`) to stdout |
| Agent interface | `method: "GET"`, `path: "/users/:id"`, `auth: "admin"` |

The `Response[User]` wrapper's `Data` field is unwrapped for output.

#### `akc admin users create`

Creates a new user.

| Aspect | Detail |
|--------|--------|
| Positional args | None |
| Flags | `--username` (string, required), `--email` (string, required), `--provider` (string, required), `--provider-id` (string, required) |
| SDK method | `client.CreateUser(ctx, &apikit.CreateUserRequest{...})` |
| Success output | JSON user object (`apikit.User`) to stdout (HTTP 201 from server) |
| Agent interface | `method: "POST"`, `path: "/users"`, `auth: "admin"` |

All four flags are required. If any is missing (flag absent entirely), exit
with code 2 and a JSON error envelope with message
`"missing required flag: --<flag_name>"`. If a flag is present but empty
(e.g., `--username ""`), the value is passed through to the SDK; the server
validates and returns a 4xx error if the value is invalid.

#### `akc admin users update <id>`

Updates a user's full name.

| Aspect | Detail |
|--------|--------|
| Positional args | `<id>` (required) |
| Flags | `--full-name` (string, **required**) |
| SDK method | `client.UpdateUserByID(ctx, id, &apikit.UpdateUserRequest{FullName: fullName})` |
| Success output | JSON user object (`apikit.User`) to stdout |
| Agent interface | `method: "PATCH"`, `path: "/users/:id"`, `auth: "admin"` |

The `--full-name` flag is **required**. An empty string is a valid value
(clears the full name field on the user). If the flag is absent entirely, exit
with code 2 and a JSON error envelope.

> **Design note — asymmetry with `admin orgs update`:** This requirement is
> intentionally different from `admin orgs update`, where both `--name` and
> `--url` are optional. The difference arises from the Go SDK type: `UpdateUserRequest`
> uses a plain `string` for `FullName`, so the CLI cannot distinguish "flag not
> provided" from "flag provided as empty string" without a required-flag check.
> `UpdateOrgRequest` uses `*string` fields, allowing `nil` (not provided) vs.
> `""` (explicitly cleared) to be expressed. This asymmetry is confirmed
> intentional and should not be "fixed" by making `--full-name` optional.

#### `akc admin users promote <id>`

Grants the admin role to a user.

| Aspect | Detail |
|--------|--------|
| Positional args | `<id>` (required) |
| Flags | None |
| SDK method | `client.PromoteUser(ctx, id)` |
| Success output | JSON user object (`apikit.User`) to stdout |
| Agent interface | `method: "POST"`, `path: "/users/:id/promote"`, `auth: "admin"` |

#### `akc admin users demote <id>`

Revokes the admin role from a user. The server enforces the last-admin
safeguard; the CLI does not duplicate this check.

| Aspect | Detail |
|--------|--------|
| Positional args | `<id>` (required) |
| Flags | None |
| SDK method | `client.DemoteUser(ctx, id)` |
| Success output | JSON user object (`apikit.User`) to stdout |
| Agent interface | `method: "POST"`, `path: "/users/:id/demote"`, `auth: "admin"` |

#### `akc admin users block <id>`

Blocks a user.

| Aspect | Detail |
|--------|--------|
| Positional args | `<id>` (required) |
| Flags | None |
| SDK method | `client.BlockUser(ctx, id)` |
| Success output | JSON user object (`apikit.User`) to stdout |
| Agent interface | `method: "POST"`, `path: "/users/:id/block"`, `auth: "admin"` |

#### `akc admin users unblock <id>`

Unblocks a user.

| Aspect | Detail |
|--------|--------|
| Positional args | `<id>` (required) |
| Flags | None |
| SDK method | `client.UnblockUser(ctx, id)` |
| Success output | JSON user object (`apikit.User`) to stdout |
| Agent interface | `method: "POST"`, `path: "/users/:id/unblock"`, `auth: "admin"` |

### Admin Organization Commands

#### `akc admin orgs list [--include-blocked]`

Lists all organizations. Blocked organizations excluded by default.

| Aspect | Detail |
|--------|--------|
| Positional args | None |
| Flags | `--include-blocked` (bool, default `false`) |
| SDK method | `client.ListOrgs(ctx, &apikit.ListOrgsOptions{IncludeBlocked: includeBlocked})` |
| Success output | JSON array of organization objects (`[]*apikit.Organization`) to stdout |
| Agent interface | `method: "GET"`, `path: "/orgs"`, `auth: "admin"` |

#### `akc admin orgs create`

Creates an organization.

| Aspect | Detail |
|--------|--------|
| Positional args | None |
| Flags | `--name` (string, required), `--slug` (string, required), `--url` (string, optional) |
| SDK method | `client.CreateOrg(ctx, &apikit.CreateOrgRequest{Name: name, Slug: slug, URL: urlPtr})` |
| Success output | JSON organization object (`apikit.Organization`) to stdout (HTTP 201 from server) |
| Agent interface | `method: "POST"`, `path: "/orgs"`, `auth: "admin"` |

`--name` and `--slug` are required (flag must be present; exit code 2 if
absent). If present but empty (e.g., `--name ""`), the value is passed through
to the SDK; the server validates and returns a 4xx error. `--url` is optional;
when not provided, `URL` in the request struct is `nil`. Missing required flags
exit with code 2.

#### `akc admin orgs update <id>`

Updates an organization's name and/or URL.

| Aspect | Detail |
|--------|--------|
| Positional args | `<id>` (required) |
| Flags | `--name` (string, optional), `--url` (string, optional) |
| SDK method | `client.UpdateOrg(ctx, id, &apikit.UpdateOrgRequest{Name: namePtr, URL: urlPtr})` |
| Success output | JSON organization object (`apikit.Organization`) to stdout |
| Agent interface | `method: "PATCH"`, `path: "/orgs/:id"`, `auth: "admin"` |

Both flags are individually optional. When a flag is provided, its value is
sent as a non-nil pointer in `UpdateOrgRequest`; when omitted, the
corresponding field is `nil` (the server leaves it unchanged).

**Empty-patch behaviour:** If neither `--name` nor `--url` is provided, the
CLI emits a warning to stderr via the shared `warnf` helper from CLI core
(spec 13) — the exact format and newline convention are defined there — and
then proceeds to call the SDK with an empty `UpdateOrgRequest`. The server
determines the outcome — typically it returns the unchanged organization object
(HTTP 200). The CLI prints whatever JSON the SDK returns to stdout and exits 0.
This is not treated as a client-side validation error (exit code 2); it is the
caller's responsibility to supply at least one field when a meaningful update
is intended.

> **Test coverage:** `TestAdminOrgsUpdateNoFlags` should verify that the
> warning appears on stderr, the SDK is still called, and the command exits 0
> on a successful server response. The exact stderr content should match the
> output of `warnf("no fields specified for update")` as defined in spec 13.

#### `akc admin orgs delete <id>`

Deletes an organization. This is destructive and irreversible — no
confirmation prompt.

| Aspect | Detail |
|--------|--------|
| Positional args | `<id>` (required) |
| Flags | None |
| SDK method | `client.DeleteOrg(ctx, id)` |
| Success output | Empty JSON object `{}` to stdout |
| Agent interface | `method: "DELETE"`, `path: "/orgs/:id"`, `auth: "admin"` |

The SDK method returns only `error` (HTTP 204 from server). On success, the
CLI prints `{}` to stdout to maintain the invariant that every successful
command produces valid JSON.

#### `akc admin orgs block <id>`

Blocks an organization.

| Aspect | Detail |
|--------|--------|
| Positional args | `<id>` (required) |
| Flags | None |
| SDK method | `client.BlockOrg(ctx, id)` |
| Success output | JSON organization object (`apikit.Organization`) to stdout |
| Agent interface | `method: "POST"`, `path: "/orgs/:id/block"`, `auth: "admin"` |

#### `akc admin orgs unblock <id>`

Unblocks an organization.

| Aspect | Detail |
|--------|--------|
| Positional args | `<id>` (required) |
| Flags | None |
| SDK method | `client.UnblockOrg(ctx, id)` |
| Success output | JSON organization object (`apikit.Organization`) to stdout |
| Agent interface | `method: "POST"`, `path: "/orgs/:id/unblock"`, `auth: "admin"` |

### Admin Organization Member Commands

#### `akc admin orgs members list <id>`

Lists members of an organization.

| Aspect | Detail |
|--------|--------|
| Positional args | `<id>` (required, org ID) |
| Flags | None |
| SDK method | `client.ListOrgMembers(ctx, orgID)` |
| Success output | JSON array of user objects (`[]*apikit.User`) to stdout |
| Agent interface | `method: "GET"`, `path: "/orgs/:id/members"`, `auth: "admin"` |

#### `akc admin orgs members add <org_id> <user_id>`

Adds a user to an organization.

| Aspect | Detail |
|--------|--------|
| Positional args | `<org_id>` (required), `<user_id>` (required) |
| Flags | None |
| SDK method | `client.AddOrgMember(ctx, orgID, userID)` |
| Success output | Empty JSON object `{}` to stdout |
| Agent interface | `method: "PUT"`, `path: "/orgs/:id/members/:user_id"`, `auth: "admin"` |

The SDK method returns only `error` (HTTP 204). On success, prints `{}`.

#### `akc admin orgs members remove <org_id> <user_id>`

Removes a user from an organization.

| Aspect | Detail |
|--------|--------|
| Positional args | `<org_id>` (required), `<user_id>` (required) |
| Flags | None |
| SDK method | `client.RemoveOrgMember(ctx, orgID, userID)` |
| Success output | Empty JSON object `{}` to stdout |
| Agent interface | `method: "DELETE"`, `path: "/orgs/:id/members/:user_id"`, `auth: "admin"` |

### Admin Key Commands

#### `akc admin keys list <user_id>`

Lists a user's API keys (metadata only).

| Aspect | Detail |
|--------|--------|
| Positional args | `<user_id>` (required) |
| Flags | None |
| SDK method | `client.ListUserKeys(ctx, userID)` |
| Success output | JSON array of API key metadata objects (`[]*apikit.APIKeyMeta`) to stdout |
| Agent interface | `method: "GET"`, `path: "/users/:id/keys"`, `auth: "admin"` |

#### `akc admin keys revoke <user_id> <key_id>`

Revokes a user's API key.

| Aspect | Detail |
|--------|--------|
| Positional args | `<user_id>` (required), `<key_id>` (required) |
| Flags | None |
| SDK method | `client.RevokeUserKey(ctx, userID, keyID)` |
| Success output | Empty JSON object `{}` to stdout |
| Agent interface | `method: "DELETE"`, `path: "/users/:id/keys/:key_id"`, `auth: "admin"` |

The SDK method returns only `error` (HTTP 204). On success, prints `{}`.

### Admin Token Commands

#### `akc admin tokens list <user_id>`

Lists a user's personal access tokens (metadata only).

| Aspect | Detail |
|--------|--------|
| Positional args | `<user_id>` (required) |
| Flags | None |
| SDK method | `client.ListUserTokens(ctx, userID)` |
| Success output | JSON array of PAT metadata objects (`[]*apikit.PAT`) to stdout |
| Agent interface | `method: "GET"`, `path: "/users/:id/tokens"`, `auth: "admin"` |

#### `akc admin tokens revoke <user_id> <token_id>`

Revokes a user's personal access token.

| Aspect | Detail |
|--------|--------|
| Positional args | `<user_id>` (required), `<token_id>` (required) |
| Flags | None |
| SDK method | `client.RevokeUserToken(ctx, userID, tokenID)` |
| Success output | Empty JSON object `{}` to stdout |
| Agent interface | `method: "DELETE"`, `path: "/users/:id/tokens/:token_id"`, `auth: "admin"` |

### Agent Interface Metadata

Every command registers metadata for `akc help --json` via the CLI core
infrastructure. Each command entry includes:

- `name` — the full command name (e.g., `"admin users list"`)
- `description` — one-line description
- `method` — HTTP method the command maps to
- `path` — API path relative to mount point
- `args` — array of positional argument descriptors
- `flags` — array of flag descriptors with name, type, required, default,
  and description
- `auth` — always `"admin"` for commands in this spec

The exact JSON schema and registration mechanism for agent interface metadata
are defined in spec 13 (CLI core). This spec specifies the values each admin
command contributes; spec 13 defines the format.

### Error Output

All error output follows the API error envelope format:

```json
{"error": {"code": <int>, "message": "<string>"}}
```

- **API errors** (`*apikit.APIError`): The `code` and `message` from the SDK
  error are used directly. Exit code 1.
- **Client errors** (missing config, invalid flags, network failure): `code`
  is `0` (not an HTTP status), `message` describes the error. Exit code 2.

### JSON Output Formatting

The `printJSON` helper from CLI core infrastructure serializes SDK response
objects to JSON and writes to stdout. The output uses `json.MarshalIndent`
with two-space indentation for human readability while remaining valid JSON
for machine parsing.

If `printJSON` itself fails (e.g., stdout is closed), the process exits with
code 2. In practice this is not expected to occur because all SDK response
types are always JSON-serializable (they contain only JSON-compatible Go
types); however, the `printJSON` implementation in spec 13 handles the error
path.

### Context Usage

All SDK calls use `context.Background()`. The CLI does not set deadlines or
timeouts on the context. Cancellation happens via OS signals (SIGINT/SIGTERM),
which Cobra handles by terminating the process.

---

## Interfaces

### Command Registration

Each command group provides a function that returns a `*cobra.Command`:

```go
// NewAdminCmd returns the "admin" parent command with all subcommands.
// Called by CLI core (spec 13) to register the admin tree on the root command.
func NewAdminCmd() *cobra.Command

// newAdminUsersCmd returns the "admin users" command group.
func newAdminUsersCmd() *cobra.Command

// newAdminOrgsCmd returns the "admin orgs" command group.
func newAdminOrgsCmd() *cobra.Command

// newAdminOrgsMembersCmd returns the "admin orgs members" command group.
func newAdminOrgsMembersCmd() *cobra.Command

// newAdminKeysCmd returns the "admin keys" command group.
func newAdminKeysCmd() *cobra.Command

// newAdminTokensCmd returns the "admin tokens" command group.
func newAdminTokensCmd() *cobra.Command
```

`NewAdminCmd` is exported and called by the CLI core (spec 13) to register the
admin tree on the root command. Spec 13 is the authoritative source for the
registration call site. All other functions are unexported.

### Test-Scoped Mock Interface

Unit tests inject a test double for the SDK client via an unexported interface
defined in the test files (e.g., `admin_users_test.go`). The interface is
test-scoped only — it is not exported or defined in production code. Each test
file defines the minimal interface needed to mock the SDK methods exercised by
that file's commands. For example:

```go
// In admin_users_test.go (test-scoped, unexported)
type adminUsersClient interface {
    ListUsers(ctx context.Context, opts *apikit.ListUsersOptions) ([]*apikit.User, error)
    GetUserByID(ctx context.Context, id string) (*apikit.User, error)
    CreateUser(ctx context.Context, req *apikit.CreateUserRequest) (*apikit.User, error)
    UpdateUserByID(ctx context.Context, id string, req *apikit.UpdateUserRequest) (*apikit.User, error)
    PromoteUser(ctx context.Context, id string) (*apikit.User, error)
    DemoteUser(ctx context.Context, id string) (*apikit.User, error)
    BlockUser(ctx context.Context, id string) (*apikit.User, error)
    UnblockUser(ctx context.Context, id string) (*apikit.User, error)
}
```

Similar interfaces are defined in `admin_orgs_test.go`, `admin_keys_test.go`,
and `admin_tokens_test.go` covering the methods relevant to each file. The
production command constructors accept the interface type (or a wrapper) to
enable injection; the exact injection mechanism (constructor parameter,
package-level variable, or functional option) is left to the implementor.

### CLI Core Dependencies

The admin commands depend on these CLI core infrastructure functions (spec 13):

```go
// loadConfig loads endpoint_url, user_id, and api_key from config file,
// environment variables, and command-line flags (in precedence order).
// Precedence: command-line flags > environment variables > config file.
// See spec 13 for full precedence rules and env var names.
func loadConfig(cmd *cobra.Command) (*Config, error)

// newClient constructs an apikit.Client from the loaded config.
func newClient(cfg *Config) *apikit.Client

// printJSON serializes v as indented JSON to stdout.
// Returns an error if serialization or writing fails; callers exit with code 2.
func printJSON(v interface{}) error

// handleError formats an error as a JSON error envelope to stdout and
// returns the appropriate exit code (1 for API errors, 2 for client errors).
func handleError(err error) int

// warnf writes a formatted warning message to stderr.
// The exact format (prefix, newline) is defined in spec 13.
func warnf(format string, args ...interface{})
```

---

## Error Handling

| Condition | Exit Code | Error Output |
|-----------|-----------|-------------|
| Missing positional argument | 2 | `{"error": {"code": 0, "message": "missing required argument: <name>"}}` |
| Missing required flag | 2 | `{"error": {"code": 0, "message": "missing required flag: --<name>"}}` |
| Missing endpoint URL or API key in config | 2 | `{"error": {"code": 0, "message": "..."}}` |
| Network failure | 2 | `{"error": {"code": 0, "message": "..."}}` |
| API returns 403 (not admin) | 1 | `{"error": {"code": 403, "message": "forbidden"}}` |
| API returns 404 (not found) | 1 | `{"error": {"code": 404, "message": "user not found"}}` |
| API returns 400 (malformed ID or invalid field) | 1 | `{"error": {"code": 400, "message": "<server message>"}}` |
| API returns 409 (conflict) | 1 | `{"error": {"code": 409, "message": "username already exists"}}` |
| API returns 409 (last admin) | 1 | `{"error": {"code": 409, "message": "cannot demote the last admin"}}` |
| Other API error | 1 | `{"error": {"code": <status>, "message": "<server message>"}}` |

---

## Testing Strategy

### Unit Tests

Tests use a test-scoped mock interface for the SDK client (defined in the test
files, unexported) to verify that each command:
1. Parses positional arguments and flags correctly.
2. Calls the correct SDK method with the correct parameters.
3. Formats the SDK response as JSON to stdout.
4. Handles errors with the correct exit code and error envelope.

Integration tests (against a live or stubbed HTTP server) are **out of scope**
for this component. Unit tests with mocks are sufficient.

#### Admin User Command Tests

- `TestAdminUsersListCommand` — calls `ListUsers` with default options,
  prints JSON array to stdout.
- `TestAdminUsersListIncludeBlocked` — `--include-blocked` flag sets
  `IncludeBlocked: true` in options.
- `TestAdminUsersShowCommand` — calls `GetUserByID` with the positional
  `id` arg, prints user JSON.
- `TestAdminUsersShowMissingID` — missing `<id>` arg exits with code 2.
- `TestAdminUsersCreateCommand` — calls `CreateUser` with all four flags,
  prints user JSON.
- `TestAdminUsersCreateMissingUsername` — missing `--username` exits code 2.
- `TestAdminUsersCreateMissingEmail` — missing `--email` exits code 2.
- `TestAdminUsersCreateMissingProvider` — missing `--provider` exits code 2.
- `TestAdminUsersCreateMissingProviderID` — missing `--provider-id` exits
  code 2.
- `TestAdminUsersUpdateCommand` — calls `UpdateUserByID` with `id` and
  `--full-name`, prints user JSON.
- `TestAdminUsersUpdateMissingID` — missing `<id>` exits code 2.
- `TestAdminUsersUpdateMissingFullName` — missing `--full-name` exits code 2
  (flag is required; absence is a client error).
- `TestAdminUsersUpdateEmptyFullName` — `--full-name ""` is valid (clears
  the field); command proceeds to call SDK and exits 0.
- `TestAdminUsersPromoteCommand` — calls `PromoteUser` with `id`.
- `TestAdminUsersPromoteMissingID` — exits code 2.
- `TestAdminUsersDemoteCommand` — calls `DemoteUser` with `id`.
- `TestAdminUsersDemoteMissingID` — exits code 2.
- `TestAdminUsersDemoteLastAdmin` — SDK returns 409 error, CLI exits code 1.
- `TestAdminUsersBlockCommand` — calls `BlockUser` with `id`.
- `TestAdminUsersBlockMissingID` — exits code 2.
- `TestAdminUsersUnblockCommand` — calls `UnblockUser` with `id`.
- `TestAdminUsersUnblockMissingID` — exits code 2.
- `TestAdminUsersAPIError` — SDK returns `*APIError`, CLI prints error
  envelope and exits code 1.
- `TestAdminUsersNetworkError` — SDK returns plain error, CLI exits code 2.

#### Admin Organization Command Tests

- `TestAdminOrgsListCommand` — calls `ListOrgs`, prints JSON array.
- `TestAdminOrgsListIncludeBlocked` — `--include-blocked` sets option.
- `TestAdminOrgsCreateCommand` — calls `CreateOrg` with `--name`, `--slug`,
  `--url`.
- `TestAdminOrgsCreateWithoutURL` — `--url` omitted, URL is nil in request.
- `TestAdminOrgsCreateMissingName` — exits code 2.
- `TestAdminOrgsCreateMissingSlug` — exits code 2.
- `TestAdminOrgsUpdateCommand` — calls `UpdateOrg` with `id`, `--name`,
  `--url`.
- `TestAdminOrgsUpdateNameOnly` — only `--name` provided.
- `TestAdminOrgsUpdateURLOnly` — only `--url` provided.
- `TestAdminOrgsUpdateMissingID` — exits code 2.
- `TestAdminOrgsUpdateNoFlags` — neither `--name` nor `--url` provided;
  verifies warning on stderr (via `warnf`), SDK is still called, exits 0
  on successful server response.
- `TestAdminOrgsDeleteCommand` — calls `DeleteOrg`, prints `{}`.
- `TestAdminOrgsDeleteMissingID` — exits code 2.
- `TestAdminOrgsBlockCommand` — calls `BlockOrg`.
- `TestAdminOrgsBlockMissingID` — exits code 2.
- `TestAdminOrgsUnblockCommand` — calls `UnblockOrg`.
- `TestAdminOrgsUnblockMissingID` — exits code 2.

#### Admin Organization Member Command Tests

- `TestAdminOrgsMembersListCommand` — calls `ListOrgMembers`, prints JSON array.
- `TestAdminOrgsMembersListMissingID` — exits code 2.
- `TestAdminOrgsMembersAddCommand` — calls `AddOrgMember` with both IDs,
  prints `{}`.
- `TestAdminOrgsMembersAddMissingArgs` — missing arg(s) exits code 2.
- `TestAdminOrgsMembersRemoveCommand` — calls `RemoveOrgMember`, prints `{}`.
- `TestAdminOrgsMembersRemoveMissingArgs` — exits code 2.

#### Admin Key Command Tests

- `TestAdminKeysListCommand` — calls `ListUserKeys`, prints JSON array.
- `TestAdminKeysListMissingUserID` — exits code 2.
- `TestAdminKeysRevokeCommand` — calls `RevokeUserKey`, prints `{}`.
- `TestAdminKeysRevokeMissingArgs` — missing arg(s) exits code 2.

#### Admin Token Command Tests

- `TestAdminTokensListCommand` — calls `ListUserTokens`, prints JSON array.
- `TestAdminTokensListMissingUserID` — exits code 2.
- `TestAdminTokensRevokeCommand` — calls `RevokeUserToken`, prints `{}`.
- `TestAdminTokensRevokeMissingArgs` — exits code 2.

#### Agent Interface Tests

- `TestAdminCommandsHelpJSON` — verify all admin commands appear in
  `akc help --json` output with correct `method`, `path`, `auth: "admin"`,
  `args`, and `flags`.

---

## Design Decisions

- **Thin wrapper pattern.** Each CLI command is a thin wrapper around a single
  SDK method call. The command parses arguments, constructs the SDK request,
  calls the method, and formats the output. No business logic lives in the CLI
  layer.

- **No confirmation prompts.** Destructive operations (delete, revoke, block)
  execute immediately. The CLI is designed for agent and script consumption
  where interactive prompts would break automation. Confirmation, if needed,
  is the caller's responsibility.

- **`{}` for void responses.** Commands that wrap SDK methods returning only
  `error` (HTTP 204: `DeleteOrg`, `AddOrgMember`, `RemoveOrgMember`,
  `RevokeUserKey`, `RevokeUserToken`) print `{}` on success. This maintains
  the invariant that every successful command produces valid JSON to stdout,
  which agents and scripts can always safely parse.

- **No UUID format validation.** The CLI passes any non-empty string ID through
  to the SDK and server. This avoids duplicating server-side validation and
  keeps the CLI thin. Malformed IDs result in server-returned 4xx errors (exit
  code 1), not client errors (exit code 2).

- **Empty string flags pass through to the server.** For required flags that
  accept string values (e.g., `--name`, `--slug`, `--username`), an explicitly
  provided empty string (e.g., `--name ""`) is not rejected client-side. The
  server validates and returns an appropriate 4xx error. The one exception is
  `--full-name ""` for `admin users update`, which is explicitly valid
  (intentionally clears the field).

- **Client validation only for argument/flag presence.** The CLI validates that
  required positional arguments and flags are present. It does not duplicate
  server-side validation (e.g., email format, slug uniqueness, UUID format).
  The server returns appropriate error responses via the SDK, which the CLI
  forwards.

- **`context.Background()` for all SDK calls.** The CLI is a short-lived
  process. Context deadlines are unnecessary; cancellation happens via OS
  signals that terminate the process.

- **Flag naming follows CLI conventions.** Flags use kebab-case
  (`--include-blocked`, `--full-name`, `--provider-id`) per Go CLI conventions
  and the master PRD's command listing. The SDK request struct field names use
  Go conventions (`IncludeBlocked`, `FullName`, `ProviderID`).

- **Positional arguments for IDs.** Resource IDs are positional arguments, not
  flags. This matches the master PRD's command signatures and is more ergonomic
  for both human and agent callers: `akc admin users show <id>` is more natural
  than `akc admin users show --id <id>`.

- **`admin orgs members` as a nested command group.** The org members commands
  are nested under `admin orgs members` rather than flattened (e.g.,
  `admin orgs add-member`). This mirrors the URL structure (`/orgs/:id/members`)
  and groups related operations.

- **Admin commands do not mutate config.** No admin command writes to the CLI
  config file. Config mutations are restricted to `login`, `keys refresh`, and
  `keys revoke` (specs 13/14). This keeps the admin commands purely
  operational.

- **`--full-name` is required for `admin users update`.** The `update` command
  uses a required `--full-name` flag rather than making it optional, because
  `UpdateUserRequest.FullName` is a plain `string` — there is no way to
  represent "not provided" vs. "intentionally empty" without requiring the
  flag explicitly. This is intentionally asymmetric with `admin orgs update`,
  where `UpdateOrgRequest` uses `*string` fields that can be `nil`.

- **`--name` and `--url` are optional for `admin orgs update`.** Both fields
  in `UpdateOrgRequest` are `*string`. A `nil` pointer means "do not update
  this field". Providing neither flag is permitted: the CLI warns on stderr
  (via the shared `warnf` helper from spec 13) and still calls the SDK,
  deferring outcome to the server. This is not a client-side error.

- **JSON output to stdout; all other messages to stderr.** This deliberately
  deviates from conventional CLI behavior where validation messages often go to
  stderr. The design priority is agent parseability: stdout is always valid
  JSON (either a result object or an error envelope). Warnings and informational
  messages (e.g., the empty-patch warning) go to stderr via `warnf` and are
  safe for agents to ignore.

- **Test-double interface is test-scoped.** The mock interface for SDK client
  methods is defined as an unexported interface in each test file, not in
  production code. This keeps production types clean while enabling full unit
  test isolation.

- **`NewAdminCmd` wired by spec 13.** The admin command tree is registered on
  the Cobra root command by the CLI core (spec 13), which is the single
  authoritative registration point for all top-level subcommands. This prevents
  fragmentation of root command setup across multiple specs.

- **`warnf` is a shared CLI core helper.** Warning messages (e.g., the
  empty-patch warning for `admin orgs update`) are written to stderr via the
  `warnf` function defined in spec 13. This ensures consistent warning format
  across all CLI commands. The exact format (prefix, newline) is defined in
  spec 13.

---

## Glossary

| Term | Definition |
|------|------------|
| **Admin command** | A CLI command under `akc admin` that requires admin-level access (admin role API key or break-glass admin token). |
| **Positional argument** | A required value specified by position on the command line, not by a named flag. Used for resource IDs. |
| **Exit code** | The process exit status: 0 for success, 1 for API errors (server returned 4xx/5xx), 2 for client errors (missing config, invalid args, network failure). |
| **Error envelope** | The JSON error format `{"error": {"code": N, "message": "..."}}` used for all error output to stdout, matching the API's error response format. |
| **Void response** | A command that wraps an SDK method returning only `error` (HTTP 204). The CLI prints `{}` on success. |
| **Agent interface** | The structured JSON output of `akc help --json` that describes all commands, their arguments, flags, HTTP mappings, and auth requirements. Format defined in spec 13. |
| **CLI core** | The shared CLI infrastructure (spec 13) providing config loading, client construction, error handling, JSON output, and warning helpers. |
| **SDK method** | A typed Go function on `apikit.Client` that wraps a single API endpoint. The CLI delegates all API communication to these methods. |
| **kebab-case** | The naming convention for CLI flags: words separated by hyphens (e.g., `--include-blocked`). |
| **Empty patch** | A PATCH request where no fields are modified (e.g., `admin orgs update` called with no flags). The CLI warns on stderr via `warnf` and proceeds; the server determines the outcome. |
| **Test-scoped interface** | An unexported Go interface defined in a `_test.go` file, used only for injecting mock SDK clients in unit tests. Not part of the production API surface. |
| **Pass-through validation** | The policy of passing flag or argument values (including empty strings and non-UUID IDs) to the SDK and server without client-side format validation, relying on server-returned 4xx errors for rejection. |

---

## Owner

Michael Kuehl
