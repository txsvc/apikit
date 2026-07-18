package cli

import (
	"github.com/spf13/cobra"
)

// NewTokensCmd returns the Cobra parent command for `akc tokens`.
// It registers list, create, show, and revoke subcommands.
// Stub — will be implemented in task group 8.
func NewTokensCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "placeholder",
	}
	return cmd
}
