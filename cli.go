package apikit

import (
	"github.com/spf13/cobra"
	"github.com/txsvc/apikit/internal/cli"
)

// RootCommand returns the embeddable CLI command tree.
// Consuming projects call rootCmd.AddCommand(apikit.RootCommand())
// to embed the full akc command subtree.
func RootCommand() *cobra.Command {
	return cli.RootCommand()
}
