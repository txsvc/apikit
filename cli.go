package apikit

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/txsvc/apikit/internal/cli"
)

// RootCommand returns the base CLI command tree (version, help,
// persistent flags for --endpoint-url, --api-key, --user-id).
// Add subcommands with rootCmd.AddCommand().
func RootCommand() *cobra.Command {
	return cli.RootCommand()
}

// LoginCmd returns the `login` command for browser-based OAuth authentication.
func LoginCmd() *cobra.Command { return cli.NewLoginCmd() }

// UserCmd returns the `user` command group (show, update).
func UserCmd() *cobra.Command { return cli.NewUserCmd() }

// KeysCmd returns the `keys` command group (list, refresh, revoke).
func KeysCmd() *cobra.Command { return cli.NewKeysCmd() }

// TokensCmd returns the `tokens` command group (create, list, show, revoke).
func TokensCmd() *cobra.Command { return cli.NewTokensCmd() }

// OrgsCmd returns the `orgs` command group (list, show, members).
func OrgsCmd() *cobra.Command { return cli.NewOrgsCmd() }

// AdminCmd returns the `admin` command group (users, orgs, keys, tokens).
func AdminCmd() *cobra.Command { return cli.NewAdminCmd() }

// CLIExecute runs the root command with centralized error handling.
// Returns the error from Execute() for the caller to handle exit codes.
func CLIExecute() error { return cli.Execute() }

// CLIPrintError writes a JSON error envelope to stdout for a non-nil error.
// Skips errors already printed by command-level handlers.
func CLIPrintError(err error) { cli.PrintError(err) }

// CLIExitCode maps an error to an exit code: 0 (nil), 1 (API error), 2 (other).
func CLIExitCode(err error) int { return cli.ExitCode(err) }

// ---------------------------------------------------------------------------
// Public CLI client API for custom commands
// ---------------------------------------------------------------------------

// CLIClient is an authenticated HTTP client for CLI commands. It handles
// Bearer-token injection, JSON request/response marshaling, the /api/v1
// path prefix, and server error envelope decoding.
//
// Custom commands should retrieve the client from the Cobra context via
// CLIClientFromCmd rather than constructing one directly — this ensures
// the client inherits credentials resolved by apikit's PersistentPreRunE
// (config file, environment variables, and CLI flags).
type CLIClient = cli.CmdClient

// CLIError represents a client-side or API error with an integer code and
// message. It satisfies the error interface and is used by CLIHandleError
// to render JSON error envelopes.
type CLIError = cli.CmdError

// NewCLIClient constructs a CLIClient with the given endpoint URL and API
// key. Prefer CLIClientFromCmd for commands registered on apikit's root
// command tree — it automatically uses resolved credentials.
func NewCLIClient(endpointURL, apiKey string) *CLIClient {
	return cli.NewCmdClient(endpointURL, apiKey)
}

// NewCLIError creates a CLIError with the given code and message.
func NewCLIError(code int, message string) *CLIError {
	return cli.NewCmdError(code, message)
}

// CLIClientFromCmd retrieves the authenticated CLIClient from a Cobra
// command's context. The client is injected by apikit's PersistentPreRunE
// after resolving credentials from flags, environment variables, and the
// config file. Returns an error if no client is available (e.g., the user
// has not run "login" yet).
func CLIClientFromCmd(cmd *cobra.Command) (*CLIClient, error) {
	return cli.NewAuthenticatedCmdClient(cmd)
}

// CLIPrintResult writes v as indented JSON (two-space indent, no HTML
// escaping) to cmd's stdout. Use this for successful command output.
func CLIPrintResult(cmd *cobra.Command, v any) error {
	return cli.CmdPrintJSON(cmd, v)
}

// CLIHandleError writes a JSON error envelope to cmd's stdout and returns
// the error wrapped so that CLIPrintError will not double-print it.
// Use this to report errors from custom command RunE functions.
func CLIHandleError(cmd *cobra.Command, err error) error {
	return cli.CmdHandleError(cmd, err)
}

// CLIResolveOrgSlug resolves an organization slug to its UUID by listing
// the authenticated user's organizations and matching on slug. Useful for
// custom commands that accept org slugs as user-friendly identifiers.
func CLIResolveOrgSlug(ctx context.Context, client *CLIClient, slug string) (string, error) {
	respBody, status, err := client.DoRequestRaw(ctx, "GET", "/user/orgs", nil)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", NewCLIError(status, "failed to list organizations")
	}

	return resolveOrgSlugFromJSON(respBody, slug)
}
