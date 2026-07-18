package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newAdminUsersCmd returns the admin users command group with subcommands:
// list, show, create, update, promote, demote, block, unblock.
// The parent command has no RunE — invoking it directly prints help.
func newAdminUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage users (admin)",
	}

	// list subcommand — no positional args, optional --include-blocked flag.
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all users",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}
	listCmd.Flags().Bool("include-blocked", false, "Include blocked users in the response")

	// show subcommand — requires exactly one positional arg (user ID).
	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show a user by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// create subcommand — no positional args; four required flags.
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}
	createCmd.Flags().String("username", "", "Username for the new user")
	createCmd.Flags().String("email", "", "Email address for the new user")
	createCmd.Flags().String("provider", "", "OAuth provider name")
	createCmd.Flags().String("provider-id", "", "Provider-specific user ID")

	// update subcommand — requires exactly one positional arg (user ID); --full-name flag.
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update a user by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}
	updateCmd.Flags().String("full-name", "", "Full name of the user")

	// promote subcommand — requires exactly one positional arg (user ID).
	promoteCmd := &cobra.Command{
		Use:   "promote",
		Short: "Grant admin role to a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// demote subcommand — requires exactly one positional arg (user ID).
	demoteCmd := &cobra.Command{
		Use:   "demote",
		Short: "Revoke admin role from a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// block subcommand — requires exactly one positional arg (user ID).
	blockCmd := &cobra.Command{
		Use:   "block",
		Short: "Block a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// unblock subcommand — requires exactly one positional arg (user ID).
	unblockCmd := &cobra.Command{
		Use:   "unblock",
		Short: "Unblock a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	cmd.AddCommand(
		listCmd,
		showCmd,
		createCmd,
		updateCmd,
		promoteCmd,
		demoteCmd,
		blockCmd,
		unblockCmd,
	)

	return cmd
}
