package cli

import (
	"github.com/spf13/cobra"
)

// RootCommand constructs and returns the root Cobra command for the CLI.
// Stub — will be implemented in task group 8.
func RootCommand() *cobra.Command {
	return &cobra.Command{}
}

// Execute wraps rootCmd.Execute() with centralized error handling.
// Stub — will be implemented in task group 8.
func Execute() error {
	return nil
}

// ExitCode maps an error to an integer exit code.
// Stub — will be implemented in task group 9.
func ExitCode(_ error) int {
	return 0
}

// PrintError writes a JSON error envelope to stdout.
// Stub — will be implemented in task group 9.
func PrintError(_ error) {
}

// PrintJSON marshals a value to indented JSON and writes it to stdout.
// Stub — will be implemented in task group 9.
func PrintJSON(_ any) error {
	return nil
}
