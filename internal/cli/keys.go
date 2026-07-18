package cli

import (
	"github.com/spf13/cobra"
)

// NewKeysCmd returns the Cobra parent command for `akc keys`.
// It registers list, refresh, and revoke subcommands.
// Stub — will be implemented in task group 7.
func NewKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "placeholder",
	}
	return cmd
}
