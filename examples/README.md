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

A custom CLI that embeds apikit's base command tree (version, help) and adds a custom command.

```bash
cd cli
make build
bin/mycli hello
bin/mycli version
```

**What it demonstrates:**

- `apikit.RootCommand()` as the starting point
- Adding custom Cobra commands alongside apikit's built-in ones

> **Note:** The full `akc` command set (login, user, keys, tokens, orgs, admin) is available via the `akc` binary shipped with apikit. These commands use internal packages and are not individually importable. To embed them in your own CLI, use `cmd/akc/main.go` as a reference.

## Using in your own project

1. Copy the example directory
2. Update `go.mod`: change the module path and replace the `replace` directive with a versioned dependency:

```
require github.com/txsvc/apikit v0.1.0
```

3. Run `go mod tidy`
