package cli

import (
	"github.com/spf13/cobra"
)

// newAdminTokensCmd returns the admin tokens command group with subcommands:
// list and revoke.
func newAdminTokensCmd() *cobra.Command {
	// Stub: will be implemented in task group 5.
	cmd := &cobra.Command{
		Use: "placeholder",
	}
	return cmd
}
