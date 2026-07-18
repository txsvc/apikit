package cli

import (
	"github.com/spf13/cobra"
)

// NewOrgsCmd returns the Cobra parent command for `akc orgs`.
// It registers list, show, and members subcommands.
// Stub — will be implemented in task group 8.
func NewOrgsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "placeholder",
	}
	return cmd
}
