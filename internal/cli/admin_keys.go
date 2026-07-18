package cli

import (
	"github.com/spf13/cobra"
)

// newAdminKeysCmd returns the admin keys command group with subcommands:
// list and revoke.
func newAdminKeysCmd() *cobra.Command {
	// Stub: will be implemented in task group 5.
	cmd := &cobra.Command{
		Use: "placeholder",
	}
	return cmd
}
