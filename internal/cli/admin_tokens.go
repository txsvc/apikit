package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newAdminTokensCmd returns the admin tokens command group with subcommands:
// list and revoke.
// The parent command has no RunE — invoking it directly prints help.
func newAdminTokensCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tokens",
		Short: "Manage user personal access tokens (admin)",
	}

	// list subcommand — requires exactly one positional arg (user ID).
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List a user's personal access tokens",
		Args:  adminCheckMissingArg("user_id"),
		Annotations: map[string]string{
			"method": "GET",
			"path":   "/users/:id/tokens",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// revoke subcommand — requires exactly two positional args (user ID, token ID).
	revokeCmd := &cobra.Command{
		Use:   "revoke",
		Short: "Revoke a user's personal access token",
		Args:  cobra.ExactArgs(2),
		Annotations: map[string]string{
			"method": "DELETE",
			"path":   "/users/:id/tokens/:token_id",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	cmd.AddCommand(
		listCmd,
		revokeCmd,
	)

	return cmd
}
