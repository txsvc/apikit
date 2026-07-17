---
spec_id: '13'
spec_name: cli_core
title: Cli Core
status: draft
created_at: '2026-07-17T13:06:05.997365+00:00'
updated_at: '2026-07-17T13:37:08.053421+00:00'
owner: ''
source: interactive
schema_version: 1
---
# CLI Core

## Source Reference

This spec is derived from the [apikit master PRD](docs/PRD.md). It covers the
**CLI Core** component — spec 13 of 15. The master PRD sections on "CLI" (binary,
commands, config, agent interface), "Persistent Client Configuration," and
"Error Handling" are the primary sources. The Go SDK (spec 12) defines the
client methods that every CLI command delegates to.

## Intent

Implement the core CLI binary `akc` in Go using Cobra. The CLI is a thin wrapper
around the Go SDK — it does NOT make HTTP calls directly. Every command delegates
to the corresponding `apikit.Client` method from spec 12. The CLI serves two
purposes: standalone tool for managing the auth/user layer of any apikit-based
service, and embeddable command tree that consuming projects import via Cobra's
`AddCommand`.

This spec covers: binary entry point, root command, Cobra command tree skeleton,
persistent client configuration, credential resolution, config file management,
output formatting, error handling, exit codes, build-time variables, `version`
command, and the agent interface (`help --json`). Individual command
implementations for login, user management, and admin operations are covered by
specs 14 and 15.

## Goals

- Implement the `akc` CLI binary entry point at `cmd/akc/main.go`.
- Create the root Cobra command with persistent flags (`--endpoint-url`,
  `--user-id`, `--api-key`) and global `--json` support.
- Implement persistent client configuration at `$HOME/.<prefix>/config.toml`:
  - Three fields only: `endpoint_url`, `user_id`, `api_key`.
  - Auto-create config directory (mode 0700) and config file (mode 0600) using
    a hard-coded template string (see Persistent Client Configuration) on
    startup if they do not exist.
  - Existing config files are not modified on startup.
- Implement credential resolution with precedence: CLI flag > env var > config
  file > error. The three fields and their corresponding sources are:
  - `endpoint_url`: flag `--endpoint-url`, env `ENDPOINT_URL`, config `endpoint_url`
  - `user_id`: flag `--user-id`, env `USER_ID`, config `user_id`
  - `api_key`: flag `--api-key`, env `API_KEY`, config `api_key`
  - An empty string in the config file is treated as unset.
- Implement config-mutating write operations using atomic writes (write to temp
  file in the same directory, then `os.Rename` into place; use `defer os.Remove`
  to clean up the temp file on failure).
- Implement the `akc version` command:
  - Print CLI version, build info, configured prefix to stdout as JSON.
  - Fetch and include server version by calling `client.Version(ctx)` if
    the endpoint is reachable.
  - If server is unreachable, omit server version from output and print a
    warning to stderr.
- Implement the agent interface:
  - `akc help --json` returns the complete machine-readable command tree as
    JSON to stdout. The JSON structure includes: `name` (CLI name), `version`,
    and `commands` array. Each command entry contains:
    - `name` — full command path (e.g., "user show", "admin users list")
    - `description` — one-line description
    - `method` — HTTP method the command maps to (e.g., "GET", "POST"), or
      `null` for composite commands
    - `path` — API path relative to mount point (e.g., "/user"), or `null`
      for composite commands
    - `args` — array of positional argument descriptors
    - `flags` — array of flag descriptors with `name`, `type`, `required`,
      `default` (omitted when the value equals the zero value for the type,
      for all flag types including bool), and `description`
    - `auth` — required auth level: "none", "api_key", or "admin"
    - `composite` (optional) — `true` for multi-step commands like `login`
  - `akc <command> --help --json` returns the entry for that command only.
  - Commands added by consuming projects via `AddCommand` appear in the
    `help --json` output automatically.
- All commands print JSON to stdout, human-readable messages to stderr.
- Error handling: on failure, write a JSON error object to stdout matching the
  API error envelope format: `{"error":{"code":<int>,"message":"<string>"}}`.
  No additional output is written to stderr for errors — the JSON envelope is
  the sole error output on any stream.
- Exit codes:
  - `0` — success
  - `1` — API error (4xx/5xx from the server; details in stdout JSON)
  - `2` — client error (missing config, invalid flags, network failure;
    details in stdout JSON)
- Build-time variables injected via `-ldflags`:
  - `Version` — semantic version string
  - `Build` — build timestamp or commit hash
  - `TokenPrefix` — the configurable token prefix (determines config directory
    `$HOME/.<prefix>/` and is used for display in `version` output)
- Provide an exported function (e.g., `NewCLI()` or `RootCommand()`) that
  returns the root `*cobra.Command` so consuming projects can embed the
  command tree via `AddCommand`.

## Non-Goals

- **Individual command implementations.** The login flow, user commands,
  key/token commands, org commands, and admin commands are covered by specs 14
  and 15. This spec provides the skeleton and infrastructure they plug into.
- **OAuth flow orchestration.** Browser opening, callback server, state
  parameter management are spec 14 concerns.
- **OS keychain or secret-store integration.** Tokens stored in plaintext per
  the master PRD.
- **Multi-profile or named-context support.** One active config at a time.
- **Windows-specific path conventions.** CLI targets Unix-like systems using
  `$HOME`. Containerized or CI environments must set `HOME` explicitly; see
  Error Handling for behavior when `$HOME` is unresolvable.
- **Shell completion generation.** Deferred to a future iteration. Cobra's
  built-in `completion` command is **not** enabled by default; it must not be
  registered in the root command setup. This also prevents the `completion`
  command from appearing as an unexpected leaf in `help --json` output.
- **Interactive prompts.** All input is via flags and arguments; the CLI is
  designed for both human and agent use.
- **Color output.** All output is JSON (stdout) or plain text (stderr); no
  ANSI color codes.
- **Config directory override via env var.** There is no `AKC_CONFIG_DIR`
  or equivalent override mechanism. Operators who need a non-standard config
  path must set `$HOME` appropriately.
- **`help --json` schema versioning.** A `schema_version` field in the
  `help --json` output is deferred to a future iteration. The CLI `version`
  field serves as an indirect indicator of schema changes in the interim.
- **Concurrent config access safety.** The atomic write prevents partial
  writes, but simultaneous CLI invocations that both write to `config.toml`
  may result in lost updates (last rename wins). This is an accepted limitation;
  file-level locking is deferred to a future iteration.

## Dependencies

| Spec | From Group | To Group | Relationship |
|------|-----------|----------|--------------|
| `12_go_sdk` | 1 | 1 | CLI wraps Go SDK client for all API calls. Every CLI command creates an `apikit.Client` and calls the corresponding SDK method. The CLI reuses the SDK's typed response structs and `*APIError` for error handling. The `version` command embeds the SDK's `VersionResponse` type directly in its output struct. |
| `01_server_core` | 1 | 1 | Uses build-time `TokenPrefix` variable. The same prefix variable is used by the server to parse tokens and by the CLI to determine the config directory path `$HOME/.<prefix>/`. The `TokenPrefix` value must be coordinated across server and CLI builds — both must be compiled with the same value to ensure the correct config directory is used and token formats align. This is a build-time coordination concern, not a runtime dependency. |

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go (matching the project's Go version) |
| CLI framework | Cobra (`github.com/spf13/cobra`) |
| Config format | TOML (`github.com/BurntSushi/toml`) |
| Config directory | `$HOME/.<prefix>/` (prefix is build-time variable) |
| JSON output | `encoding/json` (stdlib) |
| File I/O | `os`, `io`, `path/filepath` (stdlib) |
| Build | `go build` with `-ldflags` for version injection |
| Test | `go test` (stdlib) |

## Repository Layout

```
cmd/
  akc/
    main.go                 CLI binary entry point
internal/
  cli/
    root.go                 Root command, persistent flags, config init
    config.go               Config loading, resolution, atomic writes
    output.go               JSON output helpers, error envelope formatting
    version.go              Version command implementation
    help.go                 Agent interface: help --json rendering
    context.go              Unexported context key types and helpers
    version_info.go         Build-time variable declarations
    root_test.go            Root command tests
    config_test.go          Config loading and resolution tests
    output_test.go          Output formatting tests
    version_test.go         Version command tests
    help_test.go            Agent interface tests
```

The CLI implementation lives under `internal/cli/` because the command
implementations are not intended for direct import by consuming projects.
Consuming projects embed the CLI via the exported `RootCommand()` function
in the root `apikit` package, which delegates to `internal/cli/`.

The root `apikit` package exposes the shim:

```go
// apikit.go (root package)
package apikit

import (
    "github.com/spf13/cobra"
    "github.com/txsvc/apikit/internal/cli"
)

// RootCommand returns the embeddable akc CLI command tree.
func RootCommand() *cobra.Command {
    return cli.RootCommand()
}
```

This shim is the only public surface for the CLI; all implementation detail
remains in `internal/cli/`.

## Functional Requirements

### Binary Entry Point

`cmd/akc/main.go` is the entry point for the standalone CLI binary. It calls
the root command's `Execute()` method. Build-time variables (`Version`, `Build`,
`TokenPrefix`) are injected into the `internal/cli` package via `-ldflags`.

`Execute()` handles **all** output — including error envelopes — before
returning. `main.go` only maps the returned error to an exit code. The root
command is configured with `SilenceErrors: true` and `SilenceUsage: true` so
that Cobra does not print its own error messages or usage text; all output is
under `Execute()`'s control.

```go
// cmd/akc/main.go
package main

import (
    "os"
    "github.com/txsvc/apikit/internal/cli"
)

func main() {
    if err := cli.Execute(); err != nil {
        os.Exit(cli.ExitCode(err))
    }
}
```

### Execute() Error Interception

`Execute()` in `internal/cli` is a wrapper around Cobra's `rootCmd.Execute()`.
It centralizes error output so that no individual `RunE` function needs to call
`PrintError` itself. The pattern is:

```go
// internal/cli/root.go
func Execute() error {
    err := rootCmd.Execute()
    if err != nil {
        PrintError(err)
    }
    return err
}
```

This means:
- Cobra calls the appropriate `RunE`, which returns an error on failure.
- Cobra's `rootCmd.Execute()` propagates that error to `Execute()`.
- `Execute()` calls `PrintError(err)` to write the JSON envelope to stdout.
- `Execute()` returns the error to `main.go`, which calls `cli.ExitCode(err)`
  and passes it to `os.Exit`.
- Individual `RunE` functions return errors without printing them; they call
  `PrintJSON` on success only.
- `SilenceErrors: true` and `SilenceUsage: true` ensure Cobra does not print
  its own error or usage text between `RunE` returning and `Execute()` returning.

**Division of output responsibility:**
- Individual `RunE` functions: call `PrintJSON` on success, return errors on failure.
- `Execute()`: calls `PrintError` on any non-nil error returned by `rootCmd.Execute()`.
- `main.go`: calls `os.Exit(cli.ExitCode(err))`; produces no output of any kind.

### Root Command

The root command (`akc`) defines three persistent flags that are available to
all subcommands:

- `--endpoint-url` (string) — server base URL
- `--user-id` (string) — authenticated user's UUID
- `--api-key` (string) — active API key

The root command also defines `--json` as a persistent flag for help output
formatting. It is checked by the custom `SetHelpFunc` and the `help`
subcommand; it has no effect on non-help invocations. Because `--json` is a
persistent flag, Cobra will accept it on any command without error. **Non-help
commands silently ignore `--json` with no warning or error** — it is reserved
for help contexts only. For example, `akc version --json` behaves identically
to `akc version`; the `--json` flag is simply not consulted.

The root command is configured with:
```go
rootCmd.SilenceErrors = true
rootCmd.SilenceUsage  = true
```

**Bare invocation behavior:** The root command has no `RunE`. When `akc` is
invoked with no subcommand and no flags, Cobra prints its default help text to
**stdout** and exits 0. This is Cobra's standard behavior for commands without
`RunE`. `SilenceUsage: true` only suppresses usage output on error; it does
not suppress the default help text printed on bare invocation.

The root command's `PersistentPreRunE` hook:
1. Inspects the invoked command's `Annotations` map for `"auth": "none"`. If
   present, skips steps 2–6 entirely and returns nil.
2. Checks that `$HOME` is resolvable via `os.UserHomeDir()`; returns exit-2
   error immediately if not.
3. Validates that `TokenPrefix` is non-empty; returns exit-2 error immediately
   if it is empty (before any config initialization).
4. Initializes the config directory and file if they don't exist.
5. Loads the config file.
6. Resolves each credential field using the precedence chain.
7. Creates an `apikit.Client` instance configured with the resolved values, and
   stores both the client and the resolved `user_id` in the command context.

**Cobra passes the leaf command as `cmd` in `PersistentPreRunE`:** Cobra passes
the actually invoked (leaf) command — not the root command — as the `cmd`
argument to every `PersistentPreRunE` function. This is standard Cobra behavior:
a parent's `PersistentPreRunE` is called with the matched leaf command as its
`cmd` argument. The annotation check therefore reads from the correct command:

```go
// In root PersistentPreRunE — cmd is the leaf command, not the root.
if cmd.Annotations["auth"] == "none" {
    return nil // skip client initialization entirely
}
```

Implementors must not assume `cmd` is the root command in `PersistentPreRunE`.
This is a known Cobra pitfall. If the annotation check were written against the
root command (`rootCmd.Annotations`), it would never find `"auth": "none"` and
auth-exempt commands would incorrectly undergo client initialization.

**Auth-exempt commands:** Any command that sets `"auth": "none"` in its
`Annotations` map is exempt from client creation. This includes `version` and
`help`. Specs 14 and 15 must annotate their auth-exempt commands accordingly.

**Hook chaining with child commands:** The root command is the **only** command
that defines `PersistentPreRunE`. Commands in specs 14 and 15 use `PreRunE`
(not `PersistentPreRunE`) for any command-specific setup. Because Cobra
automatically calls a parent's `PersistentPreRunE` before a child's `PreRunE`,
client initialization runs transparently without any manual chaining. Child
commands must not define `PersistentPreRunE`; doing so would shadow the root's
hook and break client initialization. This constraint must be documented in
the contributing guide and enforced via code review.

### TokenPrefix Validation

`TokenPrefix` is a build-time variable. If it resolves to an empty string at
runtime (e.g., due to an accidental blank value in `-ldflags`), the CLI exits
immediately with code `2` and writes a descriptive error envelope to stdout:

```json
{"error":{"code":0,"message":"TokenPrefix is empty: binary was built without a valid -ldflags TokenPrefix value"}}
```

This validation is performed in `PersistentPreRunE`, after the `"auth": "none"`
annotation check but before the `$HOME` check and any config initialization.
Auth-exempt commands that bypass `PersistentPreRunE` entirely (e.g., `help`)
are also exempt from this check. The `version` command reads `TokenPrefix`
directly in its `RunE` for display; it should validate non-emptiness
independently if needed, but typically the default value `"ak"` ensures it is
never empty in practice.

**Ordering within PersistentPreRunE:**
1. Auth annotation check (`"auth": "none"` → return nil immediately).
2. `TokenPrefix` non-empty check → exit 2 if empty.
3. `$HOME` resolvability check → exit 2 if unresolvable.
4. Config directory and file initialization.
5. Config file loading.
6. Credential resolution.
7. Client construction and context storage.

The default value `"ak"` is set in `version_info.go` so that a binary built
without any `-ldflags` override still has a valid prefix. An empty string can
only result from an explicit `-ldflags "-X ...TokenPrefix="` invocation, which
is always a build error.

### Persistent Client Configuration

**Config directory:** `$HOME/.<prefix>/` where `<prefix>` is the build-time
`TokenPrefix` variable. Created with mode `0700` if it does not exist.

**Unresolvable `$HOME`:** If `os.UserHomeDir()` returns an error or an empty
string, the CLI exits immediately with code `2` and writes a descriptive error
envelope to stdout:
```json
{"error":{"code":0,"message":"cannot determine home directory: $HOME is not set or unresolvable"}}
```
No config initialization is attempted. This error occurs in `PersistentPreRunE`
after the `TokenPrefix` check but before any config access.

**Config file:** `config.toml` inside the config directory. When created fresh,
the file is written from a hard-coded template string — not via the TOML
encoder — to guarantee all three keys are always present and the file is
self-documenting for users who open it manually. The template is:

```toml
endpoint_url = ""
user_id = ""
api_key = ""
```

This file is written with mode `0600`. Using a hard-coded template string
rather than `toml.NewEncoder` avoids any dependency on encoder behavior with
zero-value string fields (which could omit keys) and makes the initial file
format stable and predictable.

**Config loading:** Uses `github.com/BurntSushi/toml` to parse the TOML file
into a Go struct. The `BurntSushi/toml` package supports encoding via
`toml.NewEncoder` (for marshaling to a writer) and decoding via `toml.Decode`
or `toml.NewDecoder`. Config writing uses `toml.NewEncoder(w).Encode(cfg)`;
config reading uses `toml.Decode(content, &cfg)` or `toml.NewDecoder(r).Decode(&cfg)`.

```go
type CLIConfig struct {
    EndpointURL string `toml:"endpoint_url"`
    UserID      string `toml:"user_id"`
    APIKey      string `toml:"api_key"`
}
```

**Config init behavior:**
- If the config directory does not exist, create it with `os.MkdirAll` and
  mode `0700`.
- If `config.toml` does not exist, create it from the hard-coded template
  string (all three keys with empty string values) with mode `0600`.
- If `config.toml` already exists, do not modify it.

**Malformed config file behavior:** If `config.toml` exists but contains any
TOML parse error — including partially invalid content — the **entire file is
rejected**. The CLI exits with code `2` and writes an error envelope to stdout:
```json
{"error":{"code":0,"message":"config file is unparseable: <toml parse error>"}}
```
No partial results are accepted. Given that `config.toml` has only three simple
string fields, a parse error indicates corruption or manual edit mistakes that
should be corrected by the user, not silently worked around. The user must fix
or delete the config file to proceed.

### Credential Resolution

For each of the three credential fields (`endpoint_url`, `user_id`, `api_key`),
the resolution order is:

1. **CLI flag** — if the corresponding flag was explicitly set by the user
   (check with Cobra's `cmd.Flags().Changed()`).
2. **Environment variable** — `ENDPOINT_URL`, `USER_ID`, `API_KEY`. Non-empty
   value wins.
3. **Config file** — the value from `config.toml`. Empty string is treated as
   unset.
4. **Error** — if all three sources are empty/unset, return a descriptive
   error message naming the field, the flag, and the env var.

**Environment variable naming:** The environment variable names are
`ENDPOINT_URL`, `USER_ID`, and `API_KEY` — deliberately unprefixed. This
matches the master PRD convention. The CLI is designed for single-config
deployments, and the unprefixed names reflect that simplicity. Consuming
projects that require namespaced env vars (e.g., `MYAPP_API_KEY`) can define
their own env var mapping in their wrapper layer; the CLI itself does not
provide a prefix mechanism for env var names.

**Error message format:** The canonical error message format for a missing
credential field is:

```
<field> is not set: provide via <flag>, $<ENV_VAR>, or config file
```

Concrete examples for each credential:
- `endpoint_url is not set: provide via --endpoint-url, $ENDPOINT_URL, or config file`
- `user_id is not set: provide via --user-id, $USER_ID, or config file`
- `api_key is not set: provide via --api-key, $API_KEY, or config file`

These messages are produced by `ResolveField` and propagated into error
envelopes with `code: 0`.

**Which fields are required in `PersistentPreRunE`:** For non-auth-exempt
commands, `PersistentPreRunE` resolves all three fields. `endpoint_url` and
`api_key` are **required** — a missing value for either causes `PersistentPreRunE`
to return an exit-2 error. `user_id` is resolved with the `required` parameter
set to `false` — a missing `user_id` returns an empty string instead of an
error and is stored in context as the empty string. Commands that need `user_id`
check `UserIDFromContext` themselves and handle the empty case appropriately.

**`ResolveField` call site pattern for `user_id`:**

```go
// endpoint_url and api_key — required
endpointURL, err := ResolveField("endpoint_url", "--endpoint-url", flagEndpointURL, endpointURLChanged, "ENDPOINT_URL", cfg.EndpointURL, true)
if err != nil { return err }

apiKey, err := ResolveField("api_key", "--api-key", flagAPIKey, apiKeyChanged, "API_KEY", cfg.APIKey, true)
if err != nil { return err }

// user_id — optional; empty string stored in context, not an error
userID, _ := ResolveField("user_id", "--user-id", flagUserID, userIDChanged, "USER_ID", cfg.UserID, false)
```

When `required` is `false`, `ResolveField` returns `("", nil)` instead of
`("", error)` when all sources are unset. This eliminates any need for the
call site to selectively discard errors.

**Resolution is lazy:** Commands annotated with `"auth": "none"` bypass
credential resolution entirely. Auth-exempt commands that optionally need a
credential (e.g., `version` optionally uses `endpoint_url`) use the
`ResolveEndpointURL` helper directly in their `RunE`.

### Credential Resolution for Auth-Exempt Commands

Auth-exempt commands (those with `"auth": "none"`) skip `PersistentPreRunE`
and therefore cannot rely on the standard resolution path. For commands that
need to optionally resolve `endpoint_url` (such as `version`), a package-level
helper is provided:

```go
// ResolveEndpointURL reads the persistent --endpoint-url flag from the root
// command and applies the standard precedence chain (flag > env > config).
// It is intended for auth-exempt commands that optionally need an endpoint.
// Returns an empty string (not an error) if no endpoint is configured.
func ResolveEndpointURL(cmd *cobra.Command) string
```

The implementation reads the persistent flag value and its `Changed` state
from `cmd.Root().PersistentFlags()`, loads the config file, and calls
`ResolveField` with `required: false`. Because the endpoint is optional for
auth-exempt commands (no endpoint → skip the server call, not an error),
`ResolveEndpointURL` returns an empty string rather than an error when no
endpoint is configured.

The `version` command's `RunE` calls `ResolveEndpointURL(cmd)` to obtain the
endpoint URL, constructs a minimal `apikit.Client` if a non-empty URL is
returned, and calls `client.Version(ctx)`. If `ResolveEndpointURL` returns an
empty string, the version command omits `server_version` from the output and
prints no warning (no endpoint configured is a normal state, not an error).
If a URL is returned but the server is unreachable, `server_version` is omitted
and a warning is printed to stderr.

Any other auth-exempt command in specs 14 or 15 that needs to resolve a
credential independently should follow the same pattern — reading the persistent
flag directly from `cmd.Root().PersistentFlags()` and calling `ResolveField`
with `required: false` explicitly, rather than relying on `PersistentPreRunE`.

### Context Storage

The `PersistentPreRunE` hook stores two values in the command context using
**unexported struct-typed keys** to prevent collisions with consuming projects:

```go
// internal/cli/context.go
package cli

// Unexported key types — zero-size structs prevent any external package
// from constructing or colliding with these keys.
type clientContextKey struct{}
type userIDContextKey struct{}
```

The client and `user_id` are stored and retrieved via package-level helpers:

```go
// Store (called from PersistentPreRunE)
ctx = context.WithValue(ctx, clientContextKey{}, client)
ctx = context.WithValue(ctx, userIDContextKey{}, resolvedUserID)

// Retrieve (called from subcommand RunE)
func ClientFromContext(ctx context.Context) *apikit.Client {
    c, _ := ctx.Value(clientContextKey{}).(*apikit.Client)
    return c // nil if not set (auth-exempt commands)
}

func UserIDFromContext(ctx context.Context) string {
    s, _ := ctx.Value(userIDContextKey{}).(string)
    return s
}
```

The `user_id` value is **not** passed to `apikit.NewClient`. The Go SDK
`Client` uses only `endpoint_url` and `api_key` for authentication. `user_id`
is a CLI-level value available to specific commands that need to know the
authenticated user's identity (e.g., for display or as a parameter), retrieved
via `UserIDFromContext`. A missing `user_id` is stored as an empty string in
context; commands that require it check for emptiness themselves.

### Config Mutation (Atomic Writes)

Config-mutating commands (`login`, `keys refresh`, `keys revoke` — implemented
in specs 14-15) use the config write function provided by this spec. The
atomic write procedure:

1. Marshal the updated `CLIConfig` struct to TOML using `toml.NewEncoder(w).Encode(cfg)`.
2. Write the TOML bytes to a temporary file in the same directory as
   `config.toml` (using `os.CreateTemp` with the config directory as the
   temp dir). Using the same directory guarantees the subsequent `os.Rename`
   is an atomic same-filesystem operation and cannot fail with a cross-device
   link error. If `os.CreateTemp` itself fails (e.g., the directory is
   unwritable), the error is returned immediately; no temp file exists to clean
   up.
3. Immediately register `defer os.Remove(tmpFile.Name())` to clean up the
   temp file on any failure path. (On success, the file is renamed before
   the deferred remove runs; `os.Remove` on a non-existent path is a no-op
   on most Unix systems, so the deferred call is safe regardless.)
4. Set the temp file permissions to `0600`.
5. Close the temp file.
6. Rename the temp file to `config.toml` using `os.Rename`.
7. If `os.Rename` succeeds, the deferred remove targets a path that no longer
   exists — this is safe and does not affect the renamed file.

This ensures that `config.toml` is never partially written. If any step after
step 2 fails, the deferred `os.Remove` deletes the temp file, leaving the
original config intact and the config directory clean.

**Rename failure:** If `os.Rename` fails after the temp file has been written
(e.g., due to filesystem-level errors), the deferred `os.Remove` removes the
temp file, leaving the original `config.toml` untouched. The error from
`os.Rename` is returned to the caller, which propagates it as a client error
(exit code 2) with an appropriate error envelope.

**Concurrent access:** The atomic rename prevents partial writes but does not
protect against lost updates when two CLI processes write `config.toml`
simultaneously. The last `os.Rename` wins. This is an accepted limitation;
file-level locking is deferred to a future iteration (see Non-Goals).

### Output Formatting

**Success output:** Commands print their result as JSON to stdout. The JSON
matches the API response shapes from the Go SDK's typed structs. List results
are bare JSON arrays. Single-resource results are JSON objects. Output is
formatted with `json.MarshalIndent` using two-space indentation for human
readability (agents parse it regardless of formatting).

**Human-readable messages:** Progress indicators, warnings, and informational
messages are printed to stderr using `fmt.Fprintf(os.Stderr, ...)`.

**Error output:** On failure, the CLI writes a JSON error object to stdout
matching the API error envelope:

```json
{"error":{"code":<int>,"message":"<string>"}}
```

**Error code semantics:** `code` is always a non-zero HTTP status code (400+)
when the error originates from the server (i.e., the SDK returned an
`*apikit.APIError`). Server error codes are always HTTP status codes and are
therefore always ≥ 400. `code: 0` is a **CLI sentinel value** meaning the
error is client-side — no HTTP status applies (e.g., missing config, invalid
flags, network failure, unresolvable `$HOME`). The value `0` will never be
returned by the server; it unambiguously identifies a local error condition.

For API errors (`*apikit.APIError` from the SDK), the `code` and `message`
are taken directly from the error. For client-side errors (config, flags,
network), `code` is `0` (not an HTTP status) and `message` describes the
problem. **No additional output is written to stderr for errors of any
kind.** The JSON envelope on stdout is the complete error representation,
ensuring agents always receive valid JSON on stdout regardless of whether the
error is an API error or a client error.

`PrintError` derives the error code and message internally from the error type:
- If `errors.As(err, &apiErr)` succeeds, `code` is taken from `apiErr.Code`
  and `message` from `apiErr.Message`.
- Otherwise, `code` is `0` and `message` is `err.Error()`.

### Exit Codes

| Code | Meaning | When |
|------|---------|------|
| `0` | Success | Command completed successfully |
| `1` | API error | SDK returned `*apikit.APIError` (4xx/5xx from server) |
| `2` | Client error | Config missing/malformed, invalid flags, network failure, unresolvable `$HOME`, empty `TokenPrefix`, or other local error |

The exit code is determined by the error type:
- No error → exit 0
- `errors.As(err, &apiErr)` → exit 1
- All other errors → exit 2

`Execute()` always prints the error envelope to stdout before returning a
non-nil error. `main.go` calls `cli.ExitCode(err)` to determine the exit code
and passes it to `os.Exit`. This contract means Cobra's own usage/error output
is suppressed (`SilenceErrors: true`, `SilenceUsage: true`) and all printed
output is under the CLI's control.

**Division of output responsibility:** `Execute()` is responsible for all
output before it returns — both successful command output (delegated to
individual command `RunE` functions that call `PrintJSON`) and error envelopes
(printed via `PrintError` before returning the error). `main.go` produces
no output of any kind; it only inspects the error to determine the exit code.
Individual command `RunE` functions are responsible for calling `PrintJSON`
on success; they return errors to `Execute()` which then calls `PrintError`
and returns the error to `main.go`.

### Version Command

`akc version` outputs a JSON object to stdout. The output is defined by an
explicit Go struct to prevent drift between the CLI output and the SDK types:

```go
// internal/cli/version.go

// VersionOutput is the JSON structure printed by `akc version`.
// server_version directly embeds the SDK's VersionResponse type so that
// field names and values are always in sync with the Go SDK (spec 12).
type VersionOutput struct {
    CLIVersion    string                  `json:"cli_version"`
    Build         string                  `json:"build"`
    Prefix        string                  `json:"prefix"`
    ServerVersion *apikit.VersionResponse `json:"server_version,omitempty"`
}
```

`apikit.VersionResponse` is the SDK's typed response struct for the
`GET /version` endpoint (defined in spec 12). Its fields must include at
minimum: `version`, `build_time`, `commit`, and `mount_point`. Using the SDK
struct directly (via pointer, with `omitempty`) ensures the `server_version`
sub-object is omitted entirely when nil, and that its field names stay in sync
with the SDK without manual duplication.

Example output:

```json
{
  "cli_version": "0.1.0",
  "build": "2026-07-17T00:00:00Z",
  "prefix": "ak",
  "server_version": {
    "version": "0.1.0",
    "build_time": "2026-07-17T00:00:00Z",
    "commit": "abc123",
    "mount_point": "/api/v1"
  }
}
```

Behavior:
- `cli_version`, `build`, and `prefix` are always present (from build-time
  variables).
- The version command is annotated with `"auth": "none"` and skips
  `PersistentPreRunE` client creation entirely.
- To optionally contact the server, `version`'s `RunE` calls
  `ResolveEndpointURL(cmd)`. If a non-empty endpoint URL is returned, a
  minimal `apikit.Client` is constructed solely for the `Version(ctx)` call.
- If `ResolveEndpointURL` returns an empty string (no endpoint configured),
  `server_version` is omitted from the JSON output silently — no warning is
  printed, as this is a normal unconfigured state.
- If an endpoint URL is configured but the server is unreachable,
  `server_version` is omitted from the JSON output (not `null`, omitted) and
  a warning is printed to stderr: `"warning: could not reach server: <reason>"`.

### Agent Interface (help --json)

#### Mechanical Implementation

The agent interface is implemented via two complementary mechanisms:

1. **`akc help --json` (full tree):** A dedicated `help` subcommand replaces
   Cobra's default help command. When invoked with `--json`, it walks the full
   Cobra command tree and outputs the JSON command list. Without `--json`, it
   delegates to Cobra's default help behavior — including handling
   `akc help <subcommand>` (e.g., `akc help user` shows standard Cobra help
   text for the `user` command group). This delegation ensures that named
   subcommand help continues to work exactly as it did with Cobra's built-in
   help command.

   **Routing logic for the `help` subcommand:** The `help` subcommand checks
   the persistent `--json` flag first. If `--json` is set, it outputs the full
   JSON command tree to stdout, regardless of any positional arguments (whether
   before or after `--json` in the invocation). If `--json` is not set, it
   delegates to Cobra's default help behavior for any positional arguments
   (e.g., treating the first positional arg as a subcommand name).

   **`--json` flag position in `akc help`:** Because `--json` is a persistent
   flag, Cobra parses it regardless of its position relative to positional
   arguments. All of the following invocations are equivalent and output the
   full JSON tree:
   - `akc help --json`
   - `akc help --json user`
   - `akc help user --json`

   When `--json` is present in any position, the full JSON tree is output and
   positional arguments are ignored. There is no per-command filtering in
   `akc help --json`. This simple two-branch routing (JSON tree vs. Cobra
   default) avoids ambiguity and is trivial to implement and test.

2. **`akc <command> --help --json` (single command):** The root command's
   `SetHelpFunc` is overridden. The custom help function checks for the
   persistent `--json` flag. If set, it outputs JSON for that single command's
   metadata instead of the standard usage text. If `--json` is not set, it
   falls back to Cobra's default help text rendering.

This means `--json` is a persistent flag (defined on the root) that is
meaningful only in a help context. Non-help commands silently ignore it.

#### JSON Structure

`akc help --json` returns the complete command tree as JSON to stdout:

```json
{
  "name": "akc",
  "version": "0.1.0",
  "commands": [
    {
      "name": "version",
      "description": "Show CLI version, build info, prefix, and server version",
      "method": null,
      "path": null,
      "args": [],
      "flags": [],
      "auth": "none",
      "composite": true
    },
    {
      "name": "user show",
      "description": "Show my profile",
      "method": "GET",
      "path": "/user",
      "args": [],
      "flags": [],
      "auth": "api_key"
    }
  ]
}
```

#### Command Metadata Annotation

Each Cobra command carries metadata via Cobra's `Annotations` map:

```go
cmd.Annotations = map[string]string{
    "method": "GET",
    "path":   "/user",
    "auth":   "api_key",
}
```

Composite commands (like `login`, `version`) use:
```go
cmd.Annotations = map[string]string{
    "composite": "true",
    "auth":      "none",
}
```

**Default annotations for unannotated commands:** Commands that do not set
`"auth"` in their `Annotations` map are treated as requiring `"api_key"` auth
for both `help --json` output and `PersistentPreRunE` behavior. That is,
absence of `"auth": "none"` implies full credential resolution and client
construction. This is the safe default — a command must explicitly opt out of
authentication.

#### Tree Walking

`help --json` walks the entire Cobra command tree recursively, collecting
**only leaf commands** — commands that have a `RunE` (or `Run`) function set.
Non-leaf group/parent commands (e.g., `user`, `admin`, `admin users`) are not
included as entries in the output. An agent infers the existence of group
commands from the space-separated name prefix of leaf commands (e.g.,
`"user show"` implies a `user` group). This keeps the agent interface clean
and avoids confusing agents with non-executable entries.

**Commands with both `RunE` and child subcommands:** If a Cobra command has
`RunE` set and also has child subcommands, it is treated as a **leaf command**
and included in the `help --json` output. The presence of `RunE` is the sole
criterion; child subcommands do not exclude a command from the output. This is
the simplest rule and avoids implementors needing to reason about dual-mode
commands. Agents receiving such an entry can invoke the command directly.

**Filtering built-in Cobra commands:** The custom `help` subcommand explicitly
replaces Cobra's default help command (so it appears in the tree as the
custom implementation, not a built-in). Cobra's `completion` command is **not**
registered (shell completion is a non-goal; see Non-Goals). Any other
built-in command that Cobra might register and that lacks the `method`/`path`
annotations is filtered from the `help --json` output by the walker: commands
without a `method` annotation that also lack an explicit `"auth"` annotation
are excluded. In practice, the only annotation-free commands in the tree will
be Cobra internals; all apikit-owned commands carry at minimum an `"auth"`
annotation. This filter is intentionally conservative — it is simpler and safer
to define "apikit command" as "has at least one annotation key" than to
maintain a name-based exclusion list.

For each leaf command that passes the filter, the walker extracts: the full
command path (space-separated parent names), the description, annotations for
method/path/auth, positional arguments from `Args` and `Use`, and flags from
the command's flag set.

**Flag descriptors:** Each flag is described with:
- `name` — the flag name including `--` prefix
- `type` — the flag value type ("string", "int", "bool", "stringSlice")
- `required` — boolean, from Cobra's required flag annotation
- `default` — the default value; **omitted when the value equals the zero
  value for the type** (empty string for string, 0 for int, `false` for bool,
  empty array for stringSlice). This rule applies uniformly to all flag types,
  including bool. Agents should treat absence of `default` as meaning the type's
  zero value applies.
- `description` — the flag usage text

**Per-command help:** `akc <command> --help --json` returns the entry for that
specific command only (not the full tree). The custom `SetHelpFunc` detects
the `--json` flag and outputs JSON for that command's metadata only.

**Consuming project integration:** Because the help walker traverses the entire
Cobra command tree, commands added by consuming projects via `AddCommand`
automatically appear in `help --json` output. Consuming projects should set
the same `Annotations` on their commands to populate method/path/auth fields.
Commands without annotations will be filtered from the output by the walker
(see Filtering built-in Cobra commands above). Consuming project commands that
wish to appear in `help --json` **must** set at least the `"auth"` annotation
key; this is the minimum required to pass the annotation filter.

### Build-Time Variables

Three variables are injected at build time via `go build -ldflags`:

```go
// internal/cli/version_info.go
package cli

var (
    Version     = "dev"     // -ldflags "-X github.com/txsvc/apikit/internal/cli.Version=0.1.0"
    Build       = "unknown" // -ldflags "-X github.com/txsvc/apikit/internal/cli.Build=..."
    TokenPrefix = "ak"      // -ldflags "-X github.com/txsvc/apikit/internal/cli.TokenPrefix=myapp"
)
```

The `TokenPrefix` determines:
- The config directory path: `$HOME/.<TokenPrefix>/`
- The display name in `version` output

**Default value and empty-string guard:** The default value `"ak"` is set in
`version_info.go` so that binaries built without any `-ldflags` override
have a valid prefix. An empty `TokenPrefix` can only result from an explicit
`-ldflags "-X ...TokenPrefix="` invocation. Because an empty prefix creates
the invalid path `$HOME/./`, the CLI validates `TokenPrefix` at startup and
exits with code `2` if it is empty (see TokenPrefix Validation).

**Build-time coordination:** The `TokenPrefix` value must be identical across
the server build and the CLI build. A mismatch means the CLI will look for
credentials in the wrong directory, and token format parsing on the server side
may not align. Operators are responsible for ensuring both binaries are compiled
with the same `TokenPrefix` value (e.g., via a shared `Makefile` variable or
build script).

### Embeddable Command Tree

The root `apikit` package exports a shim function that returns the root Cobra
command (see Repository Layout for the shim file location):

```go
// apikit package (root)
func RootCommand() *cobra.Command
```

Consuming projects use this to embed the apikit CLI:

```go
// In consuming project's CLI
rootCmd.AddCommand(apikit.RootCommand())
```

The returned command tree includes all apikit commands. The consuming project's
`AddCommand` integrations appear in `help --json` automatically, provided
they set at least the `"auth"` annotation key (see Filtering built-in Cobra
commands).

### Client Construction for Commands

Commands that need an API client retrieve it from the Cobra command context
using `ClientFromContext`. The `PersistentPreRunE` on the root command
constructs the client only when the invoked leaf command does **not** have
`"auth": "none"` in its `Annotations`:

```go
client := apikit.NewClient(
    endpointURL,
    apikit.WithAPIKey(apiKey),
)
```

The client and the resolved `user_id` are stored in the command's context via
`context.WithValue` using unexported struct-typed keys (see Context Storage).

Commands that don't require a client (like `version` when no endpoint is
configured) handle the nil case gracefully. The `"auth": "none"` annotation
is the canonical signal — no separate skip list or override mechanism is used.

---

## Interfaces

### Exported API Surface (root apikit package)

- `RootCommand() *cobra.Command` — returns the embeddable CLI command tree.

### Internal API Surface (internal/cli package)

- `Execute() error` — wraps `rootCmd.Execute()`. Calls `PrintError(err)` on
  any non-nil error returned by Cobra before returning the error to `main.go`.
  Always prints error envelopes to stdout before returning a non-nil error;
  never returns an error silently. Individual command `RunE` functions are
  responsible for printing their own success output via `PrintJSON`; `Execute()`
  does not intercept or buffer success output. On error, `Execute()` calls
  `PrintError` and returns the error to `main.go`, which determines the exit
  code via `ExitCode(err)`.
- `CLIConfig` struct — config file representation.
- `LoadConfig(configDir string) (*CLIConfig, error)` — loads config.toml.
  Returns an error (exit code 2) if the file exists but contains any TOML
  parse error. The entire file is rejected on any parse error; no partial
  results are returned.
- `SaveConfig(configDir string, cfg *CLIConfig) error` — atomic write with
  deferred temp-file cleanup. Marshals `*CLIConfig` to TOML using
  `toml.NewEncoder(w).Encode(cfg)`. Returns an error if `os.CreateTemp` fails
  (directory unwritable) or if `os.Rename` fails after the temp file is
  written. On rename failure, the deferred `os.Remove` cleans up the temp file,
  leaving the original `config.toml` untouched.
- `InitConfig(configDir string) error` — creates dir (mode 0700) and file
  (mode 0600, from hard-coded template string) if they do not exist. Does not
  modify existing files.
- `ResolveField(fieldName, flagName, flagValue string, flagChanged bool, envVarName, configValue string, required bool) (string, error)` — credential resolution for a single field. When `required` is `true` and all sources are unset, returns an error with the canonical message `"<fieldName> is not set: provide via <flagName>, $<envVarName>, or config file"`. When `required` is `false` and all sources are unset, returns `("", nil)`. The caller passes `fieldName`, `flagName`, and `envVarName` so that `ResolveField` can compose the canonical error message internally without the caller needing to construct it.
- `ResolveEndpointURL(cmd *cobra.Command) string` — resolves `endpoint_url`
  for auth-exempt commands by reading the persistent `--endpoint-url` flag
  from `cmd.Root().PersistentFlags()` and calling `ResolveField` with
  `required: false`. Returns an empty string (not an error) when no endpoint
  is configured.
- `ClientFromContext(ctx context.Context) *apikit.Client` — retrieves client
  from context using the unexported `clientContextKey{}` type.
- `UserIDFromContext(ctx context.Context) string` — retrieves resolved `user_id`
  from context using the unexported `userIDContextKey{}` type. Returns empty
  string if not set (auth-exempt commands or commands where `user_id` was not
  configured).
- `PrintJSON(v interface{}) error` — prints JSON to stdout.
- `PrintError(err error)` — prints error envelope to stdout. Derives `code`
  from `*apikit.APIError` if applicable (always a non-zero HTTP status ≥ 400),
  otherwise uses `0` as the sentinel for client-side errors. Derives `message`
  from the error's `Error()` string. Writes only to stdout; no stderr output.
- `ExitCode(err error) int` — determines exit code from error type.

---

## Error Handling

| Condition | Exit Code | Behavior |
|-----------|-----------|----------|
| Success | 0 | JSON result to stdout |
| API error (4xx/5xx) | 1 | Error envelope JSON to stdout only; `code` is the HTTP status (always ≥ 400) |
| Config directory unwritable | 2 | Error envelope JSON to stdout; `code` is 0 (client sentinel) |
| Config file unparseable (any parse error) | 2 | Error envelope JSON to stdout; entire file rejected; `code` is 0 (client sentinel) |
| Missing required credential (`endpoint_url` or `api_key`) | 2 | Error envelope JSON to stdout naming the missing field, flag, and env var; `code` is 0 (client sentinel) |
| Missing `user_id` | 0 or 2 | Not an error at `PersistentPreRunE` level; stored as empty string; individual commands that require it return exit-2 if empty |
| Invalid flag value | 2 | Error envelope JSON to stdout; `code` is 0 (client sentinel) |
| Network unreachable | 2 | Error envelope JSON to stdout; `code` is 0 (client sentinel) |
| Unresolvable `$HOME` | 2 | Error envelope JSON to stdout: `"cannot determine home directory: $HOME is not set or unresolvable"`; `code` is 0 (client sentinel) |
| Empty `TokenPrefix` at startup | 2 | Error envelope JSON to stdout: `"TokenPrefix is empty: binary was built without a valid -ldflags TokenPrefix value"`; `code` is 0 (client sentinel) |
| Server unreachable (version cmd) | 0 | Partial result (no server_version) to stdout, warning to stderr |
| `os.Rename` fails during atomic write | 2 | Error envelope JSON to stdout; temp file cleaned up by deferred `os.Remove`; original config intact |
| Concurrent config write (two simultaneous processes) | 0 (last writer wins) | Accepted limitation; last `os.Rename` wins; no error is returned to the user; file-level locking deferred to future iteration |

**Error code `0` is a CLI sentinel** meaning no HTTP status applies. It will
never be returned by the server (server errors are always HTTP status codes
≥ 400) and unambiguously identifies a client-side error condition.

All error envelopes are written to **stdout** (not stderr) so that agents
parsing stdout always receive valid JSON. No stderr output accompanies error
envelopes — the JSON envelope is the complete error representation. Human-readable
context (e.g., warnings for the `version` command when the server is
unreachable) is the sole exception to stdout-only output, and only applies to
non-error informational messages. `Execute()` always completes error output
before returning; `main.go` only calls `os.Exit` with the code from
`ExitCode(err)`.

---

## Testing Strategy

### Unit Tests

- **Config init:** Verify directory created with mode 0700, file created with
  mode 0600. Verify the initial file contains all three keys explicitly set to
  empty strings (matching the hard-coded template string). Verify existing files
  are not modified.
- **Config loading:** Verify TOML parsing into `CLIConfig` struct. Test with
  populated values, empty values, and missing keys. Test with malformed TOML
  (e.g., invalid syntax, unknown field with strict parsing) — verify the entire
  file is rejected with an error and no partial results are returned.
- **Config saving (atomic write):** Verify file written with correct content
  and mode 0600. Verify atomic behavior (temp file + rename). Verify original
  file preserved if write fails. Verify temp file is removed on write failure
  (i.e., no stale temp files left in config directory after a failed write).
  Verify that if `os.CreateTemp` fails (e.g., directory unwritable), the error
  is returned immediately with no temp file left to clean up. Verify that if
  `os.Rename` fails after the temp file is written, the deferred `os.Remove`
  cleans up the temp file and the original `config.toml` remains unchanged.
- **Credential resolution:** Test all precedence levels for `required: true`
  fields (`endpoint_url`, `api_key`):
  - Flag set → flag value wins regardless of env/config.
  - Flag unset, env set → env value wins.
  - Flag unset, env unset, config set (non-empty) → config value wins.
  - All unset → error returned with canonical message format
    (e.g., `"endpoint_url is not set: provide via --endpoint-url, $ENDPOINT_URL, or config file"`).
  - Config empty string → treated as unset.
  Test `required: false` behavior (`user_id`):
  - All unset → returns `("", nil)` (no error).
  - One source set → returns that value normally.
- **Required vs. optional credential fields:** Verify that `PersistentPreRunE`
  returns an exit-2 error when `endpoint_url` is missing. Verify that
  `PersistentPreRunE` returns an exit-2 error when `api_key` is missing.
  Verify that `PersistentPreRunE` succeeds and stores an empty string when
  `user_id` is missing (not an error at this level).
- **ResolveField signature:** Verify the 7-parameter signature
  `(fieldName, flagName, flagValue string, flagChanged bool, envVarName, configValue string, required bool)`
  produces the correct canonical error message from `fieldName`, `flagName`, and
  `envVarName`. Verify the `required: false` path returns `("", nil)` when all
  sources are unset.
- **ResolveEndpointURL helper:** Verify that it reads from the persistent flag,
  falls back to env var and config, and returns an empty string (not an error)
  when no endpoint is configured anywhere.
- **Context key safety:** Verify that `ClientFromContext` and `UserIDFromContext`
  return nil/empty when called with a plain context, and return the correct
  values after being stored with the unexported key types. Verify that a
  consuming project storing its own value under a string key `"client"` does
  not interfere with `ClientFromContext`.
- **Output formatting:** Verify `PrintJSON` produces valid JSON to stdout.
  Verify `PrintError` produces the error envelope format on stdout with no
  stderr output. Verify `PrintError` with an `*apikit.APIError` uses the
  error's code (a non-zero HTTP status ≥ 400) and message. Verify `PrintError`
  with a plain error uses code 0 (client sentinel). Verify that `code: 0`
  is never produced by `PrintError` when given an `*apikit.APIError`.
- **Exit code determination:** Verify `*apikit.APIError` → 1, plain error → 2,
  nil → 0.
- **Build-time variables:** Verify default values ("dev", "unknown", "ak") are
  present when not overridden.
- **TokenPrefix validation:** Verify that an empty `TokenPrefix` causes
  `PersistentPreRunE` to return an exit-2 error with the expected message and
  `code: 0` before any config initialization is attempted. Verify the check
  occurs after the auth-annotation check but before the `$HOME` check.
- **Auth annotation opt-out:** Verify that commands with `"auth": "none"` skip
  client creation in `PersistentPreRunE`. Verify that commands without the
  annotation (or with `"auth": "api_key"` / `"auth": "admin"`) proceed through
  the full credential resolution and client construction path.
- **`PersistentPreRunE` receives leaf command:** Verify that the `cmd` argument
  in `PersistentPreRunE` is the leaf command (not the root), specifically that
  its `Annotations` map reflects the leaf command's annotations, not the root's.
  This test guards against the known Cobra pitfall.
- **Unresolvable `$HOME`:** Simulate an unresolvable home directory and verify
  exit code 2 with the expected error envelope message and `code: 0`.
- **Hook chaining:** Verify that a child command defining only `PreRunE` (not
  `PersistentPreRunE`) still triggers the root's `PersistentPreRunE`. Verify
  that a child command defining `PersistentPreRunE` shadows the root's hook
  (negative test — this is the forbidden pattern documented as a constraint).
- **user_id context storage:** Verify that `UserIDFromContext` returns the
  resolved `user_id` after `PersistentPreRunE` runs, and that it is independent
  of the `apikit.Client` stored under `clientContextKey{}`. Verify that
  `UserIDFromContext` returns an empty string (not an error) when `user_id` was
  not configured.
- **Flag descriptor defaults:** Verify that bool flags with default `false`,
  string flags with default `""`, and int flags with default `0` all omit the
  `default` field in `help --json` output. Verify that a bool flag with default
  `true` includes `"default": true`.
- **`--json` flag on non-help commands:** Verify that passing `--json` to a
  non-help command (e.g., `akc version --json`) produces no error, no warning,
  and identical output to the command invoked without `--json`.
- **`help --json` routing:** Verify that `akc help --json` outputs the full
  JSON tree. Verify that `akc help --json user` also outputs the full JSON tree
  (positional args after `--json` are ignored when `--json` is set). Verify
  that `akc help user --json` (flag after positional arg) also outputs the
  full JSON tree — Cobra parses persistent flags regardless of position.
- **`help --json` built-in filtering:** Verify that Cobra internal commands
  lacking any annotation key are excluded from `help --json` output. Verify
  that a command carrying at least an `"auth"` annotation is included.
- **Leaf command with children:** Verify that a command with both `RunE` and
  child subcommands appears in `help --json` output as a leaf entry.
- **Malformed config file:** Verify that a `config.toml` containing partially
  valid TOML (e.g., two valid fields and one syntactically invalid field)
  causes `LoadConfig` to return an error and produce no `CLIConfig` struct.
- **Execute() error interception:** Verify that when a `RunE` returns an error,
  `Execute()` calls `PrintError` (writing the envelope to stdout) before
  returning. Verify that `Execute()` does not call `PrintError` when `RunE`
  returns nil. Verify that `RunE` functions do not call `PrintError` directly.
- **VersionOutput struct:** Verify that `VersionOutput.ServerVersion` is the
  SDK's `*apikit.VersionResponse` type and that JSON serialization produces
  field names matching the documented example (`version`, `build_time`,
  `commit`, `mount_point`).
- **InitConfig hard-coded template:** Verify that the initial `config.toml`
  written by `InitConfig` contains exactly the three lines
  `endpoint_url = ""`, `user_id = ""`, and `api_key = ""`, confirming the
  hard-coded template approach (not relying on encoder behavior).

### Integration Tests

- **Root command initialization:** Verify that running `akc` with no args
  prints help text to **stdout** (Cobra's default behavior for commands without
  `RunE`) and exits 0. Verify nothing is printed to stderr.
- **Version command (no endpoint configured):** Verify JSON output contains
  `cli_version`, `build`, and `prefix`. Verify `server_version` is omitted.
  Verify no warning is printed to stderr (no endpoint is a normal state).
- **Version command (endpoint configured, server unreachable):** Verify
  `server_version` is omitted from JSON. Verify a warning is printed to stderr.
- **Version command (mock server):** Verify `server_version` is included when
  a mock server is reachable. Verify the `server_version` sub-object fields
  match the SDK's `VersionResponse` struct field names.
- **Help JSON (full tree):** Verify `akc help --json` returns valid JSON with
  the expected structure (name, version, commands array). Verify commands have
  required fields. Verify only leaf commands (those with `RunE` and at least
  one annotation key) appear — no bare group commands like `user` or `admin`
  are present as standalone entries unless they also have `RunE` and an annotation.
  Uses the actual registered Cobra command tree with stub subcommands registered
  to validate the leaf-only filtering behavior.
- **Help JSON built-in exclusion:** Verify that Cobra built-in commands (if any
  are registered) do not appear in `help --json` output due to the annotation
  filter. Verify the custom `help` subcommand itself does not appear as a
  redundant entry.
- **Help named subcommand (no --json):** Verify `akc help user` shows standard
  Cobra help text for the `user` command group (delegation to Cobra's default
  behavior). Uses the actual Cobra tree with a registered `user` command group
  stub. Verify this does not output JSON.
- **`akc help user --json` flag position:** Verify that `akc help user --json`
  (flag after positional arg) outputs the full JSON tree, identical to
  `akc help --json user`. Cobra's persistent flag parsing is position-agnostic.
- **Per-command help JSON:** Verify `akc version --help --json` returns a
  single command entry (via `SetHelpFunc` + persistent `--json` flag).
- **Persistent flags:** Verify `--endpoint-url`, `--user-id`, `--api-key` are
  recognized on any subcommand.
- **Env var resolution:** Set env vars (`ENDPOINT_URL`, `USER_ID`, `API_KEY`),
  verify they override config file values.
- **Flag override:** Set flag, verify it overrides both env var and config.
- **Error envelope — API error:** Trigger an API error (mocked), verify JSON
  error envelope on stdout with `code` ≥ 400 and exit code 1. Verify nothing
  is printed to stderr for this error path.
- **Error envelope — client error:** Trigger a client error (e.g., missing
  `endpoint_url` credential), verify JSON error envelope on stdout with
  `code: 0` and exit code 2. Verify nothing is printed to stderr. Verify the
  canonical error message format is used.
- **Missing user_id not an error:** Trigger a command with `endpoint_url` and
  `api_key` configured but `user_id` missing; verify that `PersistentPreRunE`
  succeeds and the command runs (or fails for its own reasons, not due to
  missing `user_id`).
- **Config directory creation:** Run in a temp directory, verify config dir
  and file are created with correct permissions. Verify the written file matches
  the hard-coded template (all three keys with empty string values).
- **Malformed config file (integration):** Place a malformed `config.toml` in
  the config directory, run any authenticated command, verify exit code 2 and
  a JSON error envelope with `code: 0` on stdout.
- **Auth annotation skip:** Run `akc version` with no credentials configured,
  verify it does not fail with a missing-credential error.
- **Execute() error contract:** Trigger an error condition (e.g., missing
  required credential) and verify that `Execute()` writes the error envelope
  to stdout before returning, so `main.go` does not need to print anything
  additional.
- **SilenceErrors/SilenceUsage:** Trigger an invalid flag and verify that
  Cobra's own error/usage text is suppressed; only the CLI's JSON error
  envelope appears on stdout.
- **`--json` on non-help command (integration):** Run `akc version --json`
  and verify output is identical to `akc version` with no `--json` flag.
- **Atomic write rename failure:** Simulate `os.Rename` failure in `SaveConfig`
  (e.g., by making the config directory read-only after `os.CreateTemp`
  succeeds), verify that the temp file is cleaned up and the original
  `config.toml` is unchanged, and verify that the error is returned as exit
  code 2.
- **TokenPrefix empty string (integration):** Build or configure a test binary
  with an empty `TokenPrefix` and verify exit code 2 with the expected error
  envelope on stdout.
- **Integration test stubs:** Stub command groups (`user`, `admin`) required
  for `help --json` and `help <subcommand>` integration tests are test-only
  fixtures registered in test setup code within `help_test.go`. They are not
  part of the spec 13 deliverables. Specs 14 and 15 provide the real
  implementations; the stubs exist only to validate the walker's filtering and
  delegation behavior in isolation.

---

## Design Decisions

- **CLI wraps the Go SDK exclusively.** The CLI does not make HTTP calls
  directly. This ensures the CLI and SDK stay in sync and that the SDK is the
  single implementation of API client logic in Go. If the SDK method doesn't
  exist, the CLI command cannot exist.
- **`internal/cli/` package.** The CLI command implementations are internal
  because consuming projects don't import individual commands — they embed the
  entire command tree via `RootCommand()`. The internal package prevents
  accidental coupling to CLI internals.
- **Root-package shim for `RootCommand()`.** The public `RootCommand()` in the
  root `apikit` package is a thin shim delegating to `internal/cli`. This
  keeps the exported API surface minimal and the implementation fully internal.
- **Unexported struct-typed context keys.** Using `type clientContextKey struct{}`
  and `type userIDContextKey struct{}` prevents key collisions with consuming
  projects. No external package can construct these key values, so no accidental
  or malicious shadowing is possible. This is the idiomatic Go pattern for
  context key safety.
- **`user_id` stored in context, not in the client.** The Go SDK `Client` is
  initialized with `endpoint_url` and `api_key` only; these are the credentials
  needed for API authentication. `user_id` is a CLI-level value that some
  commands need (e.g., to identify the current user for display). Storing it
  separately in the context via `UserIDFromContext` avoids widening the SDK
  client interface for a CLI-only concern. A missing `user_id` is not an error
  at the `PersistentPreRunE` level; it is stored as an empty string.
- **`endpoint_url` and `api_key` are required; `user_id` is optional.**
  `PersistentPreRunE` fails if `endpoint_url` or `api_key` cannot be resolved.
  `user_id` is resolved with best-effort and stored as an empty string if
  unavailable. This allows commands that don't need `user_id` to succeed in
  environments where it is not configured, without widening or complicating
  `PersistentPreRunE`.
- **`ResolveField` accepts a `required` bool parameter.** Rather than having
  the caller conditionally discard errors for optional fields, `ResolveField`
  encodes the required/optional distinction internally. When `required` is
  `false`, a missing value returns `("", nil)`. This keeps the call site for
  `user_id` clean and symmetrical with the required fields, and makes the
  optional semantics self-documenting at the call site.
- **`Execute()` wraps `rootCmd.Execute()` for centralized error printing.**
  Rather than having each `RunE` call `PrintError`, the `Execute()` function
  checks the error returned by `rootCmd.Execute()` and calls `PrintError`
  centrally. This guarantees exactly one `PrintError` call per error, prevents
  duplicate envelopes, and ensures no `RunE` implementation accidentally omits
  error printing. `RunE` functions only call `PrintJSON` on success.
- **Root-only `PersistentPreRunE` with child `PreRunE`.** Cobra automatically
  calls a parent's `PersistentPreRunE` before a child's `PreRunE`. By
  restricting specs 14 and 15 to `PreRunE` only, client initialization happens
  transparently without manual hook chaining. Child commands defining
  `PersistentPreRunE` would shadow the root's hook — this is a forbidden pattern
  documented as a constraint.
- **Cobra passes the leaf command as `cmd` in `PersistentPreRunE`.** This is
  standard Cobra behavior and is explicitly documented to prevent the common
  implementor mistake of reading annotations from the wrong command. The
  `"auth": "none"` check reads `cmd.Annotations` where `cmd` is the leaf
  command, not `rootCmd.Annotations`. A unit test verifies this behavior.
- **`ResolveEndpointURL` helper for auth-exempt commands.** Auth-exempt commands
  that skip `PersistentPreRunE` but still optionally need `endpoint_url` (like
  `version`) use this helper to read the persistent flag and apply the
  resolution precedence chain. It calls `ResolveField` with `required: false`,
  returning an empty string rather than an error because the endpoint is optional
  for these commands. Any auth-exempt command needing other credentials should
  follow the same pattern of reading persistent flags directly via
  `cmd.Root().PersistentFlags()`.
- **Canonical error message format for `ResolveField`.** The format
  `"<field> is not set: provide via <flag>, $<ENV_VAR>, or config file"` is
  short, actionable, and consistent across all three credential fields. It is
  specified as the canonical format to prevent implementor variation and to make
  integration tests deterministic. `ResolveField` composes the message
  internally from `fieldName`, `flagName`, and `envVarName` parameters — the
  caller never constructs the message string directly.
- **Dedicated `help` subcommand + `SetHelpFunc` for `--json`.** The `help`
  subcommand handles `akc help --json` (full tree) and delegates non-JSON
  invocations (including `akc help <subcommand>`) to Cobra's default help
  behavior. `SetHelpFunc` handles `akc <cmd> --help --json` (single command).
  Both check the persistent `--json` flag. This avoids hacking Cobra's built-in
  `--help` processing while still supporting both invocation styles, and
  preserves named-subcommand help (`akc help user`) without any custom routing.
- **`help --json` outputs full tree regardless of flag position or positional args.**
  Because `--json` is a persistent flag, Cobra parses it regardless of where it
  appears in the `akc help` invocation — before or after positional arguments.
  All of `akc help --json`, `akc help --json user`, and `akc help user --json`
  output the full JSON tree. This simple, position-agnostic rule avoids
  ambiguity and is trivial to implement and test.
- **`--json` silently ignored by non-help commands.** Because `--json` is a
  persistent flag, Cobra accepts it on any command without error. Non-help
  commands simply do not consult it. This avoids special-casing or error
  messaging for a flag that was intentionally designed as optional and contextual.
  Documenting this behavior prevents implementors from adding defensive checks.
- **`code: 0` as a CLI sentinel for client-side errors.** Server errors are
  always HTTP status codes (≥ 400); they are never 0. Using `code: 0` for
  client-side errors (missing config, invalid flags, etc.) creates a clean
  semantic distinction. `PrintError` enforces this: API errors use the error's
  HTTP status code, all other errors use 0. This is documented in both the
  error envelope spec and the error handling table.
- **Config file rejected entirely on any parse error.** The config file has
  only three string fields. A parse error indicates corruption or a bad manual
  edit. Accepting partial results would silently use incorrect values, which
  could be worse than failing loudly. The user must fix or delete the file.
  This is the simplest, safest behavior.
- **`RunE` presence is the sole leaf criterion for `help --json`.** A command
  with both `RunE` and child subcommands is treated as a leaf and included in
  the output. This is the simplest rule — no dual-mode handling, no exclusion
  logic based on the presence of children. Agents can invoke any entry in the
  output directly.
- **`help --json` filters commands by annotation presence.** Rather than
  maintaining a name-based exclusion list for Cobra built-ins (e.g.,
  `completion`), the walker uses "has at least one annotation key" as the
  criterion for an apikit-owned command. Cobra built-ins carry no annotations;
  all apikit commands carry at minimum `"auth"`. Consuming project commands
  that want to appear in the output must also carry at least `"auth"`. This
  is documented in the Embeddable Command Tree section and the Consuming Project
  Integration subsection of the Agent Interface. The `completion` command is
  additionally excluded structurally because it is never registered (shell
  completion is a non-goal).
- **`SilenceErrors` and `SilenceUsage` on root.** All output is under the
  CLI's control. Cobra's default error/usage printing is suppressed. `Execute()`
  prints error envelopes before returning; `main.go` only sets the exit code.
  `SilenceUsage` suppresses usage output on errors only; it does not suppress
  the default help text printed when the root command is invoked with no args
  and no `RunE`.
- **Root command has no `RunE`.** Cobra's default behavior for a command
  without `RunE` is to print help text to stdout on bare invocation and exit 0.
  This is intentional — `akc` with no arguments should guide users to available
  subcommands.
- **Error envelopes on stdout only, no stderr for errors.** All error output
  is the JSON envelope on stdout. No human-readable message accompanies errors
  on stderr. This ensures agents always receive valid JSON on stdout and can
  parse errors without stream-splitting. The sole exception is informational
  warnings (e.g., server unreachable in `version`) which are not errors.
- **`PrintError` derives code and message internally.** `PrintError(err error)`
  checks `errors.As(err, &apiErr)` to determine the code and extracts the
  message from the error. Callers don't construct an envelope struct — they
  pass the raw error, and `PrintError` handles the distinction between API
  errors (code from `apiErr.Code`, always ≥ 400) and client errors (code 0).
  This prevents implementors from producing inconsistent envelopes.
- **Deferred temp-file cleanup in atomic writes.** `defer os.Remove(tmpFile.Name())`
  registered immediately after `os.CreateTemp` ensures temp files are cleaned
  up on any failure path, including `os.Rename` failure. On success, `os.Remove`
  on the renamed-away path is a safe no-op. If `os.CreateTemp` itself fails, no
  deferred remove is registered and no cleanup is needed.
- **Atomic config writes use the same directory.** The temp file is created in
  the config directory to guarantee `os.Rename` is a same-filesystem operation.
  This eliminates any possibility of a cross-device link error from `os.Rename`.
- **Concurrent config write is an accepted limitation.** The atomic rename
  prevents partial writes. Simultaneous writes result in the last rename
  winning — no data corruption, but potentially a lost update. This is
  acceptable for a single-user CLI tool. File-level locking is deferred.
- **Credential resolution is lazy.** Commands that don't need all three
  credentials (e.g., `version` only optionally uses `endpoint_url`) don't fail
  because `api_key` is missing. Resolution happens per-field, per-command.
  Commands with `"auth": "none"` bypass resolution entirely via the
  `PersistentPreRunE` annotation check.
- **Auth annotation drives `PersistentPreRunE` skip.** Using `"auth": "none"`
  in Cobra's `Annotations` map as the opt-out signal is consistent with how the
  `help --json` walker already reads annotations. There is no separate skip
  list or command-name registry — the annotation is the single source of truth
  for whether a command needs a client.
- **Default auth is `"api_key"`.** Commands that omit `"auth"` from their
  annotations are treated as requiring full credential resolution and client
  construction. A command must explicitly opt out with `"auth": "none"`. This
  is the secure default.
- **Flag `default` field omitted for zero values, uniformly.** Whether a flag
  is a string, int, bool, or stringSlice, the `default` field is omitted from
  the `help --json` descriptor when the default equals the type's zero value.
  Agents treat absence of `default` as the zero value. This is consistent,
  simple to implement, and avoids cluttering the output with `"default": false`
  or `"default": ""` entries that convey no information.
- **`help --json` collects only leaf commands.** Group/parent commands (e.g.,
  `user`, `admin`) without `RunE` are not included in the agent interface
  output. Commands with both `RunE` and children are included as leaves. An
  agent infers groupings from name prefixes. This avoids ambiguity between
  executable commands and navigational groupings, and keeps the command list
  actionable.
- **`help --json` schema versioning deferred.** Adding a `schema_version` field
  is deferred to a future iteration. The `version` field in the root JSON object
  serves as an indirect indicator of schema changes in the interim. When schema
  versioning is added, it will be a non-breaking addition to the existing
  structure.
- **`VersionOutput` struct embeds `*apikit.VersionResponse` directly.**
  Rather than duplicating the server version fields, `VersionOutput` uses the
  SDK's typed struct as its `ServerVersion` field. This ensures field names and
  values stay in sync with the SDK without manual duplication and makes the
  spec 12/13 coupling explicit and compile-time verifiable.
- **`BurntSushi/toml` marshaling via `toml.NewEncoder`.** The
  `github.com/BurntSushi/toml` package provides `toml.NewEncoder(w).Encode(v)`
  for marshaling (writing to an `io.Writer`) and `toml.Decode` / `toml.NewDecoder`
  for unmarshaling. This is the correct API for the version in use; callers
  must not assume a top-level `toml.Marshal` function exists.
- **Hard-coded template string for `InitConfig`.** Rather than relying on
  `toml.NewEncoder` to write the initial config file (which may omit zero-value
  string keys), `InitConfig` writes from a hard-coded template string. This
  guarantees all three keys are always present in the file and the format is
  stable regardless of encoder version behavior.
- **Unprefixed env var names match the master PRD.** `ENDPOINT_URL`, `USER_ID`,
  and `API_KEY` are used without a prefix such as `AKC_`. This is intentional
  and follows the master PRD convention. The CLI's single-config design does
  not require namespacing at the env var level. Consuming projects that require
  namespaced env vars can define their own wrapper layer; the CLI does not
  provide a prefix mechanism for env var names.
- **Build-time variables in `internal/cli`.** Variables live in the internal
  package because only the CLI uses them. The server has its own version info
  mechanism. `-ldflags` injects values directly into package-level vars.
- **`TokenPrefix` must be coordinated across server and CLI builds.** Both
  the server and the CLI use `TokenPrefix` — the server for token parsing, the
  CLI for the config directory path. A build-time mismatch produces subtle
  bugs. Operators must use a shared build variable (e.g., a `Makefile` variable
  or CI environment variable) to keep them in sync. An empty `TokenPrefix` is
  caught at startup and produces an immediate exit-2 error.
- **`json.MarshalIndent` for output.** Pretty-printed JSON is easier for
  humans to read during development. Agents parse it fine. The two-space
  indent is conventional.
- **Cobra Annotations for command metadata.** Using Cobra's built-in
  `Annotations` map avoids custom type registries. The `help --json` walker
  reads annotations generically, which is why consuming project commands
  automatically appear — no registration step needed.
- **`help --json` walks the Cobra tree.** Rather than maintaining a separate
  command registry, the agent interface derives its output from the live Cobra
  tree. This guarantees the JSON output matches the actual CLI behavior,
  including commands added by consuming projects.
- **Version command is composite.** It touches two sources (local build info +
  remote server version), so method/path is null and composite is true.
- **`TokenPrefix` default is "ak".** This matches the master PRD's example
  prefix. Consuming projects override it at build time to use their own
  branding (e.g., "myapp" → `$HOME/.myapp/config.toml`).
- **Integration test stubs are test-only fixtures.** Stub command groups
  (`user`, `admin`) needed for help walker and delegation integration tests
  live in `help_test.go` test setup code. They are not deliverables of spec 13.
  The real implementations are provided by specs 14 and 15.

---

## Glossary

| Term | Definition |
|------|------------|
| **akc** | The CLI binary name. Short for "apikit client." |
| **TokenPrefix** | A build-time variable that determines the config directory name (`$HOME/.<prefix>/`) and is used in version display. Default: "ak". Must be coordinated with the server build. An empty value is caught at startup and causes an immediate exit-2 error. |
| **config directory** | `$HOME/.<prefix>/` — the directory holding `config.toml`. Created with mode 0700. |
| **credential resolution** | The four-level precedence chain (flag > env > config > error) used to determine the value of `endpoint_url`, `user_id`, and `api_key` for each CLI invocation. `endpoint_url` and `api_key` are required for non-auth-exempt commands; `user_id` is optional (stored as empty string if missing). |
| **atomic write** | A write strategy that creates a temp file (in the same directory as the target, with deferred `os.Remove` for cleanup on failure, including `os.Rename` failure) and renames it into place, preventing partial writes from corrupting the target file. Using the same directory ensures `os.Rename` is always a same-filesystem operation. |
| **error envelope** | The JSON structure `{"error":{"code":<int>,"message":"<string>"}}` written to stdout on failure. Matches the API's error response format. `code` is a non-zero HTTP status (≥ 400) for server errors, or `0` (CLI sentinel) for client-side errors. No stderr output accompanies error envelopes. |
| **error code sentinel (0)** | The value `code: 0` in an error envelope, indicating a client-side error with no associated HTTP status code. Server errors always use HTTP status codes (≥ 400); `0` is never returned by the server. |
| **composite command** | A CLI command that involves multiple API calls or non-API operations (e.g., `login`, `version`). In `help --json`, composite commands have `method: null`, `path: null`, and `composite: true`. |
| **command tree** | The hierarchy of Cobra commands that make up the CLI. Can be embedded into consuming projects via `RootCommand()`. |
| **agent interface** | The structured discoverability mechanism (`help --json`) that enables LLMs and autonomous agents to discover and use the CLI programmatically. Only leaf commands (those with `RunE` and at least one annotation key) appear in the output, including commands with both `RunE` and child subcommands. Schema versioning is deferred to a future iteration; the `version` field serves as an interim indicator. |
| **consuming project** | A Go project that imports apikit as a library and optionally embeds the CLI command tree via `RootCommand()` and `AddCommand`. |
| **auth annotation** | The `"auth"` key in a Cobra command's `Annotations` map. Valid values: `"none"`, `"api_key"`, `"admin"`. Commands with `"auth": "none"` skip `PersistentPreRunE` client creation entirely. Commands without `"auth"` default to `"api_key"`. |
| **leaf command** | A Cobra command that has a `RunE` (or `Run`) function set and at least one entry in its `Annotations` map. Only leaf commands are collected by the `help --json` walker. Group/parent commands without `RunE` are excluded; commands with both `RunE` and children are included. Annotation-less commands (e.g., Cobra built-ins) are also excluded by the walker. |
| **context key safety** | The use of unexported struct types (`clientContextKey{}`, `userIDContextKey{}`) as `context.WithValue` keys to prevent collision with consuming project code. |
| **hook chaining constraint** | The rule that only the root command may define `PersistentPreRunE`. Commands in specs 14 and 15 use `PreRunE` only, allowing Cobra's automatic parent-hook invocation to work correctly. |
| **ResolveEndpointURL** | A package-level helper for auth-exempt commands that need to optionally resolve `endpoint_url` without going through `PersistentPreRunE`. Calls `ResolveField` with `required: false`; returns empty string (not an error) when no endpoint is configured. |
| **`--json` flag** | A persistent flag defined on the root command. Meaningful only in help contexts (`akc help --json`, `akc <cmd> --help --json`). Non-help commands silently ignore it with no warning or error. Position-agnostic: Cobra parses it regardless of where it appears relative to positional arguments. |
| **Execute() wrapper** | The `Execute()` function in `internal/cli` that wraps `rootCmd.Execute()`. It centrally calls `PrintError` on any non-nil error before returning, ensuring exactly one error envelope is produced per error and no `RunE` function needs to call `PrintError` directly. |
| **VersionOutput** | The Go struct defining the `akc version` JSON output. Contains `cli_version`, `build`, `prefix` (always present), and `server_version` (a pointer to the SDK's `apikit.VersionResponse`, omitted when nil). |
| **canonical error message format** | The format `"<field> is not set: provide via <flag>, $<ENV_VAR>, or config file"` used by `ResolveField` for missing-credential errors. Consistent across all three credential fields. Composed internally by `ResolveField` from the `fieldName`, `flagName`, and `envVarName` parameters. |
| **annotation filter** | The rule used by the `help --json` walker to distinguish apikit-owned commands from Cobra built-ins: a command must have at least one entry in its `Annotations` map to appear in the output. All apikit commands carry at minimum the `"auth"` annotation. |
| **required bool (ResolveField)** | The seventh parameter of `ResolveField`. When `true`, a missing value produces an error with the canonical message. When `false`, a missing value returns `("", nil)`. Used to implement optional credential resolution for `user_id` and auth-exempt helpers. |
| **hard-coded config template** | The literal string used by `InitConfig` to write the initial `config.toml`, ensuring all three keys (`endpoint_url`, `user_id`, `api_key`) always appear in the file regardless of TOML encoder behavior with zero-value fields. |
| **concurrent config write** | Simultaneous writes to `config.toml` by two CLI processes. The atomic rename prevents partial writes; the last rename wins. This is an accepted limitation; file-level locking is deferred. |

---

## Owner

Michael Kuehl
