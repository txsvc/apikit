# Building Custom CLI Commands

apikit's CLI is designed to be extended. Your project gets the built-in
commands (login, user, keys, tokens, orgs, admin) for free, and you add
custom commands that call your own API endpoints using the same
authenticated client, error handling, and JSON output conventions.

## Architecture

```
apikit.RootCommand()       <-- base command tree (version, help, flags)
  ├─ apikit.LoginCmd()     <-- built-in commands
  ├─ apikit.UserCmd()
  ├─ apikit.KeysCmd()
  ├─ ...
  └─ yourCmd()             <-- your custom command group
       ├─ listCmd()
       └─ createCmd()
```

When a command runs, apikit's `PersistentPreRunE` on the root command
resolves credentials using the four-level precedence chain (CLI flags,
environment variables, config file, error) and injects an authenticated
`CLIClient` into the Cobra context. Your custom commands retrieve the
client with `CLIClientFromCmd` and make API calls — no manual credential
handling needed.

## Quick Start

```go
package main

import (
    "net/http"
    "os"

    "github.com/txsvc/apikit"
)

func main() {
    root := apikit.RootCommand()
    root.Use = "mycli"

    root.AddCommand(
        apikit.LoginCmd(),
        apikit.UserCmd(),
        apikit.KeysCmd(),
        apikit.TokensCmd(),
        apikit.OrgsCmd(),
        apikit.AdminCmd(),
        widgetListCmd(),  // your custom command
    )

    err := apikit.CLIExecute()
    if err != nil {
        apikit.CLIPrintError(err)
    }
    os.Exit(apikit.CLIExitCode(err))
}
```

See `examples/cli/` for a complete working example with CRUD commands.

---

## API Reference

### CLIClient

The authenticated HTTP client for CLI commands. Handles Bearer-token
injection, JSON marshaling, the `/api/v1` path prefix, and server error
envelope decoding.

#### Obtaining a client

```go
// From a Cobra command context (preferred — uses resolved credentials):
client, err := apikit.CLIClientFromCmd(cmd)

// Direct construction (for standalone scripts or testing):
client := apikit.NewCLIClient("https://api.example.com", "ak_key_secret")
```

#### DoRequest

```go
func (c *CLIClient) DoRequest(ctx context.Context, method, path string, body any) (any, error)
```

Makes an authenticated request to `<endpoint>/api/v1<path>`. The `body`
is marshaled to JSON (pass `nil` for bodyless requests). Returns the
decoded JSON response as `any` (`map[string]any` for objects, `[]any`
for arrays). On 4xx/5xx responses, decodes the server's error envelope
and returns a `*CLIError`.

```go
// GET /api/v1/widgets
result, err := client.DoRequest(ctx, http.MethodGet, "/widgets", nil)

// POST /api/v1/widgets with JSON body
body := map[string]string{"name": "gear"}
result, err := client.DoRequest(ctx, http.MethodPost, "/widgets", body)
```

#### DoRequestRaw

```go
func (c *CLIClient) DoRequestRaw(ctx context.Context, method, path string, body any) ([]byte, int, error)
```

Like `DoRequest` but returns the raw response bytes and HTTP status code
instead of decoded JSON. Use this when you need to unmarshal into a
specific Go struct:

```go
data, status, err := client.DoRequestRaw(ctx, http.MethodGet, "/widgets", nil)
if err != nil {
    return apikit.CLIHandleError(cmd, err)
}

var widgets []Widget
if err := json.Unmarshal(data, &widgets); err != nil {
    return apikit.CLIHandleError(cmd,
        apikit.NewCLIError(2, "unexpected response format"))
}
```

#### Accessors

```go
client.EndpointURL()  // configured server endpoint URL
client.APIKey()       // configured API key
```

---

### Output Functions

#### CLIPrintResult

```go
func CLIPrintResult(cmd *cobra.Command, v any) error
```

Writes `v` as indented JSON (two-space indent, no HTML escaping) to
`cmd`'s stdout. Use for successful command output.

```go
return apikit.CLIPrintResult(cmd, result)
```

#### CLIHandleError

```go
func CLIHandleError(cmd *cobra.Command, err error) error
```

Writes a JSON error envelope (`{"error": {"code": N, "message": "..."}}`)
to `cmd`'s stdout and returns the error wrapped so that `CLIPrintError`
won't double-print it. Use this as the return value from `RunE`.

```go
if err != nil {
    return apikit.CLIHandleError(cmd, err)
}
```

#### NewCLIError

```go
func NewCLIError(code int, message string) *CLIError
```

Creates a typed error with a code and message. Use code `2` for
client-side validation errors (convention: 1 = API error, 2 = client
error). The error is rendered by `CLIHandleError` into the standard
JSON error envelope.

```go
if name == "" {
    return apikit.CLIHandleError(cmd,
        apikit.NewCLIError(2, "--name flag is required"))
}
```

---

### Utilities

#### CLIResolveOrgSlug

```go
func CLIResolveOrgSlug(ctx context.Context, client *CLIClient, slug string) (string, error)
```

Resolves an organization slug (e.g., `"acme"`) to its UUID by listing
the authenticated user's organizations. Useful when your custom commands
accept human-friendly org slugs.

```go
orgID, err := apikit.CLIResolveOrgSlug(cmd.Context(), client, orgSlug)
if err != nil {
    return apikit.CLIHandleError(cmd, err)
}
body["org_id"] = orgID
```

---

## Patterns

### Standard CRUD command

This is the recommended pattern for a custom command:

```go
func widgetListCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "list",
        Short: "List all widgets",
        Args:  cobra.NoArgs,
        RunE: func(cmd *cobra.Command, args []string) error {
            // 1. Get the authenticated client.
            client, err := apikit.CLIClientFromCmd(cmd)
            if err != nil {
                return apikit.CLIHandleError(cmd, err)
            }

            // 2. Make the API call.
            result, err := client.DoRequest(cmd.Context(),
                http.MethodGet, "/widgets", nil)
            if err != nil {
                return apikit.CLIHandleError(cmd, err)
            }

            // 3. Print the result.
            return apikit.CLIPrintResult(cmd, result)
        },
    }
}
```

### Command with flags

```go
func widgetCreateCmd() *cobra.Command {
    var name string

    cmd := &cobra.Command{
        Use:   "create",
        Short: "Create a widget",
        RunE: func(cmd *cobra.Command, args []string) error {
            if name == "" {
                return apikit.CLIHandleError(cmd,
                    apikit.NewCLIError(2, "--name flag is required"))
            }

            client, err := apikit.CLIClientFromCmd(cmd)
            if err != nil {
                return apikit.CLIHandleError(cmd, err)
            }

            body := map[string]string{"name": name}
            result, err := client.DoRequest(cmd.Context(),
                http.MethodPost, "/widgets", body)
            if err != nil {
                return apikit.CLIHandleError(cmd, err)
            }

            return apikit.CLIPrintResult(cmd, result)
        },
    }

    cmd.Flags().StringVar(&name, "name", "", "Widget name (required)")
    return cmd
}
```

### Destructive command with confirmation

```go
func widgetDeleteCmd() *cobra.Command {
    var confirm bool

    cmd := &cobra.Command{
        Use:  "delete <id>",
        Args: cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            if !confirm {
                return apikit.CLIHandleError(cmd,
                    apikit.NewCLIError(2, "--confirm is required"))
            }

            client, err := apikit.CLIClientFromCmd(cmd)
            if err != nil {
                return apikit.CLIHandleError(cmd, err)
            }

            _, err = client.DoRequest(cmd.Context(),
                http.MethodDelete, "/widgets/"+args[0], nil)
            if err != nil {
                return apikit.CLIHandleError(cmd, err)
            }

            // Human-readable confirmation goes to stderr.
            fmt.Fprintf(cmd.ErrOrStderr(), "Widget deleted.\n")
            return nil
        },
    }

    cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm deletion")
    return cmd
}
```

### Human-readable messages

Follow apikit's output convention:

- **stdout** -- JSON data output only (machine-readable)
- **stderr** -- human-readable messages (progress, confirmations, warnings)

```go
// Good: human messages to stderr
fmt.Fprintln(cmd.ErrOrStderr(), "Widget created")

// Bad: human messages to stdout (breaks piping and scripting)
fmt.Fprintln(cmd.OutOrStdout(), "Widget created")
```

### Auth-exempt commands

If a custom command does not need authentication (like `version`), annotate
it so `PersistentPreRunE` skips credential resolution:

```go
cmd := &cobra.Command{
    Use: "status",
    Annotations: map[string]string{"auth": "none"},
    RunE: func(cmd *cobra.Command, args []string) error {
        // No client needed — this command works without login.
        fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
        return nil
    },
}
```

---

## Build-Time Configuration

Custom CLIs should inject their own prefix and version via `-ldflags`:

```bash
go build -ldflags "\
  -X github.com/txsvc/apikit/internal/cli.Version=1.0.0 \
  -X github.com/txsvc/apikit/internal/cli.Build=$(git rev-parse --short HEAD) \
  -X github.com/txsvc/apikit/internal/cli.TokenPrefix=myapp" \
  -o myapp ./cmd/myapp
```

The `TokenPrefix` determines the config directory name
(`$HOME/.<prefix>/config.toml`). Each custom CLI should use its own prefix
to avoid colliding with other apikit-based CLIs.
