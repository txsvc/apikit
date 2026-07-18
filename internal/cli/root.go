package cli

import (
	"context"

	"github.com/spf13/cobra"
)

// Context key types — unexported struct types prevent collision with
// consuming project code (see 13-REQ-7.E1).
type clientContextKey struct{}
type userIDContextKey struct{}

// ContextWithClient stores an API client in the context.
// The client is stored as any to avoid import cycles between
// internal/cli and the root apikit package.
func ContextWithClient(ctx context.Context, client any) context.Context {
	return context.WithValue(ctx, clientContextKey{}, client)
}

// ClientFromContext retrieves the API client from a context.
// Returns nil if no client was stored (e.g., auth-exempt commands).
// Stub — will be implemented in task group 9.
func ClientFromContext(ctx context.Context) any {
	return ctx.Value(clientContextKey{})
}

// UserIDFromContext retrieves the user_id string from a context.
// Returns "" if no user_id was stored.
// Stub — will be implemented in task group 9.
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(userIDContextKey{}).(string)
	return v
}

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
