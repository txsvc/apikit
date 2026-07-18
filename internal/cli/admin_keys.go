package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newAdminKeysCmd returns the admin keys command group with subcommands:
// list and revoke.
// The parent command has no RunE — invoking it directly prints help.
func newAdminKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage user API keys (admin)",
	}

	// list subcommand — requires exactly one positional arg (user ID).
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List a user's API keys",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// revoke subcommand — requires exactly two positional args (user ID, key ID).
	revokeCmd := &cobra.Command{
		Use:   "revoke",
		Short: "Revoke a user's API key",
		Args:  cobra.ExactArgs(2),
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
