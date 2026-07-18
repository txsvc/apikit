package cli

import (
	"github.com/spf13/cobra"
)

// newAdminOrgsCmd returns the admin orgs command group with subcommands:
// list, create, update, delete, block, unblock, and members sub-group.
func newAdminOrgsCmd() *cobra.Command {
	// Stub: will be implemented in task group 5.
	cmd := &cobra.Command{
		Use: "placeholder",
	}
	return cmd
}

// newAdminOrgsMembersCmd returns the admin orgs members sub-group with
// subcommands: list, add, remove.
func newAdminOrgsMembersCmd() *cobra.Command {
	// Stub: will be implemented in task group 5.
	cmd := &cobra.Command{
		Use: "placeholder",
	}
	return cmd
}
