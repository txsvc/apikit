package cli

import (
	"github.com/spf13/cobra"
)

// newAdminUsersCmd returns the admin users command group with subcommands:
// list, show, create, update, promote, demote, block, unblock.
func newAdminUsersCmd() *cobra.Command {
	// Stub: will be implemented in task group 5.
	cmd := &cobra.Command{
		Use: "placeholder",
	}
	return cmd
}
