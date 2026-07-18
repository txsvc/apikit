package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newAdminOrgsCmd returns the admin orgs command group with subcommands:
// list, create, update, delete, block, unblock, and members sub-group.
// The parent command has no RunE — invoking it directly prints help.
func newAdminOrgsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orgs",
		Short: "Manage organizations (admin)",
	}

	// list subcommand — no positional args, optional --include-blocked flag.
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all organizations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}
	listCmd.Flags().Bool("include-blocked", false, "Include blocked organizations in the response")

	// create subcommand — no positional args; --name and --slug required, --url optional.
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new organization",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}
	createCmd.Flags().String("name", "", "Organization name")
	createCmd.Flags().String("slug", "", "Organization slug")
	createCmd.Flags().String("url", "", "Organization URL")

	// update subcommand — requires exactly one positional arg (org ID); --name and --url optional.
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update an organization by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}
	updateCmd.Flags().String("name", "", "Organization name")
	updateCmd.Flags().String("url", "", "Organization URL")

	// delete subcommand — requires exactly one positional arg (org ID).
	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete an organization by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// block subcommand — requires exactly one positional arg (org ID).
	blockCmd := &cobra.Command{
		Use:   "block",
		Short: "Block an organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// unblock subcommand — requires exactly one positional arg (org ID).
	unblockCmd := &cobra.Command{
		Use:   "unblock",
		Short: "Unblock an organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	cmd.AddCommand(
		listCmd,
		createCmd,
		updateCmd,
		deleteCmd,
		blockCmd,
		unblockCmd,
		newAdminOrgsMembersCmd(),
	)

	return cmd
}

// newAdminOrgsMembersCmd returns the admin orgs members sub-group with
// subcommands: list, add, remove.
// The parent command has no RunE — invoking it directly prints help.
func newAdminOrgsMembersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Manage organization members (admin)",
	}

	// list subcommand — requires exactly one positional arg (org ID).
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List members of an organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// add subcommand — requires exactly two positional args (org ID, user ID).
	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a member to an organization",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// remove subcommand — requires exactly two positional args (org ID, user ID).
	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a member from an organization",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	cmd.AddCommand(
		listCmd,
		addCmd,
		removeCmd,
	)

	return cmd
}
