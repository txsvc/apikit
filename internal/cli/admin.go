// Package cli implements the akc CLI command tree.
package cli

import (
	"github.com/spf13/cobra"
)

// NewAdminCmd returns the root *cobra.Command for the admin command tree.
// It registers users, orgs, keys, and tokens subcommand groups as children.
func NewAdminCmd() *cobra.Command {
	// Stub: will be implemented in task group 5.
	cmd := &cobra.Command{
		Use: "placeholder",
	}
	return cmd
}
