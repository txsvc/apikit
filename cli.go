package apikit

import (
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
