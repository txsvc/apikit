// Package cli implements the akc CLI command tree.
package cli

import (
	"github.com/spf13/cobra"
)

// NewAdminCmd returns the root *cobra.Command for the admin command tree.
// It registers users, orgs, keys, and tokens subcommand groups as children.
// The admin command itself has no RunE — invoking it directly prints help.
func NewAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administrative commands for managing users, orgs, keys, and tokens",
	}

	cmd.AddCommand(
		newAdminUsersCmd(),
		newAdminOrgsCmd(),
		newAdminKeysCmd(),
		newAdminTokensCmd(),
	)

	return cmd
}
