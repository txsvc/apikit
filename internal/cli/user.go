package cli

import (
	"github.com/spf13/cobra"
)

// NewUserCmd returns the Cobra parent command for `akc user`.
// It registers show and update subcommands.
// Stub — will be implemented in task group 7.
func NewUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "placeholder",
	}
	return cmd
}
