# apikit examples

Starter templates for building projects with apikit. Copy either directory into your own project and adjust the module path in `go.mod`.

## server/

A custom API server with all built-in apikit handlers (OAuth, users, orgs, keys, PATs) plus a custom route.

```bash
cd server
make build

# First run: bootstrap with an admin email
make reset

# Subsequent runs
make run
```

The server starts on `http://localhost:8080`. Edit `config.toml` to configure the port, database path, and GitHub OAuth credentials.

**What it demonstrates:**

- `LoadConfig` / `OpenDatabase` / `Bootstrap` / `NewServer` lifecycle
- `MountHandlers` to register all built-in routes
- Adding custom routes on `server.APIGroup()` alongside the defaults
- Health checker wiring with `database.Ping`

## cli/

A custom CLI that embeds apikit's base command tree (version, help) and adds custom API-backed commands.

```bash
cd cli
make build
bin/mycli version
bin/mycli widget list
bin/mycli widget create --name "gear"
bin/mycli widget get <id>
bin/mycli widget delete <id> --confirm
```

**What it demonstrates:**

- `apikit.RootCommand()` as the base command tree
- All built-in commands: `LoginCmd()`, `UserCmd()`, `KeysCmd()`, `TokensCmd()`, `OrgsCmd()`, `AdminCmd()`
- Custom commands using `CLIClientFromCmd()` to get the authenticated client
- `DoRequest()` for authenticated API calls with automatic error envelope handling
- `CLIPrintResult()` for consistent JSON output
- `CLIHandleError()` / `NewCLIError()` for structured error reporting
- `CLIExecute()` / `CLIPrintError()` / `CLIExitCode()` for centralized error handling

See [docs/custom-cli.md](../docs/custom-cli.md) for the full guide on building custom CLI commands.

## Using in your own project

1. Copy the example directory
2. Update `go.mod`: change the module path and replace the `replace` directive with a versioned dependency:

```
require github.com/txsvc/apikit v0.1.0
```

3. Run `go mod tidy`
